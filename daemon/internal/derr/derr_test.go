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
