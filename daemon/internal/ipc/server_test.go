package ipc_test

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"elepn/daemon/internal/ipc"
	"elepn/daemon/internal/platform"
)

// requireXraydGroup skips the test if the current user is not in the xrayd
// group (because auth.AuthAccept will reject the connection). On CI hosts
// where the test setup doesn't create that group, the test is skipped rather
// than failing.
func requireXraydGroup(t *testing.T) {
	t.Helper()
	u, err := user.Current()
	if err != nil {
		t.Skipf("user.Current: %v", err)
	}
	if u.Uid == "0" {
		return // root bypasses the check
	}
	gids, err := u.GroupIds()
	if err != nil {
		t.Skipf("user.GroupIds: %v", err)
	}
	xg, err := user.LookupGroup(ipc.AuthGroup)
	if err != nil {
		t.Skipf("xrayd group not present: %v", err)
	}
	for _, g := range gids {
		if g == xg.Gid {
			return
		}
	}
	t.Skipf("current user not in %s group", ipc.AuthGroup)
}

func startServer(t *testing.T) (*ipc.Server, string) {
	t.Helper()
	sockPath := filepath.Join(t.TempDir(), "test.sock")
	srv := ipc.NewServer(sockPath, platform.XrayInfo{
		Found:   true,
		Path:    "/usr/local/bin/xray",
		Version: "TestXray 0.0.0",
	}, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err := srv.Listen(context.Background()); err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = srv.Close() })
	return srv, sockPath
}

func dialAndReadLine(t *testing.T, sockPath, request string) string {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte(request + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	return line
}

func TestPingRoundtrip(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)
	got := dialAndReadLine(t, sockPath, `{"jsonrpc":"2.0","id":"1","method":"Daemon.Ping"}`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("response not JSON: %v (%q)", err, got)
	}
	if parsed["id"] != "1" {
		t.Fatalf("id = %v, want 1", parsed["id"])
	}
	res := parsed["result"].(map[string]any)
	if res["ok"] != true {
		t.Fatalf("result.ok = %v, want true", res["ok"])
	}
}

func TestGetVersionRoundtrip(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)
	got := dialAndReadLine(t, sockPath, `{"jsonrpc":"2.0","id":"v","method":"Daemon.GetVersion"}`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("response not JSON: %v", err)
	}
	res := parsed["result"].(map[string]any)
	if res["xray"] != "TestXray 0.0.0" {
		t.Fatalf("xray = %v, want TestXray 0.0.0", res["xray"])
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)
	got := dialAndReadLine(t, sockPath, `{"jsonrpc":"2.0","id":"x","method":"NoSuch.Method"}`)
	var parsed map[string]any
	_ = json.Unmarshal([]byte(got), &parsed)
	errObj := parsed["error"].(map[string]any)
	data := errObj["data"].(map[string]any)
	if data["symbol"] != "method_not_found" {
		t.Fatalf("symbol = %v, want method_not_found", data["symbol"])
	}
}

func TestServerEchoesIDOnInvalidRequest(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)
	// Wrong jsonrpc version — semantic error. The server must echo the id.
	got := dialAndReadLine(t, sockPath, `{"jsonrpc":"1.0","id":"abc","method":"X"}`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("response not JSON: %v (%q)", err, got)
	}
	if parsed["id"] != "abc" {
		t.Fatalf("id = %v, want \"abc\" (server must echo id on invalid-request errors)", parsed["id"])
	}
	errObj := parsed["error"].(map[string]any)
	data := errObj["data"].(map[string]any)
	if data["symbol"] != "invalid_request" {
		t.Fatalf("symbol = %v, want invalid_request", data["symbol"])
	}
}

func TestServerNullIDOnParseError(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)
	// Unparseable JSON — id was never detectable, must be null.
	got := dialAndReadLine(t, sockPath, `{garbage`)
	var parsed map[string]any
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("response not JSON: %v (%q)", err, got)
	}
	if parsed["id"] != nil {
		t.Fatalf("id = %v, want null (parse error)", parsed["id"])
	}
}

func TestServerSilentlyDropsNotification(t *testing.T) {
	requireXraydGroup(t)
	_, sockPath := startServer(t)

	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// Send a notification (no id field). Per JSON-RPC §4.1 the server MUST
	// NOT respond. We expect a read deadline to expire with no bytes.
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","method":"Daemon.Ping"}` + "\n")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	buf := make([]byte, 256)
	n, err := conn.Read(buf)
	if err == nil {
		t.Fatalf("server replied to a notification: %q", string(buf[:n]))
	}
	// Any timeout-shaped error is fine; bytes-on-the-wire is the failure.

	// Sanity: the connection is still usable for a normal request afterwards.
	if _, err := conn.Write([]byte(`{"jsonrpc":"2.0","id":"1","method":"Daemon.Ping"}` + "\n")); err != nil {
		t.Fatalf("write follow-up request: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	line, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		t.Fatalf("read follow-up: %v", err)
	}
	var parsed map[string]any
	_ = json.Unmarshal([]byte(line), &parsed)
	if parsed["id"] != "1" {
		t.Fatalf("follow-up id = %v, want 1", parsed["id"])
	}
}

func TestServerCloseUnlinksSocket(t *testing.T) {
	requireXraydGroup(t)
	srv, sockPath := startServer(t)
	if _, err := net.Dial("unix", sockPath); err != nil {
		t.Fatalf("pre-close dial failed: %v", err)
	}
	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := os.Stat(sockPath)
	if err == nil {
		t.Fatalf("expected socket file to be unlinked, but it still exists at %s", sockPath)
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected stat error: %v", err)
	}
}
