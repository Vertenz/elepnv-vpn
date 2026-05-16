package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/xrayconfig"
)

func TestPingReturnsOK(t *testing.T) {
	d := newDispatch(platform.XrayInfo{}, nil, nil)
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.Ping"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	if got, ok := result.(pingResult); !ok || !got.OK {
		t.Fatalf("result = %v, want pingResult{OK:true}", result)
	}
}

func TestGetVersionWithXrayInstalled(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: true, Version: "Xray 1.8.4 (test)"}, nil, nil)
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.GetVersion"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	got := result.(versionResult)
	if got.Xray == nil || *got.Xray != "Xray 1.8.4 (test)" {
		t.Fatalf("xray = %v, want pointer to %q", got.Xray, "Xray 1.8.4 (test)")
	}
}

func TestGetVersionWithoutXrayReturnsNull(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: false}, nil, nil)
	result, _ := d.handle(context.Background(), Request{Method: "Daemon.GetVersion"})
	got := result.(versionResult)
	if got.Xray != nil {
		t.Fatalf("xray = %v, want nil", got.Xray)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	d := newDispatch(platform.XrayInfo{}, nil, nil)
	_, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Foo"})
	if !errors.Is(derrVal, derr.ErrMethodNotFound) {
		t.Fatalf("err = %v, want ErrMethodNotFound", derrVal)
	}
}

func TestAsDerrOrInternalPassesThroughTypedErrors(t *testing.T) {
	in := derr.ErrConfigUnknown.With(errors.New("inner"))
	got := asDerrOrInternal(in)
	if !errors.Is(got, derr.ErrConfigUnknown) {
		t.Fatalf("typed error was rewrapped: %v", got)
	}
}

func TestAsDerrOrInternalWrapsPlainErrors(t *testing.T) {
	// Regression for the silent-success bug: a plain fmt.Errorf must become
	// ErrInternal, never nil — otherwise the dispatcher would send
	// {"result": null} for what was really an I/O failure.
	got := asDerrOrInternal(errors.New("disk full"))
	if got == nil {
		t.Fatal("plain error must NOT yield nil (would cause silent-success response)")
	}
	if !errors.Is(got, derr.ErrInternal) {
		t.Fatalf("plain error should wrap as ErrInternal, got %v", got)
	}
}

func TestAsDerrOrInternalNilStaysNil(t *testing.T) {
	if got := asDerrOrInternal(nil); got != nil {
		t.Fatalf("nil should pass through, got %v", got)
	}
}

// --- Plan 2: Configs.* tests ---

const validCfg = `{
  "inbounds":[{"tag":"socks-in","listen":"127.0.0.1","port":10808,
    "protocol":"socks","settings":{"auth":"noauth","udp":true}}]
}`

// newStoreWithFakeXray sets up an xrayconfig.Store rooted at a TempDir with a
// fake `xray` script that exits 0 (so Add always validates OK).
func newStoreWithFakeXray(t *testing.T) *xrayconfig.Store {
	t.Helper()
	binDir := t.TempDir()
	xrayPath := filepath.Join(binDir, "xray")
	if err := os.WriteFile(xrayPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return xrayconfig.NewStore(t.TempDir(), xrayPath, "127.0.0.1:10808")
}

// recorderBroadcaster captures every Event a dispatcher emits.
type recorderBroadcaster struct{ got []Event }

func (r *recorderBroadcaster) Broadcast(e Event) { r.got = append(r.got, e) }

func TestConfigsAddStoresAndBroadcasts(t *testing.T) {
	store := newStoreWithFakeXray(t)
	rec := &recorderBroadcaster{}
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec)

	params, _ := json.Marshal(map[string]any{"json": validCfg})
	res, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: params})
	if derrVal != nil {
		t.Fatalf("Configs.Add err: %v", derrVal)
	}
	got := res.(addResult)
	if len(got.ID) != 26 {
		t.Fatalf("ID = %q, want 26-char ULID", got.ID)
	}
	if len(rec.got) != 1 || rec.got[0].Method != "Configs.Changed" {
		t.Fatalf("expected one Configs.Changed broadcast, got %+v", rec.got)
	}
}

func TestConfigsAddSurfacesPathUnsafe(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{})

	bad := `{"inbounds":[{"listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{"auth":"noauth"},"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"/etc/passwd"}]}}}]}`
	params, _ := json.Marshal(map[string]any{"json": bad})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: params})
	if !errors.Is(derrVal, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", derrVal)
	}
}

func TestConfigsAddRejectsMissingParams(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{})

	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: nil})
	if !errors.Is(derrVal, derr.ErrInvalidParams) {
		t.Fatalf("err = %v, want ErrInvalidParams", derrVal)
	}
}

func TestConfigsListReturnsAllStored(t *testing.T) {
	store := newStoreWithFakeXray(t)
	rec := &recorderBroadcaster{}
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec)

	for i := 0; i < 3; i++ {
		params, _ := json.Marshal(map[string]any{"json": validCfg})
		_, _ = d.handle(context.Background(), Request{Method: "Configs.Add", Params: params})
	}
	res, derrVal := d.handle(context.Background(), Request{Method: "Configs.List"})
	if derrVal != nil {
		t.Fatalf("List err: %v", derrVal)
	}
	got := res.(listResult)
	if len(got.Configs) != 3 {
		t.Fatalf("got %d configs, want 3", len(got.Configs))
	}
}

func TestConfigsRemoveDeletesAndBroadcasts(t *testing.T) {
	store := newStoreWithFakeXray(t)
	rec := &recorderBroadcaster{}
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec)

	addParams, _ := json.Marshal(map[string]any{"json": validCfg})
	addRes, _ := d.handle(context.Background(), Request{Method: "Configs.Add", Params: addParams})
	id := addRes.(addResult).ID

	rec.got = nil // forget the Add event
	rmParams, _ := json.Marshal(map[string]any{"id": id})
	res, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if derrVal != nil {
		t.Fatalf("Remove err: %v", derrVal)
	}
	if !res.(removeResult).OK {
		t.Fatal("Remove ok = false, want true")
	}
	if len(rec.got) != 1 || rec.got[0].Method != "Configs.Changed" {
		t.Fatalf("expected one Configs.Changed broadcast, got %+v", rec.got)
	}
	params := rec.got[0].Params.(ConfigsChangedParams)
	if len(params.Removed) != 1 || params.Removed[0] != id {
		t.Fatalf("Removed = %v, want [%q]", params.Removed, id)
	}
}

func TestConfigsRemoveUnknownReturnsConfigUnknown(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{})

	rmParams, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if !errors.Is(derrVal, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", derrVal)
	}
}

func TestConfigsRemoveMalformedIDReturnsConfigUnknown(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{})

	rmParams, _ := json.Marshal(map[string]any{"id": "not-a-ulid"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if !errors.Is(derrVal, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", derrVal)
	}
}

func TestConfigsValidateReturnsOK(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{})

	addParams, _ := json.Marshal(map[string]any{"json": validCfg})
	addRes, _ := d.handle(context.Background(), Request{Method: "Configs.Add", Params: addParams})
	id := addRes.(addResult).ID

	vParams, _ := json.Marshal(map[string]any{"id": id})
	res, derrVal := d.handle(context.Background(), Request{Method: "Configs.Validate", Params: vParams})
	if derrVal != nil {
		t.Fatalf("Validate err: %v", derrVal)
	}
	if !res.(xrayconfig.ValidateResult).OK {
		t.Fatal("Validate OK = false, want true")
	}
}
