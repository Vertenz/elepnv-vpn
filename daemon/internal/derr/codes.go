package derr

// Sentinel errors. Each plan appends to this list as new error paths are introduced.
//
// JSON-RPC reserves -32000..-32099 for server-defined codes; -32600..-32603
// for protocol-level codes emitted by the codec on framing failures.
var (
	// Server-defined.
	ErrInternal            = &Error{Code: -32000, Symbol: "internal", Message: "internal error"}
	ErrXrayNotFound        = &Error{Code: -32001, Symbol: "xray_not_found", Message: "xray binary not found in PATH"}
	ErrConfigUnknown       = &Error{Code: -32002, Symbol: "config_unknown", Message: "config id not found"}
	ErrConfigMalformedJSON = &Error{Code: -32003, Symbol: "config_malformed_json", Message: "config is not valid JSON"}
	ErrConfigInvalid       = &Error{Code: -32004, Symbol: "config_invalid", Message: "xray rejected config"}
	ErrConfigInUse         = &Error{Code: -32005, Symbol: "config_in_use", Message: "config is currently active; disconnect first"}
	ErrUnauthorized        = &Error{Code: -32012, Symbol: "unauthorized", Message: "peer not in xrayd group"}
	ErrDaemonShuttingDown  = &Error{Code: -32014, Symbol: "daemon_shutting_down", Message: "daemon is shutting down"}
	ErrRequestTooLarge     = &Error{Code: -32016, Symbol: "request_too_large", Message: "request line exceeds max_request_bytes"}
	ErrPathUnsafe          = &Error{Code: -32015, Symbol: "path_unsafe", Message: "config references a path outside the allowed roots"}
	ErrInboundUnsafe       = &Error{Code: -32020, Symbol: "inbound_unsafe", Message: "config inbound failed safety policy"}

	// JSON-RPC reserved.
	ErrParseError     = &Error{Code: -32700, Symbol: "parse_error", Message: "request was not valid JSON"}
	ErrInvalidRequest = &Error{Code: -32600, Symbol: "invalid_request", Message: "request is not a valid JSON-RPC 2.0 request"}
	ErrMethodNotFound = &Error{Code: -32601, Symbol: "method_not_found", Message: "method not implemented"}
	ErrInvalidParams  = &Error{Code: -32602, Symbol: "invalid_params", Message: "params do not match method signature"}
)
