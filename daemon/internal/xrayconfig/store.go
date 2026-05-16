package xrayconfig

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/renameio/v2"
	"github.com/oklog/ulid/v2"

	"elepn/daemon/internal/derr"
)

// ULID is re-exported so callers don't need to import oklog/ulid/v2 just to
// hold an ID value. We don't bury the type — Store API uses it directly.
type ULID = ulid.ULID

// ParseULID parses a 26-character Crockford-base32 ULID string. Returns
// ErrConfigUnknown if the input isn't a syntactically valid ULID, so callers
// can treat "bad id from the wire" the same as "id not in store".
func ParseULID(s string) (ULID, error) {
	id, err := ulid.ParseStrict(s)
	if err != nil {
		return ULID{}, derr.ErrConfigUnknown.With(err)
	}
	return id, nil
}

// ConfigInfo is one entry in the response of Configs.List. Fields are
// derived from the filesystem on each List call — there is no in-memory
// index. AddedAt comes from the ULID's encoded timestamp (millisecond
// precision), not from filesystem mtime, so it survives backup/restore.
type ConfigInfo struct {
	ID      ULID      `json:"id"`
	SHA256  string    `json:"sha256"` // hex-encoded
	AddedAt time.Time `json:"addedAt"`
}

// Store owns /var/lib/xrayd/configs/. One instance per daemon process. All
// methods are safe for concurrent use.
type Store struct {
	dir               string
	xrayPath          string
	expectedSocksAddr string

	// entropy is the ULID entropy source. Wrapped in a mutex because
	// math/rand-style monotonic readers aren't goroutine-safe.
	mu      sync.Mutex
	entropy *ulid.MonotonicEntropy
}

// NewStore constructs a Store rooted at dir. xrayPath is the binary used for
// `xray run -test` validation; expectedSocksAddr is what checkInboundSafety
// compares against. The directory is created on demand (0700, owner xrayd).
func NewStore(dir, xrayPath, expectedSocksAddr string) *Store {
	return &Store{
		dir:               dir,
		xrayPath:          xrayPath,
		expectedSocksAddr: expectedSocksAddr,
		entropy:           ulid.Monotonic(rand.Reader, 0),
	}
}

// Add performs, in strict order:
//
//  1. checkPathSafety(jsonBytes)                        (§6.6, also validates JSON)
//  2. checkInboundSafety(jsonBytes, expectedSocksAddr)  (§6.7)
//  3. ulid.Make() + atomic write to <ulid>.json via renameio
//  4. `xrayPath run -test -c <path>`
//  5. On success: keep the file. On any failure: unlink it.
//
// Steps 1-2 reject without spawning any subprocess — cheap and safe.
func (s *Store) Add(ctx context.Context, jsonBytes []byte) (ULID, error) {
	if err := checkPathSafety(jsonBytes); err != nil {
		return ULID{}, err
	}
	if err := checkInboundSafety(jsonBytes, s.expectedSocksAddr); err != nil {
		return ULID{}, err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return ULID{}, fmt.Errorf("ensure config dir: %w", err)
	}

	id := s.makeULID()
	finalPath := s.pathFor(id)
	// renameio writes to a sibling temp file then renames atomically; on any
	// error from WriteFile the temp is cleaned up automatically.
	if err := renameio.WriteFile(finalPath, jsonBytes, 0o600); err != nil {
		return ULID{}, fmt.Errorf("atomic write: %w", err)
	}

	res, err := Validate(ctx, s.xrayPath, finalPath)
	if err != nil {
		_ = os.Remove(finalPath)
		return ULID{}, derr.ErrInternal.With(err)
	}
	if !res.OK {
		_ = os.Remove(finalPath)
		detail := derr.Detail{"summary": res.Error}
		if res.Stderr != "" {
			detail["stderr"] = res.Stderr
		}
		return ULID{}, derr.ErrConfigInvalid.WithDetail(detail).WithMessage(res.Error)
	}
	return id, nil
}

// PathFor returns the absolute path of <id>.json or ErrConfigUnknown.
func (s *Store) PathFor(id ULID) (string, error) {
	p := s.pathFor(id)
	if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
		return "", derr.ErrConfigUnknown
	} else if err != nil {
		return "", fmt.Errorf("stat config: %w", err)
	}
	return p, nil
}

// List returns one ConfigInfo per *.json file in the dir, with SHA-256
// computed on demand. Order is not specified — callers must sort if needed.
func (s *Store) List() ([]ConfigInfo, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read configs dir: %w", err)
	}
	out := make([]ConfigInfo, 0, len(entries))
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		id, err := ulid.ParseStrict(strings.TrimSuffix(name, ".json"))
		if err != nil {
			continue // skip anything that isn't a ULID-named json file
		}
		data, err := os.ReadFile(filepath.Join(s.dir, name))
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", name, err)
		}
		sum := sha256.Sum256(data)
		out = append(out, ConfigInfo{
			ID:      id,
			SHA256:  hex.EncodeToString(sum[:]),
			AddedAt: ulid.Time(id.Time()),
		})
	}
	return out, nil
}

// Remove unlinks <id>.json. Returns ErrConfigUnknown if the file is already
// gone. The IPC layer is the enforcement point for "is this id active" — see
// internal/ipc/methods.go (Plan 3 will wire Machine.IsActive there).
func (s *Store) Remove(id ULID) error {
	err := os.Remove(s.pathFor(id))
	switch {
	case err == nil:
		return nil
	case errors.Is(err, fs.ErrNotExist):
		return derr.ErrConfigUnknown
	default:
		return fmt.Errorf("unlink config: %w", err)
	}
}

// Validate re-runs `xray run -test` against an already-stored config. Useful
// after geodata updates or to assert a config is still acceptable to the
// current xray-core version.
func (s *Store) Validate(ctx context.Context, id ULID) (ValidateResult, error) {
	path, err := s.PathFor(id)
	if err != nil {
		return ValidateResult{}, err
	}
	return Validate(ctx, s.xrayPath, path)
}

func (s *Store) pathFor(id ULID) string {
	return filepath.Join(s.dir, id.String()+".json")
}

func (s *Store) makeULID() ULID {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.MustNew(ulid.Timestamp(time.Now()), s.entropy)
}
