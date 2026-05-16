package supervisor

import "os"

// MinimalChildEnv returns the env list passed to xray-spawned children. We
// strip the daemon's env to a minimal set so an operator's HTTP_PROXY drop-in
// doesn't accidentally route xray's traffic through a proxy (bypassing the
// tunnel).
func MinimalChildEnv() []string {
	env := []string{
		"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
		"HOME=/nonexistent",
	}
	if tz := os.Getenv("TZ"); tz != "" {
		env = append(env, "TZ="+tz)
	}
	return env
}
