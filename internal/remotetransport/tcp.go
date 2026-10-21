// Package remotetransport — tcp.go: the TCP+WebSocket JSON-RPC
// transport's package-glue surface.
//
// Cycle rc-ws-transport adds this adapter so the same Dispatcher
// can be served over a TCP port with a WebSocket upgrade on the
// /rpc path. The reference (Antigravity CLI's Remote Control) does
// the same: a single ws endpoint that takes JSON-RPC frames and
// returns JSON-RPC frames, one per ws message.
//
// Why a separate adapter rather than reusing the Unix-socket one:
//
//   - The framing is different. Unix-socket transport uses
//     newline-delimited JSON-RPC over a raw net.Conn; the WebSocket
//     transport uses ws messages (each message is a single JSON-RPC
//     frame, ws handles the framing).
//
//   - The lifecycle is different. The Unix-socket transport has one
//     accept loop on a single net.Listener; the TCP+WS transport
//     uses net/http with http.ServeMux, which multiplexes
//     connections across the listener and routes the /rpc path to
//     our handler.
//
// Cycle rc-bidi-loop splits handleWS into:
//
//   - a reader goroutine that reads incoming JSON-RPC requests
//     and spawns one dispatcher goroutine per request (so the
//     reader can immediately pick up the next frame — the
//     behaviour rc-handle-user-interaction's parked-stub path
//     needs to round-trip HandleRemoteAgentCascadeUserInteraction
//     on the same ws connection).
//   - a writer goroutine that drains a bounded per-conn channel
//     and writes one ws text message per frame. All outbound
//     frames (synchronous responses from the dispatcher +
//     asynchronous notifications from pushFrame) go through the
//     same channel, so the writer is the sole caller of
//     (*websocket.Conn).Writer — coder/websocket does not allow
//     concurrent writers.
//
// Wire protocol shape (per ws message):
//
//	{ "jsonrpc": "2.0", "id": <any>, "method": "<name>", "params": {…} }
//
// Response (one ws message per request):
//
//	{ "jsonrpc": "2.0", "id": <any>, "result": … | "error": {…} }
//
// Server-push notifications (rc-streaming-protocol):
//
//	{ "jsonrpc": "2.0", "method": "<name>", "params": {…} }
//
// (no "id" — JSON-RPC 2.0 §4.1 Notification; the receiver MUST NOT
// reply.)
//
// Subprotocols, ping/pong cadence, and close-handshake semantics
// are handled by github.com/coder/websocket — we do not hand-roll
// any of those.
//
// File topology (cycle rc-filesize-internal-remotetransport
// splits the original 1171-LOC monolith along feature-cohesion
// boundaries; this file is the slim "types + small accessors"
// surface, the others split out per concern):
//
//   - tcp.go           — TCPServer / wsConn types, TCPConfig,
//     outboundChCap, sentinel errors, package-
//     glue accessors (Addr / HTTPURL /
//     ConnCount / DroppedFrames / Accepted)
//   - tcp_server.go    — NewTCPServer + Shutdown (orchestrators +
//     config defaulting + listener bind + the
//     closeAllConns / shutdownHTTPServer
//     helpers)
//   - tcp_handlers.go  — handleWS orchestrator + the per-conn
//     serveWSConn / handleWSReadLoop /
//     teardownWSConn helpers (the WS upgrade +
//     read-loop path)
//   - tcp_origin.go    — OriginDecision + handleOriginCheck +
//     SetOriginPolicy + OriginPolicySnapshot +
//     the writeOriginForbidden 403 helper
//   - tcp_pumps.go     — pushFrame + broadcastFrame (dispatcher
//     notifier callbacks) + enqueueFrame +
//     writePump + dispatchOne (the outbound
//     channel and per-frame ws writer)
package remotetransport

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"

	"github.com/justwasm/pigasus/internal/originpolicy"
)

