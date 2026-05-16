package ipc

// Event is the payload carried by Server.Broadcast. Plan 1 declared the type
// but emitted nothing; Plan 2 starts emitting Configs.Changed.
type Event struct {
	Method string // e.g. "Configs.Changed", "State.Changed", "Health.Changed"
	Params any    // method-specific; JSON-marshaled by the writer goroutine
}

// ConfigsChangedParams is the wire payload for the Configs.Changed
// notification. Exactly one of Added/Removed is populated per event so the
// renderer can apply the diff without re-listing.
type ConfigsChangedParams struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
}

// Broadcaster is the subset of Server's API the dispatch layer needs to emit
// events. It exists so methods.go can be unit-tested without a real Server.
type Broadcaster interface {
	Broadcast(Event)
}
