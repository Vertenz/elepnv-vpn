package ipc

import (
	"context"
	"encoding/json"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/version"
)

// MethodHandler is the per-method signature. It receives the request's params
// as raw JSON and must return either a JSON-marshalable result or a derr.
type MethodHandler func(ctx context.Context, params json.RawMessage) (result any, err *derr.Error)

// Server-facing dispatch state held by the IPC server. Construction by main.go
// wires the actual handlers; plans 2/3/4 add more.
type dispatch struct {
	methods  map[string]MethodHandler
	xrayInfo platform.XrayInfo
}

func newDispatch(xrayInfo platform.XrayInfo) *dispatch {
	d := &dispatch{
		methods:  make(map[string]MethodHandler),
		xrayInfo: xrayInfo,
	}
	d.methods["Daemon.Ping"] = d.handlePing
	d.methods["Daemon.GetVersion"] = d.handleGetVersion
	return d
}

// dispatch a single request. If the method is unknown, returns ErrMethodNotFound.
func (d *dispatch) handle(ctx context.Context, req Request) (any, *derr.Error) {
	h, ok := d.methods[req.Method]
	if !ok {
		return nil, derr.ErrMethodNotFound.WithMessage(req.Method)
	}
	return h(ctx, req.Params)
}

// Daemon.Ping — trivial liveness probe. Synchronous; does not consult the
// state machine (§8.4 method semantics).
type pingResult struct {
	OK bool `json:"ok"`
}

func (d *dispatch) handlePing(_ context.Context, _ json.RawMessage) (any, *derr.Error) {
	return pingResult{OK: true}, nil
}

// Daemon.GetVersion — returns the cached daemon and xray-core versions.
// xray is null when xray-core is not installed (§8.4).
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
	return versionResult{
		Daemon: version.Version,
		Xray:   xv,
	}, nil
}
