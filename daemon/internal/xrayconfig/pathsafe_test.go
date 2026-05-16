package xrayconfig

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elepn/daemon/internal/derr"
)

// allowedRoot points to a directory in /usr/local/share/xray/ that we
// pre-populate in tests via the same fake-fs hook the implementation uses.
// In Plan 2 v1 there is exactly one allowed root; we override it per test
// so we don't need to actually write to /usr/local/share/.

func TestCheckPathSafetyAcceptsConfigWithoutPaths(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"inbounds":[{"port":10808,"protocol":"socks","listen":"127.0.0.1"}]}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("expected nil for path-free config, got %v", err)
	}
}

func TestCheckPathSafetyRejectsRelativeCertificateFile(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"inbounds":[{"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"cert.pem"}]}}}]}`)
	err := checkPathSafety(cfg)
	if !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", err)
	}
}

func TestCheckPathSafetyRejectsHomePath(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"log":{"access":"/home/user/access.log"}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", err)
	}
}

func TestCheckPathSafetyRejectsExtPrefix(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"routing":{"rules":[{"path":"ext:Geosite:cn-domain.dat"}]}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", err)
	}
}

func TestCheckPathSafetyRejectsDotDot(t *testing.T) {
	dir := t.TempDir()
	withAllowedRoot(t, dir)
	// Even though /tmp/.../../etc/passwd would *clean* to /etc/passwd,
	// any ".." segment is an immediate reject (don't even try to resolve).
	cfg := []byte(`{"log":{"error":"` + dir + `/../etc/passwd"}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", err)
	}
}

func TestCheckPathSafetyAcceptsAllowedAbsolutePath(t *testing.T) {
	dir := t.TempDir()
	withAllowedRoot(t, dir)
	geosite := filepath.Join(dir, "geosite.dat")
	if err := os.WriteFile(geosite, []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := []byte(`{"dns":{"dat":"` + geosite + `"}}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("expected nil for allowed absolute path, got %v", err)
	}
}

func TestCheckPathSafetyRejectsDSLInPathBearingKey(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"log":{"access":"geoip:US"}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe (DSL string in a path-bearing key)", err)
	}
}

func TestCheckPathSafetyAllowsDSLInUnknownKey(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	// DSL strings under non-path keys (e.g. routing rules) must pass.
	cfg := []byte(`{"routing":{"rules":[{"domain":["geosite:cn"]}]}}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("expected nil for DSL in non-path-bearing key, got %v", err)
	}
}

func TestCheckPathSafetyRejectsMalformedJSON(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	if err := checkPathSafety([]byte(`{not json`)); !errors.Is(err, derr.ErrConfigMalformedJSON) {
		t.Fatalf("err = %v, want ErrConfigMalformedJSON", err)
	}
}

func TestCheckPathSafetyDetailPointsAtOffendingKey(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"inbounds":[{"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"cert.pem"}]}}}]}`)
	err := checkPathSafety(cfg)
	de := derr.AsDerr(err)
	if de == nil {
		t.Fatalf("err is not a *derr.Error: %v", err)
	}
	raw := de.JSON()
	if !strings.Contains(string(raw), "/inbounds/0/streamSettings/tlsSettings/certificates/0/certificateFile") {
		t.Fatalf("JSON pointer not in error detail: %s", raw)
	}
}

// withAllowedRoot swaps the package-level allowedAbsoluteRoots variable for
// the duration of the test, then restores it on cleanup.
func withAllowedRoot(t *testing.T, dir string) {
	t.Helper()
	if !strings.HasSuffix(dir, "/") {
		dir += "/"
	}
	orig := allowedAbsoluteRoots
	allowedAbsoluteRoots = []string{dir}
	t.Cleanup(func() { allowedAbsoluteRoots = orig })
}
