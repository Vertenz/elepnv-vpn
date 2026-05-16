package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/health"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/state"
	"elepn/daemon/internal/xrayconfig"
)

func TestPingReturnsOK(t *testing.T) {
	d := newDispatch(platform.XrayInfo{}, nil, nil, nil, nil)
	result, derrVal := d.handle(context.Background(), Request{Method: "Daemon.Ping"})
	if derrVal != nil {
		t.Fatalf("derr: %v", derrVal)
	}
	if got, ok := result.(pingResult); !ok || !got.OK {
		t.Fatalf("result = %v, want pingResult{OK:true}", result)
	}
}

func TestGetVersionWithXrayInstalled(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: true, Version: "Xray 1.8.4 (test)"}, nil, nil, nil, nil)
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
	d := newDispatch(platform.XrayInfo{Found: false}, nil, nil, nil, nil)
	result, _ := d.handle(context.Background(), Request{Method: "Daemon.GetVersion"})
	got := result.(versionResult)
	if got.Xray != nil {
		t.Fatalf("xray = %v, want nil", got.Xray)
	}
}

func TestUnknownMethodReturnsMethodNotFound(t *testing.T) {
	d := newDispatch(platform.XrayInfo{}, nil, nil, nil, nil)
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
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec, nil, nil)

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
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, nil, nil)

	bad := `{"inbounds":[{"listen":"127.0.0.1","port":10808,"protocol":"socks","settings":{"auth":"noauth"},"streamSettings":{"tlsSettings":{"certificates":[{"certificateFile":"/etc/passwd"}]}}}]}`
	params, _ := json.Marshal(map[string]any{"json": bad})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: params})
	if !errors.Is(derrVal, derr.ErrPathUnsafe) {
		t.Fatalf("err = %v, want ErrPathUnsafe", derrVal)
	}
}

func TestConfigsAddRejectsMissingParams(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, nil, nil)

	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: nil})
	if !errors.Is(derrVal, derr.ErrInvalidParams) {
		t.Fatalf("err = %v, want ErrInvalidParams", derrVal)
	}
}

func TestConfigsListReturnsAllStored(t *testing.T) {
	store := newStoreWithFakeXray(t)
	rec := &recorderBroadcaster{}
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec, nil, nil)

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
	d := newDispatch(platform.XrayInfo{Found: true}, store, rec, nil, nil)

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
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, nil, nil)

	rmParams, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if !errors.Is(derrVal, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", derrVal)
	}
}

