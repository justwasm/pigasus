// Package remotecontrol — streaming.go: the dispatcher's side of
// the streaming turn.
//
// Cycle rc-cascade-message shipped the synchronous end-of-turn
// path: the dashboard sends one user message and gets the assistant's
// final reply back in a single response. Cycle rc-cascade-stream
// (this cycle) adds the streaming counterpart: while a turn is
// running, the server pushes JSON-RPC 2.0 notifications (no "id"
// field, per §4.1 of the spec) to the originating ws connection,
// mirroring the agy reference's `StreamRemoteAgentStateUpdates`
// notification.
//
// The turn's *vocabulary* — the state frames, the phases, the
// notification names, the EventSink interface a backend emits
// through — belongs to the cascade domain and lives in
// internal/remotecontrol/cascade/events.go. This file owns what
// is the dispatcher's: the per-connection id plumbing and the
// sink that turns a turn's events into wire notifications.
//
// Why a separate file:
//
//   - The dispatcher's sink needs the dispatcher's *Dispatcher
//     pointer and the per-conn connID; both are runtime
//     values that don't belong in the cascade package.
//
//   - The connID-context plumbing lives here because it's
//     the dispatcher's read path into the sink, not a
//     concern of the transport layer (which already passes
//     the connID to pushFrame directly).
//
// File layout:
//
//   - streaming.go      — package doc, connID plumbing,
//     Dispatcher.WithConnID, the sink struct + constructor
//   - streaming_sink.go — the sink's OnState / OnTextDelta /
//     OnToolCall / OnError / OnUserInteraction
//
// Notification name choice:
//
//   - We emit ONE notification per event under the
//     `cascade/state` / `cascade/text_delta` /
//     `cascade/tool_call` method names. These are the
//     namespaces the dashboard protocol targets. The agy
//     reference uses `StreamRemoteAgentStateUpdates` as a
//     single-method name and packs the frame kind into
//     the params (the decompiled
//     1018df9e0_func1.c frames are objects with a "phase"
//     or similar field). Both shapes carry the same
//     information; we prefer the "many methods, no
//     discriminator" shape because (a) it's simpler for
//     dashboards to subscribe to (one method per event
//     kind) and (b) JSON-RPC 2.0 doesn't constrain the
//     method-name cardinality.
//
//   - The agy alias `StreamRemoteAgentStateUpdates` is
//     exposed as a constant (cascade.NotifStreamRemoteAgentStateUpdates)
//     so a dashboard that subscribes to that name can
//     do so without re-declaring it. We do NOT emit
//     frames under that name — emitting both would
//     duplicate events on a dashboard that subscribes to
//     both, doubling the per-turn wire load.
//
// Back-pressure choice:
//
//   - "Best-effort, drop on error". The dispatcher's
//     Notify returns an error (slow client, unknown
//     connID, ws writer failed) which the sink
//     discards via `_ = s.disp.Notify(...)`. A slow
//     client whose ws Writer blocks therefore CAN
//     stall the backend turn briefly — the ws library
//     will time out the write and return an error,
//     and subsequent frames are dropped. A future
//     `rc-backpressure` cycle can replace this with
//     a per-conn buffered channel + drop policy.
//
// Stdlib-only. No third-party deps.
package remotecontrol

import (
	"context"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// connIDContextKey is the unexported context key the transport
// uses to thread the per-conn connID into the dispatcher's
// per-request context. The dispatcher reads it via
// connIDFromContext to know which ws connection to push
// notifications to. Nil/empty connID means "no notifier is
// set up" (the Unix-socket transport, or a context-less
// in-process test driver); the sink is a no-op in that case.
type connIDContextKey struct{}

// withConnID returns a context carrying the given connID. The
// caller (transport) is the only party that should call this;
// the dispatcher reads via connIDFromContext.
func withConnID(ctx context.Context, connID string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, connIDContextKey{}, connID)
}

// connIDFromContext returns the connID stored in ctx by
// withConnID, or "" when no connID is present (Unix-socket
// transport, in-process test drivers). The dispatcher calls
// this once per Send…Message request and threads the value
// through to the sink.
func connIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	v, _ := ctx.Value(connIDContextKey{}).(string)
	return v
}

// WithConnID is the public Dispatcher-interface entry point
// the remotetransport package calls. Defined here (next to
// withConnID) so the connID plumbing lives in one file. The
// method is the seam that lets the TCP+WS transport thread
// the per-connection connID into the dispatcher's per-request
// context without importing this package directly —
// remotetransport's Dispatcher interface declares the
// method, and *remotecontrol.Dispatcher satisfies it.
//
// Nil-safe: a nil receiver returns the original ctx with no
// connID attached (defensive — the production transport
// always passes a non-nil dispatcher).
func (d *Dispatcher) WithConnID(ctx context.Context, connID string) context.Context {
	if d == nil {
		if ctx == nil {
			return context.Background()
		}
		return ctx
	}
	return withConnID(ctx, connID)
}

// ConnNotifierSink is the default EventSink the dispatcher
// installs on a SendRemoteAgentCascadeMessage call. It does
// two things:
//
//  1. Emits JSON-RPC notifications through Dispatcher.Notify
//     using the connID captured at construction. The
//     notifications carry no "id" field, per JSON-RPC 2.0 §4.1.
//
//  2. Updates the captured cascade's per-cascade state as a
//     side effect of the events: OnTextDelta appends to
//     LastReply; OnState(phase=done) calls RecordReply to
//     stamp LastReplyAt.
//
// Why capture cascade at construction (rather than pass it
// through each method): the cycle's tests assert both the
// wire envelope AND the cascade's post-turn state. The
// captured cascade keeps the wire-emission and the
// state-mutation coupled — a future cycle that wants to
// aggregate events across cascades would use the EventSink
// interface's per-call cascade param to do so; for our
// one-sink-per-call model, the captured value is the
// cascade the dispatcher is running a turn for.
//
// Concurrency: the sink's methods are called by the
// dispatcher (or the backend goroutine the dispatcher is
// driving) — a single goroutine per turn. The cascade's
// mu is held briefly during state mutation; the dispatcher's
// Notify takes its own lock. The ConnNotifierSink itself
// has no mutex (the captured fields are immutable after
// construction; only the cascade and dispatcher are
// mutable, and they're goroutine-safe themselves).
//
// Nil-safe: every method is a no-op on a nil receiver or a
// sink with a nil captured cascade. The dispatcher always
// passes non-nil values, but a test that constructs a
// ConnNotifierSink directly without setting fields is
// safe.
type ConnNotifierSink struct {
	disp    *Dispatcher
	connID  string
	cascade *cascades.Cascade
	// startedAt is stamped at construction so OnState(phase=done)
	// can emit a durationMs that spans the full turn. nil-safe
	// (zero-value means durationMs is omitted).
	startedAt time.Time
}

// NewConnNotifierSink returns a sink that routes notifications
// through disp.Notify to the connection identified by connID,
// and applies per-event state updates to cascade. nil-safe
// (nil disp / nil cascade is allowed; methods become no-ops).
//
// The dispatcher is the only intended caller. A future
// `rc-backpressure` cycle may replace this with a buffered
// variant; the interface signature is preserved so the
// replacement is wire-compatible.
func NewConnNotifierSink(d *Dispatcher, connID string, c *cascades.Cascade) *ConnNotifierSink {
	return &ConnNotifierSink{
		disp:      d,
		connID:    connID,
		cascade:   c,
		startedAt: time.Now().UTC(),
	}
}
