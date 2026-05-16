package derr

// Sentinel errors used in Plan 1. Subsequent plans append to this list.
//
// JSON-RPC reserves -32000..-32099 for server-defined codes; -32600..-32603
// for protocol-level codes emitted by the codec on framing failures.
var (
	// Server-defined.
	ErrInternal            = &Error{Code: -32000, Symbol: "internal", Message: "internal error"}
	ErrXrayNotFound        = &Error{Code: -32001, Symbol: "xray_not_found", Message: "xray binary not found in PATH"}
	ErrUnauthorized        = &Error{Code: -32012, Symbol: "unauthorized", Message: "peer not in xrayd group"}
	ErrDaemonShuttingDown  = &Error{Code: -32014, Symbol: "daemon_shutting_down", Message: "daemon is shutting down"}
	ErrRequestTooLarge     = &Error{Code: -32016, Symbol: "request_too_large", Message: "request line exceeds max_request_bytes"}

	// JSON-RPC reserved.
	ErrParseError     = &Error{Code: -32700, Symbol: "parse_error", Message: "request was not valid JSON"}
	ErrInvalidRequest = &Error{Code: -32600, Symbol: "invalid_request", Message: "request is not a valid JSON-RPC 2.0 request"}
	ErrMethodNotFound = &Error{Code: -32601, Symbol: "method_not_found", Message: "method not implemented"}
	ErrInvalidParams  = &Error{Code: -32602, Symbol: "invalid_params", Message: "params do not match method signature"}
)
