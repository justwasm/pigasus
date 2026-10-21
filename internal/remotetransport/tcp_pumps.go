// Package remotetransport — tcp_pumps.go: the per-conn outbound
// channel and the dispatcher notifier callbacks.
//
// This file owns:
//
//   - (*TCPServer).pushFrame — the dispatcher.Notifier
//     callback. Looks up the ws connection by connID and
//     enqueues the frame (one ws text message) onto its bounded
//     outbound channel.
//   - (*TCPServer).broadcastFrame — the
//     dispatcher.BroadcastNotify callback. Marshals the
//     notification once and fans it out to every open ws
//     connection under a single connsMu read-lock snapshot.
//   - (*wsConn).enqueueFrame — the shared sender-side helper.
//     Both pushFrame and dispatchOne route through this so they
//     serialise on outboundMu and respect the closing signal.
//   - (*TCPServer).dispatchOne — runs one JSON-RPC request
//     through the dispatcher and enqueues the response (if
//     any) onto the per-conn outbound channel. Runs on its own
//     goroutine, spawned from handleWSReadLoop, so the reader
//     can immediately accept the next frame.
//   - (*TCPServer).writePump — the SOLE goroutine that calls
//     (*websocket.Conn).Writer / Write / Close. Drains
//     wc.outbound. coder/websocket does not allow concurrent
//     writers, so this serialisation is what keeps the ws
//     layer happy when dispatchOne (responses) and pushFrame
//     (notifications) both want to publish frames.
//
// Every helper is ≤ 50 LOC.
package remotetransport

import (
	"context"
	"encoding/json"
	"sync/atomic"

	"github.com/coder/websocket"
)

// pushFrame is the dispatcher.Notifier callback. It looks up
// the ws connection by connID in the server's registry and
// enqueues frame (one ws text message) onto its bounded outbound
// channel. Returns:
//
//   - ErrConnNotFound when connID is unknown (the conn has been
//     closed and removed from the registry);
//   - ErrBackpressure when the channel is full (the writer is
//     behind; the frame is dropped and the conn's dropped counter
//     is incremented);
//   - ctx.Err() when ctx is cancelled before the enqueue could
//     complete (the request that triggered the push has already
//     been cancelled by the dashboard);
//   - nil on success — the frame is queued for the writer
//     goroutine.
//
// pushFrame takes the read lock on connsMu so the Shutdown
// path's snapshot is consistent with concurrent handleWS
// insert/delete. The actual enqueue happens outside the lock
// to keep the critical section short.
func (s *TCPServer) pushFrame(ctx context.Context, connID string, frame []byte) error {
	s.connsMu.RLock()
	wc, ok := s.conns[connID]
	s.connsMu.RUnlock()
	if !ok {
		return ErrConnNotFound
	}
	return wc.enqueueFrame(ctx, frame)
}