func TestConfigsRemoveMalformedIDReturnsConfigUnknown(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, nil, nil)

	rmParams, _ := json.Marshal(map[string]any{"id": "not-a-ulid"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if !errors.Is(derrVal, derr.ErrConfigUnknown) {
		t.Fatalf("err = %v, want ErrConfigUnknown", derrVal)
	}
}

func TestConfigsValidateReturnsOK(t *testing.T) {
	store := newStoreWithFakeXray(t)
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, nil, nil)

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

// --- Plan 3: Tunnel.* dispatcher tests ---

type fakeMachine struct {
	connectCalls    []xrayconfig.ULID
	connectErr      error
	disconnectCalls int
	disconnectErr   error
	status          state.Status
	isActive        bool
	subsCh          chan state.ConnStatus
}

func (f *fakeMachine) Connect(_ context.Context, id xrayconfig.ULID) error {
	f.connectCalls = append(f.connectCalls, id)
	return f.connectErr
}
func (f *fakeMachine) Disconnect(_ context.Context) error {
	f.disconnectCalls++
	return f.disconnectErr
}
func (f *fakeMachine) GetStatus(_ context.Context) state.Status { return f.status }
func (f *fakeMachine) IsActive(_ xrayconfig.ULID) bool          { return f.isActive }
func (f *fakeMachine) Subscribe() (<-chan state.ConnStatus, func()) {
	if f.subsCh == nil {
		f.subsCh = make(chan state.ConnStatus, 4)
	}
	return f.subsCh, func() {}
}

func TestTunnelConnectAcceptedReturnsValidating(t *testing.T) {
	fm := &fakeMachine{}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	params, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	res, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Connect", Params: params})
	if derrVal != nil {
		t.Fatalf("err = %v", derrVal)
	}
	got := res.(tunnelStateResult)
	if got.State != "Validating" {
		t.Fatalf("State = %q, want Validating", got.State)
	}
	if len(fm.connectCalls) != 1 {
		t.Fatalf("expected 1 Connect call, got %d", len(fm.connectCalls))
	}
}

func TestTunnelConnectSurfacesAlreadyConnected(t *testing.T) {
	fm := &fakeMachine{connectErr: derr.ErrAlreadyConnected}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	params, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Connect", Params: params})
	if !errors.Is(derrVal, derr.ErrAlreadyConnected) {
		t.Fatalf("err = %v, want ErrAlreadyConnected", derrVal)
	}
}

func TestTunnelDisconnectAcceptedReturnsDisconnecting(t *testing.T) {
	fm := &fakeMachine{}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	res, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Disconnect", Params: nil})
	if derrVal != nil {
		t.Fatalf("err = %v", derrVal)
	}
	got := res.(tunnelStateResult)
	if got.State != "Disconnecting" {
		t.Fatalf("State = %q, want Disconnecting", got.State)
	}
}

func TestTunnelDisconnectSurfacesNotConnected(t *testing.T) {
	fm := &fakeMachine{disconnectErr: derr.ErrNotConnected}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	_, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Disconnect", Params: nil})
	if !errors.Is(derrVal, derr.ErrNotConnected) {
		t.Fatalf("err = %v, want ErrNotConnected", derrVal)
	}
}

func TestTunnelGetStatusReturnsMachineSnapshot(t *testing.T) {
	fm := &fakeMachine{
		status: state.Status{Conn: state.ConnStatus{State: state.StateConnected, ConfigID: "01HX7N9KQ8R3JCBVB6Z3K9V4FK"}},
	}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	res, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.GetStatus", Params: nil})
	if derrVal != nil {
		t.Fatalf("err = %v", derrVal)
	}
	got := res.(state.Status)
	if got.Conn.State != state.StateConnected {
		t.Fatalf("State = %q, want Connected", got.Conn.State)
	}
}

func TestConfigsRemoveRejectsActiveConfig(t *testing.T) {
	store := newStoreWithFakeXray(t)
	fm := &fakeMachine{isActive: true}
	d := newDispatch(platform.XrayInfo{Found: true}, store, &recorderBroadcaster{}, fm, nil)

	addParams, _ := json.Marshal(map[string]any{"json": validCfg})
	addRes, _ := d.handle(context.Background(), Request{Method: "Configs.Add", Params: addParams})
	id := addRes.(addResult).ID

	rmParams, _ := json.Marshal(map[string]any{"id": id})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: rmParams})
	if !errors.Is(derrVal, derr.ErrConfigInUse) {
		t.Fatalf("err = %v, want ErrConfigInUse", derrVal)
	}
}

func TestTunnelConnectReturnsXrayNotFoundWhenXrayMissing(t *testing.T) {
	fm := &fakeMachine{}
	// xrayInfo.Found is false (zero value)
	d := newDispatch(platform.XrayInfo{Found: false}, nil, &recorderBroadcaster{}, fm, nil)
	params, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Tunnel.Connect", Params: params})
	if !errors.Is(derrVal, derr.ErrXrayNotFound) {
		t.Fatalf("err = %v, want ErrXrayNotFound", derrVal)
	}
}

func TestConfigsAddReturnsXrayNotFoundWhenXrayMissing(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: false}, nil, &recorderBroadcaster{}, nil, nil)
	params, _ := json.Marshal(map[string]any{"json": `{"inbounds":[]}`})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Add", Params: params})
	if !errors.Is(derrVal, derr.ErrXrayNotFound) {
		t.Fatalf("err = %v, want ErrXrayNotFound", derrVal)
	}
}

func TestConfigsListReturnsXrayNotFoundWhenXrayMissing(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: false}, nil, &recorderBroadcaster{}, nil, nil)
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.List", Params: nil})
	if !errors.Is(derrVal, derr.ErrXrayNotFound) {
		t.Fatalf("err = %v, want ErrXrayNotFound", derrVal)
	}
}

