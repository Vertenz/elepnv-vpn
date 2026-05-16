package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type procInfo struct {
	pid       int
	pgid      int
	uid       int
	exe       string
	cmdline   string
	starttime uint64
}

// recoveryScan reaps any xray processes attributable to a prior daemon run.
// Spec §11.1: triple-signal ownership (uid=xrayd + exe basename "xray" +
// cmdline contains configsDir/) protects multi-user hosts from killing
// unrelated xray instances.
func recoveryScan(ctx context.Context, log *slog.Logger, configsDir string) error {
	procs, err := listOurXray(configsDir)
	if err != nil {
		return fmt.Errorf("scan /proc: %w", err)
	}
	for _, p := range procs {
		log.Warn("reaping orphan xray from prior daemon run",
			"pid", p.pid, "pgid", p.pgid, "uid", p.uid, "cmdline", p.cmdline)
		target := p.pid
		if p.pgid == p.pid {
			target = -p.pid
		}
		if err := killGraceful(ctx, target, 5*time.Second); err != nil {
			log.Error("killGraceful failed during recovery",
				"pid", p.pid, "target", target, "err", err)
		}
	}
	return nil
}

func listOurXray(configsDir string) ([]procInfo, error) {
	xrayd, err := user.Lookup("xrayd")
	if err != nil {
		// User not set up — no daemon-spawned children possible.
		return nil, nil
	}
	wantUid, _ := strconv.Atoi(xrayd.Uid)

	// Match the trailing slash so /var/lib/xrayd/configsxyz doesn't false-match.
	needle := configsDir
	if !strings.HasSuffix(needle, "/") {
		needle += "/"
	}

	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	var out []procInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p := readProcInfo(pid)
		if p == nil {
			continue
		}
		if p.uid != wantUid {
			continue
		}
		if filepath.Base(p.exe) != "xray" {
			continue
		}
		if !strings.Contains(p.cmdline, needle) {
			continue
		}
		out = append(out, *p)
	}
	return out, nil
}

func readProcInfo(pid int) *procInfo {
	statusBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return nil
	}
	var uid int
	for _, line := range strings.Split(string(statusBytes), "\n") {
		if strings.HasPrefix(line, "Uid:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				uid, _ = strconv.Atoi(fields[1])
			}
			break
		}
	}

	// /proc/<pid>/stat: the second field is `(comm)` and may contain spaces or
	// parens — split from the LAST ')' so we don't get confused by them.
	statBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil
	}
	stat := string(statBytes)
	rp := strings.LastIndex(stat, ")")
	if rp < 0 {
		return nil
	}
	rest := strings.Fields(stat[rp+1:])
	if len(rest) < 20 {
		return nil
	}
	// rest[0] is field 3 (state); pgrp is field 5 → rest[2]; starttime is
	// field 22 → rest[19].
	pgid, _ := strconv.Atoi(rest[2])
	starttime, _ := strconv.ParseUint(rest[19], 10, 64)

	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return nil
	}
	cmdlineBytes, err := os.ReadFile(fmt.Sprintf("/proc/%d/cmdline", pid))
	if err != nil {
		return nil
	}
	cmdline := strings.ReplaceAll(string(cmdlineBytes), "\x00", " ")

	return &procInfo{
		pid:       pid,
		pgid:      pgid,
		uid:       uid,
		exe:       exe,
		cmdline:   strings.TrimSpace(cmdline),
		starttime: starttime,
	}
}

// procsByPgid is the SIGKILL backstop scanner (spec §11.3 P2-3).
func procsByPgid(pgid int) []procInfo {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []procInfo
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		p := readProcInfo(pid)
		if p == nil {
			continue
		}
		if p.pgid == pgid {
			out = append(out, *p)
		}
	}
	return out
}

// killGraceful sends SIGTERM to target (negative = pgid), polls for exit up
// to grace, then escalates to SIGKILL plus a per-pid pgid sweep. Spec §11.3.
func killGraceful(ctx context.Context, target int, grace time.Duration) error {
	probePid := target
	if probePid < 0 {
		probePid = -probePid
	}
	if err := syscall.Kill(target, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if errors.Is(syscall.Kill(probePid, 0), syscall.ESRCH) {
			return nil
		}
		select {
		case <-time.After(100 * time.Millisecond):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if err := syscall.Kill(target, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	if target < 0 {
		pgid := -target
		for _, stray := range procsByPgid(pgid) {
			_ = syscall.Kill(stray.pid, syscall.SIGKILL)
		}
	}
	return nil
}
