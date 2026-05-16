package health

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// State is the wire-visible health enum (§3.1, §8.4).
type State string

const (
	StateUnknown  State = "Unknown"
	StateOnline   State = "Online"
	StateDegraded State = "Degraded"
	StateOffline  State = "Offline"
)

// Status is the snapshot delivered to subscribers and returned by Probe().
type Status struct {
	Health      State     `json:"health"`
	LatencyMs   int64     `json:"latencyMs,omitempty"`
	LastChecked time.Time `json:"lastChecked,omitempty"`
}

// Config is the constructor input. Endpoint/IntervalSeconds default if blank.
type Config struct {
	SocksAddr       string
	Endpoint        string
	IntervalSeconds int
}

// Health owns the probe scheduler and the broadcast fan-out for State.Changed.
// Concurrency model:
//   - enableMu serializes SetEnabled calls to prevent TOCTOU double-spawn.
//   - The enabled flag is atomic (fast read path for IsEnabled / Probe).
//   - The in-flight loop cancellation is via atomic.Pointer[context.CancelFunc].
//   - The status snapshot + subscriber map are guarded by mu.
//   - baseCtx/baseCancel give the probe loop a daemon-lifecycle lifetime that
//     is independent of any IPC request context.
type Health struct {
	cfg        Config
	log        *slog.Logger
	cli        *http.Client
	enabled    atomic.Bool
	cancelLoop atomic.Pointer[context.CancelFunc]

	enableMu   sync.Mutex         // serializes SetEnabled — prevents TOCTOU double-spawn
	baseCtx    context.Context    // daemon-lifecycle ctx; probe loop derives from this
	baseCancel context.CancelFunc // cancelled by Close

	mu     sync.RWMutex
	status Status
	subs   map[uint64]chan Status
	subSeq uint64
}

// New constructs a Health bound to xray's local SOCKS inbound at cfg.SocksAddr.
// The endpoint and interval default to the spec §8.5 values if blank/out-of-range.
func New(cfg Config, log *slog.Logger) *Health {
	switch {
	case cfg.IntervalSeconds == 0:
		cfg.IntervalSeconds = 10 // unset → spec default
	case cfg.IntervalSeconds < 5:
		cfg.IntervalSeconds = 5 // clamp to spec minimum
	case cfg.IntervalSeconds > 600:
		cfg.IntervalSeconds = 600 // clamp to spec maximum
	}
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://www.gstatic.com/generate_204"
	}
	tr := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			if network != "tcp" {
				return nil, fmt.Errorf("only tcp")
			}
			return dialThroughSocks(ctx, cfg.SocksAddr, addr)
		},
		// CRITICAL: Proxy must be nil — Go's default ProxyFromEnvironment would
		// honor HTTP_PROXY/http_proxy and bypass our SOCKS-only dial path,
		// potentially leaking DNS and traffic via the user's ISP.
		Proxy:              nil,
		DisableKeepAlives:  true,
		DisableCompression: true,
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	return &Health{
		cfg:        cfg,
		log:        log,
		cli:        &http.Client{Transport: tr, Timeout: 3 * time.Second},
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
		status:     Status{Health: StateUnknown},
		subs:       make(map[uint64]chan Status),
	}
}

// Close cancels the probe loop (if running) and the base ctx. Idempotent.
// Call from daemon shutdown after SetEnabled(false).
func (h *Health) Close() {
	h.baseCancel()
}

func (h *Health) GetConfig() Config { return h.cfg }

func (h *Health) GetStatus() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.status
}

func (h *Health) IsEnabled() bool { return h.enabled.Load() }