func TestConfigsRemoveReturnsXrayNotFoundWhenXrayMissing(t *testing.T) {
	d := newDispatch(platform.XrayInfo{Found: false}, nil, &recorderBroadcaster{}, nil, nil)
	params, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})
	_, derrVal := d.handle(context.Background(), Request{Method: "Configs.Remove", Params: params})
	if !errors.Is(derrVal, derr.ErrXrayNotFound) {
		t.Fatalf("err = %v, want ErrXrayNotFound", derrVal)
	}
}

func TestTunnelConnectHonorsRateLimit(t *testing.T) {
	fm := &fakeMachine{}
	d := newDispatch(platform.XrayInfo{Found: true}, nil, &recorderBroadcaster{}, fm, nil)
	bucket := newTokenBucket(2, time.Minute)
	ctx := context.WithValue(context.Background(), ctxKeyConnRate{}, bucket)
	params, _ := json.Marshal(map[string]any{"id": "01HX7N9KQ8R3JCBVB6Z3K9V4FK"})

	for i := 0; i < 2; i++ {
		_, derrVal := d.handle(ctx, Request{Method: "Tunnel.Connect", Params: params})
		if derrVal != nil {
			t.Fatalf("attempt %d: err = %v", i+1, derrVal)
		}
	}
	_, derrVal := d.handle(ctx, Request{Method: "Tunnel.Connect", Params: params})
	if !errors.Is(derrVal, derr.ErrRateLimited) {
		t.Fatalf("attempt 3: err = %v, want ErrRateLimited", derrVal)
	}
}

// --- Plan 4 Task 7: Health.* dispatcher tests ---

type fakeHealth struct {
	enabled    bool
	setCalls   []bool
	probeCalls int
	probeErr   error
	status     health.Status
	cfg        health.Config
	subsCh     chan health.Status
}

func (f *fakeHealth) SetEnabled(_ context.Context, b bool) {
	f.enabled = b
	f.setCalls = append(f.setCalls, b)
}
func (f *fakeHealth) Probe(_ context.Context) (health.Status, error) {
	f.probeCalls++
	return f.status, f.probeErr
}
func (f *fakeHealth) GetConfig() health.Config { return f.cfg }
func (f *fakeHealth) IsEnabled() bool          { return f.enabled }
func (f *fakeHealth) Subscribe() (<-chan health.Status, func()) {
	if f.subsCh == nil {
		f.subsCh = make(chan health.Status, 4)
	}
	return f.subsCh, func() {}
}

func TestHealthSetEnabledTogglesAndOK(t *testing.T) {
	fh := &fakeHealth{}
	d := newDispatch(platform.XrayInfo{}, nil, &recorderBroadcaster{}, nil, fh)
	params, _ := json.Marshal(map[string]any{"enabled": true})
	res, derrVal := d.handle(context.Background(), Request{Method: "Health.SetEnabled", Params: params})
	if derrVal != nil {
		t.Fatalf("err = %v", derrVal)
	}
	if !res.(healthOKResult).OK {
		t.Fatal("OK = false")
	}
	if len(fh.setCalls) != 1 || !fh.setCalls[0] {
		t.Fatalf("SetEnabled calls = %v", fh.setCalls)
	}
}

func TestHealthProbeWhenDisabledReturnsHealthDisabled(t *testing.T) {
	fh := &fakeHealth{probeErr: health.ErrHealthDisabled()}
	d := newDispatch(platform.XrayInfo{}, nil, &recorderBroadcaster{}, nil, fh)
	_, derrVal := d.handle(context.Background(), Request{Method: "Health.Probe", Params: nil})
	if !errors.Is(derrVal, derr.ErrHealthDisabled) {
		t.Fatalf("err = %v, want ErrHealthDisabled", derrVal)
	}
}

func TestHealthGetConfigReturnsSnapshot(t *testing.T) {
	fh := &fakeHealth{
		enabled: true,
		cfg:     health.Config{Endpoint: "http://x/204", IntervalSeconds: 30},
	}
	d := newDispatch(platform.XrayInfo{}, nil, &recorderBroadcaster{}, nil, fh)
	res, derrVal := d.handle(context.Background(), Request{Method: "Health.GetConfig", Params: nil})
	if derrVal != nil {
		t.Fatalf("err = %v", derrVal)
	}
	got := res.(healthConfigResult)
	if !got.Enabled || got.IntervalSeconds != 30 {
		t.Fatalf("got = %+v", got)
	}
}
