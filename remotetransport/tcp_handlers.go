// Package remotetransport — tcp_handlers.go: the WS handler
// registered at cfg.Path, plus its per-connection helpers.
//
// handleWS is split into:
//
//   - handleWS (the http.HandlerFunc registered at cfg.Path;
//     origin gate + ws.Accept + setup + delegate to serveWSConn).
//   - serveWSConn (mint connID + per-conn state + spawn
//     writePump + run the read loop; owns the deferred
//     teardown).
//   - handleWSReadLoop (the per-conn for-range over c.Read that
//     spawns one dispatcher goroutine per ws message —
//     rc-bidi-loop's "free reader for parked-stub round-trip"
//     invariant).
//   - teardownWSConn (the close(closing) → writeCancel →
//     closeOnce(Close) → outboundMu + close(outbound) →
//     readCancel → deregister sequence — pulled out so the
//     serveWSConn body is readable).
//
// Every helper is ≤ 50 LOC.
package remotetransport

import (
	"context"
	"fmt"
	"net/http"

	"github.com/coder/websocket"
)

// handleWS upgrades the HTTP request to a WebSocket and serves one
// client. The orchestrator here only handles the origin gate, the
// WS upgrade, and the per-conn notification bookkeeping; the
// per-connection state machine lives in serveWSConn.
//
// Cycle rc-bidi-loop splits the per-connection work into:
//
//  1. A reader goroutine (serveWSConn / handleWSReadLoop) reads
//     one JSON-RPC request per ws message and spawns one
//     dispatcher goroutine per request, so the reader can
//     immediately pick up the next frame. This is the
//     round-trip gap rc-handle-user-interaction left behind:
//     with the single-goroutine read-then-dispatch loop, the
//     server could not read a follow-up
//     HandleRemoteAgentCascadeUserInteraction while a
//     SendRemoteAgentCascadeMessage was parked on
//     intr.ResultCh. The reader now stays free.
//  2. A writePump goroutine (spawned inside serveWSConn) drains
//     a bounded per-conn channel and writes one ws text
//     message per frame. All outbound frames — synchronous
//     responses from the dispatcher goroutines +
//     asynchronous notifications from pushFrame — share this
//     channel so the writer is the sole caller of
//     (*websocket.Conn).Writer (coder/websocket does not
//     allow concurrent writers).
//  3. Close coordination on return: signal senders via
//     wc.closing, cancel the writer's ctx, close the ws
//     (once, via closeOnce), close the outbound channel
//     (under outboundMu so concurrent senders cannot be
//     mid-send), and deregister from the server's registry
//     — all owned by teardownWSConn below.
//
// Wire format matches the Unix-socket transport byte-for-byte
// modulo the framing (ws messages vs '\n' lines).
//
// handleWS is intentionally not concurrency-bounded — http.Server
// dispatches each request on its own goroutine, so multiple clients
// share the listener concurrently. The dispatcher is safe for
// concurrent calls (every handler is read-only on its own cfg
// field, and SetTurnOutput-style writers are mutex-guarded inside
// SessionState).
//
// Cycle rc-auth-tokens: handleWS consults the
// server's active OriginPolicy via (*TCPServer).handleOriginCheck
// BEFORE the WS upgrade. A denier returns
// HTTP 403 + a JSON body + the conn closed; an allow falls
// through to the existing websocket.Accept path.
func (s *TCPServer) handleWS(w http.ResponseWriter, r *http.Request) {
	s.wg.Add(1)
	defer s.wg.Done()

	// Origin check before the WS upgrade — rc-auth-tokens.
	// Deny returns 403 + a JSON body and bails without
	// touching coder/websocket's Accept. The response
	// writer is handed to writeOriginForbidden (in
	// tcp_origin.go) which produces a single Write
	// followed by no further access.
	originDecision := s.handleOriginCheck(r)
	if !originDecision.Allowed {
		writeOriginForbidden(w, originDecision)
		return
	}

	// Secret check before the WS upgrade — rc-auth-bearer-required.
	// The order matters: the Origin gate is the cheap one and the
	// browser-shaped rule, the secret is the one that answers the
	// non-browser client that sends no Origin at all. A denied
	// request gets HTTP 401 + a JSON body and never reaches
	// coder/websocket's Accept.
	authDecision := s.handleTokenCheck(r)
	if !authDecision.Allowed {
		s.writeTokenUnauthorizedFor(w, authDecision)
		return
	}

	c, err := websocket.Accept(w, r, acceptOriginOptions())
	if err != nil {
		// Accept already writes the HTTP error response; nothing
		// more for us to do.
		return
	}
	// Limit a single message to 16 MiB to match the Unix-socket
	// transport's bufio.Scanner buffer ceiling.
	c.SetReadLimit(16 << 20)

	s.dispatcher.MarkAccepted()
	s.dispatcher.NotifyConnected()
	defer s.dispatcher.NotifyDisconnected()

	s.serveWSConn(r.Context(), c)
}

// acceptOriginOptions returns the AcceptOptions handleWS
// passes to websocket.Accept. Extracted so handleWS's body
// fits under the 50-LOC function ceiling while keeping the
// long rc-auth-tokens rationale attached to the option it
// documents.
//
// The library's own origin verification
// (authenticateOrigin) is disabled because
// (*TCPServer).handleOriginCheck (called before Accept) is
// the canonical gatekeeper — the library's built-in check
// has no knowledge of the brief-pinned allowlist (it would
// reject every non-empty Origin with 403). Pre-cycle the
// clone ran with InsecureSkipVerify:false and a permissive
// `OriginPatterns` (matching the reference's local-process
// semantics); now that the dispatcher mutates
// (*TCPServer).originPolicy via SetOriginPolicySetter, we
// own the policy in one place.
func acceptOriginOptions() *websocket.AcceptOptions {
	return &websocket.AcceptOptions{
		InsecureSkipVerify: true,
	}
}

