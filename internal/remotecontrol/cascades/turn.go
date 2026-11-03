// Package cascades — turn.go: the cascade's
// terminal-output / reply-state helpers and the streaming
// SendStream pipeline. Pure file split; the methods here
// were previously inline in cascade.go.
package cascades

import (
	"context"
	"errors"
	"time"
)

// LastReplySnapshot returns the cascade's LastReply verbatim.
// Read-only (the field is replaced atomically inside setReply
// and accumulated inside AppendReplyText). Nil-safe: a nil
// *Cascade returns "". Surfaced for future rc-status-tool
// cycles' per-cascade status dump.
func (c *Cascade) LastReplySnapshot() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.LastReply
}

// setReply replaces the cascade's LastReply with reply. The
// synchronous (*Registry).Send success path calls this
// after the backend returns the final reply. The streaming
// ConnNotifierSink uses AppendReplyText instead (so each
// OnTextDelta accumulates); the two paths converge because
// the streaming sink's OnState(phase=done) doesn't touch
// LastReply — by that point the accumulated text IS the final
// reply.
//
// Nil-safe: a nil *Cascade is a no-op. Concurrent with
// LastReplySnapshot / AppendReplyText under the per-cascade
// mu.
func (c *Cascade) setReply(reply string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.LastReply = reply
	c.mu.Unlock()
}

// AppendReplyText accumulates a mid-turn text delta into
// LastReply. The streaming ConnNotifierSink calls this on every
// OnTextDelta. Nil-safe and empty-safe: a nil *Cascade is a
// no-op; an empty delta is also a no-op (avoids spurious
// whitespace turns on stubs that emit trailing empties).
//
// Concurrency: takes the per-cascade mu briefly. Multiple
// appenders within a single turn are serialised by the mu;
// the brief's "single goroutine drives the turn" rule means
// there's only one appender in practice.
func (c *Cascade) AppendReplyText(delta string) {
	if c == nil || delta == "" {
		return
	}
	c.mu.Lock()
	c.LastReply = c.LastReply + delta
	c.mu.Unlock()
}

// Send drives one cascade turn end-to-end. It is the
// in-process face of SendRemoteAgentCascadeMessage — the
// dispatcher's handler parses params, builds the
// Prompt (with per-turn overrides applied against the
// cascade's pinned values), and hands the work to this
// method.
//
// Error mapping:
//
//   - cascade id not present → ErrNotFound
//     (dispatcher surfaces as JSON-RPC -32004)
//   - cascade.Closed → ErrClosed
//     (dispatcher surfaces as JSON-RPC -32005)
//   - opts.Timeout elapses before the backend returns →
//     ErrTurnTimeout wrapping context.DeadlineExceeded
//     (dispatcher surfaces as JSON-RPC -32010)
//   - backend returns any other error → ErrTurnFailed
//     wrapping the original (dispatcher surfaces as
//     JSON-RPC -32603 with the original message in `data`)
//
// Concurrency: the registry lookups take r.mu briefly (RLock
// for the existence check; the cascade is then released and
// its own mu locks the per-cascade mutation). The backend
// Send call does NOT hold any registry lock, so a slow
// backend does not serialise other operations on other
// cascades in the same registry.
//
// Send is nil-safe at the receiver level (a nil
// *Registry returns ErrNotFound).
//
// Cycle rc-cascade-stream: Send is now a thin shim around
// SendStream (passes a nil sink). The streaming code path
// lives in SendStream; Send exists for cycle-3a's tests and
// any future caller that wants the sync-only shape.
func (r *Registry) Send(ctx context.Context, cascadeID string, prompt Prompt, opts SendOptions) (Result, error) {
	return r.SendStream(ctx, cascadeID, prompt, nil, opts)
}

