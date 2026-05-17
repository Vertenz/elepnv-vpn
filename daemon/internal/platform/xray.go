// Package platform contains Linux-specific runtime checks the daemon performs
// at startup. Read-only: nothing here changes system state.
package platform

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"
)

// XrayInfo describes the xray-core binary the daemon will spawn.
type XrayInfo struct {
	Path    string // e.g. /usr/local/bin/xray; empty when Found is false
	Version string // first line of `xray version`; empty when Found is false
	Found   bool   // false when LookPath failed
}

// InstallerServiceState describes the XTLS installer's own xray.service.
// We log a warning when it's enabled; the daemon does not modify it.
type InstallerServiceState int

const (
	InstallerServiceUnknown      InstallerServiceState = iota // systemctl missing, killed, etc.
	InstallerServiceEnabled                                   // is-enabled exit 0
	InstallerServiceDisabled                                  // is-enabled exit 1..3 (disabled, masked, static)
	InstallerServiceNotInstalled                              // is-enabled exit 4 (no such unit) — the common case
)

// Discover runs the daemon's xray-core discovery at startup. It never returns
// an error: if xray is missing we surface that via Found=false, and the daemon
// continues to start (Tunnel.Connect calls will return xray_not_found).
func Discover(ctx context.Context, log *slog.Logger) XrayInfo {
	path, err := exec.LookPath("xray")
	if err != nil {
		log.Warn("xray binary not found; install with XTLS installer",
			"PATH", os.Getenv("PATH"))
		return XrayInfo{Found: false}
	}
	switch installerServiceState() {
	case InstallerServiceEnabled:
		log.Warn("XTLS installer's xray.service is enabled; conflicts with xrayd",
			"fix", "sudo systemctl disable --now xray.service")
	case InstallerServiceUnknown:
		log.Debug("could not determine xray.service state")
		// NotInstalled and Disabled are the expected silent cases.
	}
	return XrayInfo{Path: path, Version: runXrayVersion(ctx, path), Found: true}
}

// runXrayVersion executes `xray version` with a short deadline and returns
// the first non-blank line. Uses CombinedOutput so we capture the banner
// whether xray writes to stdout or stderr (build-config-dependent). Returns
// "" on any error.
func runXrayVersion(ctx context.Context, path string) string {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// installerServiceState runs `systemctl is-enabled --quiet xray.service` and
// classifies the result. Best-effort: returns Unknown on any unexpected error
// (systemctl missing, non-systemd host, etc.).
func installerServiceState() InstallerServiceState {
	cmd := exec.Command("systemctl", "is-enabled", "--quiet", "xray.service")
	return classifyIsEnabledExit(cmd.Run())
}

// classifyIsEnabledExit maps the exit code from `systemctl is-enabled --quiet
// xray.service` to an InstallerServiceState. Exit codes per systemctl(1):
//
//	0    → enabled / enabled-runtime
//	1..3 → disabled, masked, or static (all "not actively starting")
//	4    → unit file does not exist (common case: official installer never ran)
//	-1   → killed by signal
//	other → systemctl missing / non-systemd host
func classifyIsEnabledExit(err error) InstallerServiceState {
	if err == nil {
		return InstallerServiceEnabled
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		switch exitErr.ExitCode() {
		case 4:
			return InstallerServiceNotInstalled
		case 1, 2, 3:
			return InstallerServiceDisabled
		}
	}
	return InstallerServiceUnknown
}
