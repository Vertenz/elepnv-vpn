package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"elepn/daemon/internal/derr"
	"elepn/daemon/internal/platform"
	"elepn/daemon/internal/xrayconfig"
)

// Server is the daemon's IPC layer. One instance per process. Lifecycle:
//
//	Listen — opens the socket and starts accepting.
//	StopAccept — stop accepting new connections, keep existing ones open.
//	Close — terminate all open connections and unlink the socket.
type Server struct {
	sockPath string
	log      *slog.Logger

	dispatch *dispatch
	subs     *subscribers

	// baseCtx is cancelled on Close; per-connection contexts derive from it
	// so an in-flight RPC observes daemon shutdown.
	baseCtx    context.Context
	cancelBase context.CancelFunc

	mu        sync.Mutex
	listener  *net.UnixListener
	listening atomic.Bool
	conns     map[*connHandle]struct{}
	closing   bool // guarded by mu; set true by Close so late registers bail

	closeOnce sync.Once
}

type connHandle struct {
	conn net.Conn
	// id is the subscriber id; 0 until subscribe() returns. Read by
	// closeBySubscriberID from the broadcast goroutine, so accesses go
	// through atomic to satisfy the race detector.
	id atomic.Uint64

	// wmu serializes writes to conn so the reader-side response writes and
	// the writerLoop's event writes never interleave bytes on the wire.
	// Per-connection only; different connections write concurrently.
	wmu sync.Mutex

	// rate is the per-connection token bucket for Tunnel.Connect (§8.3).
	// Allocated once in serveConn; isolates a misbehaving renderer from
	// throttling other connections that happen to share the same uid.
	rate *tokenBucket
}

// write sends b (which must already be a complete NDJSON line including the
// trailing '\n') under wmu. Single Write per call; safe under concurrent use.
func (h *connHandle) write(b []byte) error {
	h.wmu.Lock()
	defer h.wmu.Unlock()
	_, err := h.conn.Write(b)
	return err
}

// NewServer constructs a server that will bind sockPath on Listen.
// xrayInfo is used by Daemon.GetVersion (cached at startup).
// store is the config registry; pass nil in tests that don't exercise Configs.*.
// machine is the tunnel state actor; pass nil in tests that don't exercise Tunnel.*.
// hm is the health subsystem; pass nil in tests that don't exercise Health.*.
func NewServer(sockPath string, xrayInfo platform.XrayInfo, store *xrayconfig.Store, machine TunnelMachine, hm HealthMachine, log *slog.Logger) *Server {
	baseCtx, cancel := context.WithCancel(context.Background())
	s := &Server{
		sockPath:   sockPath,
		log:        log,
		conns:      make(map[*connHandle]struct{}),
		baseCtx:    baseCtx,
		cancelBase: cancel,
	}
	s.dispatch = newDispatch(xrayInfo, store, s, machine, hm)
	// onSlowClient: close the offending connection so the renderer reconnects
	// and refetches state via Tunnel.GetStatus.
	s.subs = newSubscribers(log, func(id uint64) { s.closeBySubscriberID(id) })
	if machine != nil {
		go s.runStateChangedBridge(machine)
	}
	if hm != nil {
		go s.runHealthChangedBridge(hm)
	}
	return s
}

// runStateChangedBridge subscribes to the Machine's ConnStatus channel and
// rebroadcasts each event as a State.Changed notification through the existing
// Broadcaster (which fans out to all open IPC clients).
func (s *Server) runStateChangedBridge(machine TunnelMachine) {
	ch, unsub := machine.Subscribe()
	defer unsub()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case st, ok := <-ch:
			if !ok {
				return
			}
			s.Broadcast(Event{
				Method: "State.Changed",
				Params: st,
			})
		}
	}
}

// runHealthChangedBridge subscribes to the Health subsystem's Status channel
// and rebroadcasts each event as a Health.Changed notification. Mirrors the
// pattern of runStateChangedBridge.
func (s *Server) runHealthChangedBridge(hm HealthMachine) {
	ch, unsub := hm.Subscribe()
	defer unsub()
	for {
		select {
		case <-s.baseCtx.Done():
			return
		case st, ok := <-ch:
			if !ok {
				return
			}
			s.Broadcast(Event{Method: "Health.Changed", Params: st})
		}
	}
}

