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
// Audited against xray-core source as of v25.x. Future xray bumps must
// re-audit and update this list — see docs/xray-core-linux-sources.md.
var pathBearingKeys = map[string]struct{}{
	"certificateFile":   {},
	"keyFile":           {},
	"caCertificateFile": {},
	"access":            {}, // log.access
	"error":             {}, // log.error
	"path":              {}, // various sub-objects
	"dat":               {}, // geo-loaders
	"file":              {},
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
		// Defense-in-depth: path-shaped string under an unknown key.
		if looksLikePath(v) && !isAllowedDSL(v) && !isAllowedAbsolutePath(v) {
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
	// Reject any `..` component pre-clean so we never even try to resolve
	// traversal attempts.
	cleaned := filepath.Clean(s)
	if cleaned == ".." ||
		strings.HasPrefix(cleaned, "../") ||
		strings.Contains(cleaned, "/../") ||
		strings.HasSuffix(cleaned, "/..") {
		return derr.NewPathUnsafe(ptr, s)
	}
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

func looksLikePath(s string) bool {
	if strings.Contains(s, "/") {
		return true
	}
	if strings.HasPrefix(s, "~") {
		return true
	}
	// Windows path heuristic (e.g. "C:\foo") — defense in depth on Linux too.
	if len(s) > 2 && s[1] == ':' && (s[2] == '\\' || s[2] == '/') {
		return true
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
