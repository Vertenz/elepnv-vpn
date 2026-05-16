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

// Error is the daemon's IPC-visible error value. It implements both the Go
// error interface and JSON-RPC's error.Code/error.Message/error.data layout.
type Error struct {
	Code    int    // JSON-RPC error code (-32000..-32099 server-defined)
	Symbol  string // stable identifier, e.g. "config_unknown"
	Message string // human-readable default; may be overridden per-call via With
	Cause   error  // optional wrapped cause (logged but not sent on the wire)
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
func (e *Error) With(cause error) *Error {
	return &Error{
		Code:    e.Code,
		Symbol:  e.Symbol,
		Message: e.Message,
		Cause:   cause,
	}
}

// WithMessage returns a copy of e with a per-call message override. The Symbol
// stays stable; the message can carry context like the offending JSON pointer.
func (e *Error) WithMessage(msg string) *Error {
	return &Error{
		Code:    e.Code,
		Symbol:  e.Symbol,
		Message: msg,
		Cause:   e.Cause,
	}
}

// JSON returns the JSON-RPC `error` object for this error.
// Output shape: {"code": -32004, "message": "config_invalid: <msg>",
//                "data": {"symbol": "config_invalid"}}.
func (e *Error) JSON() json.RawMessage {
	type errObj struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	data := struct {
		Symbol string `json:"symbol"`
	}{Symbol: e.Symbol}
	dataBytes, _ := json.Marshal(data)
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
