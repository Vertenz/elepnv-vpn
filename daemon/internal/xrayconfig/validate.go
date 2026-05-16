package xrayconfig

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/supervisor"
)

// ValidateResult is the structured result of one `xray run -test` invocation.
// Mirrors the wire shape of Configs.Validate's RPC reply (§8.4).
//
// Timeout is true when the daemon's validateTimeout fired instead of xray
// reporting a verdict. The IPC handler translates this into a typed
// ErrValidationTimeout (-32013) so the renderer can distinguish "config was
// rejected" from "we couldn't tell" and retry policy can differ.
type ValidateResult struct {
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	Stderr  string `json:"stderr,omitempty"`
	Timeout bool   `json:"timeout,omitempty"`
}

// validateTimeout caps each `xray run -test` call. xray-core parser typically
// finishes in 10-50ms; 10s is a generous backstop against a hung subprocess.
const validateTimeout = 10 * time.Second

// MaxValidateConcurrent and MaxValidateQueue are package-level vars (not
// consts) so tests can lower them without spawning real workloads. Production
// code should treat them as constants after process startup.
var (
	MaxValidateConcurrent = 4
	MaxValidateQueue      = 16
)

var (
	validateSem     chan struct{}
	validateSemOnce sync.Once
	validateWaiters atomic.Int32
)

// validateSemaphore returns the shared channel semaphore, creating it on first
// use. The sync.Once ensures a single allocation under concurrent first calls.
func validateSemaphore() chan struct{} {
	validateSemOnce.Do(func() {
		validateSem = make(chan struct{}, MaxValidateConcurrent)
	})
	return validateSem
}

// ResetValidateLimitsForTests rebuilds the semaphore using the current
// MaxValidateConcurrent / MaxValidateQueue values. Exported for tests only —
// do not call from production code; the Once is intentionally bypassed here.
func ResetValidateLimitsForTests() {
	validateSem = make(chan struct{}, MaxValidateConcurrent)
	validateWaiters.Store(0)
	// Reset the Once so future calls to validateSemaphore() don't re-init with
	// the old capacity (tests may call ResetValidateLimitsForTests multiple
	// times in a single process run).
	validateSemOnce = sync.Once{}
}

// stderrRingCap is the in-memory cap on captured xray stderr. 4 KiB holds the
// banner + a handful of error lines, which is all summarize() needs.
const stderrRingCap = 4 << 10

// Validate runs `xrayPath run -test -c cfgPath`. The timeout is the smaller
// of validateTimeout and any deadline already on ctx. Errors that originate
// from running the subprocess (timeout, non-zero exit) become a non-OK
// ValidateResult with err == nil; the err return is reserved for truly
// unexpected I/O failures (exec lookup failure, etc.).
//
// At most MaxValidateConcurrent xray processes run simultaneously; up to
// MaxValidateQueue additional callers may wait. Beyond that the call returns
// derr.ErrValidationBusy immediately so the daemon's goroutine count stays
// bounded under bursts (§8.3).
func Validate(ctx context.Context, xrayPath, cfgPath string) (ValidateResult, error) {
	// Reject immediately when the wait queue is already at capacity. The check
	// is intentionally before Add so we never inflate the counter past the
	// limit in the happy path.
	if validateWaiters.Load() >= int32(MaxValidateQueue) {
		return ValidateResult{}, derr.ErrValidationBusy
	}
	validateWaiters.Add(1)
	defer validateWaiters.Add(-1)

	// Block until a concurrency slot is free or the caller cancels. Honoring
	// ctx here is critical: a daemon shutdown while many Validates are queued
	// would otherwise block until all slots drained.
	sem := validateSemaphore()
	select {
	case sem <- struct{}{}:
	case <-ctx.Done():
		return ValidateResult{}, ctx.Err()
	}
	defer func() { <-sem }()

	ctx, cancel := context.WithTimeout(ctx, validateTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, xrayPath, "run", "-test", "-c", cfgPath)
	cmd.Env = supervisor.MinimalChildEnv()
	// Put the subprocess in its own process group so we can SIGKILL the whole
	// group (covering grandchild processes such as pipelines in shell scripts).
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid:   true,
		Pdeathsig: syscall.SIGTERM,
	}
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
		return ValidateResult{OK: false, Timeout: true, Error: "validation timed out"}, nil
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
