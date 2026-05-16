// Package version exposes the daemon's build-time version string.
//
// Set via -ldflags at build time:
//
//	go build -ldflags="-X elepn/daemon/internal/version.Version=$(git describe --tags --always)" ./cmd/xrayd
package version

// Version is the build-time version string; "dev" until ldflags overrides it.
var Version = "dev"
