package ipc

import (
	"context"
	"encoding/json"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/version"
	"elepn/daemon/internal/xrayconfig"
)

// MethodHandler is the per-method signature. It receives the request's params
// as raw JSON and must return either a JSON-marshalable result or a derr.
type MethodHandler func(ctx context.Context, params json.RawMessage) (result any, err *derr.Error)

// dispatch is the IPC method routing table. It owns nothing — collaborators
// (XrayInfo, Store, Broadcaster) are injected at construction so tests can
// substitute fakes.
type dispatch struct {
	methods  map[string]MethodHandler
	xrayInfo platform.XrayInfo
	configs  *xrayconfig.Store // nil-permitted in Plan-1-only tests
	bcast    Broadcaster       // nil-permitted in Plan-1-only tests
}

func newDispatch(xrayInfo platform.XrayInfo, store *xrayconfig.Store, bcast Broadcaster) *dispatch {
	d := &dispatch{
		methods:  make(map[string]MethodHandler),
		xrayInfo: xrayInfo,
		configs:  store,
		bcast:    bcast,
	}
	d.methods["Daemon.Ping"] = d.handlePing
	d.methods["Daemon.GetVersion"] = d.handleGetVersion
	d.methods["Configs.Add"] = d.handleConfigsAdd
	d.methods["Configs.List"] = d.handleConfigsList
	d.methods["Configs.Remove"] = d.handleConfigsRemove
	d.methods["Configs.Validate"] = d.handleConfigsValidate
	return d
}

func (d *dispatch) handle(ctx context.Context, req Request) (any, *derr.Error) {
	h, ok := d.methods[req.Method]
	if !ok {
		return nil, derr.ErrMethodNotFound.WithMessage(req.Method)
	}
	return h(ctx, req.Params)
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
		return nil, derr.AsDerr(err)
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
		return nil, derr.AsDerr(err)
	}
	// TODO(plan-3): consult Machine.IsActive(id) and return ErrConfigInUse
	// before calling Store.Remove.
	if err := d.configs.Remove(id); err != nil {
		return nil, derr.AsDerr(err)
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

func (d *dispatch) handleConfigsValidate(ctx context.Context, raw json.RawMessage) (any, *derr.Error) {
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
		return nil, derr.AsDerr(err)
	}
	res, err := d.configs.Validate(ctx, id)
	if err != nil {
		return nil, derr.AsDerr(err)
	}
	return res, nil
}
