//go:build linux

package supervisor

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
)

func fakexrayScript(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "xray")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	return p
}

func TestStartReturnsErrXrayNotFoundWhenPATHEmpty(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	s := &Supervisor{}
	_, err := s.Start(context.Background(), "/tmp/ignored.json")
	if !errors.Is(err, derr.ErrXrayNotFound) {
		t.Fatalf("err = %v, want ErrXrayNotFound", err)
	}
}

func TestStartSpawnsChildAndExitsCleanly(t *testing.T) {
	fakexrayScript(t, "exit 0\n")
	s := &Supervisor{}
	child, err := s.Start(context.Background(), "/tmp/ignored.json")
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if child.Pid <= 0 {
		t.Fatalf("Pid = %d, want > 0", child.Pid)
	}
	if child.Pgid != child.Pid {
		t.Fatalf("Pgid = %d, want %d (Setpgid makes leader its own group)", child.Pgid, child.Pid)
	}
	<-child.ExitC()
	ex, ok := child.Result()
	if !ok {
		t.Fatal("Result() returned !ok after ExitC close")
	}
	if ex.Err != nil {
		t.Fatalf("Exit.Err = %v, want nil", ex.Err)
	}
}

func TestStartCapturesStderrIntoExit(t *testing.T) {
	fakexrayScript(t, "echo XRAY-ERR-SENTINEL >&2; exit 7\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	<-child.ExitC()
	ex, _ := child.Result()
	if ex.Err == nil {
		t.Fatal("expected non-nil exit err on exit-7 script")
	}
	if !strings.Contains(ex.Stderr, "XRAY-ERR-SENTINEL") {
		t.Fatalf("Stderr missing sentinel: %q", ex.Stderr)
	}
}

func TestStopSIGTERMsThenSIGKILLsHungChild(t *testing.T) {
	// Trap SIGTERM and ignore — Stop should escalate to SIGKILL on the group.
	fakexrayScript(t, "trap '' TERM; sleep 30\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	time.Sleep(50 * time.Millisecond) // give trap time to install
	start := time.Now()
	if err := s.Stop(context.Background(), child, 200*time.Millisecond); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("Stop took %v, expected ≤ ~300ms (grace + reap window)", elapsed)
	}
}

func TestStopRespectsAbortedContext(t *testing.T) {
	fakexrayScript(t, "trap '' TERM; sleep 30\n")
	s := &Supervisor{}
	child, _ := s.Start(context.Background(), "/tmp/ignored.json")
	time.Sleep(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Stop(ctx, child, 5*time.Second); err == nil {
		t.Fatal("Stop with cancelled ctx must error")
	}
	// Cleanup: SIGKILL the group so we don't leak.
	_ = exec.Command("kill", "-9", "-"+itoa(child.Pgid)).Run()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

var _ = syscall.SIGTERM