// outboundChCap is the per-conn bounded-channel capacity for
// frames the writer goroutine drains. Sized so a typical
// dashboard turn (one cascade/state starting + one cascade/text_delta
// chunk + one cascade/state done + the synchronous end-of-turn
// envelope) fits without backpressure, while a pathological
// slow-peer still loses frames (counted in (*TCPServer).DroppedFrames)
// rather than blocking every server goroutine that publishes
// notifications. cap=64 is the value the cycle brief pins.
const outboundChCap = 64

// TCPConfig is the start-time wiring for a TCP+WebSocket transport.
// The transport brings up an HTTP server on Addr that registers an
// upgrader on Path (default "/rpc"); HTTP requests to any other path
// get a 404.
type TCPConfig struct {
	// Addr is the bind address. Use "0.0.0.0:0" to let the OS pick a
	// free port (the bound address is then available via
	// (*TCPServer).Addr).
	Addr string
	// Path is the URL path the WebSocket upgrader is registered on.
	// Defaults to "/rpc" — matches the reference's endpoint and is
	// what the README / cycle report will document.
	Path string
	// HandshakeTimeout caps how long a single Accept call may take.
	// Defaults to 0 (no explicit cap; the OS-default is fine for
	// local development). Set explicitly when the server runs on a
	// slow / contested network.
	HandshakeTimeout time.Duration
	// ReadTimeout is the per-message read deadline. Defaults to 0
	// (no deadline; the daemon is long-lived, the OS-level TCP keepalive
	// handles dead peers).
	ReadTimeout time.Duration
	// WriteTimeout is the per-message write deadline. Same default
	// rationale as ReadTimeout.
	WriteTimeout time.Duration
	// OriginPolicy is the snapshot the WS upgrade consults on
	// every Accept. nil (the default) means "allow all origins" —
	// matches the pre-rc-auth-tokens semantics. Cycle
	// rc-auth-tokens adds this. Production wires
	// originpolicy.DefaultOriginPolicy() (or
	// originpolicy.LoadOriginPolicyFromEnv()) here at construction
	// time so the first Accept already sees the canonical
	// allowlist. Tests can pass a custom policy via
	// NewTestTCPServerWithOrigin (a thin wrapper) or by calling
	// (*TCPServer).SetOriginPolicy after construction.
	OriginPolicy *originpolicy.OriginPolicy
}

// TCPServer is the running TCP+WebSocket JSON-RPC server. Construct
// with NewTCPServer, call Shutdown to tear down. A non-nil error
// from NewTCPServer means the listener never came up.
type TCPServer struct {
	cfg        TCPConfig
	dispatcher Dispatcher
	httpServer *http.Server
	listener   net.Listener
	mux        *http.ServeMux

	// connsMu guards conns. RCU-style read-heavy access (every
	// Notify walks the map; every Shutdown iterates once) makes
	// an RWMutex cheap enough.
	connsMu sync.RWMutex
	// conns is the registry of open ws connections, keyed by
	// the connID string the dispatcher sees on Notify. Each
	// entry is the per-conn struct that holds the live
	// websocket.Conn and the connID label. The map is mutated
	// only by handleWS (insert on entry, delete on exit) and
	// read by pushFrame / Shutdown. Size = number of currently
	// open ws clients, typically 0 or 1 in steady state but
	// can spike during the rc-cascade-stream cycle's
	// broadcast-to-N-dashboards path.
	conns map[string]*wsConn

	// setOriginPolicyMu guards the originPolicy field. Reads
	// take the read lock; SetOriginPolicy takes the write
	// lock. A shared mutex (rather than atomic.Pointer) keeps
	// the contract with the future rc-auth-bearer-required
	// cycle's bearer-token check uniform — both reader paths
	// happen on the Accept hot loop where a single per-Accept
	// critical section is fine. Cycle rc-auth-tokens adds
	// this.
	setOriginPolicyMu sync.RWMutex
	// originPolicy is the active OriginPatterns allowlist
	// the WS upgrade consults. nil → "allow all" (the
	// pre-cycle default; matches the existing
	// InsecureSkipVerify:false semantic for non-browser
	// callers). Cycle rc-auth-tokens adds this. Production
	// wires originpolicy.DefaultOriginPolicy() through
	// (*RemoteControlOriginPolicyStore).SetPatterns at start;
	// a SetRemoteControlOriginPatterns RPC mutates the field
	// via (*TCPServer).SetOriginPolicy on every successful
	// dispatch.
	originPolicy *originpolicy.OriginPolicy
	// nextConnID mints stable, monotonically-increasing IDs for
	// new connections. Atomic so NewTCPServer's notifier-setup
	// path and handleWS don't have to coordinate. Format:
	// "ws-<N>" (the leading "ws-" lets a future cycle scope by
	// transport if the daemon ever mixes transports in one
	// process).
	nextConnID atomic.Uint64

	wg       sync.WaitGroup
	stopOnce sync.Once
}

