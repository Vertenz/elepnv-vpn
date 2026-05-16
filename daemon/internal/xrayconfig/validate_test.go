package xrayconfig

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
)

// requirePOSIXShell skips the test on platforms where /bin/sh is unavailable
// (Windows). Validate's SysProcAttr.Setpgid + SIGKILL on negative PID is a
// POSIX construct and the test scripts use POSIX shell syntax — keeping the
// guard explicit prevents Plan-3 contributors from accidentally extending
// this test family to a platform where it can't run.
func requirePOSIXShell(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/bin/sh"); err != nil {
		t.Skipf("no POSIX shell at /bin/sh: %v", err)
	}
}

// fakeXray writes a small shell script to dir/xray that exits with a chosen
// code and prints stderr; returns the absolute script path.
func fakeXray(t *testing.T, dir, body string) string {
	t.Helper()
	requirePOSIXShell(t)
	path := filepath.Join(dir, "xray")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake xray: %v", err)
	}
	return path
}

func TestValidateOKWhenXrayExitsZero(t *testing.T) {
	dir := t.TempDir()
	xray := fakeXray(t, dir, "exit 0\n")
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{}`), 0o644)

	got, err := Validate(context.Background(), xray, cfg)
	if err != nil {
		t.Fatalf("Validate err = %v", err)
	}
	if !got.OK {
		t.Fatalf("OK = false, want true")
	}
}

func TestValidateSummarizesXrayErrorLine(t *testing.T) {
	dir := t.TempDir()
	xray := fakeXray(t, dir, `cat >&2 <<'EOF'
Xray 1.8.4 (build linux/amd64)
A unified platform for anti-censorship.
2024/01/01 12:00:00 [Error] config: invalid outbound: unknown protocol "vlessx"
EOF
exit 23
`)
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{}`), 0o644)

	got, err := Validate(context.Background(), xray, cfg)
	if err != nil {
		t.Fatalf("Validate err = %v", err)
	}
	if got.OK {
		t.Fatalf("OK = true, want false")
	}
	if !strings.Contains(got.Error, "unknown protocol") {
		t.Fatalf("Error = %q, want it to summarise the [Error] line", got.Error)
	}
	if !strings.Contains(got.Stderr, "Xray 1.8.4") {
		t.Fatalf("Stderr should contain the full banner, got %q", got.Stderr)
	}
}

func TestValidateFallsBackToLastNonBannerLine(t *testing.T) {
	dir := t.TempDir()
	xray := fakeXray(t, dir, `cat >&2 <<'EOF'
Xray 1.8.4 (build linux/amd64)
A unified platform for anti-censorship.
something broke
EOF
exit 1
`)
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{}`), 0o644)

	got, _ := Validate(context.Background(), xray, cfg)
	if got.Error != "something broke" {
		t.Fatalf("Error = %q, want %q", got.Error, "something broke")
	}
}

func TestValidateReportsTimeout(t *testing.T) {
	dir := t.TempDir()
	xray := fakeXray(t, dir, "sleep 30\n")
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{}`), 0o644)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, err := Validate(ctx, xray, cfg)
	if err != nil {
		t.Fatalf("Validate should swallow timeout as a non-error result, got err = %v", err)
	}
	if got.OK {
		t.Fatalf("OK = true on timeout, want false")
	}
	if !got.Timeout {
		t.Fatalf("Timeout = false, want true (the IPC handler keys on this to surface ErrValidationTimeout)")
	}
	if !strings.Contains(strings.ToLower(got.Error), "time") {
		t.Fatalf("Error = %q, want a timeout-y message", got.Error)
	}
}

func TestValidateCapsStderrAt4KiB(t *testing.T) {
	dir := t.TempDir()
	// Spam stderr with a recognizable tail so we can confirm only the tail is retained.
	// Write 10000 bytes then a sentinel line; ring buffer should retain only the tail.
	xray := fakeXray(t, dir, `dd if=/dev/zero bs=1000 count=10 2>/dev/null | tr '\0' 'X' >&2
echo END_OF_STDERR >&2
exit 5
`)
	cfg := filepath.Join(dir, "config.json")
	_ = os.WriteFile(cfg, []byte(`{}`), 0o644)

	got, _ := Validate(context.Background(), xray, cfg)
	if len(got.Stderr) > 4096 {
		t.Fatalf("Stderr len = %d, want ≤ 4096 (4 KiB ring buffer)", len(got.Stderr))
	}
	if !strings.Contains(got.Stderr, "END_OF_STDERR") {
		t.Fatalf("Stderr should retain the tail, got %q", got.Stderr)
	}
}

func TestValidateRespectsConcurrencyAndQueue(t *testing.T) {
	// Lower limits so the test doesn't need to spawn many real processes.
	MaxValidateConcurrent = 2
	MaxValidateQueue = 3
	ResetValidateLimitsForTests()
	t.Cleanup(func() {
		MaxValidateConcurrent = 4
		MaxValidateQueue = 16
		ResetValidateLimitsForTests()
	})

	binDir := t.TempDir()
	// Fake xray that sleeps long enough for queue pressure to build before
	// any slot is released.
	xrayPath := fakeXray(t, binDir, "sleep 0.15\nexit 0\n")
	cfgPath := filepath.Join(t.TempDir(), "cfg.json")
	if err := os.WriteFile(cfgPath, []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	// 7 concurrent Validates: 2 run immediately, 3 queue, 2 are rejected.
	// With 5 ms stagger and 150 ms per Validate the queue fills before the
	// first slot returns, so at least the 6th and 7th callers see ErrValidationBusy.
	var wg sync.WaitGroup
	var busy, ok atomic.Int32
	for i := 0; i < 7; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Validate(context.Background(), xrayPath, cfgPath)
			if errors.Is(err, derr.ErrValidationBusy) {
				busy.Add(1)
			} else if err == nil {
				ok.Add(1)
			}
		}()
		// Small stagger so goroutines arrive in order and the queue has time
		// to develop before we check the busy counter.
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()
	if busy.Load() < 1 {
		t.Fatalf("expected ErrValidationBusy on at least one Validate; got busy=%d ok=%d",
			busy.Load(), ok.Load())
	}
}
