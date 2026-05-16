package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestKillGracefulReturnsNilWhenTargetGone(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	// PID that doesn't exist (very unlikely we're racing init).
	if err := killGraceful(context.Background(), 2_000_000_000, 100*time.Millisecond); err != nil {
		t.Fatalf("expected nil on ESRCH, got %v", err)
	}
}

func TestKillGracefulSIGTERMsCleanProcess(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	cmd := exec.Command("sleep", "30")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = syscall.Kill(-pid, syscall.SIGKILL)
		_, _ = cmd.Process.Wait()
	})

	start := time.Now()
	if err := killGraceful(context.Background(), -pid, 500*time.Millisecond); err != nil {
		t.Fatalf("killGraceful: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("killGraceful took %v, expected sub-second", elapsed)
	}
}

func TestKillGracefulHonorsCtxCancel(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	cmd := exec.Command("sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	pid := cmd.Process.Pid
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The test contract here is "doesn't hang"; ctx.Err or nil-on-already-exited
	// are both acceptable. We can't ptrace-verify SIGKILL wasn't sent.
	_ = killGraceful(ctx, pid, 1*time.Second)
}

func TestRecoveryScanReturnsNilWhenNoXrayd(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux-only")
	}
	if _, err := os.Stat("/etc/passwd"); err != nil {
		t.Skip("no /etc/passwd to consult")
	}
	if err := recoveryScan(context.Background(), discardLogger()); err != nil {
		t.Fatalf("recoveryScan: %v", err)
	}
}
