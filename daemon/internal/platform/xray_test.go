package platform_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"elepn/daemon/internal/platform"
)

// silentLogger discards every log line; we don't assert on logs in unit tests.
func silentLogger(t *testing.T) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}

func TestDiscoverFailsClosedWhenNoXray(t *testing.T) {
	// Construct a PATH that contains only an empty temp dir.
	dir := t.TempDir()
	t.Setenv("PATH", dir)
	info := platform.Discover(context.Background(), silentLogger(t))
	if info.Found {
		t.Fatalf("expected Found=false on empty PATH, got %+v", info)
	}
}

func TestDiscoverPopulatesPathAndVersion(t *testing.T) {
	// Build a fake `xray` shell script that prints a banner on `xray version`.
	dir := t.TempDir()
	script := filepath.Join(dir, "xray")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho 'FakeXray 0.0.0 (test)'\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	info := platform.Discover(context.Background(), silentLogger(t))
	if !info.Found {
		t.Fatalf("expected Found=true, got %+v", info)
	}
	if info.Path != script {
		t.Fatalf("Path = %q, want %q", info.Path, script)
	}
	if !strings.HasPrefix(info.Version, "FakeXray") {
		t.Fatalf("Version = %q, want prefix FakeXray", info.Version)
	}
}
