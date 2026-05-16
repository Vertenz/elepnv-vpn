package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/platform"
)

func TestPingReturnsOK(t *testing.T) {
	d := newDispatch(platform.XrayInfo{})
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.Ping"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	if got, ok := result.(pingResult); !ok || !got.OK {
		t.Fatalf("result = %v, want pingResult{OK:true}", result)
	}
}

func TestGetVersionWithXrayInstalled(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: true, Version: "Xray 1.8.4 (test)"})
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.GetVersion"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	got := result.(versionResult)
	if got.Xray == nil || *got.Xray != "Xray 1.8.4 (test)" {
		t.Fatalf("xray = %v, want \"Xray 1.8.4 (test)\"", got.Xray)
	}
}

func TestGetVersionWithoutXrayReturnsNull(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: false})
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.GetVersion"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	got := result.(versionResult)
	if got.Xray != nil {
		t.Fatalf("xray = %v, want nil", got.Xray)
	}
	// Confirm JSON serialization is `null`, not `""`.
	b, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(b, &parsed)
	if parsed["xray"] != nil {
		t.Fatalf("JSON xray = %v, want null", parsed["xray"])
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	d := newDispatch(platform.XrayInfo{})
	_, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Foo"})
	if !errors.Is(derrVal, derr.ErrMethodNotFound) {
		t.Fatalf("err = %v, want ErrMethodNotFound", derrVal)
	}
}
