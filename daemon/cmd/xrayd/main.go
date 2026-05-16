// Command xrayd is the elepn privileged helper daemon.
// See docs/superpowers/specs/2026-05-15-xrayd-daemon-backbone-design.md
// for the full design.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/coreos/go-systemd/v22/daemon"

	"elepn/daemon/internal/ipc"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/version"
	"elepn/daemon/internal/xrayconfig"
)

// exitOK / exitTransient / exitUnrecoverable are the three exit codes the
// systemd unit (§15) discriminates via RestartPreventExitStatus=2.
const (
	exitOK            = 0
	exitTransient     = 1
	exitUnrecoverable = 2
)

func main() {
	os.Exit(run())
}

func run() int {
	// Plain JSON to stderr. journald captures all stderr lines at priority 6
	// (INFO) uniformly — `journalctl -p err -u xrayd` will not filter by
	// slog level until Plan 4 lands the §13.2 `<priority>` prefix handler.
	// Until then, level information is in the JSON `"level"` field, which
	// `journalctl -o json --output-fields=level,msg` surfaces.
	level := parseLogLevel(os.Getenv("XRAYD_LOG_LEVEL"))
	log := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level}))
	log.Info("xrayd starting", "version", version.Version)

	sockPath := os.Getenv("XRAYD_SOCK")
	if sockPath == "" {
		sockPath = "/run/xrayd/control.sock"
	}

	cfgDir := os.Getenv("XRAYD_CONFIGS_DIR")
	if cfgDir == "" {
		cfgDir = "/var/lib/xrayd/configs"
	}
	expectedSocksAddr := os.Getenv("XRAYD_EXPECTED_SOCKS_ADDR")
	if expectedSocksAddr == "" {
		expectedSocksAddr = "127.0.0.1:10808"
	}
	// Fail fast on misconfigured env so the operator sees the problem at
	// startup; otherwise every Configs.Add would silently surface a
	// less-actionable internal_error response.
	if err := xrayconfig.ValidateExpectedSocksAddr(expectedSocksAddr); err != nil {
		log.Error("XRAYD_EXPECTED_SOCKS_ADDR invalid",
			"err", err,
			"hint", `use host:port form, e.g. "127.0.0.1:10808" or "[::1]:10808"`)
		return exitUnrecoverable
	}

	// appCtx is signal-driven. It tells us when to START shutting down.
	// It is NOT the actor's context; in Plan 3, the Machine will own its own
	// ctx that is cancelled inside Machine.Shutdown AFTER cleanup completes.
	appCtx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	xrayInfo := platform.Discover(appCtx, log)
	if xrayInfo.Found {
		log.Info("xray discovered", "path", xrayInfo.Path, "version", xrayInfo.Version)
	}

	// Spec §8.6:1697 — daemon must still start if the xrayd group is missing,
	// but it MUST log an actionable ERROR so the operator can fix postinst
	// rather than chase per-connection 'unauthorized' errors.
	if err := ipc.CheckGroupExists(ipc.AuthGroup); err != nil {
		log.Error("xrayd group is missing — every IPC connection will be rejected as unauthorized",
			"err", err,
			"group", ipc.AuthGroup,
			"fix", "sudo groupadd --system xrayd && sudo usermod -aG xrayd $USER")
	}

	var store *xrayconfig.Store
	if xrayInfo.Found {
		store = xrayconfig.NewStore(cfgDir, xrayInfo.Path, expectedSocksAddr)
		log.Info("config registry ready",
			"dir", cfgDir,
			"expectedSocksAddr", expectedSocksAddr)
	} else {
		log.Warn("xray not found; Configs.Add will return internal_error until xray is installed")
	}

	srv := ipc.NewServer(sockPath, xrayInfo, store, log)
	if err := srv.Listen(appCtx); err != nil {
		log.Error("ipc listen failed", "err", err, "sock", sockPath)
		return exitUnrecoverable
	}

	// Tell systemd we are ready to serve. Best-effort — running outside
	// systemd is fine, sd_notify returns (false, nil) and we move on.
	if sent, err := daemon.SdNotify(false, daemon.SdNotifyReady); err != nil {
		log.Warn("sd_notify failed", "err", err)
	} else if !sent {
		log.Debug("sd_notify skipped (not running under systemd)")
	}
	log.Info("xrayd ready", "sock", sockPath)

	<-appCtx.Done()
	log.Info("shutdown signal received")

	if sent, err := daemon.SdNotify(false, daemon.SdNotifyStopping); err != nil {
		log.Warn("sd_notify STOPPING failed", "err", err)
	} else if sent {
		log.Debug("sd_notify STOPPING=1 sent")
	}

	// Plan 1 has no Machine yet. In Plan 3, the next two lines become
	//   shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	//   _ = machine.Shutdown(shutCtx)
	srv.StopAccept()
	if err := srv.Close(); err != nil {
		log.Warn("ipc server close error", "err", err)
	}

	log.Info("xrayd exited cleanly")
	return exitOK
}

func parseLogLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
