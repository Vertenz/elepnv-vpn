package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"
)

// ----- helpers ---------------------------------------------------------------
// All helpers here use names that don't collide with smoke_test.go's helpers
// (readResponse, requireLinux, requireXraydGroup).
// requireLinux and requireXraydGroup from smoke_test.go are intentionally
// reused; they live in the same package so no import is needed.

func buildFakexray(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "fakexray-bin")
	cmd := exec.Command("go", "build", "-o", out, "elepn/daemon/cmd/fakexray")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fakexray: %v", err)
	}
	return out
}

func buildXrayd(t *testing.T) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "xrayd-bin")
	cmd := exec.Command("go", "build", "-o", out, ".")
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build xrayd: %v", err)
	}
	return out
}

type startEnv struct {
	binPath, sockPath, cfgDir, stateDir, fakexrayPath, socksAddr string
}

// startDaemon starts xrayd with fakexray symlinked as "xray" on PATH.
// The symlink is re-created idempotently each call so the kill-9 test can
// restart the daemon without re-building.
func startDaemon(t *testing.T, e startEnv) *exec.Cmd {
	t.Helper()
	binDir := filepath.Dir(e.fakexrayPath)
	xrayPath := filepath.Join(binDir, "xray")
	// Idempotent symlink — second daemon start in the kill-9 test reuses the dir.
	_ = os.Remove(xrayPath)
	if err := os.Symlink(e.fakexrayPath, xrayPath); err != nil {
		t.Fatalf("symlink fakexray->xray: %v", err)
	}
	cmd := exec.Command(e.binPath)
	cmd.Env = append(os.Environ(),
		"XRAYD_SOCK="+e.sockPath,
		"XRAYD_LOG_LEVEL=info",
		"XRAYD_CONFIGS_DIR="+e.cfgDir,
		"XRAYD_STATE_DIR="+e.stateDir,
		"XRAYD_EXPECTED_SOCKS_ADDR="+e.socksAddr,
		"PATH="+binDir+":"+os.Getenv("PATH"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start xrayd: %v", err)
	}
	return cmd
}

func waitForSocket(t *testing.T, sockPath string, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, err := os.Stat(sockPath); err == nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket %s did not appear within %v", sockPath, deadline)
}

func waitForSocketGone(t *testing.T, sockPath string, deadline time.Duration) {
	t.Helper()
	end := time.Now().Add(deadline)
	for time.Now().Before(end) {
		if _, err := os.Stat(sockPath); err != nil {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("socket %s still present after %v", sockPath, deadline)
}

func dialDaemon(t *testing.T, sockPath string) (net.Conn, *bufio.Reader) {
	t.Helper()
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	return conn, bufio.NewReader(conn)
}

// rpcCall writes a JSON-RPC request and drains lines until it finds the
// response whose "id" field matches. This is necessary because the daemon
// pushes State.Changed and Health.Changed notifications on the same
// connection, which may arrive before the response.
func rpcCall(t *testing.T, r *bufio.Reader, conn net.Conn, id, method string, params any) map[string]any {
	t.Helper()
	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	b, _ := json.Marshal(req)
	if _, err := conn.Write(append(b, '\n')); err != nil {
		t.Fatalf("write %s: %v", method, err)
	}
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("read %s: %v", method, err)
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err != nil {
			t.Fatalf("%s resp not JSON: %v (%q)", method, err, line)
		}
		if parsed["id"] != id {
			continue // discard notifications and out-of-order messages
		}
		return parsed
	}
}

// waitForConnState polls Tunnel.GetStatus until the state matches want or the
// budget expires. Returns the last successful GetStatus response.
func waitForConnState(t *testing.T, r *bufio.Reader, conn net.Conn, want string, budget time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(budget)
	idCounter := 1000
	for time.Now().Before(deadline) {
		idCounter++
		resp := rpcCall(t, r, conn, strconv.Itoa(idCounter), "Tunnel.GetStatus", nil)
		if res, ok := resp["result"].(map[string]any); ok {
			if c, ok := res["conn"].(map[string]any); ok && c["state"] == want {
				return resp
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("did not observe Tunnel state = %q within %v", want, budget)
	return nil
}

// acceptableConfig builds a minimal xray config JSON that satisfies
// checkInboundSafety for the given socks port. The "fakexray" key carries
// scaffold instructions for the fakexray helper.
func acceptableConfig(socksPort int) []byte {
	cfg := map[string]any{
		"inbounds": []any{
			map[string]any{
				"tag":      "socks-in",
				"listen":   "127.0.0.1",
				"port":     socksPort,
				"protocol": "socks",
				"settings": map[string]any{"auth": "noauth", "udp": true},
			},
		},
		"fakexray": map[string]any{
			"run_socks_port": socksPort,
		},
	}
	b, _ := json.Marshal(cfg)
	return b
}

// ----- tests -----------------------------------------------------------------

// TestKillDashNineRecoveryReapsOrphans exercises §18 done-criterion #4.
//
// Sequence:
//  1. Start daemon; add config; Connect → Connected; capture XrayPid.
//  2. SIGKILL the daemon (fake xray child becomes an orphan).
//  3. Verify orphan is still alive.
//  4. Restart daemon with same dirs — recoveryScan should reap the orphan.
//  5. Verify orphan is ESRCH within 5s; GetStatus → Disconnected.
//
// Skipped when:
//   - not Linux
//   - caller not in xrayd group
//   - xrayd system user is absent (recoveryScan would no-op)
//
// NOTE: recoveryScan matches processes by uid=xrayd AND cmdline containing
// /var/lib/xrayd/configs/. Because acceptance tests use t.TempDir() for
// cfgDir the cmdline filter will NOT match in most environments. The test is
// therefore expected to SKIP unless explicitly run in a properly configured
// integration environment (e.g. CI with the xrayd user and the daemon writing
// into /var/lib/xrayd/configs/).
func TestKillDashNineRecoveryReapsOrphans(t *testing.T) {
	requireLinux(t)
	requireXraydGroup(t)
	if _, err := user.Lookup("xrayd"); err != nil {
		t.Skipf("xrayd system user not present; recoveryScan would no-op")
	}

	fakexrayBin := buildFakexray(t)
	binPath := buildXrayd(t)
	sockPath := filepath.Join(t.TempDir(), "x.sock")
	cfgDir := t.TempDir()
	stateDir := t.TempDir()
	socksAddr := "127.0.0.1:10809"

	env := startEnv{
		binPath:      binPath,
		sockPath:     sockPath,
		cfgDir:       cfgDir,
		stateDir:     stateDir,
		fakexrayPath: fakexrayBin,
		socksAddr:    socksAddr,
	}

	// --- first daemon run ---
	cmd1 := startDaemon(t, env)
	t.Cleanup(func() {
		_ = cmd1.Process.Signal(syscall.SIGTERM)
		_, _ = cmd1.Process.Wait()
	})
	waitForSocket(t, sockPath, 3*time.Second)

	conn, r := dialDaemon(t, sockPath)
	addResp := rpcCall(t, r, conn, "1", "Configs.Add", map[string]any{
		"json": string(acceptableConfig(10809)),
	})
	if addResp["error"] != nil {
		t.Fatalf("Configs.Add: %v", addResp["error"])
	}
	id := addResp["result"].(map[string]any)["id"].(string)

	connectResp := rpcCall(t, r, conn, "2", "Tunnel.Connect", map[string]any{"id": id})
	if connectResp["error"] != nil {
		t.Fatalf("Tunnel.Connect: %v", connectResp["error"])
	}

	statusResp := waitForConnState(t, r, conn, "Connected", 8*time.Second)
	xrayPid := int(statusResp["result"].(map[string]any)["conn"].(map[string]any)["xrayPid"].(float64))
	conn.Close()

	// --- kill daemon; child becomes an orphan ---
	if err := cmd1.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatalf("SIGKILL daemon: %v", err)
	}
	_, _ = cmd1.Process.Wait()
	waitForSocketGone(t, sockPath, 3*time.Second)

	// Orphan should still be alive at this point.
	if err := syscall.Kill(xrayPid, 0); err != nil {
		t.Fatalf("orphan xray pid %d already gone before reaper ran: %v", xrayPid, err)
	}

	// --- restart daemon; recoveryScan during NewMachine startup should reap ---
	cmd2 := startDaemon(t, env)
	t.Cleanup(func() {
		_ = cmd2.Process.Signal(syscall.SIGTERM)
		_, _ = cmd2.Process.Wait()
	})
	waitForSocket(t, sockPath, 3*time.Second)

	// Within 5s the orphan must be ESRCH.
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		if err := syscall.Kill(xrayPid, 0); errors.Is(err, syscall.ESRCH) {
			// Orphan reaped — confirm state is Disconnected.
			conn2, r2 := dialDaemon(t, sockPath)
			t.Cleanup(func() { conn2.Close() })
			status2 := rpcCall(t, r2, conn2, "1", "Tunnel.GetStatus", nil)
			if res, ok := status2["result"].(map[string]any); ok {
				if c, ok := res["conn"].(map[string]any); ok && c["state"] == "Disconnected" {
					return // success
				}
			}
			t.Fatalf("orphan reaped but GetStatus state != Disconnected: %v", status2)
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("orphan pid %d not reaped within 5s of daemon restart", xrayPid)
}

// TestFullRPCCatalogCallable exercises §18 done-criterion #3: every RPC method
// defined in §8.4 is reachable and returns the documented response or error.
func TestFullRPCCatalogCallable(t *testing.T) {
	requireLinux(t)
	requireXraydGroup(t)

	fakexrayBin := buildFakexray(t)
	binPath := buildXrayd(t)
	sockPath := filepath.Join(t.TempDir(), "x.sock")
	socksAddr := "127.0.0.1:10810"

	cmd := startDaemon(t, startEnv{
		binPath:      binPath,
		sockPath:     sockPath,
		cfgDir:       t.TempDir(),
		stateDir:     t.TempDir(),
		fakexrayPath: fakexrayBin,
		socksAddr:    socksAddr,
	})
	t.Cleanup(func() {
		_ = cmd.Process.Signal(syscall.SIGTERM)
		_, _ = cmd.Process.Wait()
	})
	waitForSocket(t, sockPath, 3*time.Second)

	conn, r := dialDaemon(t, sockPath)
	defer conn.Close()

	// --- Daemon group ---

	// Daemon.Ping
	pingResp := rpcCall(t, r, conn, "1", "Daemon.Ping", nil)
	if pingResp["error"] != nil {
		t.Fatalf("Daemon.Ping error: %v", pingResp["error"])
	}
	if res, ok := pingResp["result"].(map[string]any); !ok || res["ok"] != true {
		t.Fatalf("Daemon.Ping result.ok != true: %v", pingResp["result"])
	}

	// Daemon.GetVersion
	verResp := rpcCall(t, r, conn, "2", "Daemon.GetVersion", nil)
	if verResp["error"] != nil {
		t.Fatalf("Daemon.GetVersion error: %v", verResp["error"])
	}
	if verResp["result"] == nil {
		t.Fatal("Daemon.GetVersion returned nil result")
	}

	// --- Health group (pre-enable) ---

	// Health.GetConfig — must work even when disabled
	hcResp := rpcCall(t, r, conn, "3", "Health.GetConfig", nil)
	if hcResp["error"] != nil {
		t.Fatalf("Health.GetConfig error: %v", hcResp["error"])
	}

	// Health.Probe when disabled — must return health_disabled error
	hpDisabledResp := rpcCall(t, r, conn, "4", "Health.Probe", nil)
	if hpDisabledResp["error"] == nil {
		t.Fatal("Health.Probe should return an error when disabled")
	}
	if errObj, ok := hpDisabledResp["error"].(map[string]any); ok {
		sym, _ := errObj["data"].(map[string]any)["symbol"].(string)
		if sym != "health_disabled" {
			t.Fatalf("Health.Probe disabled error symbol = %q, want health_disabled", sym)
		}
	}

	// Health.SetEnabled(true)
	hseResp := rpcCall(t, r, conn, "5", "Health.SetEnabled", map[string]any{"enabled": true})
	if hseResp["error"] != nil {
		t.Fatalf("Health.SetEnabled(true) error: %v", hseResp["error"])
	}

	// --- Configs group ---

	// Configs.Add
	addResp := rpcCall(t, r, conn, "6", "Configs.Add", map[string]any{
		"json": string(acceptableConfig(10810)),
	})
	if addResp["error"] != nil {
		t.Fatalf("Configs.Add error: %v", addResp["error"])
	}
	addResult, ok := addResp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Configs.Add result is not an object: %v", addResp["result"])
	}
	id, _ := addResult["id"].(string)
	if len(id) != 26 {
		t.Fatalf("Configs.Add result.id len=%d want 26 (got %q)", len(id), id)
	}

	// Configs.List
	listResp := rpcCall(t, r, conn, "7", "Configs.List", nil)
	if listResp["error"] != nil {
		t.Fatalf("Configs.List error: %v", listResp["error"])
	}
	if listResp["result"] == nil {
		t.Fatal("Configs.List returned nil result")
	}

	// Configs.Validate
	validateResp := rpcCall(t, r, conn, "8", "Configs.Validate", map[string]any{"id": id})
	if validateResp["error"] != nil {
		t.Fatalf("Configs.Validate error: %v", validateResp["error"])
	}

	// --- Tunnel group ---

	// Tunnel.Connect
	connectResp := rpcCall(t, r, conn, "9", "Tunnel.Connect", map[string]any{"id": id})
	if connectResp["error"] != nil {
		t.Fatalf("Tunnel.Connect error: %v", connectResp["error"])
	}

	// Poll until Connected.
	_ = waitForConnState(t, r, conn, "Connected", 8*time.Second)

	// Configs.Remove on the ACTIVE config — must fail with config_in_use.
	rmActiveResp := rpcCall(t, r, conn, "10", "Configs.Remove", map[string]any{"id": id})
	if rmActiveResp["error"] == nil {
		t.Fatal("Configs.Remove of active config should fail")
	}
	if errObj, ok := rmActiveResp["error"].(map[string]any); ok {
		sym, _ := errObj["data"].(map[string]any)["symbol"].(string)
		if sym != "config_in_use" {
			t.Fatalf("Configs.Remove active error symbol = %q, want config_in_use", sym)
		}
	}

	// Tunnel.GetStatus — must show Connected.
	gsResp := rpcCall(t, r, conn, "11", "Tunnel.GetStatus", nil)
	if gsResp["error"] != nil {
		t.Fatalf("Tunnel.GetStatus error: %v", gsResp["error"])
	}

	// Tunnel.Disconnect
	discResp := rpcCall(t, r, conn, "12", "Tunnel.Disconnect", nil)
	if discResp["error"] != nil {
		t.Fatalf("Tunnel.Disconnect error: %v", discResp["error"])
	}

	// Poll until Disconnected.
	_ = waitForConnState(t, r, conn, "Disconnected", 5*time.Second)

	// Configs.Remove now that config is no longer active — must succeed.
	rmResp := rpcCall(t, r, conn, "13", "Configs.Remove", map[string]any{"id": id})
	if rmResp["error"] != nil {
		t.Fatalf("Configs.Remove (post-disconnect) error: %v", rmResp["error"])
	}

	// Health.Probe after SetEnabled(true) and with fakexray serving the socks
	// port — expect success (no error). The health endpoint defaults to
	// "https://connectivitycheck.gstatic.com/generate_204" which is a real URL;
	// in the test environment the probe will fail the HTTP check but that is
	// still a valid probe execution (not health_disabled). Accept either nil
	// error or any non-health_disabled error here — the important assertion is
	// that the disabled gate was lifted.
	hpResp := rpcCall(t, r, conn, "14", "Health.Probe", nil)
	if hpResp["error"] != nil {
		if errObj, ok := hpResp["error"].(map[string]any); ok {
			if sym, _ := errObj["data"].(map[string]any)["symbol"].(string); sym == "health_disabled" {
				t.Fatal("Health.Probe returned health_disabled after SetEnabled(true)")
			}
		}
		// Non-disabled error (e.g. network unreachable) is acceptable.
		t.Logf("Health.Probe returned non-disabled error (acceptable in offline env): %v", hpResp["error"])
	}
}
