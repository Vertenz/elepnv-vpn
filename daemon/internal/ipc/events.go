package ipc

// Event is the payload carried by subscribers.broadcast. Plan 1 declares the
// type but emits no events; Plan 3 adds State.Changed and Plan 2 adds
// Configs.Changed.
type Event struct {
	Method string // e.g. "State.Changed", "Configs.Changed", "Health.Changed"
	Params any    // method-specific; JSON-marshaled by the writer goroutine
}
