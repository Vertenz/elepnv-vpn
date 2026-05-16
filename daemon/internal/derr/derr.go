// Package derr defines the daemon's stable, typed error model.
//
// Every error that crosses the IPC boundary is a *Error with a stable Code,
// Symbol, and human-readable Message. The renderer matches on Symbol via
// the JSON-RPC error.data.symbol field; the free-text Message may change
// between versions without breaking clients.
package derr

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Detail is an optional structured payload merged into the JSON-RPC
// error.data object alongside `symbol`. Constructors like NewPathUnsafe
// populate it so renderers can highlight the offending JSON pointer.
type Detail map[string]any

// Error is the daemon's IPC-visible error value. It implements both the Go
// error interface and JSON-RPC's error.Code/error.Message/error.data layout.
type Error struct {
	Code    int    // JSON-RPC error code (-32000..-32099 server-defined)
	Symbol  string // stable identifier, e.g. "config_unknown"
	Message string // human-readable default; may be overridden per-call via With
	Cause   error  // optional wrapped cause (logged but not sent on the wire)
	detail  Detail // optional; surfaces under data.detail when non-nil
}

// Error implements the error interface.
func (e *Error) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %s: %v", e.Symbol, e.Message, e.Cause)
	}
	return fmt.Sprintf("%s: %s", e.Symbol, e.Message)
}

// Unwrap exposes Cause so errors.Is / errors.As work.
func (e *Error) Unwrap() error { return e.Cause }

// Is enables errors.Is matching by Symbol (Code differs only across versions).
func (e *Error) Is(target error) bool {
	t, ok := target.(*Error)
	if !ok {
		return false
	}
	return e.Symbol == t.Symbol
}

// With returns a copy of e with the given cause attached. The original
// sentinel pointer is preserved as the chain's tail via Cause wrapping.
//
// The struct-copy keeps the original `detail` map reference. WithDetail is
// the only mutator that defensively clones; callers MUST treat e.detail as
// immutable after construction (which the public API enforces — there is
// no way to mutate a non-nil detail through the exported surface).
func (e *Error) With(cause error) *Error {
	cp := *e
	cp.Cause = cause
	return &cp
}

// WithMessage returns a copy of e with a per-call message override. The Symbol
// stays stable; the message can carry context like the offending JSON pointer.
//
// Like With, this shallow-copies. See With's note on detail immutability.
func (e *Error) WithMessage(msg string) *Error {
	cp := *e
	cp.Message = msg
	return &cp
}

// WithDetail returns a copy of e with the given structured detail attached.
// The detail surfaces in JSON()'s error.data.detail object.
//
// Defensively shallow-copies the input map so the caller can mutate their
// copy without affecting the returned *Error. This is asymmetric with With
// and WithMessage — those reuse the existing detail by reference, which is
// safe because callers can't mutate e.detail through the exported API.
func (e *Error) WithDetail(d Detail) *Error {
	cp := *e
	if d != nil {
		cp.detail = make(Detail, len(d))
		for k, v := range d {
			cp.detail[k] = v
		}
	}
	return &cp
}

// JSON returns the JSON-RPC `error` object for this error.
// Output shape: {"code": -32004, "message": "config_invalid: <msg>",
//
//	"data": {"symbol": "config_invalid"}}.
//
// When detail is present, data also contains a "detail" key.
func (e *Error) JSON() json.RawMessage {
	type errObj struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	dataMap := map[string]any{"symbol": e.Symbol}
	if e.detail != nil {
		dataMap["detail"] = e.detail
	}
	dataBytes, _ := json.Marshal(dataMap)
	b, _ := json.Marshal(errObj{
		Code:    e.Code,
		Message: e.Symbol + ": " + e.Message,
		Data:    dataBytes,
	})
	return b
}

// AsDerr extracts a *Error from err if one is in the chain, else returns nil.
func AsDerr(err error) *Error {
	var de *Error
	if errors.As(err, &de) {
		return de
	}
	return nil
}

// NewPathUnsafe returns ErrPathUnsafe with a JSON-pointer location and the
// offending value, so the renderer can highlight the field in the config UI.
func NewPathUnsafe(pointer, value string) *Error {
	return ErrPathUnsafe.WithDetail(Detail{
		"pointer": pointer,
		"value":   value,
	})
}

// NewInboundUnsafe returns ErrInboundUnsafe with a JSON-pointer location and
// a human-readable reason string.
func NewInboundUnsafe(pointer, reason string) *Error {
	return ErrInboundUnsafe.WithDetail(Detail{
		"pointer": pointer,
		"reason":  reason,
	}).WithMessage(reason)
}

// WrapSpawn returns ErrXraySpawnFailed wrapping cause for diagnostics.
func WrapSpawn(cause error) *Error { return ErrXraySpawnFailed.With(cause) }

// WrapDiedEarly returns ErrXrayDiedEarly wrapping cause; cause should embed
// the captured xray stderr tail for operator triage.
func WrapDiedEarly(cause error) *Error { return ErrXrayDiedEarly.With(cause) }

// WrapInbound returns ErrInboundNotReady wrapping cause (typically a SOCKS
// dial/handshake failure or deadline-exceeded).
func WrapInbound(cause error) *Error { return ErrInboundNotReady.With(cause) }
