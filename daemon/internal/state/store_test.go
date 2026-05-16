package state_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"elepn/daemon/internal/state"
)

func TestLoadOnFirstRunReturnsErrNotExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := state.NewStore(path)
	_, err := s.Load()
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestSaveAndLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := state.NewStore(path)
	in := state.State{
		Version:  state.CurrentVersion,
		State:    "Connected",
		ConfigID: "01HX7N9KQ8R3JCBVB6Z3K9V4FK",
		XrayPid:  4321,
		Since:    time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	fi, _ := os.Stat(path)
	if fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
	got, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if got.State != in.State || got.ConfigID != in.ConfigID || got.XrayPid != in.XrayPid {
		t.Fatalf("roundtrip mismatch: got %+v, want %+v", got, in)
	}
}

func TestLoadCorruptReturnsErrCorrupt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := state.NewStore(path)
	_, err := s.Load()
	if !errors.Is(err, state.ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
}

func TestLoadTooNewReturnsErrTooNew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	if err := os.WriteFile(path, []byte(`{"version":999,"state":"Disconnected"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := state.NewStore(path)
	_, err := s.Load()
	if !errors.Is(err, state.ErrTooNew) {
		t.Fatalf("err = %v, want ErrTooNew", err)
	}
}

func TestQuarantineRenamesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state.json")
	if err := os.WriteFile(path, []byte(`{garbage`), 0o600); err != nil {
		t.Fatal(err)
	}
	s := state.NewStore(path)
	if err := s.Quarantine(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err == nil {
		t.Fatal("expected state.json to be gone after Quarantine")
	}
	entries, _ := os.ReadDir(dir)
	found := false
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "state.json.corrupt-") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected state.json.corrupt-<ts>, got %v", entries)
	}
}

func TestSaveDefaultsToCurrentVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s := state.NewStore(path)
	// Caller forgets to set Version — Save should default.
	in := state.State{State: "Disconnected", Since: time.Now()}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Load()
	if got.Version != state.CurrentVersion {
		t.Fatalf("Version = %d, want %d", got.Version, state.CurrentVersion)
	}
}
