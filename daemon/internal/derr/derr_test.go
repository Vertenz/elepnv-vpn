package derr_test

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"elepn/daemon/internal/derr"
)

func TestErrorImplementsError(t *testing.T) {
	var e error = derr.ErrUnauthorized
	if !strings.Contains(e.Error(), "unauthorized") {
		t.Fatalf("Error() did not include symbol: %q", e.Error())
	}
}

func TestErrorIsBySymbol(t *testing.T) {
	wrapped := derr.ErrUnauthorized.With(errors.New("ucred failed"))
	if !errors.Is(wrapped, derr.ErrUnauthorized) {
		t.Fatal("errors.Is failed to match by symbol")
	}
	if errors.Is(wrapped, derr.ErrInternal) {
		t.Fatal("errors.Is matched the wrong symbol")
	}
}

func TestJSONShape(t *testing.T) {
	raw := derr.ErrMethodNotFound.WithMessage("Tunnel.Foo").JSON()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("JSON() produced non-JSON: %v (%s)", err, string(raw))
	}
	if got["code"].(float64) != -32601 {
		t.Fatalf("code = %v, want -32601", got["code"])
	}
	msg := got["message"].(string)
	if !strings.HasPrefix(msg, "method_not_found: Tunnel.Foo") {
		t.Fatalf("message = %q, want prefix %q", msg, "method_not_found: Tunnel.Foo")
	}
	data := got["data"].(map[string]any)
	if data["symbol"] != "method_not_found" {
		t.Fatalf("data.symbol = %v, want method_not_found", data["symbol"])
	}
}

func TestAsDerr(t *testing.T) {
	wrapped := derr.ErrUnauthorized.With(errors.New("boom"))
	got := derr.AsDerr(wrapped)
	if got == nil || got.Symbol != "unauthorized" {
		t.Fatalf("AsDerr returned %v, want unauthorized", got)
	}
	if derr.AsDerr(errors.New("not a derr")) != nil {
		t.Fatal("AsDerr returned non-nil for non-derr error")
	}
}

func TestPlan2SentinelsHaveStableSymbols(t *testing.T) {
	cases := []struct {
		name string
		err  *derr.Error
		code int
		sym  string
	}{
		{"config_unknown", derr.ErrConfigUnknown, -32002, "config_unknown"},
		{"config_malformed_json", derr.ErrConfigMalformedJSON, -32003, "config_malformed_json"},
		{"config_invalid", derr.ErrConfigInvalid, -32004, "config_invalid"},
		{"config_in_use", derr.ErrConfigInUse, -32005, "config_in_use"},
		{"path_unsafe", derr.ErrPathUnsafe, -32015, "path_unsafe"},
		{"inbound_unsafe", derr.ErrInboundUnsafe, -32020, "inbound_unsafe"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.err.Code != c.code {
				t.Fatalf("%s.Code = %d, want %d", c.name, c.err.Code, c.code)
			}
			if c.err.Symbol != c.sym {
				t.Fatalf("%s.Symbol = %q, want %q", c.name, c.err.Symbol, c.sym)
			}
		})
	}
}

func TestNewPathUnsafeEmbedsDetail(t *testing.T) {
	e := derr.NewPathUnsafe("/inbounds/0/streamSettings/tlsSettings/certificates/0/certificateFile", "/etc/passwd")
	if !errors.Is(e, derr.ErrPathUnsafe) {
		t.Fatal("NewPathUnsafe must be Is-comparable to ErrPathUnsafe")
	}
	raw := e.JSON()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("JSON unmarshal: %v", err)
	}
	data := got["data"].(map[string]any)
	if data["symbol"] != "path_unsafe" {
		t.Fatalf("symbol = %v, want path_unsafe", data["symbol"])
	}
	detail := data["detail"].(map[string]any)
	if detail["pointer"] != "/inbounds/0/streamSettings/tlsSettings/certificates/0/certificateFile" {
		t.Fatalf("detail.pointer = %v", detail["pointer"])
	}
	if detail["value"] != "/etc/passwd" {
		t.Fatalf("detail.value = %v", detail["value"])
	}
}

func TestNewInboundUnsafeEmbedsDetail(t *testing.T) {
	e := derr.NewInboundUnsafe("/inbounds/0/listen", `public bind not allowed: "0.0.0.0"`)
	if !errors.Is(e, derr.ErrInboundUnsafe) {
		t.Fatal("NewInboundUnsafe must be Is-comparable to ErrInboundUnsafe")
	}
	raw := e.JSON()
	var got map[string]any
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	detail := got["data"].(map[string]any)["detail"].(map[string]any)
	if detail["pointer"] != "/inbounds/0/listen" {
		t.Fatalf("detail.pointer = %v", detail["pointer"])
	}
	if detail["reason"] != `public bind not allowed: "0.0.0.0"` {
		t.Fatalf("detail.reason = %v", detail["reason"])
	}
}
