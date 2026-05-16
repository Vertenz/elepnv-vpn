package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/health"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/state"
	"elepn/daemon/internal/version"
	"elepn/daemon/internal/xrayconfig"
)

// MethodHandler is the per-method signature. It receives the request's params
// as raw JSON and must return either a JSON-marshalable result or a derr.
type MethodHandler func(ctx context.Context, params json.RawMessage) (result any, err *derr.Error)

// dispatch is the IPC method routing table. It owns nothing — collaborators
// (XrayInfo, Store, Broadcaster, TunnelMachine, HealthMachine) are injected at
// construction so tests can substitute fakes.
type dispatch struct {
	methods  map[string]MethodHandler
	xrayInfo platform.XrayInfo
	configs  *xrayconfig.Store // nil-permitted in Plan-1-only tests
	bcast    Broadcaster       // nil-permitted in Plan-1-only tests
	machine  TunnelMachine     // nil-permitted in Plan-1/2-only tests
	health   HealthMachine     // nil-permitted in tests that don't exercise Health.*
	log      *slog.Logger
}

func newDispatch(xrayInfo platform.XrayInfo, store *xrayconfig.Store, bcast Broadcaster, machine TunnelMachine, hm HealthMachine, log ...*slog.Logger) *dispatch {
	d := &dispatch{
		methods:  make(map[string]MethodHandler),
		xrayInfo: xrayInfo,
		configs:  store,
		bcast:    bcast,
		machine:  machine,
		health:   hm,
	}
	if len(log) > 0 && log[0] != nil {
		d.log = log[0]
	} else {
		d.log = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	d.methods["Daemon.Ping"] = d.handlePing
	d.methods["Daemon.GetVersion"] = d.handleGetVersion
	d.methods["Configs.Add"] = d.handleConfigsAdd
	d.methods["Configs.List"] = d.handleConfigsList
	d.methods["Configs.Remove"] = d.handleConfigsRemove
	d.methods["Configs.Validate"] = d.handleConfigsValidate
	d.methods["Configs.Get"] = d.handleConfigsGet
	d.methods["Tunnel.Connect"] = d.handleTunnelConnect
	d.methods["Tunnel.Disconnect"] = d.handleTunnelDisconnect
	d.methods["Tunnel.Switch"] = d.handleTunnelSwitch
	d.methods["Tunnel.GetStatus"] = d.handleTunnelGetStatus
	d.methods["Health.SetEnabled"] = d.handleHealthSetEnabled
	d.methods["Health.Probe"] = d.handleHealthProbe
	d.methods["Health.GetConfig"] = d.handleHealthGetConfig
	return d
}

func (d *dispatch) handle(ctx context.Context, req Request) (any, *derr.Error) {
	h, ok := d.methods[req.Method]
	if !ok {
		return nil, derr.ErrMethodNotFound.WithMessage(req.Method)
	}
	result, derrErr := h(ctx, req.Params)
	if derrErr != nil && derrErr.Code == derr.ErrInternal.Code && derrErr.Cause != nil {
		// Log internal-error causes so operators can triage; the cause is not
		// serialized to the wire per spec §9.3.
		d.log.Error("internal error from handler", "method", req.Method, "cause", derrErr.Cause)
	}
	return result, derrErr
}

// asDerrOrInternal turns a non-nil error into a *derr.Error. If err already
// wraps one (via With / WithMessage / WithDetail), that typed error is
// returned unchanged. Otherwise the plain error is wrapped in ErrInternal
// so the JSON-RPC response carries a real error code instead of being
// silently promoted to {result: null} — which is what bare derr.AsDerr(err)
// does when err is a plain fmt.Errorf (e.g. an I/O failure from os.Remove).
// Returns nil iff err == nil.
func asDerrOrInternal(err error) *derr.Error {
	if err == nil {
		return nil
	}
	if de := derr.AsDerr(err); de != nil {
		return de
	}
	return derr.ErrInternal.With(err)
}

// --- Daemon.Ping / Daemon.GetVersion (unchanged from Plan 1) ---

type pingResult struct {
	OK bool `json:"ok"`
}

func (d *dispatch) handlePing(_ context.Context, _ json.RawMessage) (any, *derr.Error) {
	return pingResult{OK: true}, nil
}

type versionResult struct {
	Daemon string  `json:"daemon"`
	Xray   *string `json:"xray"`
}

func (d *dispatch) handleGetVersion(_ context.Context, _ json.RawMessage) (any, *derr.Error) {
	var xv *string
	if d.xrayInfo.Found && d.xrayInfo.Version != "" {
		v := d.xrayInfo.Version
		xv = &v
	}
	return versionResult{Daemon: version.Version, Xray: xv}, nil
}

// --- Configs.* ---

type addParams struct {
	JSON string `json:"json"`
}

type addResult struct {
	ID string `json:"id"`
}

func (d *dispatch) handleConfigsAdd(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.configs == nil {
		return nil, derr.ErrInternal.WithMessage("config store not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Configs.Add requires {json: string}")
	}
	var p addParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	if p.JSON == "" {
		return nil, derr.ErrInvalidParams.WithMessage("Configs.Add: json field is empty")
	}
	id, err := d.configs.Add(ctx, []byte(p.JSON))
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	if d.bcast != nil {
		d.bcast.Broadcast(Event{
			Method: "Configs.Changed",
			Params: ConfigsChangedParams{Added: []string{id.String()}},
		})
	}
	return addResult{ID: id.String()}, nil
}

type listResult struct {
	Configs []xrayconfig.ConfigInfo `json:"configs"`
}

func (d *dispatch) handleConfigsList(_ context.Context, _ json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.configs == nil {
		return nil, derr.ErrInternal.WithMessage("config store not wired")
	}
	infos, err := d.configs.List()
	if err != nil {
		return nil, derr.ErrInternal.With(err)
	}
	if infos == nil {
		infos = []xrayconfig.ConfigInfo{} // marshal as [], not null
	}
	return listResult{Configs: infos}, nil
}

type removeParams struct {
	ID string `json:"id"`
}

type removeResult struct {
	OK bool `json:"ok"`
}

func (d *dispatch) handleConfigsRemove(_ context.Context, raw json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.configs == nil {
		return nil, derr.ErrInternal.WithMessage("config store not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Configs.Remove requires {id: string}")
	}
	var p removeParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	id, err := xrayconfig.ParseULID(p.ID)
	if err != nil {
		// ParseULID already wraps as ErrConfigUnknown — treat malformed
		// client id the same as missing id.
		return nil, asDerrOrInternal(err)
	}
	if d.machine != nil && d.machine.IsActive(id) {
		return nil, derr.ErrConfigInUse
	}
	if err := d.configs.Remove(id); err != nil {
		return nil, asDerrOrInternal(err)
	}
	if d.bcast != nil {
		d.bcast.Broadcast(Event{
			Method: "Configs.Changed",
			Params: ConfigsChangedParams{Removed: []string{id.String()}},
		})
	}
	return removeResult{OK: true}, nil
}

type validateParams struct {
	ID string `json:"id"`
}

type getParams struct {
	ID string `json:"id"`
}

type getResult struct {
	JSON string `json:"json"`
}

func (d *dispatch) handleConfigsGet(_ context.Context, raw json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.configs == nil {
		return nil, derr.ErrInternal.WithMessage("config store not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Configs.Get requires {id: string}")
	}
	var p getParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	id, err := xrayconfig.ParseULID(p.ID)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	body, err := d.configs.Get(id)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	return getResult{JSON: body}, nil
}

func (d *dispatch) handleConfigsValidate(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.configs == nil {
		return nil, derr.ErrInternal.WithMessage("config store not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Configs.Validate requires {id: string}")
	}
	var p validateParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	id, err := xrayconfig.ParseULID(p.ID)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	res, err := d.configs.Validate(ctx, id)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	if res.Timeout {
		// Surface as a typed error so the renderer can distinguish "we
		// couldn't tell" from "xray rejected" and pick its own retry policy.
		return nil, derr.ErrValidationTimeout
	}
	return res, nil
}

// --- Tunnel.* ---

type tunnelConnectParams struct {
	ID string `json:"id"`
}

type tunnelStateResult struct {
	State string `json:"state"`
}

func (d *dispatch) handleTunnelConnect(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
	// Rate-limit before any other work so a flooding client can't exhaust
	// validation resources even with malformed requests.
	if rate, ok := ctx.Value(ctxKeyConnRate{}).(*tokenBucket); ok && rate != nil {
		if !rate.take() {
			return nil, derr.ErrRateLimited
		}
	}
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.machine == nil {
		return nil, derr.ErrInternal.WithMessage("tunnel machine not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Tunnel.Connect requires {id: string}")
	}
	var p tunnelConnectParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	id, err := xrayconfig.ParseULID(p.ID)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	if err := d.machine.Connect(ctx, id); err != nil {
		return nil, asDerrOrInternal(err)
	}
	return tunnelStateResult{State: state.StateValidating}, nil
}

func (d *dispatch) handleTunnelSwitch(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.machine == nil {
		return nil, derr.ErrInternal.WithMessage("tunnel machine not wired")
	}
	if rate, ok := ctx.Value(ctxKeyConnRate{}).(*tokenBucket); ok && rate != nil {
		if !rate.take() {
			return nil, derr.ErrRateLimited
		}
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Tunnel.Switch requires {id: string}")
	}
	var p tunnelConnectParams // reuse {id: string} struct
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	id, err := xrayconfig.ParseULID(p.ID)
	if err != nil {
		return nil, asDerrOrInternal(err)
	}
	if err := d.machine.Switch(ctx, id); err != nil {
		return nil, asDerrOrInternal(err)
	}
	// Return the immediate post-switch state. The final outcome (Connected to
	// the new config) flows via State.Changed events.
	return tunnelStateResult{State: string(d.machine.GetStatus(ctx).Conn.State)}, nil
}

func (d *dispatch) handleTunnelDisconnect(ctx context.Context, _ json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.machine == nil {
		return nil, derr.ErrInternal.WithMessage("tunnel machine not wired")
	}
	if err := d.machine.Disconnect(ctx); err != nil {
		return nil, asDerrOrInternal(err)
	}
	return tunnelStateResult{State: state.StateDisconnecting}, nil
}

func (d *dispatch) handleTunnelGetStatus(ctx context.Context, _ json.RawMessage) (any, *derr.Error) {
	if !d.xrayInfo.Found {
		return nil, derr.ErrXrayNotFound
	}
	if d.machine == nil {
		return nil, derr.ErrInternal.WithMessage("tunnel machine not wired")
	}
	return d.machine.GetStatus(ctx), nil
}

// --- Health.* ---

type healthSetEnabledParams struct {
	Enabled bool `json:"enabled"`
}

type healthOKResult struct {
	OK bool `json:"ok"`
}

type healthConfigResult struct {
	Enabled         bool   `json:"enabled"`
	Endpoint        string `json:"endpoint"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

func (d *dispatch) handleHealthSetEnabled(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
	if d.health == nil {
		return nil, derr.ErrInternal.WithMessage("health subsystem not wired")
	}
	if len(raw) == 0 || string(raw) == "null" {
		return nil, derr.ErrInvalidParams.WithMessage("Health.SetEnabled requires {enabled: bool}")
	}
	var p healthSetEnabledParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, derr.ErrInvalidParams.With(err)
	}
	d.health.SetEnabled(ctx, p.Enabled)
	return healthOKResult{OK: true}, nil
}

func (d *dispatch) handleHealthProbe(ctx context.Context, _ json.RawMessage) (any, *derr.Error) {
	if d.health == nil {
		return nil, derr.ErrInternal.WithMessage("health subsystem not wired")
	}
	s, err := d.health.Probe(ctx)
	if err != nil {
		if errors.Is(err, health.ErrHealthDisabled()) {
			return nil, derr.ErrHealthDisabled
		}
		return nil, derr.ErrInternal.With(err)
	}
	return s, nil
}

func (d *dispatch) handleHealthGetConfig(_ context.Context, _ json.RawMessage) (any, *derr.Error) {
	if d.health == nil {
		return nil, derr.ErrInternal.WithMessage("health subsystem not wired")
	}
	cfg := d.health.GetConfig()
	return healthConfigResult{
		Enabled:         d.health.IsEnabled(),
		Endpoint:        cfg.Endpoint,
		IntervalSeconds: cfg.IntervalSeconds,
	}, nil
}
