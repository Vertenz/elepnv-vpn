package xrayconfig

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ValidateResult is the structured result of one `xray run -test` invocation.
// Mirrors the wire shape of Configs.Validate's RPC reply (§8.4).
type ValidateResult struct {
	OK     bool   `json:"ok"`
	Error  string `json:"error,omitempty"`
	Stderr string `json:"stderr,omitempty"`
}

// validateTimeout caps each `xray run -test` call. xray-core parser typically
// finishes in 10-50ms; 10s is a generous backstop against a hung subprocess.
const validateTimeout = 10 * time.Second

// stderrRingCap is the in-memory cap on captured xray stderr. 4 KiB holds the
// banner + a handful of error lines, which is all summarize() needs.
const stderrRingCap = 4 << 10

// Validate runs `xrayPath run -test -c cfgPath`. The timeout is the smaller
// of validateTimeout and any deadline already on ctx. Errors that originate
// from running the subprocess (timeout, non-zero exit) become a non-OK
// ValidateResult with err == nil; the err return is reserved for truly
// unexpected I/O failures (exec lookup failure, etc.).
func Validate(ctx context.Context, xrayPath, cfgPath string) (ValidateResult, error) {
	ctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, xrayPath, "run", "-test", "-c", cfgPath)
	// Put the subprocess in its own process group so we can SIGKILL the whole
	// group (covering grandchild processes such as pipelines in shell scripts).
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Override the default cancel function: kill the entire process group so
	// that grandchildren (e.g., `yes` in a pipeline) are reaped along with the
	// shell and stdout/stderr pipes are closed promptly.
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	// After the process is killed, give the pipe-drain goroutines a short window
	// before forcibly closing the I/O. This ensures we still collect stderr for
	// normal exits while not blocking forever if a grandchild holds the pipe.
	cmd.WaitDelay = 200 * time.Millisecond
	stderr := newRingBuf(stderrRingCap)
	cmd.Stderr = stderr

	runErr := cmd.Run()
	switch {
	case runErr == nil:
		return ValidateResult{OK: true}, nil
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return ValidateResult{OK: false, Error: "validation timed out"}, nil
	default:
		captured := stderr.String()
		return ValidateResult{
			OK:     false,
			Error:  summarize(captured),
			Stderr: captured,
		}, nil
	}
}

// summarize returns the most informative single line from xray's stderr.
// Prefers any line containing "[Error]" or "[Fatal]"; otherwise the last
// non-blank, non-banner line. Falls back to a constant string when nothing
// usable is present.
func summarize(stderr string) string {
	lines := strings.Split(strings.TrimSpace(stderr), "\n")
	for _, l := range lines {
		if strings.Contains(l, "[Error]") || strings.Contains(l, "[Fatal]") {
			return strings.TrimSpace(l)
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if l == "" {
			continue
		}
		if strings.HasPrefix(l, "Xray ") || strings.HasPrefix(l, "A unified platform") {
			continue
		}
		return l
	}
	return "xray rejected config (no error message)"
}