// Listen performs §8.1 socket hardening (stale-unlink + umask + chmod) and
// starts the accept loop in a background goroutine. Returns once the listener
// is bound; the accept goroutine continues until StopAccept + Close.
//
// The ctx parameter is intentionally unused — shutdown is driven by
// StopAccept / Close (which cancel the server's baseCtx). It exists so
// callers can pass appCtx today and so a future refactor that needs
// cancellation during bind (e.g. awaiting RuntimeDirectory readiness) can
// honor it without a breaking signature change.
func (s *Server) Listen(_ context.Context) error {
	l, err := bindControlSocket(s.sockPath, s.log)
	if err != nil {
		s.cancelBase()
		return err
	}
	s.mu.Lock()
	s.listener = l
	s.listening.Store(true)
	s.mu.Unlock()
	go s.acceptLoop()
	return nil
}

// StopAccept closes the listener so no new connections are accepted. Existing
// connections continue to serve until Close. Idempotent.
func (s *Server) StopAccept() {
	s.mu.Lock()
	l := s.listener
	s.listening.Store(false)
	s.mu.Unlock()
	if l != nil {
		_ = l.Close()
	}
}

// Close terminates every open connection. The socket file is unlinked
// automatically by net.UnixListener.Close() (its unlink-on-close default for
// listeners created by net.Listen("unix", ...) and net.ListenUnix is true),
// but we explicitly os.Remove as a belt-and-braces guard against future
// stdlib default changes. The ErrNotExist branch silences the expected case.
// Idempotent.
func (s *Server) Close() error {
	var firstErr error
	s.closeOnce.Do(func() {
		s.StopAccept()
		s.cancelBase()
		s.mu.Lock()
		// Block any late acceptLoop hand-off that won the race against
		// l.Close(). Without this flag a connection accepted in the tiny
		// window between StopAccept and this snapshot would slip past the
		// loop below — bufio.Scanner doesn't honor ctx, so its goroutine
		// would block until the peer eventually closed.
		s.closing = true
		conns := make([]*connHandle, 0, len(s.conns))
		for c := range s.conns {
			conns = append(conns, c)
		}
		s.mu.Unlock()
		for _, c := range conns {
			_ = c.conn.Close()
		}
		if err := os.Remove(s.sockPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			firstErr = fmt.Errorf("unlink socket: %w", err)
		}
	})
	return firstErr
}

// Broadcast pushes an event to every open subscriber. Non-blocking; called by
// the state machine (Plan 3) and config registry (Plan 2). Plan 1 emits none.
func (s *Server) Broadcast(evt Event) {
	s.subs.broadcast(evt)
}

func (s *Server) acceptLoop() {
	for {
		c, err := s.listener.AcceptUnix()
		if err != nil {
			if !s.listening.Load() {
				return // StopAccept closed us
			}
			// Treat transient accept errors as recoverable.
			s.log.Warn("accept error", "err", err)
			continue
		}
		go s.serveConn(c)
	}
}

func (s *Server) serveConn(c *net.UnixConn) {
	handle := &connHandle{
		conn: c,
		rate: newTokenBucket(10, time.Minute),
	}
	if !s.registerConn(handle) {
		// Server is closing; bail before doing any work or starting goroutines.
		_ = c.Close()
		return
	}
	defer s.unregisterConn(handle)
	defer c.Close()

	if err := AuthAccept(c); err != nil {
		if b, mErr := MarshalError(json.RawMessage(`null`), derr.AsDerr(err)); mErr == nil {
			_ = handle.write(b)
		} else {
			s.log.Error("marshal auth-error response failed", "err", mErr)
		}
		return
	}

	// Subscribe this connection for events. The writer goroutine below pumps
	// events onto the wire. Reader handles requests serially per connection.
	// Both writers go through handle.write, which serializes via wmu.
	events, closed, id, unsub := s.subs.subscribe()
	handle.id.Store(id)
	defer unsub()
	go s.writerLoop(handle, events, closed)

	// Per-connection context derived from the server's base context, so an
	// in-flight RPC observes daemon shutdown (Close cancels baseCtx).
	connCtx, cancel := context.WithCancel(s.baseCtx)
	defer cancel()

	sc := NewScanner(c)
	for sc.Scan() {
		line := sc.Bytes()
		s.handleLine(connCtx, handle, line)
	}
	if err := ScanErr(sc.Err()); err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, derr.ErrRequestTooLarge) {
			if b, mErr := MarshalError(json.RawMessage(`null`), derr.ErrRequestTooLarge); mErr == nil {
				_ = handle.write(b)
			} else {
				s.log.Error("marshal request-too-large response failed", "err", mErr)
			}
		} else {
			s.log.Warn("scan error on IPC connection", "err", err)
		}
	}
}

