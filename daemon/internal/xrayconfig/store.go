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

// ParseULID parses a 26-character Crockford-base32 ULID string.
//
// Returns ErrConfigUnknown — NOT ErrInvalidParams — when the input is not a
// syntactically valid ULID. The intentional conflation lets callers handle
// "you sent garbage" and "we don't have that id" with one branch: in both
// cases the right behaviour is "this id does not exist". The cost is that
// the renderer cannot distinguish a malformed id (likely a bug on its side)
// from a stale id (likely user state drift). Plan 4 may revisit if telemetry
// shows the distinction matters.
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

// stagingSuffix is appended to the ULID filename while the staged config is
// being validated. List excludes it (it doesn't end in ".json"), so a
// concurrent Configs.List call cannot observe a phantom ID that may yet
// fail validation.
const stagingSuffix = ".staging"

// MaxConfigs is the per-registry cap from spec §8.3. The limit exists to bound
// the directory-scan cost List() pays on each call; each entry is small (~few
// KiB) so 1000 is generous in absolute terms. Declared as var (not const) so
// tests can lower it without writing 1000 files.
var MaxConfigs = 1000

// Add performs, in strict order:
//
//  1. checkPathSafety(jsonBytes)                        (§6.6; subsumes json.Valid via Unmarshal)
//  2. checkInboundSafety(jsonBytes, expectedSocksAddr)  (§6.7)
//  3. os.MkdirAll(dir, 0o700)
//  4. ulid.New() (entropy read may fail under sandbox; propagated, never panicked)
//  5. atomic write to <ulid>.json.staging (renameio + WithStaticPermissions=0o600)
//  6. `xrayPath run -test -c <staging>`
//  7. On success: os.Rename staging → <ulid>.json. On failure: unlink staging.
//
// Steps 1-3 reject without spawning any subprocess — cheap and safe. The
// staging suffix matters: between write and validate the file exists on
// disk; if we wrote directly to <ulid>.json a concurrent List would see a
// not-yet-validated config and the caller could act on an ID that we will
// shortly delete. List skips anything not ending in ".json", so staging is
// invisible to clients.
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

	n, err := s.countConfigs()
	if err != nil {
		return ULID{}, fmt.Errorf("count configs: %w", err)
	}
	if n >= MaxConfigs {
		return ULID{}, derr.ErrConfigQuotaExceeded
	}

	id, err := s.makeULID()
	if err != nil {
		return ULID{}, derr.ErrInternal.With(fmt.Errorf("ulid: %w", err))
	}
	finalPath := s.pathFor(id)
	stagingPath := finalPath + stagingSuffix

	// renameio writes to its own internal temp file then renames atomically to
	// stagingPath; on any error the temp is cleaned up automatically.
	// WithStaticPermissions sets 0o600 ignoring the process umask, satisfying
	// spec §6.1 rev-4 P1-4 which mandates exactly 0o600 regardless of umask.
	if err := renameio.WriteFile(stagingPath, jsonBytes, 0o600, renameio.WithStaticPermissions(0o600)); err != nil {
		return ULID{}, fmt.Errorf("atomic write: %w", err)
	}

	res, vErr := Validate(ctx, s.xrayPath, stagingPath)
	if vErr != nil {
		_ = os.Remove(stagingPath)
		return ULID{}, derr.ErrInternal.With(vErr)
	}
	if res.Timeout {
		_ = os.Remove(stagingPath)
		return ULID{}, derr.ErrValidationTimeout
	}
	if !res.OK {
		_ = os.Remove(stagingPath)
		detail := derr.Detail{"summary": res.Error}
		if res.Stderr != "" {
			detail["stderr"] = res.Stderr
		}
		return ULID{}, derr.ErrConfigInvalid.WithDetail(detail).WithMessage(res.Error)
	}

	// Atomic publish. The staging file is now valid and ready to surface
	// through List.
	if err := os.Rename(stagingPath, finalPath); err != nil {
		_ = os.Remove(stagingPath)
		return ULID{}, fmt.Errorf("publish staging: %w", err)
	}
	return id, nil
}

// Get returns the raw JSON contents of <id>.json. Returns ErrConfigUnknown
// if the file is absent (or any other os-error wrapped).
func (s *Store) Get(id ULID) (string, error) {
	p, err := s.PathFor(id)
	if err != nil {
		return "", err // already ErrConfigUnknown
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p, err)
	}
	return string(data), nil
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

// makeULID returns a fresh monotonic ULID. The entropy source can fail under
// pathological conditions (sandbox without /dev/urandom, fd exhaustion); we
// propagate the error rather than panicking via ulid.MustNew, so a single
// transient entropy hiccup doesn't take down the daemon.
func (s *Store) makeULID() (ULID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return ulid.New(ulid.Timestamp(time.Now()), s.entropy)
}

// countConfigs returns the number of valid <ULID>.json entries in the registry.
// Cheaper than List: no file reads, no sha256 hashing.
func (s *Store) countConfigs() (int, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	n := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := ulid.ParseStrict(strings.TrimSuffix(name, ".json")); err != nil {
			continue
		}
		n++
	}
	return n, nil
}