// SetEnabled (true) starts the periodic probe loop; (false) cancels it and
// posts Unknown to subscribers. The parent ctx parameter is intentionally
// ignored for the loop's lifetime — the loop derives from h.baseCtx (a
// daemon-lifecycle context) so it survives IPC request/connection teardown.
// The parameter is retained for API compatibility with the HealthMachine
// interface declared in ipc/events.go.
//
// Concurrent calls are serialized by enableMu to prevent TOCTOU double-spawn.
func (h *Health) SetEnabled(_ context.Context, enabled bool) {
	h.enableMu.Lock()
	defer h.enableMu.Unlock()
	if enabled == h.enabled.Load() {
		return
	}
	h.enabled.Store(enabled)
	if enabled {
		ctx, cancel := context.WithCancel(h.baseCtx)
		h.cancelLoop.Store(&cancel)
		go h.loop(ctx)
		return
	}
	if c := h.cancelLoop.Swap(nil); c != nil {
		(*c)()
	}
	h.update(Status{Health: StateUnknown, LastChecked: time.Now()})
}

// Probe runs a one-shot probe outside the schedule. Returns ErrHealthDisabled()
// if SetEnabled(true) has not been called.
func (h *Health) Probe(ctx context.Context) (Status, error) {
	if !h.enabled.Load() {
		return Status{}, ErrHealthDisabled()
	}
	s := h.runOnce(ctx)
	// One-shot probe does not feed the loop's broadcast — caller decides what
	// to do with the result. Keeping it out of update() avoids spurious
	// Health.Changed events from manual Probe() calls.
	return s, nil
}

// Subscribe returns a per-client buffered channel (cap 4) for Status updates.
// Sends are non-blocking — a slow client silently drops events (mirrors
// state.Subscribers' policy).
//
// The returned unsub func removes the channel from the broadcast map but does
// NOT close it. update() snapshots the map under mu, then sends outside the
// lock; closing the channel from unsub would race with those out-of-lock sends
// and cause a send-on-closed-channel panic. After calling unsub the caller
// should stop reading from ch; the channel will be GC'd when both sides drop
// their references.
func (h *Health) Subscribe() (<-chan Status, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.subSeq++
	id := h.subSeq
	ch := make(chan Status, 4)
	h.subs[id] = ch
	return ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs, id)
		// Do NOT close ch — update() may still hold a snapshot reference and
		// send to it outside the lock.
	}
}

func (h *Health) loop(ctx context.Context) {
	interval := time.Duration(h.cfg.IntervalSeconds) * time.Second
	t := time.NewTicker(interval)
	defer t.Stop()
	// Run once immediately so subscribers see a result without waiting one
	// full interval.
	h.update(h.runOnce(ctx))
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.update(h.runOnce(ctx))
		}
	}
}

func (h *Health) runOnce(ctx context.Context) Status {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, "GET", h.cfg.Endpoint, nil)
	if err != nil {
		h.log.Debug("health probe failed building request", "endpoint", h.cfg.Endpoint, "err", err)
		return Status{Health: StateOffline, LastChecked: start}
	}
	resp, err := h.cli.Do(req)
	if err != nil {
		h.log.Debug("health probe failed", "endpoint", h.cfg.Endpoint, "err", err)
		return Status{Health: StateOffline, LastChecked: start}
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	latency := time.Since(start).Milliseconds()
	state := StateOnline
	if resp.StatusCode >= 500 {
		state = StateDegraded
	}
	return Status{Health: state, LatencyMs: latency, LastChecked: start}
}

func (h *Health) update(s Status) {
	h.mu.Lock()
	prev := h.status
	h.status = s
	subs := make([]chan Status, 0, len(h.subs))
	for _, c := range h.subs {
		subs = append(subs, c)
	}
	h.mu.Unlock()
	if prev.Health != s.Health {
		h.log.Info("health state changed", "from", prev.Health, "to", s.Health, "latencyMs", s.LatencyMs)
	}
	for _, c := range subs {
		select {
		case c <- s:
		default:
		}
	}
}

var errHealthDisabled = fmt.Errorf("health probe is disabled")

// ErrHealthDisabled returns the package-internal sentinel for callers that
// need to errors.Is-match Probe()'s disabled-state error.
func ErrHealthDisabled() error { return errHealthDisabled }