// wsConn is one open WebSocket connection inside TCPServer.
//
// Field-level synchronisation contract (the cycle rc-bidi-loop
// constraint that requires every field here to be either
// goroutine-private or guarded):
//
//   - id, conn: immutable after construction (conn is the
//     underlying *websocket.Conn). coder/websocket declares
//     "All methods may be called concurrently except for Reader
//     and Read"; we honour that by routing every Write through
//     the single writePump goroutine spawned in handleWS.
//   - outbound: the bounded channel writePump drains and the
//     dispatcher / pushFrame goroutines enqueue to. Capacity is
//     outboundChCap (64); senders that find the channel full
//     receive ErrBackpressure and increment dropped. outbound is
//     closed exactly once during teardown, under outboundMu, so
//     a concurrent sender holding the mutex cannot be mid-send
//     when close(outbound) runs.
//   - closing: closed exactly once before outbound as the
//     "conn is dying" signal. Senders check this in a
//     non-blocking select, then again under outboundMu, so the
//     signal always wins against an in-flight enqueue.
//   - writeCancel: cancels writePump's per-conn ctx so a
//     pending Write bails with ctx.Err() instead of blocking
//     forever on a half-closed peer. Called once during
//     teardown.
//   - closeOnce: makes (*websocket.Conn).Close idempotent.
//     Both Shutdown's per-conn goroutine and handleWS's
//     deferred teardown try to close the conn; closeOnce
//     guarantees only one wins.
//   - outboundMu: serialises senders with the close(outbound)
//     call so we never panic with "send on closed channel".
//     Held briefly during enqueueFrame (just around the
//     channel send) and during the close(outbound) step of
//     teardown.
//   - dropped: count of frames lost to ErrBackpressure.
//     Read via (*TCPServer).DroppedFrames(connID).
type wsConn struct {
	id          string
	conn        *websocket.Conn
	outbound    chan []byte
	closing     chan struct{}
	closeOnce   sync.Once
	writeCancel context.CancelFunc
	outboundMu  sync.Mutex
	dropped     uint64
}

// ErrConnNotFound is returned by the dispatcher's notifier when
// connID doesn't match any open ws connection. Surfaced so the
// caller can drop server-push subscriptions for a closed client
// rather than retrying forever.
var ErrConnNotFound = errors.New("remotetransport: connection not found")

// ErrBackpressure is returned by enqueueFrame / pushFrame when
// the per-conn outbound channel is full. The frame is dropped
// (not blocked on) and the wsConn's dropped counter is
// incremented so a future status tool can surface the metric.
//
// Cycle rc-bidi-loop ships this as the bounded-channel overflow
// signal: it replaces the rc-cascade-stream "best-effort drop on
// ws write error" with a deterministic, countable drop on
// enqueue. Callers that care (a future per-conn back-pressure
// dashboard field, or a cycle that wants to slow down the
// dispatcher) inspect the error via errors.Is(err,
// ErrBackpressure).
var ErrBackpressure = errors.New("remotetransport: outbound channel full")

// Addr returns the bound TCP address. Useful when the original
// config passed "0.0.0.0:0" — the OS-assigned port lives here, and
// any client that wants to connect must read Addr() to find it.
func (s *TCPServer) Addr() net.Addr {
	if s == nil || s.listener == nil {
		return nil
	}
	return s.listener.Addr()
}

