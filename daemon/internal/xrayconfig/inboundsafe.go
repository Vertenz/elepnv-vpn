package xrayconfig

import (
	"encoding/json"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"

	"elepn/daemon/internal/derr"
)

type inboundSpec struct {
	Tag      string         `json:"tag"`
	Listen   string         `json:"listen"`
	Port     any            `json:"port"`
	Protocol string         `json:"protocol"`
	Settings map[string]any `json:"settings"`
}

type configRoot struct {
	Inbounds []inboundSpec `json:"inbounds"`
}

// ValidateExpectedSocksAddr parses the daemon-configured expectedSocksAddr
// (env XRAYD_EXPECTED_SOCKS_ADDR, default "127.0.0.1:10808") and returns
// an error if it can't be split into host:port. main.go calls this at
// startup so an operator misconfiguration is visible immediately rather
// than turning every Configs.Add into a silently-bypassed inbound check.
func ValidateExpectedSocksAddr(addr string) error {
	if _, _, err := net.SplitHostPort(addr); err != nil {
		return fmt.Errorf("XRAYD_EXPECTED_SOCKS_ADDR %q: %w", addr, err)
	}
	return nil
}

// checkInboundSafety enforces §6.7 (rev 4):
//
//  1. Exactly ONE inbound.
//  2. That inbound is SOCKS5 on a loopback address matching expectedSocksAddr.
//  3. No SOCKS auth (or explicit "noauth").
//
// Rev 3's "extra localhost inbounds are fine, they're not externally reachable"
// is wrong on a multi-user host: any local user can connect to a localhost
// port. Renderer always produces exactly one SOCKS5 inbound, so this rule
// has no product cost.
func checkInboundSafety(jsonBytes []byte, expectedSocksAddr string) error {
	var root configRoot
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		return derr.ErrConfigMalformedJSON.With(err)
	}
	if len(root.Inbounds) != 1 {
		return derr.NewInboundUnsafe("/inbounds",
			fmt.Sprintf("expected exactly 1 inbound, got %d", len(root.Inbounds)))
	}
	expectedHost, expectedPort, err := net.SplitHostPort(expectedSocksAddr)
	if err != nil {
		// Defense in depth: this is a daemon-configuration error (bad env
		// var). main.go should have caught it at startup via
		// ValidateExpectedSocksAddr — if we reach here a misconfigured
		// daemon would silently bypass inbound safety. Surface as a typed
		// internal error so the dispatcher cannot swallow it.
		return derr.ErrInternal.WithMessage(
			fmt.Sprintf("invalid expectedSocksAddr %q: %v", expectedSocksAddr, err))
	}
	ib := root.Inbounds[0]
	ptr := "/inbounds/0"

	switch strings.ToLower(strings.TrimSpace(ib.Listen)) {
	case "":
		return derr.NewInboundUnsafe(ptr+"/listen",
			`listen field is absent (xray defaults to 0.0.0.0); set listen to "127.0.0.1"`)
	case "0.0.0.0", "::", "*":
		return derr.NewInboundUnsafe(ptr+"/listen",
			fmt.Sprintf("public bind not allowed: %q; set listen to \"127.0.0.1\"", ib.Listen))
	}
	if !strings.EqualFold(ib.Protocol, "socks") {
		return derr.NewInboundUnsafe(ptr+"/protocol",
			fmt.Sprintf(`expected protocol "socks", got %q`, ib.Protocol))
	}
	if !isLoopback(ib.Listen, expectedHost) {
		return derr.NewInboundUnsafe(ptr+"/listen",
			fmt.Sprintf("expected loopback %q, got %q", expectedHost, ib.Listen))
	}
	if !portEquals(ib.Port, expectedPort) {
		return derr.NewInboundUnsafe(ptr+"/port",
			fmt.Sprintf("expected port %q, got %v", expectedPort, ib.Port))
	}
	if auth, _ := ib.Settings["auth"].(string); auth != "" && auth != "noauth" {
		return derr.NewInboundUnsafe(ptr+"/settings/auth",
			fmt.Sprintf("unsupported socks auth: %q (v1 expects noauth)", auth))
	}
	return nil
}

func isLoopback(listen, expectedHost string) bool {
	l := strings.ToLower(strings.TrimSpace(listen))
	switch l {
	case "127.0.0.1", "::1", "localhost":
		eh := strings.ToLower(expectedHost)
		return l == eh ||
			(eh == "127.0.0.1" && l == "localhost") ||
			(eh == "localhost" && l == "127.0.0.1")
	}
	return false
}

// portEquals matches xray's port field against the expected port. xray accepts
// `int`, `"int"`, `"int-int"` (range), `"int,int,..."` (list). We accept only
// scalar int or `"int"` — ranges and lists are explicitly rejected per §6.7.
func portEquals(p any, expected string) bool {
	expectedN, err := strconv.Atoi(expected)
	if err != nil || expectedN < 1 || expectedN > 65535 {
		return false
	}
	switch v := p.(type) {
	case float64:
		if v != math.Trunc(v) || v < 1 || v > 65535 {
			return false
		}
		return int(v) == expectedN
	case string:
		if strings.ContainsAny(v, "-,") {
			return false
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return false
		}
		return n == expectedN
	}
	return false
}
