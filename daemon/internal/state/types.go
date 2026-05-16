package state

import (
	"time"

	"elepn/daemon/internal/supervisor"
	"elepn/daemon/internal/xrayconfig"
)

// TunnelState values match spec §3.1 + §8.4 wire enum.
const (
	StateDisconnected  = "Disconnected"
	StateValidating    = "Validating"
	StateConnecting    = "Connecting"
	StateConnected     = "Connected"
	StateDisconnecting = "Disconnecting"
	StateError         = "Error"
)

// ConnStatus is the per-axis connection snapshot the actor emits as the
// payload of State.Changed and stores into state.json.
type ConnStatus struct {
	State       string    `json:"state"`
	ConfigID    string    `json:"configID,omitempty"`
	XrayPid     int       `json:"xrayPid,omitempty"`
	Since       time.Time `json:"since"`
	Message     string    `json:"message,omitempty"`
	ErrorSymbol string    `json:"errorSymbol,omitempty"` // populated when State == StateError; stable derr.Symbol
}

// Status is the combined snapshot returned by Tunnel.GetStatus.
// Health is left as an opaque type so Plan 4 can drop in its real shape
// without forcing a wire-format renegotiation.
type Status struct {
	Conn   ConnStatus `json:"conn"`
	Health any        `json:"health,omitempty"`
}

// Config holds the runtime tuning parameters the actor and worker need.
// Constructed once at Machine creation; never mutated.
type Config struct {
	SocksAddr       string
	ConnectDeadline time.Duration
	AutoRevertDelay time.Duration
	StateJSONPath   string
}

// Worker dependencies — passed once at Machine construction.
type deps struct {
	cfgs           *xrayconfig.Store
	sup            *supervisor.Supervisor
	cfg            Config
	healthSnapshot func() any // returns the current health Status; nil-permitted
}

// Command interface — implemented by the unexported cmd* structs below.
type command interface{ isCommand() }

type cmdConnect struct {
	id    xrayconfig.ULID
	reply chan<- error // cap 1; non-blocking send
}

type cmdDisconnect struct {
	reply chan<- error
}

type cmdConnectProgress struct {
	gen      int64
	newState string
}

type cmdConnectDone struct {
	gen    int64
	result connectResult
}

type cmdDisconnectDone struct {
	gen int64
}

type cmdSwitch struct {
	id    xrayconfig.ULID
	reply chan error
}

type cmdAutoRevert struct{}

type cmdChildExit struct {
	exit supervisor.Exit
}

type cmdGetStatus struct {
	reply chan<- Status
}

type cmdShutdown struct {
	done chan<- struct{}
}

func (cmdConnect) isCommand()         {}
func (cmdDisconnect) isCommand()      {}
func (cmdConnectProgress) isCommand() {}
func (cmdConnectDone) isCommand()     {}
func (cmdDisconnectDone) isCommand()  {}
func (cmdSwitch) isCommand()          {}
func (cmdAutoRevert) isCommand()      {}
func (cmdChildExit) isCommand()       {}
func (cmdGetStatus) isCommand()       {}
func (cmdShutdown) isCommand()        {}

// connectResult is the value the doConnect worker hands back to the actor
// via cmdConnectDone. cleanup is non-nil on success (actor "arms" it for
// later disarm); nil on failure (worker ran it inline).
type connectResult struct {
	id      xrayconfig.ULID
	child   *supervisor.Child
	cleanup *cleanupStack
	err     error
}

// shouldCancelAutoRevert is the P2-1 narrow whitelist: read-only commands
// like GetStatus do NOT cancel a pending Error → Disconnected timer.
func shouldCancelAutoRevert(cmd command) bool {
	switch cmd.(type) {
	case cmdConnect, cmdDisconnect, cmdSwitch, cmdAutoRevert, cmdChildExit, cmdShutdown:
		return true
	default:
		return false
	}
}
