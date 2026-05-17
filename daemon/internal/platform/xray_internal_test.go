package platform

import (
	"os/exec"
	"testing"
)

func TestClassifyIsEnabledExit(t *testing.T) {
	cases := []struct {
		name string
		exit int
		want InstallerServiceState
	}{
		{"enabled", 0, InstallerServiceEnabled},
		{"disabled", 1, InstallerServiceDisabled},
		{"masked", 2, InstallerServiceDisabled},
		{"static", 3, InstallerServiceDisabled},
		{"not-installed", 4, InstallerServiceNotInstalled},
		{"weird-exit-99", 99, InstallerServiceUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := exec.Command("sh", "-c", "exit "+itoa(tc.exit)).Run()
			if tc.exit == 0 && err != nil {
				t.Fatalf("exit-0 unexpectedly errored: %v", err)
			}
			if got := classifyIsEnabledExit(err); got != tc.want {
				t.Fatalf("classifyIsEnabledExit(exit %d) = %v, want %v", tc.exit, got, tc.want)
			}
		})
	}

	// Missing binary → Unknown (no *exec.ExitError, just a PathError-ish).
	err := exec.Command("/no/such/binary/exists/xyzzy7df3").Run()
	if got := classifyIsEnabledExit(err); got != InstallerServiceUnknown {
		t.Fatalf("missing binary → %v, want Unknown", got)
	}
}

// itoa avoids pulling strconv just for a 2-digit exit code in tests.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
