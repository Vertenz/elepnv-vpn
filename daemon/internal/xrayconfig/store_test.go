package xrayconfig_test

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/xrayconfig"
)

// validCfg is a single-loopback-SOCKS5 inbound config that passes both
// pathsafe and inboundsafe in tests.
const validCfg = `{
  "inbounds":[{
    "tag":"socks-in","listen":"127.0.0.1","port":10808,"protocol":"socks",
    "settings":{"auth":"noauth","udp":true}
  }]
}`

func newFakeXrayDir(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "xray")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func newStore(t *testing.T, xrayBody string) (*xrayconfig.Store, string) {
	t.Helper()
	dir := t.TempDir()
	xrayPath := newFakeXrayDir(t, xrayBody)
	return xrayconfig.NewStore(dir, xrayPath, "127.0.0.1:10808"), dir
}

func TestAddStoresByteExactAndReturnsULID(t *testing.T) {
	store, dir := newStore(t, "exit 0\n")
	id, err := store.Add(context.Background(), []byte(validCfg))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if len(id.String()) != 26 {
		t.Fatalf("returned id = %q, want 26-char ULID", id.String())
	}
	path := filepath.Join(dir, id.String()+".json")
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("stored file missing: %v", err)
	}
	if string(got) != validCfg {
		t.Fatalf("byte-exact storage broken:\n  got  %q\n  want %q", string(got), validCfg)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("stored file mode = %v, want 0600 (no group/other read)", fi.Mode().Perm())
	}
}

func TestAddRejectsMalformedJSONBeforeSpawning(t *testing.T) {
	store, _ := newStore(t, "echo SHOULD_NOT_RUN >&2; exit 1\n")
	_, err := store.Add(context.Background(), []byte(`{not json`))
	if !errors.Is(err, derr.ErrConfigMalformedJSON) {
		t.Fatalf("err = %v, want ErrConfigMalformedJSON", err)
	}
}

func TestAddRejectsUnsafePathBeforeSpawning(t *testing.T) {
	store, _ := newStore(t, "echo SHOULD_NOT_RUN >&2; exit 1\n")
	bad := `{"inbounds":[{"listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{"auth":"noauth"},"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"/etc/passwd"}]}}}]}`
	_, err := store.Add(context.Background(), []byte(bad))
	if !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", err)
	}
}

func TestAddRejectsUnsafeInboundBeforeSpawning(t *testing.T) {
	store, _ := newStore(t, "echo SHOULD_NOT_RUN >&2; exit 1\n")
	bad := `{"inbounds":[{"listen":"0.0.0.0","port":10808,"protocol":"socks","settings":{"auth":"noauth"}}]}`
	_, err := store.Add(context.Background(), []byte(bad))
	if !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestAddRejectsConfigXrayTestFails(t *testing.T) {
	store, dir := newStore(t, `echo "2024/01/01 [Error] config: invalid outbound" >&2; exit 23
`)
	_, err := store.Add(context.Background(), []byte(validCfg))
	if !errors.Is(err, derr.ErrConfigInvalid) {
		t.Fatalf("err = %v, want ErrConfigInvalid", err)
	}
	// CRITICAL: failed Add must NOT leave the tmp file behind.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("expected empty configs dir after failed Add, got: %v", entries)
	}
}

func TestAddIsNonIdempotent(t *testing.T) {
	store, _ := newStore(t, "exit 0\n")
	id1, _ := store.Add(context.Background(), []byte(validCfg))
	id2, _ := store.Add(context.Background(), []byte(validCfg))
	if id1 == id2 {
		t.Fatal("two Adds of identical bytes returned the same ULID; spec §6.3 says they must differ")
	}
}

func TestListReturnsAllStoredConfigs(t *testing.T) {
	store, _ := newStore(t, "exit 0\n")
	id1, _ := store.Add(context.Background(), []byte(validCfg))
	id2, _ := store.Add(context.Background(), []byte(validCfg))

	infos, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("got %d entries, want 2", len(infos))
	}
	// ConfigInfo must have ID + SHA256 + AddedAt populated.
	for _, info := range infos {
		if info.ID != id1 && info.ID != id2 {
			t.Fatalf("unexpected id: %v", info.ID)
		}
		if len(info.SHA256) != 64 {
			t.Fatalf("SHA256 = %q, want 64-hex chars", info.SHA256)
		}
		if info.AddedAt.IsZero() {
			t.Fatal("AddedAt is zero")
		}
	}
}

