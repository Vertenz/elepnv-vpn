// Package ipc implements the JSON-RPC 2.0 server over a Unix socket described
// in §8 of the daemon design spec.
//
// Wire format: newline-delimited JSON-RPC 2.0 (NDJSON). One request per line;
// one response per line; notifications (no id) for server-pushed events.
package ipc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"

	"elepn/daemon/internal/derr"
)

// MaxRequestBytes is the absolute ceiling on a single line read from the
// socket. Matches §8.3 quota and §8.2 Scanner setting.
const MaxRequestBytes = 256 * 1024

// Request is an incoming JSON-RPC request. ID is json.RawMessage so we can
// echo back exactly what the client sent (string, number, or null).
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// Response is the success branch of a JSON-RPC reply.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
}

// ErrorResponse is the error branch. Error is a marshaled derr.Error.JSON().
type ErrorResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Error   json.RawMessage `json:"error"`
}

// Notification is a server-pushed event (no id, no result).
type Notification struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// NewScanner returns a bufio.Scanner configured for the IPC framing rules:
// up to MaxRequestBytes per line. Caller assigns to a connection.
func NewScanner(r io.Reader) *bufio.Scanner {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), MaxRequestBytes)
	return sc
}

// DecodeRequest parses a single NDJSON line into a Request. Returns a typed
// *derr.Error on framing failures so the caller can serialize it directly.
func DecodeRequest(line []byte) (Request, error) {
	var req Request
	if err := json.Unmarshal(line, &req); err != nil {
		return Request{}, derr.ErrParseError.With(err)
	}
	if req.JSONRPC != "2.0" {
		return Request{}, derr.ErrInvalidRequest.WithMessage("jsonrpc field must be \"2.0\"")
	}
	if req.Method == "" {
		return Request{}, derr.ErrInvalidRequest.WithMessage("method is empty")
	}
	return req, nil
}

// MarshalResponse returns a complete NDJSON line (JSON + '\n') for a JSON-RPC
// success response. Used by the IPC server which wants to perform a single
// locked Write of the full line; see ipc/server.go connHandle.write.
func MarshalResponse(id json.RawMessage, result any) ([]byte, error) {
	resultBytes, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  resultBytes,
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// MarshalError returns a complete NDJSON line for a JSON-RPC error response.
// de must be non-nil.
func MarshalError(id json.RawMessage, de *derr.Error) ([]byte, error) {
	b, err := json.Marshal(ErrorResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   de.JSON(),
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// MarshalNotification returns a complete NDJSON line for a server-pushed event.
func MarshalNotification(method string, params any) ([]byte, error) {
	paramsBytes, err := json.Marshal(params)
	if err != nil {
		return nil, err
	}
	b, err := json.Marshal(Notification{
		JSONRPC: "2.0",
		Method:  method,
		Params:  paramsBytes,
	})
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// EncodeResponse is a convenience wrapper around MarshalResponse + Write,
// used by tests where a single goroutine controls the writer. Production
// code paths in the IPC server use MarshalResponse directly so writes can
// go through connHandle.write under its per-connection mutex.
func EncodeResponse(w io.Writer, id json.RawMessage, result any) error {
	b, err := MarshalResponse(id, result)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// EncodeError — convenience wrapper, see EncodeResponse.
func EncodeError(w io.Writer, id json.RawMessage, de *derr.Error) error {
	b, err := MarshalError(id, de)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// EncodeNotification — convenience wrapper, see EncodeResponse.
func EncodeNotification(w io.Writer, method string, params any) error {
	b, err := MarshalNotification(method, params)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}

// ScanErr classifies a bufio.Scanner error after Scan returns false. It
// returns ErrRequestTooLarge when the line exceeded MaxRequestBytes, and io.EOF
// when the peer closed cleanly. Other errors are wrapped via ErrInternal.
func ScanErr(err error) error {
	if err == nil {
		return io.EOF
	}
	if errors.Is(err, bufio.ErrTooLong) {
		return derr.ErrRequestTooLarge
	}
	return derr.ErrInternal.With(err)
}