// broadcastFrame is the dispatcher.BroadcastNotify
// callback. It walks the server's full conn registry
// and enqueues frame on each open connection. Used
// by the rc-permissions-grants Set / Add / Remove
// handlers to fan a "permissions/changed"
// notification out to every open dashboard at once.
//
// The frame is built here (json.Marshal) once per
// call and reused for every receiver — a slow
// per-conn enqueue doesn't re-marshal the same JSON.
// The actual enqueue is exactly
// (*wsConn).enqueueFrame — same bounded-channel
// backpressure semantics as pushFrame, same
// ErrConnNotFound / ErrBackpressure return values.
//
// Why we marshal here rather than in the dispatcher:
// the dispatcher package cannot be imported from
// remotetransport (the cycle is broken through
// tcp_test.go's smoke test), so we can't share a
// named helper. The duplication is small — one
// map literal + a Marshal — and matches
// (*Dispatcher).Notify's wire frame byte-for-byte.
//
// Concurrency: a single read lock on connsMu holds
// for the duration of the snapshot so handleWS's
// insert / delete paths see a consistent registry.
// The per-conn enqueue happens AFTER the lock is
// released so a slow peer can't pin the broadcast.
// The per-conn enqueue itself is racing handleWS's
// delete path (the underlying wsConn may be torn
// down mid-broadcast), so the per-conn enqueue's
// own closing-awareness logic decides whether the
// frame lands or returns ErrConnNotFound.
//
// Returns nil on full success. A partial failure
// (one conn lost the frame, others didn't) is
// treated as success — the broadcaster's caller
// has no way to address the failed conn separately,
// and dropping one conn's notification is the right
// behaviour (the dashboard's next re-poll will
// catch up; it will not lose data, just a single
// notification). An "every conn returned err" run
// returns the last error so the dispatcher can log
// it.
//
// Cycle rc-permissions-grants adds this. The
// cross-cut (a future cycle's
// SetRemoteAgentFastpushEnabled toggle may want
// the same fan-out for plugin lifecycle events)
// will route through this same closure.
func (s *TCPServer) broadcastFrame(ctx context.Context, method string, params any) error {
	frame := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		frame["params"] = params
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	s.connsMu.RLock()
	conns := make([]*wsConn, 0, len(s.conns))
	for _, wc := range s.conns {
		conns = append(conns, wc)
	}
	s.connsMu.RUnlock()
	var lastErr error
	for _, wc := range conns {
		if err := wc.enqueueFrame(ctx, data); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// enqueueFrame is the shared sender-side helper. Both the
// dispatch goroutine (for synchronous responses) and pushFrame
// (for asynchronous notifications) route through this method
// so they serialise on outboundMu and observe the same
// closing-channel signal.
//
// Contract:
//
//   - Non-blocking send. Senders that find the channel full
//     return ErrBackpressure; the frame is dropped and the
//     conn's dropped counter is incremented.
//   - Closing-aware. Once wc.closing is closed (the teardown
//     signal), enqueueFrame returns ErrConnNotFound and never
//     touches the channel.
//   - Safe under concurrent close. The pre-check on closing is
//     a cheap non-blocking select; outboundMu serialises the
//     actual send with the close(outbound) step of teardown so
//     no sender can be mid-send when the channel is closed.
//
// enqueueFrame is the only place that touches wc.outbound for
// a write (besides the close step in teardownWSConn's defer).
func (wc *wsConn) enqueueFrame(ctx context.Context, frame []byte) error {
	if wc == nil {
		return ErrConnNotFound
	}
	// Cheap pre-check on closing — non-blocking, before we
	// touch the mutex. After teardown closes closing, every
	// subsequent enqueueFrame call hits this branch and bails
	// without contending on outboundMu.
	select {
	case <-wc.closing:
		return ErrConnNotFound
	default:
	}
	wc.outboundMu.Lock()
	defer wc.outboundMu.Unlock()
	// Re-check under the lock; closing may have closed while
	// we were waiting for the mutex.
	select {
	case <-wc.closing:
		return ErrConnNotFound
	default:
	}
	// Non-blocking send. Cap=64 keeps the typical dashboard
	// turn in one shot; an over-budget sender falls into
	// ErrBackpressure so the caller (pushFrame or the
	// dispatch goroutine) knows the frame was dropped.
	select {
	case wc.outbound <- frame:
		return nil
	default:
		// Buffer full: honour ctx cancellation before
		// declaring backpressure, so a request whose ctx
		// is already done does not get counted as a
		// "drop".
		select {
		case <-wc.closing:
			return ErrConnNotFound
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		atomic.AddUint64(&wc.dropped, 1)
		return ErrBackpressure
	}
}

// dispatchOne runs one JSON-RPC request through the dispatcher
// and enqueues the response (if any) onto the per-conn outbound
// channel. Runs on its own goroutine, spawned from
// handleWSReadLoop's read loop, so the reader can immediately
// accept the next frame.
//
// A nil response (notification) is a no-op — JSON-RPC 2.0 §4.1
// says the server MUST NOT reply to a notification. The dispatcher's
// isNotification guard enforces this for any method, not just the
// special "notifications/initialized" case (rc-streaming-protocol
// promoted the check from a single-method special-case to the
// full dispatch).
//
// json.Marshal of a map[string]any should not fail; the check
// is defensive (a malformed response is silently dropped — the
// conn may be dying by the time we'd send it).
func (s *TCPServer) dispatchOne(wc *wsConn, reqCtx context.Context, raw []byte) {
	resp := s.dispatcher.Dispatch(reqCtx, raw)
	if resp == nil {
		return
	}
	data, err := json.Marshal(resp)
	if err != nil {
		// Should not happen for a map[string]any built by the
		// dispatcher; drop the frame on the (unlikely) failure
		// path. The conn may already be tearing down.
		return
	}
	_ = wc.enqueueFrame(reqCtx, data)
}

// writePump drains wc.outbound and writes one ws text message
// per frame. It is the SOLE goroutine that calls (*websocket.Conn).
// Writer / Writer.Write / Writer.Close — coder/websocket does
// not allow concurrent writers, so this serialisation is what
// keeps the ws layer happy when the dispatch goroutine (responses)
// and pushFrame (notifications) both want to publish frames.
//
// writePump exits when:
//
//   - wc.outbound is closed (teardownWSConn's defer);
//   - any per-frame ws operation fails (the conn is dying — the
//     library returns an error from Writer, Write, or Close);
//   - the writer's ctx is cancelled (writeCancel — teardownWSConn
//     calls writeCancel before closeOnce(Close)). A pending
//     Writer/Write bails with ctx.Err().
//
// The writer's lifetime is tracked by s.wg (one Add inside
// serveWSConn, one Done via this defer), so TCPServer.Shutdown's
// s.wg.Wait blocks until the writer has drained.
func (s *TCPServer) writePump(wc *wsConn, ctx context.Context) {
	defer s.wg.Done()
	for frame := range wc.outbound {
		// Each ws message requires its own Writer; reusing a
		// Writer across messages is a library-level error.
		// Writer takes the writer's per-conn ctx so
		// writeCancel aborts a pending Write with
		// ctx.Err().
		w, err := wc.conn.Writer(ctx, websocket.MessageText)
		if err != nil {
			// Conn is dying; exit. The range will
			// terminate when teardownWSConn's defer
			// closes outbound.
			return
		}
		if _, err := w.Write(frame); err != nil {
			_ = w.Close()
			return
		}
		if err := w.Close(); err != nil {
			// Close commits the frame; failure here
			// means the ws is gone.
			return
		}
	}
}
