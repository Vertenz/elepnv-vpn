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

func TestCheckPathSafetyExtPrefixInRoutingPathKeyIsAllowed(t *testing.T) {
	// Before the fix, "path" was in pathBearingKeys, so an ext: value there was
	// rejected by validatePathValue. After the fix, routing.rules[*].path is a
	// URL path field — not filesystem-bearing — and ext: in a URL path is
	// semantically odd but not a security risk (looksSuspicious won't flag it).
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"routing":{"rules":[{"path":"ext:Geosite:cn-domain.dat"}]}}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("ext: in routing path field should now be accepted, got: %v", err)
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

func TestCheckPathSafetyRejectsDotDotEvenWhenCleanedPathIsAllowed(t *testing.T) {
	dir := t.TempDir()
	// Pre-stage a file under the allowed root.
	if err := os.WriteFile(filepath.Join(dir, "geosite.dat"), []byte("fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	withAllowedRoot(t, dir)
	// Input with a `..` segment that would CLEAN to a path inside the allowed
	// root. Without the raw-input check, this would slip through. With it,
	// we reject the traversal attempt regardless of the cleaned destination.
	cfg := []byte(`{"dns":{"dat":"` + dir + `/subdir/../geosite.dat"}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe — raw `..` must be rejected even if cleaned path is in the allowlist", err)
	}
}

// --- Acceptance tests: the fix's reason for existing ---

func TestCheckPathSafetyCurrentlyRejectsWebSocketPath(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"outbounds":[{"streamSettings":{"network":"ws","wsSettings":{"path":"/ray"}}}]}`)
	if err := checkPathSafety(cfg); err == nil {
		t.Skip("bug fixed already")
	}
	t.Logf("current behavior (bug): confirmed rejected")
}

func TestCheckPathSafetyAcceptsWebSocketPath(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{
		"outbounds": [{
			"tag": "proxy",
			"protocol": "vless",
			"streamSettings": {
				"network": "ws",
				"wsSettings": { "path": "/ray", "headers": { "Host": "example.com" } }
			}
		}]
	}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("WebSocket path should be accepted, got: %v", err)
	}
}

func TestCheckPathSafetyAcceptsGRPCPath(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{
		"outbounds": [{
			"streamSettings": {
				"network": "grpc",
				"grpcSettings": { "serviceName": "GunService" }
			}
		}],
		"inbounds": [{
			"streamSettings": {
				"network": "grpc",
				"grpcSettings": { "serviceName": "S", "multiMode": true }
			}
		}]
	}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("gRPC config should be accepted, got: %v", err)
	}
}

func TestCheckPathSafetyAcceptsXHTTPPath(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{
		"outbounds": [{
			"streamSettings": {
				"network": "xhttp",
				"xhttpSettings": { "path": "/xhttp-endpoint", "mode": "auto" }
			}
		}]
	}`)
	if err := checkPathSafety(cfg); err != nil {
		t.Fatalf("XHTTP config should be accepted, got: %v", err)
	}
}

// --- Security tests: the fix must preserve these ---

func TestCheckPathSafetyStillRejectsSensitivePrefixes(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cases := []struct{ name, s string }{
		{"etc-passwd", "/etc/passwd"},
		{"root-ssh", "/root/.ssh/authorized_keys"},
		{"proc-self", "/proc/self/mem"},
		{"sys-class", "/sys/class/net"},
		{"dev-null", "/dev/null"},
		{"home-tilde", "~/.bashrc"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Place the suspicious value in a non-path-bearing key.
			cfg := []byte(`{"routing":{"rules":[{"domain":["` + tc.s + `"]}]}}`)
			if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
				t.Fatalf("err = %v, want ErrPathUnsafe for %q", err, tc.s)
			}
		})
	}
}

func TestCheckPathSafetyStillRejectsBadCertificateFile(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"inbounds":[{"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"/etc/passwd"}]}}}]}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe (certificateFile outside allowed root)", err)
	}
}

func TestCheckPathSafetyStillRejectsBadLogAccess(t *testing.T) {
	withAllowedRoot(t, t.TempDir())
	cfg := []byte(`{"log":{"access":"/etc/passwd"}}`)
	if err := checkPathSafety(cfg); !errors.Is(err, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe (log.access)", err)
	}
}

// withAllowedRoot swaps the package-level allowedAbsoluteRoots variable for
// the duration of the test, then restores it on cleanup.
// The root is canonicalised via EvalSymlinks so the comparison in
// checkPathSafety (which also calls EvalSymlinks on the candidate path)
// passes on platforms where t.TempDir() returns a symlinked path
// (e.g. macOS /var/folders/... → /private/var/folders/...).
func withAllowedRoot(t *testing.T, dir string) {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", dir, err)
	}
	if !strings.HasSuffix(resolved, "/") {
		resolved += "/"
	}
	orig := allowedAbsoluteRoots
	allowedAbsoluteRoots = []string{resolved}
	t.Cleanup(func() { allowedAbsoluteRoots = orig })
}