func TestRemoveDeletesFile(t *testing.T) {
	store, dir := newStore(t, "exit 0\n")
	id, _ := store.Add(context.Background(), []byte(validCfg))
	if err := store.Remove(id); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	_, err := os.Stat(filepath.Join(dir, id.String()+".json"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("file still exists after Remove: %v", err)
	}
}

func TestRemoveUnknownReturnsConfigUnknown(t *testing.T) {
	store, _ := newStore(t, "exit 0\n")
	// 26-char valid ULID format, never added.
	bogus, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	err := store.Remove(bogus)
	if !errors.Is(err, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", err)
	}
}

func TestPathForUnknownReturnsConfigUnknown(t *testing.T) {
	store, _ := newStore(t, "exit 0\n")
	bogus, _ := xrayconfig.ParseULID("01HX7N9KQ8R3JCBVB6Z3K9V4FK")
	_, err := store.PathFor(bogus)
	if !errors.Is(err, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", err)
	}
}

func TestValidateReRunsXrayAgainstStoredFile(t *testing.T) {
	// Two-stage: Add with xray-pass; then swap xray for a failing one and
	// confirm Validate (which respawns xray-test) catches it.
	dir := t.TempDir()
	xrayPath := filepath.Join(dir, "xray")
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	store := xrayconfig.NewStore(t.TempDir(), xrayPath, "127.0.0.1:10808")
	id, err := store.Add(context.Background(), []byte(validCfg))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	// Swap script body to a failing one (path is the same, so Store still sees it).
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\necho 'late breakage' >&2; exit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := store.Validate(context.Background(), id)
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got.OK {
		t.Fatal("Validate OK = true, want false after swap")
	}
	if !strings.Contains(got.Error, "late breakage") {
		t.Fatalf("Error = %q, want it to surface the new failure", got.Error)
	}
}

// Concurrent-safety smoke: 20 goroutines each Adds 5 configs. List sees all 100.
func TestStoreConcurrentAdds(t *testing.T) {
	// Raise the concurrency limit so 20 goroutines with an instant fake xray
	// never hit the semaphore queue cap (which would cause spurious rejections
	// unrelated to the store's own concurrency-safety).
	xrayconfig.MaxValidateConcurrent = 20
	xrayconfig.MaxValidateQueue = 20
	xrayconfig.ResetValidateLimitsForTests()
	t.Cleanup(func() {
		xrayconfig.MaxValidateConcurrent = 4
		xrayconfig.MaxValidateQueue = 16
		xrayconfig.ResetValidateLimitsForTests()
	})

	store, _ := newStore(t, "exit 0\n")
	const goroutines, perGoroutine = 20, 5
	done := make(chan struct{}, goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for j := 0; j < perGoroutine; j++ {
				if _, err := store.Add(context.Background(), []byte(validCfg)); err != nil {
					t.Errorf("Add: %v", err)
					return
				}
			}
		}()
	}
	for i := 0; i < goroutines; i++ {
		<-done
	}
	infos, err := store.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != goroutines*perGoroutine {
		t.Fatalf("got %d entries, want %d", len(infos), goroutines*perGoroutine)
	}
}

func TestAddProducesExactly0600EvenWithHostileUmask(t *testing.T) {
	// Build the store (and its temp directories) before restricting the umask so
	// that t.TempDir() / os.MkdirAll inside newStore still succeed.
	store, dir := newStore(t, "exit 0\n")

	// Force an unusual umask so a renameio call that respects umask would
	// downgrade 0o600 → 0o400. We restore the original at cleanup.
	orig := syscall.Umask(0o177)
	defer syscall.Umask(orig)

	id, err := store.Add(context.Background(), []byte(validCfg))
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	fi, err := os.Stat(filepath.Join(dir, id.String()+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600 (umask must be ignored per spec §6.1)", fi.Mode().Perm())
	}
}

// Confirm we can json-marshal ConfigInfo into the wire shape the spec promises.
func TestConfigInfoMarshalsToWireShape(t *testing.T) {
	store, _ := newStore(t, "exit 0\n")
	id, _ := store.Add(context.Background(), []byte(validCfg))
	infos, _ := store.List()
	b, err := json.Marshal(infos[0])
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	_ = json.Unmarshal(b, &got)
	if got["id"] != id.String() {
		t.Fatalf("id = %v, want %q", got["id"], id.String())
	}
	if _, ok := got["sha256"]; !ok {
		t.Fatal("missing sha256 in wire shape")
	}
	if _, ok := got["addedAt"]; !ok {
		t.Fatal("missing addedAt in wire shape")
	}
}

func TestListIgnoresStagingFiles(t *testing.T) {
	// Regression for the phantom-config bug: a leftover <ulid>.json.staging
	// from a crashed Add must NOT be returned by List as a valid ConfigInfo.
	// .staging files exist only between renameio.WriteFile and the final
	// os.Rename inside Add.
	store, dir := newStore(t, "exit 0\n")
	const stagingName = "01HX7N9KQ8R3JCBVB6Z3K9V4FK.json.staging"
	if err := os.WriteFile(filepath.Join(dir, stagingName), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	infos, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 0 {
		t.Fatalf("List returned staging file as a valid config: %v", infos)
	}
}

func TestAddRejectsBeyondMaxConfigs(t *testing.T) {
	// Lower the cap for fast testing; restore on cleanup.
	orig := xrayconfig.MaxConfigs
	xrayconfig.MaxConfigs = 3
	t.Cleanup(func() { xrayconfig.MaxConfigs = orig })

	store, _ := newStore(t, "exit 0\n")
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		if _, err := store.Add(ctx, []byte(validCfg)); err != nil {
			t.Fatalf("Add #%d (under cap): %v", i+1, err)
		}
	}
	_, err := store.Add(ctx, []byte(validCfg))
	if !errors.Is(err, derr.ErrConfigQuotaExceeded) {
		t.Fatalf("Add #4 err = %v, want ErrConfigQuotaExceeded", err)
	}
}

func TestAddSurfacesValidationTimeoutAsTypedError(t *testing.T) {
	// Regression for spec §9.2 -32013: when xray validation exceeds the
	// daemon's timeout, Store.Add must return ErrValidationTimeout (NOT
	// the generic ErrConfigInvalid). The IPC dispatcher relies on this
	// typed error to give renderers a different retry policy than rejection.
	store, dir := newStore(t, "sleep 30\n")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := store.Add(ctx, []byte(validCfg))
	if !errors.Is(err, derr.ErrValidationTimeout) {
		t.Fatalf("err = %v, want ErrValidationTimeout", err)
	}
	// The staging file must also be cleaned up.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("staging not cleaned after timeout, dir has: %v", entries)
	}
}
