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
	"syscall"
	"testing"
	"time"
)

// readResponse reads JSON-RPC lines from r until it finds a response with the
// given id, discarding any intervening State.Changed notifications that the
// daemon pushes on the same connection. Without this, a race between the
// Tunnel.Connect response writer and the State.Changed notification writer
// (both call handle.write under wmu) can make ReadString return the
// notification before the response, causing the next assertion to panic on a
// nil result map.
func readResponse(t *testing.T, r *bufio.Reader, wantID string) map[string]any {
	t.Helper()
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("readResponse (id=%s): %v", wantID, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("readResponse (id=%s): not JSON: %v (%q)", wantID, err, line)
		}
		if id, _ := parsed["id"].(string); id == wantID {
			return parsed
		}
		// Skip notifications (no "id" field or id doesn't match) and keep reading.
	}
}

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
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 not available; skipping integration smoke")
	}

	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "xrayd")
	if err := exec.Command("go", "build", "-o", binPath, ".").Run(); err != nil {
		t.Fatalf("build xrayd: %v", err)
	}

	binDir := t.TempDir()
	xrayPath := filepath.Join(binDir, "xray")
	// The fake xray must:
	//   - print a version line and exit 0 when called as "xray version"
	//     (so runXrayVersion completes immediately instead of timing out after 2s)
	//   - open a SOCKS5 listener on 10808 and block on SIGTERM otherwise
	//     (so awaitSocksReady probe succeeds and the child stays alive)
	const xrayBody = `#!/bin/sh
if [ "$1" = "version" ]; then
  echo "Xray 1.0.0 (fake)"
  exit 0
fi
# "xray run -test -c <path>" — config validation; report success so Configs.Add succeeds.
if [ "$1" = "run" ] && [ "$2" = "-test" ]; then
  exit 0
fi
exec python3 -c '
import socket, sys, threading
ls = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
ls.setsockopt(socket.SOL_SOCKET, socket.SO_REUSEADDR, 1)
ls.bind(("127.0.0.1", 10808))
ls.listen(8)
def serve():
    while True:
        try:
            c, _ = ls.accept()
        except OSError:
            return
        try:
            c.recv(3)
            c.sendall(b"\x05\x00")
        finally:
            c.close()
threading.Thread(target=serve, daemon=True).start()
import signal
signal.pause()
'
`
	if err := os.WriteFile(xrayPath, []byte(xrayBody), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgDir := t.TempDir()
	stateDir := t.TempDir()

	sockPath := filepath.Join(tmpDir, "x.sock")
	cmd := exec.Command(binPath)
	cmd.Env = append(os.Environ(),
		"XRAYD_SOCK="+sockPath,
		"XRAYD_LOG_LEVEL=info",
		"XRAYD_CONFIGS_DIR="+cfgDir,
		"XRAYD_STATE_DIR="+stateDir,
		"PATH="+binDir+":"+os.Getenv("PATH"),
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
	// Budget covers Ping + Configs.Add + Tunnel lifecycle (validate + connect + disconnect).
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":"1","method":"Daemon.Ping"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	r := bufio.NewReader(conn)
	parsed := readResponse(t, r, "1")
	res := parsed["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("result.ok = %v, want true", res["ok"])
	}

	// Configs.Add round-trip — proves Plan 2 dispatchers + Store are wired.
	addReq := `{"jsonrpc":"2.0","id":"2","method":"Configs.Add","params":{"json":"` +
		`{\"inbounds\":[{\"tag\":\"socks-in\",\"listen\":\"127.0.0.1\",\"port\":10808,` +
		`\"protocol\":\"socks\",\"settings\":{\"auth\":\"noauth\",\"udp\":true}}]}` +
		`"}}` + "\n"
	if _, err := conn.Write([]byte(addReq)); err != nil {
		t.Fatalf("write Add: %v", err)
	}
	addParsed := readResponse(t, r, "2")
	if addParsed["error"] != nil {
		t.Fatalf("Configs.Add returned error: %v", addParsed["error"])
	}
	addRes := addParsed["result"].(map[string]any)
	id, _ := addRes["id"].(string)
	if len(id) != 26 {
		t.Fatalf("ULID length = %d, want 26 (got %q)", len(id), id)
	}

	// Tunnel.Connect — synchronous accept response is "Validating".
	tcReq := `{"jsonrpc":"2.0","id":"3","method":"Tunnel.Connect","params":{"id":"` + id + `"}}` + "\n"
	if _, err := conn.Write([]byte(tcReq)); err != nil {
		t.Fatalf("write Tunnel.Connect: %v", err)
	}
	tcParsed := readResponse(t, r, "3")
	if tcParsed["error"] != nil {
		t.Fatalf("Tunnel.Connect returned error: %v", tcParsed["error"])
	}
	tcRes := tcParsed["result"].(map[string]any)
	if tcRes["state"] != "Validating" {
		t.Fatalf("Tunnel.Connect state = %v, want Validating", tcRes["state"])
	}

	// Poll Tunnel.GetStatus until we observe Connected (budget 8s — covers
	// validate (~0.5s) + spawn + awaitProcessAlive (~1s) + awaitSocksReady).
	deadline := time.Now().Add(8 * time.Second)
	var observedConnected bool
	for time.Now().Before(deadline) {
		gsReq := `{"jsonrpc":"2.0","id":"4","method":"Tunnel.GetStatus"}` + "\n"
		if _, err := conn.Write([]byte(gsReq)); err != nil {
			t.Fatalf("write GetStatus: %v", err)
		}
		gsParsed := readResponse(t, r, "4")
		if res, ok := gsParsed["result"].(map[string]any); ok {
			if c, ok := res["conn"].(map[string]any); ok && c["state"] == "Connected" {
				observedConnected = true
				break
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !observedConnected {
		t.Fatal("did not observe Connected state within 8s")
	}

	// Tunnel.Disconnect — async cleanup; wait for terminal Disconnected.
	tdReq := `{"jsonrpc":"2.0","id":"5","method":"Tunnel.Disconnect"}` + "\n"
	if _, err := conn.Write([]byte(tdReq)); err != nil {
		t.Fatalf("write Disconnect: %v", err)
	}
	tdParsed := readResponse(t, r, "5")
	if tdParsed["error"] != nil {
		t.Fatalf("Tunnel.Disconnect returned error: %v", tdParsed["error"])
	}

	deadline = time.Now().Add(3 * time.Second)
	var observedDisconnected bool
	for time.Now().Before(deadline) {
		gsReq := `{"jsonrpc":"2.0","id":"6","method":"Tunnel.GetStatus"}` + "\n"
		if _, err := conn.Write([]byte(gsReq)); err != nil {
			t.Fatalf("write GetStatus (disconnect poll): %v", err)
		}
		gsParsed := readResponse(t, r, "6")
		if res, ok := gsParsed["result"].(map[string]any); ok {
			if c, ok := res["conn"].(map[string]any); ok && c["state"] == "Disconnected" {
				observedDisconnected = true
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !observedDisconnected {
		t.Fatal("did not observe Disconnected within 3s of Disconnect")
	}

	// SIGTERM and confirm exit. Budget 5s — shutdown is fast on a quiet host
	// but `go test` under CI load (race detector, parallel packages) needs
	// margin so this isn't a flaky test.
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
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("xrayd did not exit within 5s of SIGTERM")
	}
}
