package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"time"

	"github.com/google/renameio/v2"
)

// CurrentVersion is the schema version. Bump for breaking changes; adding
// optional fields does NOT need a bump.
const CurrentVersion = 1

var (
	// ErrCorrupt is returned by Load when state.json exists but cannot be parsed.
	ErrCorrupt = errors.New("state.json: corrupt")
	// ErrTooNew is returned when Version > CurrentVersion (downgrade attempt).
	ErrTooNew = errors.New("state.json: version newer than this daemon understands")
)

// State is the persisted UX hint. Recovery (§11) does NOT depend on this
// file for cleanup — it scans /proc instead. state.json exists so the
// renderer can show "you were connected to <X> before the restart".
type State struct {
	Version  int       `json:"version"`
	State    string    `json:"state"`
	ConfigID string    `json:"configID,omitempty"`
	XrayPid  int       `json:"xrayPid,omitempty"`
	Since    time.Time `json:"since"`
}

// Store is a single-file persistence layer. Save is serialized by the actor
// (single-goroutine); renameio's atomic rename makes Load safe against
// in-flight Saves on a different goroutine.
type Store struct{ path string }

func NewStore(path string) *Store { return &Store{path: path} }

// Load returns the persisted State, or one of (fs.ErrNotExist, ErrCorrupt,
// ErrTooNew).
func (s *Store) Load() (State, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return State{}, err
		}
		return State{}, fmt.Errorf("read state.json: %w", err)
	}
	var st State
	if err := json.Unmarshal(data, &st); err != nil {
		return State{}, fmt.Errorf("%w: %v", ErrCorrupt, err)
	}
	if st.Version > CurrentVersion {
		return State{}, fmt.Errorf("%w: version=%d", ErrTooNew, st.Version)
	}
	return st, nil
}

// Save atomically replaces state.json with the JSON encoding of st. Mode is
// exactly 0o600 regardless of process umask, per spec §10.2 rev-4 P1-4 —
// file holds the active config ULID.
func (s *Store) Save(st State) error {
	if st.Version == 0 {
		st.Version = CurrentVersion
	}
	data, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}
	if err := renameio.WriteFile(s.path, data, 0o600, renameio.WithStaticPermissions(0o600)); err != nil {
		return fmt.Errorf("write state.json: %w", err)
	}
	return nil
}

// Quarantine renames state.json → state.json.corrupt-<unix-ts>. Used after
// Load returns ErrCorrupt so the next Save creates a fresh file while the
// bad copy is preserved for forensic analysis.
func (s *Store) Quarantine() error {
	dst := s.path + ".corrupt-" + strconv.FormatInt(time.Now().Unix(), 10)
	if err := os.Rename(s.path, dst); err != nil {
		return fmt.Errorf("quarantine state.json: %w", err)
	}
	return nil
}
