//go:build linux

package supervisor

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"elepn/daemon/internal/derr"
)

// Supervisor is a stateless factory that spawns and reaps xray-core children.
// It is the only package permitted to import os/exec and call syscall.Kill on
// the long-running xray child (spec §4).
type Supervisor struct{}

// Child is an opaque handle to a running xray-core process. State machine and
// other callers receive *Child and interact only via ExitC / Result / Stop.
type Child struct {
	Pid  int // process group leader pid; equal to the leader's tgid
	Pgid int // process group id == Pid (we set Setpgid: true)
	Cmd  *exec.Cmd

	exitOnce sync.Once     // ensures exitVal is recorded exactly once
	exitDone chan struct{} // closed when exitVal is set; signal-only channel
	exitVal  Exit          // populated under exitOnce; safe to read after <-exitDone
}

// Exit holds the outcome of an xray-core process after it has exited.
type Exit struct {
	Err    error  // cmd.Wait() result; nil on clean exit
	Stderr string // captured stderr, capped (4 KiB last-bytes ring buffer)
}

// ExitC returns a signal-only channel that closes when the child has exited.
// Multiple consumers (actor and Stop) can wait on this channel and all unblock
// simultaneously on close; they then read Result() to get the value.
func (c *Child) ExitC() <-chan struct{} { return c.exitDone }

// Result returns the recorded exit value once the child has exited, or
// (zero, false) before. Safe to call from any goroutine.
func (c *Child) Result() (Exit, bool) {
	select {
	case <-c.exitDone:
		return c.exitVal, true
	default:
		return Exit{}, false
	}
}

// Start spawns xray in its own process group. The caller takes ownership of
// the returned Child handle. ExitC closes exactly once when xray exits.
func (s *Supervisor) Start(ctx context.Context, configPath string) (*Child, error) {
	xrayPath, err := exec.LookPath("xray")
	if err != nil {
		return nil, derr.ErrXrayNotFound // P1-5: systemd unit sets PATH
	}

	// Use exec.Command (NOT CommandContext). Tying ctx to Go's default
	// ctx-cancel handler kills only the leader pid (cmd.Process.Kill()),
	// bypassing the Setpgid + Kill(-pgid) design. Cancellation is the caller's
	// responsibility via Stop(); ctx here is only for Start-time preflight.
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd := exec.Command(xrayPath, "run", "-c", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Pdeathsig: syscall.SIGTERM, // child dies if daemon dies
		Setpgid:   true,            // P1-2: own process group
	}

	stdout, _ := cmd.StdoutPipe()
	stderr, _ := cmd.StderrPipe()
	if err := cmd.Start(); err != nil {
		return nil, derr.WrapSpawn(err)
	}

	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Extremely rare; if the leader died between Start and Getpgid the
		// pgid equals the pid by Setpgid convention. Fall back.
		pgid = cmd.Process.Pid
	}

	child := &Child{
		Pid:      cmd.Process.Pid,
		Pgid:     pgid,
		Cmd:      cmd,
		exitDone: make(chan struct{}),
	}
	var sbuf ringBuf // 4 KiB cap, last bytes preserved

	go forwardLines(stdout, slog.LevelInfo, false, "xray.stdout")
	go forwardLines(io.TeeReader(stderr, &sbuf), slog.LevelWarn, true, "xray.stderr")
	go func() {
		waitErr := cmd.Wait()
		child.exitOnce.Do(func() {
			child.exitVal = Exit{Err: waitErr, Stderr: sbuf.String()}
			close(child.exitDone)
		})
	}()

	return child, nil
}

// Stop signals the xray process group, waits up to grace for clean exit,
// then SIGKILLs the group. Returns nil on clean (or post-SIGKILL) exit; non-nil
// error if the caller's ctx fires before reap completes OR if the child remains
// unreapable after SIGKILL + 5s safety cap (a kernel-level hang — very rare).
func (s *Supervisor) Stop(ctx context.Context, c *Child, grace time.Duration) error {
	// P1-2: signal the entire process group, not just the leader. Kills any
	// helper processes xray-core spawned.
	_ = syscall.Kill(-c.Pgid, syscall.SIGTERM)

	select {
	case <-c.ExitC():
		return nil
	case <-time.After(grace):
		_ = syscall.Kill(-c.Pgid, syscall.SIGKILL)
	case <-ctx.Done():
		return fmt.Errorf("xray pgid %d stop aborted: %w", c.Pgid, ctx.Err())
	}

	// P1-1: hard safety cap after SIGKILL.
	select {
	case <-c.ExitC():
		return nil
	case <-ctx.Done():
		return fmt.Errorf("xray pgid %d unreapable: %w", c.Pgid, ctx.Err())
	case <-time.After(5 * time.Second):
		return fmt.Errorf("xray pgid %d still alive 5s after SIGKILL", c.Pgid)
	}
}