// SendStream is the cycle-rc-cascade-stream face of
// SendRemoteAgentCascadeMessage. Same surface as Send
// (cascade lookup + closed check + timeout + error mapping)
// plus a sink parameter that the registry hands to the
// backend via the StreamingBackend type assertion.
//
// Type-assertion fallback: when the cascade's backend does
// not implement StreamingBackend, the registry calls
// backend.Send (the cycle-3a path) and silently ignores the
// sink.
//
// Sink contract:
//
//   - sink may be nil (the cycle-3a Send shim passes nil).
//   - When sink != nil and the backend implements
//     SendStream, the backend is responsible for emitting
//     events. The ConnNotifierSink this cycle ships updates
//     cascade.LastReply / LastReplyAt as a side effect; the
//     cascade-state stamp at the bottom of this method
//     re-stamps LastReplyAt / ConversationID regardless so a
//     backend that uses a non-default sink still updates
//     the cascade state.
//
// Concurrency / nil-safety: same as Send. The error mapping
// is identical so the dispatcher's switch doesn't need to
// know which path produced the error.
//
// Cycle rc-cancel-cascade: when the cancel func fires
// (cascade.Cancel), the derived ctx propagates ctx.Canceled
// back to the backend's SendStream loop. The loop checks
// ctx.Done() between events and returns partial result +
// ctx.Err(); SendStream then maps that to
// ErrTurnCancelled via the IsCancelled check.
//
// Cycle rc-filesize-internal-remotecontrol extracts the per
// sub-task helpers (prepareStreamCtx, runBackendTurn,
// mapStreamError, stampSuccessState) so this method stays
// under the 50-LOC ceiling the brief pins.
func (r *Registry) SendStream(ctx context.Context, cascadeID string, prompt Prompt, sink EventSink, opts SendOptions) (Result, error) {
	if r == nil {
		return Result{}, ErrNotFound
	}
	c, ok := r.lookupStreamCascade(cascadeID)
	if !ok {
		return Result{}, ErrNotFound
	}
	if c.IsClosed() {
		return Result{}, ErrClosed
	}
	timeoutCtx, cancelTimeout := r.applyStreamTimeout(ctx, opts)
	defer cancelTimeout()
	turnCtx := c.StartCancel(timeoutCtx)
	defer c.StopCancel()
	// The interactive tools ask the user through the turn's
	// interaction host: the host publishes the waiting step the Web
	// UI's card reads, and blocks until the user answered.
	//
	// The host belongs to the Web UI's surface, so the domain asks
	// the sink whether it can install one rather than importing
	// that surface. A sink with no host to install — the legacy
	// JSON-RPC surface's sink — leaves the turn headless.
	if installer, ok := sink.(InteractionHostInstaller); ok {
		turnCtx = installer.InstallInteractionHost(turnCtx, c, sink)
	}
	res, err := r.runStreamBackend(turnCtx, c, prompt, sink, opts)
	if err != nil {
		return Result{}, r.mapStreamError(c, err, timeoutCtx, opts)
	}
	r.stampStreamSuccess(c, res)
	return res, nil
}

// lookupStreamCascade is the per-SendStream cascade lookup
// helper. Splits the read-lock acquisition out of the main
// method body so the error mapping in the call site stays
// compact.
func (r *Registry) lookupStreamCascade(cascadeID string) (*Cascade, bool) {
	r.mu.RLock()
	c, ok := r.cascades[cascadeID]
	r.mu.RUnlock()
	return c, ok
}

// applyStreamTimeout derives a child ctx with opts.Timeout if
// the caller passed a positive Timeout value; returns the
// original ctx + a no-op cancel when Timeout<=0.
func (r *Registry) applyStreamTimeout(parent context.Context, opts SendOptions) (context.Context, context.CancelFunc) {
	if opts.Timeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, opts.Timeout)
}

// runStreamBackend invokes the cascade's backend through the
// StreamingBackend type-assertion when available, or
// falls back to backend.Send when the cascade's backend
// doesn't implement the streaming surface. Cycle-3a's shim
// relies on this fallback.
func (r *Registry) runStreamBackend(ctx context.Context, c *Cascade, prompt Prompt, sink EventSink, opts SendOptions) (Result, error) {
	backend := c.Backend()
	if backend == nil {
		// Defensive — New seeds a *StubBackend,
		// and SetBackend(nil) is the only way to land
		// here. The dispatcher translates this to -32603.
		return Result{}, errors.Join(ErrTurnFailed, errors.New("no backend installed"))
	}
	if sb, ok := backend.(StreamingBackend); ok {
		return sb.SendStream(ctx, prompt, sink, opts)
	}
	return backend.Send(ctx, prompt)
}

// mapStreamError normalises the backend's error into the
// JSON-RPC envelope the dispatcher surfaces. The ErrTurn*
// sentinels drive the dispatcher's switch:
//
//   - ErrTurnCancelled wraps a dashboard-driven cancel
//     (cascade.Cancel was called) so the dispatcher can
//     distinguish a user cancel from a transport-driven
//     ctx cancel (ws close).
//   - ErrTurnTimeout wins over generic ErrTurnFailed when
//     the per-turn Timeout fired (sendCtx.Err() is
//     DeadlineExceeded).
//   - Anything else becomes ErrTurnFailed, which the
//     dispatcher surfaces as -32603 with the original
//     message in `data`.
func (r *Registry) mapStreamError(c *Cascade, err error, sendCtx context.Context, opts SendOptions) error {
	if c.IsCancelled() {
		return errors.Join(ErrTurnCancelled, err)
	}
	if opts.Timeout > 0 && (errors.Is(sendCtx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded)) {
		return errors.Join(ErrTurnTimeout, err)
	}
	return errors.Join(ErrTurnFailed, err)
}

// stampStreamSuccess updates the cascade's per-cascade state
// (ConversationID + LastReplyAt + LastReply) on a successful
// turn. We use time.Now() here (not the caller's clock) so the
// LastReplyAt reflects wall-clock; tests that need a pinned
// value should drive SendStream through a custom backend
// that does not consult time.
//
// setReply is the cycle-3a path's "set LastReply to the full
// reply at end-of-turn"; for the streaming path the sink's
// OnTextDelta has already populated LastReply incrementally,
// but calling setReply here is idempotent (the accumulated
// text IS the final reply) and covers the edge case where a
// streaming backend completes without emitting any
// OnTextDelta.
func (r *Registry) stampStreamSuccess(c *Cascade, res Result) {
	c.setReply(res.Reply)
	c.RecordReply(res.NewConversationID, time.Now().UTC())
}