func (s *Server) handleLine(ctx context.Context, h *connHandle, line []byte) {
	req, perr := DecodeRequest(line)
	if perr != nil {
		// JSON-RPC §5: id=null only when id detection itself failed (parse
		// error). For semantic failures (bad jsonrpc field, empty method) we
		// echo the parsed id so the client can correlate the error.
		id := req.ID
		if len(id) == 0 {
			id = json.RawMessage(`null`)
		}
		if b, mErr := MarshalError(id, derr.AsDerr(perr)); mErr == nil {
			_ = h.write(b)
		} else {
			s.log.Error("marshal decode-error response failed", "err", mErr)
		}
		return
	}
	if h.rate != nil {
		ctx = context.WithValue(ctx, ctxKeyConnRate{}, h.rate)
	}
	if req.IsNotification() {
		// JSON-RPC §4.1: servers MUST NOT respond to notifications. Plan 1
		// has no client→server notifications, but a spec-compliant client
		// could legitimately send one — execute (best-effort) and discard.
		_, _ = s.dispatch.handle(ctx, req)
		return
	}
	result, derrVal := s.dispatch.handle(ctx, req)
	if derrVal != nil {
		if b, mErr := MarshalError(req.ID, derrVal); mErr == nil {
			_ = h.write(b)
		} else {
			s.log.Error("marshal dispatch-error response failed", "err", mErr, "method", req.Method)
		}
		return
	}
	if b, mErr := MarshalResponse(req.ID, result); mErr == nil {
		_ = h.write(b)
	} else {
		s.log.Error("marshal response failed", "err", mErr, "method", req.Method)
	}
}

// writerLoop pumps subscriber events to the wire under handle.write's mutex.
// Exits cleanly when closed fires (subscriber.unsub) or when a write fails.
// Selecting on both events and closed prevents the goroutine leak that would
// happen if we only ranged over events (events is never closed by unsub).
func (s *Server) writerLoop(h *connHandle, events <-chan Event, closed <-chan struct{}) {
	for {
		select {
		case <-closed:
			return
		case evt := <-events:
			b, err := MarshalNotification(evt.Method, evt.Params)
			if err != nil {
				s.log.Warn("notification marshal failed", "method", evt.Method, "err", err)
				continue
			}
			if err := h.write(b); err != nil {
				return // connection write failed; reader loop will exit on EOF too
			}
		}
	}
}

// registerConn adds h to the live-connection set. Returns false if Close has
// already begun, in which case the caller must close its conn and bail.
func (s *Server) registerConn(h *connHandle) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closing {
		return false
	}
	s.conns[h] = struct{}{}
	return true
}

func (s *Server) unregisterConn(h *connHandle) {
	s.mu.Lock()
	delete(s.conns, h)
	s.mu.Unlock()
}

// closeBySubscriberID is the onSlowClient callback: find the connection
// associated with the given subscriber id and close it. The reader/writer
// loops then exit and clean up.
func (s *Server) closeBySubscriberID(id uint64) {
	s.mu.Lock()
	var target *connHandle
	for h := range s.conns {
		if h.id.Load() == id {
			target = h
			break
		}
	}
	s.mu.Unlock()
	if target != nil {
		_ = target.conn.Close()
	}
}

// bindControlSocket implements §8.1: stale-socket unlink, umask 0117, listen,
// post-listen chmod 0660. Returns the bound listener.
func bindControlSocket(sockPath string, log *slog.Logger) (*net.UnixListener, error) {
	if fi, err := os.Lstat(sockPath); err == nil {
		if fi.Mode()&os.ModeSocket == 0 {
			return nil, fmt.Errorf("%s exists and is not a socket; refusing to unlink", sockPath)
		}
		if err := os.Remove(sockPath); err != nil {
			return nil, fmt.Errorf("unlink stale socket: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("lstat %s: %w", sockPath, err)
	}

	// syscall.Umask is process-global, not goroutine-local. Setting it here
	// affects every file created in the process until we restore via defer.
	// SAFE BECAUSE: bindControlSocket is called from main() before the accept
	// goroutine starts; no concurrent file creation can race. If a future
	// refactor calls bindControlSocket from a non-main goroutine, revisit.
	old := syscall.Umask(0o117)
	defer syscall.Umask(old)

	addr := &net.UnixAddr{Name: sockPath, Net: "unix"}
	l, err := net.ListenUnix("unix", addr)
	if err != nil {
		return nil, fmt.Errorf("listen %s: %w", sockPath, err)
	}
	if err := os.Chmod(sockPath, 0o660); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}
	log.Info("ipc socket bound", "path", sockPath, "mode", "0660")
	return l, nil
}