// serveWSConn owns the per-connection state machine: it mints a
// stable connID, builds the wsConn struct, registers it in the
// server's conn map, spawns the writePump goroutine, registers a
// teardownWSConn defer, then hands off to handleWSReadLoop for
// the read side.
//
// r.Context() is the parent of the per-conn read + write contexts
// so a server-side request cancellation still propagates to
// in-flight dispatch goroutines via WithConnID.
func (s *TCPServer) serveWSConn(parent context.Context, c *websocket.Conn) {
	// Mint a stable connID so dispatcher.Notify can address
	// this connection. Stored in the server's registry for the
	// duration of serveWSConn; removed by the deferred
	// teardown below. The leading "ws-" tag scopes future
	// multi-transport daemons — they can use "unix-1" / "ws-1"
	// / "tls-1" without collision.
	connID := fmt.Sprintf("ws-%d", s.nextConnID.Add(1))

	// Build the per-conn state. writePump uses a separate ctx
	// so teardown can cancel it independently of the reader's
	// ctx; both inherit from r.Context() so a server-side
	// request cancellation still propagates to in-flight
	// dispatch goroutines via WithConnID.
	readCtx, readCancel := context.WithCancel(parent)
	writeCtx, writeCancel := context.WithCancel(context.Background())
	wc := &wsConn{
		id:          connID,
		conn:        c,
		outbound:    make(chan []byte, outboundChCap),
		closing:     make(chan struct{}),
		writeCancel: writeCancel,
	}
	s.connsMu.Lock()
	s.conns[connID] = wc
	s.connsMu.Unlock()

	// Spawn the writer goroutine. writePump is the SOLE caller
	// of c.Writer; coder/websocket does not allow concurrent
	// writers. s.wg tracks the writer too so Shutdown waits
	// for it to drain alongside handleWS.
	s.wg.Add(1)
	go s.writePump(wc, writeCtx)

	defer s.teardownWSConn(wc, c, connID, readCancel)
	s.handleWSReadLoop(wc, readCtx, connID)
}

// handleWSReadLoop is the per-conn read pump. It pulls one ws
// message at a time off c.Read and spawns one dispatch goroutine
// per request so the reader can immediately accept the next
// frame — this is the rc-bidi-loop "free reader" invariant.
//
// Cycle rc-cascade-stream: thread the per-conn connID through
// the request context so the dispatcher's cascade handler can
// route mid-turn notifications back to this ws connection. The
// wrap is per-request (not at handleWS entry) because the
// dispatcher internally derives a per-request context anyway,
// and per-request wrapping is what makes a slow client's
// connID stale across requests impossible.
//
// A close error from the underlying c.Read exits the loop
// quietly — the deferred teardown in serveWSConn does the
// actual close coordination.
func (s *TCPServer) handleWSReadLoop(wc *wsConn, ctx context.Context, connID string) {
	for {
		_, raw, err := wc.conn.Read(ctx)
		if err != nil {
			// Closed by client or context cancelled — exit quietly.
			return
		}
		reqCtx := s.dispatcher.WithConnID(ctx, connID)
		// Cycle rc-bidi-loop: dispatch on its own
		// goroutine so the reader can immediately
		// pick up the next frame. This is the round-
		// trip the single-goroutine rc-handle-user-
		// interaction loop could not support. The
		// dispatch goroutine marshals the response
		// (or skips it on a notification) and pushes
		// the bytes onto wc.outbound; writePump
		// delivers the bytes in order. In-flight
		// dispatch goroutines see readCtx done when
		// the reader exits (via the deferred cancel
		// above) and bail in enqueueFrame.
		go s.dispatchOne(wc, reqCtx, raw)
	}
}

// teardownWSConn is the close coordination handleWS's defer
// runs. Order is pinned by the cycle brief:
//
//  1. close(closing) — signal senders (pushFrame and any
//     in-flight dispatch goroutine) that the conn is dying.
//     enqueueFrame's pre-check sees this and bails.
//  2. writeCancel — cancel the writer's per-conn ctx so a
//     pending Write bails with ctx.Err() instead of blocking on
//     a half-closed peer.
//  3. closeOnce(c.Close) — kill the ws. Safe against
//     Shutdown's parallel closeOnce.Do (only one wins).
//  4. close(outbound) under outboundMu — let writePump's
//     range exit. outboundMu serialises with any sender that
//     already passed the closing pre-check, so we never panic
//     with "send on closed channel".
//  5. cancel the reader's per-conn ctx so c.Read returns
//     promptly if it hasn't already.
//  6. Deregister from the server's conn map.
func (s *TCPServer) teardownWSConn(wc *wsConn, c *websocket.Conn, connID string, readCancel context.CancelFunc) {
	close(wc.closing)
	wc.writeCancel()
	wc.closeOnce.Do(func() {
		_ = c.Close(websocket.StatusNormalClosure, "")
	})
	wc.outboundMu.Lock()
	close(wc.outbound)
	wc.outboundMu.Unlock()
	readCancel()
	s.connsMu.Lock()
	delete(s.conns, connID)
	s.connsMu.Unlock()
}
