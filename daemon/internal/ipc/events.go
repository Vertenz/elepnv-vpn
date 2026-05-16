package ipc

import (
	"context"

	"elepn/daemon/internal/health"
	"elepn/daemon/internal/state"
	"elepn/daemon/internal/xrayconfig"
)

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

// StateChangedParams is the wire payload for the State.Changed notification.
type StateChangedParams = state.ConnStatus

// TunnelMachine is the subset of *state.Machine the IPC dispatch layer needs.
// Interface so the dispatcher can be unit-tested without a real actor.
type TunnelMachine interface {
	Connect(ctx context.Context, id xrayconfig.ULID) error
	Disconnect(ctx context.Context) error
	GetStatus(ctx context.Context) state.Status
	IsActive(id xrayconfig.ULID) bool
	Subscribe() (<-chan state.ConnStatus, func())
}

// HealthChangedParams is the wire payload for the Health.Changed notification.
type HealthChangedParams = health.Status

// HealthMachine is the subset of *health.Health the IPC dispatch layer needs.
// Interface so the dispatcher can be unit-tested without a real probe scheduler.
type HealthMachine interface {
	SetEnabled(ctx context.Context, enabled bool)
	Probe(ctx context.Context) (health.Status, error)
	GetConfig() health.Config
	IsEnabled() bool
	Subscribe() (<-chan health.Status, func())
}
