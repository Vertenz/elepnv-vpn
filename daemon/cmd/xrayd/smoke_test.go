package main

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

func requireLinux(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("xrayd smoke test is Linux-only")
	}
}

func requireXraydGroup(t *testing.T) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("user.Current: %v", err)
	}
	if u.Uid == "0" {
		return
	}
	gids, _ := u.GroupIds()
	xg, err := user.LookupGroup("xrayd")
	if err != nil {
		t.Skipf("xrayd group not present: %v", err)
	}
	for _, g := range gids {
		if g == xg.Gid {
			return
		}
	}
	t.Skipf("current user not in xrayd group")
}

func TestBinaryRespondsToPing(t *testing.T) {
	requireLinux(t)
	requireXraydGroup(t)

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xrayd")
	if err := exec.Command("go", "build", "-o", binPath, ".").Run(); err != nil {
		t.Fatalf("build xrayd: %v", err)
	}

	sockPath := filepath.Join(tmpDir, "x.sock")
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(),
		"XRAYD_SOCK="+sockPath,
		"XRAYD_LOG_LEVEL=info",
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start xrayd: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})

	// Wait for the socket to appear (daemon's startup is single-threaded but
	// involves Discover + Listen + SdNotify; budget 2 seconds).
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("socket did not appear at %s within 2s", sockPath)
		case <-time.After(50 * time.Millisecond):
		}
	}

	// Send Ping; expect {ok:true}.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":"1","method":"Daemon.Ping"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(strings.TrimRight(line, "\n")), &parsed); err != nil {
		t.Fatalf("response not JSON: %v (%q)", err, line)
	}
	if parsed["id"] != "1" {
		t.Fatalf("id = %v, want 1", parsed["id"])
	}
	res := parsed["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("result.ok = %v, want true", res["ok"])
	}

	// SIGTERM and confirm exit within 1s.
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Fatalf("send SIGTERM: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("xrayd exited with: %v", err)
		}
	case <-time.After(1 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("xrayd did not exit within 1s of SIGTERM")
	}
}
