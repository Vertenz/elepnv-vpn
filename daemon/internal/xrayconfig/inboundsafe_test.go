package xrayconfig

import (
	"errors"
	"strings"
	"testing"

	"elepn/daemon/internal/derr"
)

const expectedSocks = "127.0.0.1:10808"

func TestCheckInboundSafetyAcceptsCanonical(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"tag":"socks-in","listen":"127.0.0.1","port":10808,"protocol":"socks",
		"settings":{"auth":"noauth","udp":true}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); err != nil {
		t.Fatalf("expected nil for canonical SOCKS5 inbound, got %v", err)
	}
}

func TestCheckInboundSafetyAcceptsLocalhostHostname(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"localhost","port":10808,"protocol":"socks","settings":{"auth":"noauth"}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); err != nil {
		t.Fatalf("expected nil for listen=localhost, got %v", err)
	}
}

func TestCheckInboundSafetyAcceptsPortAsString(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"127.0.0.1","port":"10808","protocol":"socks","settings":{"auth":"noauth"}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); err != nil {
		t.Fatalf("expected nil for string port, got %v", err)
	}
}

func TestCheckInboundSafetyRejectsZeroInbounds(t *testing.T) {
	cfg := []byte(`{"inbounds":[]}`)
	if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestCheckInboundSafetyRejectsTwoInbounds(t *testing.T) {
	cfg := []byte(`{"inbounds":[
		{"listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{"auth":"noauth"}},
		{"listen":"127.0.0.1","port":10809,"protocol":"http"}
	]}`)
	if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestCheckInboundSafetyRejectsPublicBind(t *testing.T) {
	for _, listen := range []string{"0.0.0.0", "::", "*", ""} {
		t.Run(listen, func(t *testing.T) {
			cfg := []byte(`{"inbounds":[{
				"listen":"` + listen + `","port":10808,"protocol":"socks","settings":{"auth":"noauth"}
			}]}`)
			err := checkInboundSafety(cfg, expectedSocks)
			if !errors.Is(err, derr.ErrInboundUnsafe) {
				t.Fatalf("listen=%q: err = %v, want ErrInboundUnsafe", listen, err)
			}
			if !strings.Contains(err.Error(), "public bind") {
				t.Fatalf("listen=%q: detail should mention public bind: %v", listen, err)
			}
		})
	}
}

func TestCheckInboundSafetyRejectsNonSocksProtocol(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"127.0.0.1","port":10808,"protocol":"http","settings":{}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestCheckInboundSafetyRejectsWrongPort(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"127.0.0.1","port":10809,"protocol":"socks","settings":{"auth":"noauth"}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestCheckInboundSafetyRejectsPortRangeOrList(t *testing.T) {
	for _, port := range []string{"10808-10810", "10808,10809"} {
		t.Run(port, func(t *testing.T) {
			cfg := []byte(`{"inbounds":[{
				"listen":"127.0.0.1","port":"` + port + `","protocol":"socks","settings":{"auth":"noauth"}
			}]}`)
			if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
				t.Fatalf("port=%q: err = %v, want ErrInboundUnsafe", port, err)
			}
		})
	}
}

func TestCheckInboundSafetyRejectsAuthRequiringCreds(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"127.0.0.1","port":10808,"protocol":"socks",
		"settings":{"auth":"password","accounts":[{"user":"u","pass":"p"}]}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); !errors.Is(err, derr.ErrInboundUnsafe) {
		t.Fatalf("err = %v, want ErrInboundUnsafe", err)
	}
}

func TestCheckInboundSafetyAcceptsMissingAuthField(t *testing.T) {
	cfg := []byte(`{"inbounds":[{
		"listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{}
	}]}`)
	if err := checkInboundSafety(cfg, expectedSocks); err != nil {
		t.Fatalf("missing auth field should default to noauth-allowed, got %v", err)
	}
}

func TestCheckInboundSafetyRejectsMalformedJSON(t *testing.T) {
	if err := checkInboundSafety([]byte(`{not json`), expectedSocks); !errors.Is(err, derr.ErrConfigMalformedJSON) {
		t.Fatalf("err = %v, want ErrConfigMalformedJSON", err)
	}
}
