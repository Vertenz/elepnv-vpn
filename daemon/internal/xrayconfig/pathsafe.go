package xrayconfig

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"elepn/daemon/internal/derr"
)

// pathBearingKeys is the curated set of xray-core JSON keys whose string
// values are interpreted by xray as filesystem paths. Schema-aware: ANY
// string under one of these keys is treated as a path and must satisfy
// validatePathValue, even if it doesn't "look" like a path (rev 3 fix for
// the bypassable rev-2 heuristic).
//
// This list intentionally covers only the common cases. Keys NOT in this set
// (e.g. socketPath, geoFile, domainList, dnsConfigFile) still get caught by
// the defense-in-depth looksSuspicious scan inside walk() — but only when the
// value starts with a known sensitive filesystem prefix. A future xray-core
// bump that introduces a path-bearing key whose values are bare basenames could
// slip through; the v1 trade-off is acceptable because the daemon runs with
// systemd ProtectHome/ProtectSystem so even a slipped path can't escape.
// See docs/xray-core-linux-sources.md for the xray-core source-of-truth.
var pathBearingKeys = map[string]struct{}{
	"certificateFile":   {},
	"keyFile":           {},
	"caCertificateFile": {},
	"access":            {}, // log.access
	"error":             {}, // log.error
	// path is intentionally NOT here — xray uses "path" for URL paths in
	// transport configs (wsSettings.path, grpcSettings.path, xhttpSettings.path).
	// Treating it as filesystem-bearing by key name false-rejects the most
	// common VLESS/VMess-over-WS configs before xray -test even runs.
	"dat":  {}, // geo-loaders
	"file": {},
}

// allowedDSLPrefixes are xray's own DSL selectors — they look path-like
// (contain ':') but are not filesystem paths. Allowed in non-path-bearing
// keys; explicitly REJECTED inside path-bearing keys (a `certificateFile`
// holding `geosite:cn` makes no sense and may be a bypass attempt).
//
// `ext:` is conspicuously absent — it's xray's literal "load external file"
// selector and is banned in v1.
var allowedDSLPrefixes = []string{
	"geoip:", "geosite:", "regexp:", "domain:", "full:", "keyword:",
}

// allowedAbsoluteRoots is the package-level allowlist of directory prefixes
// that path-bearing keys may reference. Plan 2 v1 ships exactly one — the
// XTLS installer's geodata dir. Mutable so tests can substitute a TempDir.
var allowedAbsoluteRoots = []string{
	"/usr/local/share/xray/",
}

// checkPathSafety walks the parsed JSON tree and rejects path-bearing keys
// that reference files outside allowedAbsoluteRoots, plus a defense-in-depth
// scan that catches path-shaped strings under unknown keys.
func checkPathSafety(jsonBytes []byte) error {
	var root any
	if err := json.Unmarshal(jsonBytes, &root); err != nil {
		return derr.ErrConfigMalformedJSON.With(err)
	}
	return walk(root, "")
}

func walk(node any, ptr string) error {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			childPtr := ptr + "/" + k
			if _, isPathKey := pathBearingKeys[k]; isPathKey {
				if s, ok := child.(string); ok {
					if err := validatePathValue(childPtr, s); err != nil {
						return err
					}
					continue
				}
			}
			if err := walk(child, childPtr); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if err := walk(child, fmt.Sprintf("%s/%d", ptr, i)); err != nil {
				return err
			}
		}
	case string:
		// Defense-in-depth: flag strings that look like attempts to reach
		// sensitive filesystem paths at JSON positions xray doesn't expect.
		if looksSuspicious(v) && !isAllowedDSL(v) && !isAllowedAbsolutePath(v) {
			return derr.NewPathUnsafe(ptr, v)
		}
	}
	return nil
}

func validatePathValue(ptr, s string) error {
	if s == "" {
		return derr.NewPathUnsafe(ptr, s)
	}
	// DSL inside a path-bearing key is a category error — these keys mean
	// filesystem, not xray DSL.
	if isAllowedDSL(s) {
		return derr.NewPathUnsafe(ptr, s)
	}
	if strings.HasPrefix(s, "ext:") {
		return derr.NewPathUnsafe(ptr, s)
	}
	// Must be absolute — relative paths resolve against xray's CWD
	// (/var/lib/xrayd) which we do not want as a free-for-all.
	if !strings.HasPrefix(s, "/") {
		return derr.NewPathUnsafe(ptr, s)
	}
	// Reject any `..` component in the RAW input — filepath.Clean would fold
	// them silently, so we have to inspect the unprocessed string first.
	// This is defense in depth on top of the EvalSymlinks + allowlist check.
	if s == ".." ||
		strings.HasPrefix(s, "../") ||
		strings.Contains(s, "/../") ||
		strings.HasSuffix(s, "/..") {
		return derr.NewPathUnsafe(ptr, s)
	}
	cleaned := filepath.Clean(s)
	// EvalSymlinks requires the file to exist. v1 contract: admin pre-stages
	// referenced files under an allowed root.
	resolved, err := filepath.EvalSymlinks(cleaned)
	if err != nil {
		return derr.NewPathUnsafe(ptr, s)
	}
	if !isAllowedAbsolutePath(resolved) {
		return derr.NewPathUnsafe(ptr, s)
	}
	return nil
}

// suspiciousAbsolutePrefixes are filesystem paths that xray should never need
// to reference at any non-path-bearing position. Catches obvious bypass
// attempts (e.g. an attacker putting "/etc/passwd" in a field that xray
// happens to read as a file — even if the field isn't in our pathBearingKeys).
// Narrower than the previous "any string containing /" scan, which
// false-rejected legitimate URL paths in transport configs.
var suspiciousAbsolutePrefixes = []string{
	"/etc/",
	"/root/",
	"/proc/",
	"/sys/",
	"/dev/",
	"/boot/",
	"/var/lib/xrayd/", // xrayd's own storage — config shouldn't reference it
}

func looksSuspicious(s string) bool {
	if strings.HasPrefix(s, "~") {
		return true
	}
	// Windows path heuristic (e.g. "C:\foo") — defense in depth on Linux too.
	if len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
	}
	for _, p := range suspiciousAbsolutePrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isAllowedDSL(s string) bool {
	for _, p := range allowedDSLPrefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}

func isAllowedAbsolutePath(s string) bool {
	for _, root := range allowedAbsoluteRoots {
		if strings.HasPrefix(s, root) {
			return true
		}
	}
	return false
}