// HTTPURL returns the ws:// URL pointing at the upgrader endpoint
// (cfg.Path), so a test client can pass it straight into
// websocket.Dial. Returns "" if the bound address isn't a *net.TCPAddr.
func (s *TCPServer) HTTPURL() string {
	if s == nil {
		return ""
	}
	tcp, ok := s.listener.Addr().(*net.TCPAddr)
	if !ok {
		return ""
	}
	host := tcp.IP.String()
	if host == "" || host == "0.0.0.0" || host == "::" {
		// websocket.Dial rejects the unspecified address; rewrite to
		// loopback so tests on 127.0.0.1:0 don't trip the parser.
		host = "127.0.0.1"
	}
	return "ws://" + net.JoinHostPort(host, fmt.Sprintf("%d", tcp.Port)) + s.cfg.Path
}

// connCount returns the number of currently-open ws connections.
// Test-only helper — exposed via the same package so the cycle's
// backpressure and writer-exit tests can locate the connID
// without poking at s.conns directly.
func (s *TCPServer) connCount() int {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()
	return len(s.conns)
}

// ConnCount is the exported version of connCount.
// It returns the number of currently-open ws
// connections on this server. Cycle rc-top-level-rpc
// adds this so the dispatcher's RemoteControlStatus
// handler can include `connected_clients` in its
// snapshot without poking at the TCPServer's
// internals across the package boundary.
//
// Nil-safe: a nil receiver returns 0.
//
// Reads the same s.conns map under the same
// s.connsMu RLock that connCount does — there's no
// separate counter because the map's len is the
// single source of truth and the per-conn lock
// patterns don't double-count.
func (s *TCPServer) ConnCount() int {
	if s == nil {
		return 0
	}
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()
	return len(s.conns)
}

// firstConnID returns the connID of the (assumed unique) open
// connection, or "" when no connection is open. Test-only
// helper — the cycle's backpressure test uses this to address
// the conn it just dialled without exposing the full
// (*TCPServer).conns map.
func (s *TCPServer) firstConnID() string {
	s.connsMu.RLock()
	defer s.connsMu.RUnlock()
	for id := range s.conns {
		return id
	}
	return ""
}

// pushFrameForTest is the test-only direct-enqueue helper.
// Wraps (*wsConn).enqueueFrame so tests can fill the bounded
// outbound channel without dispatching a real JSON-RPC request.
// Production code uses (*TCPServer).pushFrame (the dispatcher
// notifier callback).
func (s *TCPServer) pushFrameForTest(ctx context.Context, connID string, frame []byte) error {
	s.connsMu.RLock()
	wc, ok := s.conns[connID]
	s.connsMu.RUnlock()
	if !ok {
		return ErrConnNotFound
	}
	return wc.enqueueFrame(ctx, frame)
}

// DroppedFrames returns the cumulative count of frames lost to
// ErrBackpressure on the given conn. Returns 0 when connID is
// unknown (the conn has been closed and removed). Used by tests
// and (in a future cycle) a per-conn backpressure metric on the
// status tool.
//
// Reads wc.dropped atomically; safe to call concurrently with
// the dispatch goroutines and pushFrame. The counter is
// monotonic for the lifetime of the conn — it never resets,
// even after Shutdown.
func (s *TCPServer) DroppedFrames(connID string) uint64 {
	s.connsMu.RLock()
	wc, ok := s.conns[connID]
	s.connsMu.RUnlock()
	if !ok {
		return 0
	}
	return atomic.LoadUint64(&wc.dropped)
}

// Accepted is a convenience accessor — delegates to the shared
// dispatcher. The TCP+WebSocket transport's connection counter is
// bumped from inside handleWS so a dashboard's status tool sees the
// same counter regardless of which transport accepted the dial.
func (s *TCPServer) Accepted() uint64 { return s.dispatcher.Accepted() }
