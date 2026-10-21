// Package remotecontrol — streaming.go: the streaming server-side
// of SendRemoteAgentCascadeMessage.
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
// Why a separate file:
//
//   - The streaming types (`StateFrame`, `EventSink`,
//     `StreamingMessageBackend`, the four notification-name
//     constants, the per-call `ConnNotifierSink`) are disjoint
//     from the synchronous types in backend.go. backend.go
//     stays the cycle-3a "stub / interface" seam; this file
//     is the cycle-5 "stub / interface + sink" surface. The
//     StubBackend fixture has both a Send method (cycle-3a)
//     and a SendStream method (this cycle); the dispatcher
//     picks one at runtime via type-assertion (see
//     dispatch.go:handleSendRemoteAgentCascadeMessage).
//
//   - ConnNotifierSink needs the dispatcher's *Dispatcher
//     pointer and the per-conn connID; both are runtime
//     values that don't belong in the backend.go file
//     (which is dispatcher-agnostic on purpose — a future
//     cycle's tests can drive the StubBackend without
//     bringing up a Dispatcher). Splitting the seam into
//     streaming.go keeps backend.go transport-agnostic.
//
//   - The connID-context plumbing lives here because it's
//     the dispatcher's read path into the sink, not a
//     concern of the transport layer (which already passes
//     the connID to pushFrame directly).
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - streaming.go         — package doc, types, interfaces,
//     connID plumbing, ConnNotifierSink struct + constructor
//   - streaming_sink.go    — ConnNotifierSink.OnState /
//     OnTextDelta / OnToolCall / OnError /
//     OnUserInteraction
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
//     exposed as a constant (NotifStreamRemoteAgentStateUpdates)
//     so a dashboard that subscribes to that name can
//     do so without re-declaring it. We do NOT emit
//     frames under that name — emitting both would
//     duplicate events on a dashboard that subscribes
//     to both, doubling the per-turn wire load.
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
)

// StateFrame is one of the cascade/state notification payloads.
// The dispatcher emits one OnState notification per lifecycle
// phase transition: starting, working (optional mid-turn
// "still going" pulse), done, error, cancelled.
//
// Phase values are the same closed set everywhere in this
// package; the StatePhase* constants below are the canonical
// names.
//
// StopReason and TotalTokens are only meaningful when
// Phase == "done". Error is only meaningful when Phase ==
// "error". They're emitted as zero-valued (absent) fields
// otherwise — see ConnNotifierSink.OnState.
//
// DurationMs is NOT in StateFrame: the sink computes it
// from its construction-time stamp so a stub backend
// doesn't have to track its own timer. Future real
// backends (rc-piagent-bind) can populate the synchronous
// result envelope's durationMs (the dispatcher computes
// that one too — `time.Since(handleSendStart)`).
//
// Cycle rc-cancel-cascade adds the cancelled phase. The
// OnState path's "Phase == StatePhaseDone" branch that
// stamps LastReplyAt is intentionally NOT taken for the
// cancelled phase (a cancelled turn is not a successful
// reply — same rule as the OnError path).
type StateFrame struct {
	Phase       string // "starting" | "working" | "done" | "error" | "cancelled"
	StopReason  string // populated when Phase == "done"; StopReason* constants
	TotalTokens int64  // populated when Phase == "done" (optional, future)
	Error       string // populated when Phase == "error"; the wrapped error message
}

// State-phase constants. Used as `frame.Phase` values; the
// dispatcher passes them through to the notification params
// unmodified.
const (
	StatePhaseStarting = "starting"
	StatePhaseWorking  = "working"
	StatePhaseDone     = "done"
	StatePhaseError    = "error"
	// StatePhaseCancelled is emitted on the cascade/state
	// notification when the dashboard's
	// CancelRemoteAgentCascade RPC interrupts an in-flight
	// turn. Distinct from "error" because cancellation
	// is a deliberate user action — the turn didn't
	// fail, the user just stopped it. Cycle rc-cancel-cascade
	// adds this.
	StatePhaseCancelled = "cancelled"
)

// Notification name constants. The dispatcher emits one frame
// per event under one of these names. StreamRemoteAgentStateUpdates
// is the agy reference's name; we expose it as a constant for
// dashboards that want to subscribe to the reference's name
// (the dispatcher does NOT emit under it — see the package
// doc for the rationale).
const (
	// NotifCascadeState is the JSON-RPC 2.0 method name the
	// dispatcher uses for StateFrame events. Params: cascade_id,
	// phase, ts, plus the optional stopReason / totalTokens /
	// durationMs / error fields.
	NotifCascadeState = "cascade/state"

	// NotifCascadeTextDelta is the JSON-RPC 2.0 method name for
	// mid-turn assistant text fragments. Params: cascade_id,
	// delta (string).
	NotifCascadeTextDelta = "cascade/text_delta"

	// NotifCascadeToolCall is the JSON-RPC 2.0 method name for
	// mid-turn tool invocations. Params: cascade_id, name,
	// args (json.RawMessage or null), result (string, optional),
	// ok (bool).
	NotifCascadeToolCall = "cascade/tool_call"

	// NotifUserInteractionRequired is the JSON-RPC 2.0 method
	// name the dispatcher emits when a backend pauses for a
	// permission / question / confirmation prompt. Cycle
	// rc-handle-user-interaction adds this. Params: cascade_id,
	// interaction_id, kind, title, prompt, options (optional),
	// dangerous (optional, kind == permission), expires_at
	// (optional RFC3339).
	//
	// A dashboard that subscribes to this notification handles
	// the prompt in its UI, then calls
	// HandleRemoteAgentCascadeUserInteraction with the
	// interaction_id + a response payload to unblock the
	// backend's parked call.
	NotifUserInteractionRequired = "cascade/user_interaction_required"

	// NotifStreamRemoteAgentStateUpdates is the agy reference's
	// notification name. Exposed as a constant for dashboards
	// that want to subscribe to the reference's name; the
	// dispatcher does NOT emit frames under this name (see
	// package doc).
	NotifStreamRemoteAgentStateUpdates = "StreamRemoteAgentStateUpdates"
)

// EventSink is the seam a streaming backend (StubBackend's
// SendStream today; a PiG-backed backend in rc-piagent-bind)
// receives from the dispatcher to emit mid-turn events. Each
// method corresponds to one notification kind; the dispatcher
// uses the cascade parameter (and the sink's captured cascade,
// see ConnNotifierSink) to populate the per-event params.
//
// Concurrency: the dispatcher invokes the sink from a single
// goroutine (the goroutine driving SendStream). The brief's
// "Optional async flush" comment anticipates a future
// backpressure cycle where the sink buffers writes; today the
// implementation is synchronous.
//
// Nil-safety: a sink that wants to be safe across calls from a
// nil cascade must implement its own nil-check (every method
// below takes a *Cascade; the ConnNotifierSink uses its captured
// one, so the cascade param is reserved for multi-cascade
// sinks that aggregate events across cascades — not our
// current use case).
type EventSink interface {
	// OnState is called when a state transition happens.
	// frame.Phase is one of the StatePhase* constants.
	OnState(cascade *Cascade, frame StateFrame)
	// OnTextDelta is called when the assistant emits a chunk
	// of text. delta is the new text fragment (the dashboard
	// concatenates successive deltas to assemble the reply).
	OnTextDelta(cascade *Cascade, delta string)
	// OnToolCall is called when the assistant invokes a tool.
	// call.Name / Args / Result / OK are the same fields the
	// cycle-4 MessageResult carries.
	OnToolCall(cascade *Cascade, call ToolCall)
	// OnError is called when the turn fails. The notification
	// goes out as a StateFrame{Phase: "error", Error: ...};
	// the cascade is NOT mutated (LastReplyAt is left as it
	// was — the brief: "an error is not a successful reply").
	OnError(cascade *Cascade, err error)
	// OnUserInteraction is called when the backend needs the
	// user's input — a permission prompt, a question, a
	// confirmation, or a multiple-choice question. The
	// dispatcher emits a cascade/user_interaction_required
	// notification carrying intr's metadata; the dashboard is
	// expected to call HandleRemoteAgentCascadeUserInteraction
	// with intr.ID + a response payload to unblock the
	// backend's parked call (the backend reads intr.ResultCh
	// to learn the answer).
	//
	// Cycle rc-handle-user-interaction adds this method.
	// Backends that pre-date this cycle (cycle-3a's
	// cycle-3a non-streaming backends) are unaffected —
	// they don't implement OnUserInteraction and the
	// dispatcher's streaming type-assertion path doesn't
	// invoke it. The ConnNotifierSink is the production
	// implementation: it adds intr to the cascade's
	// PendingInteractions registry (lazily allocated on
	// first call) and emits the cascade/user_interaction_required
	// notification.
	OnUserInteraction(cascade *Cascade, intr *PendingInteraction)
}

// StreamingMessageBackend is the optional capability the
// dispatcher type-asserts to enable mid-turn notifications.
// A backend that implements only the cycle-3a MessageBackend
// still works — the dispatcher falls back to the synchronous
// path (cycle-3a's contract).
//
// SendStream's contract:
//
//   - ctx may carry a deadline. Implementations MUST honour
//     it. The StubBackend's SendStream honours ctx on the
//     Sleep path; future real backends (rc-piagent-bind) must
//     do the same.
//   - sink may NOT be nil — the dispatcher always constructs
//     one. A streaming backend that wants to ignore events
//     can install a no-op sink; we don't provide one.
//   - The returned MessageResult.NewConversationID, when
//     non-empty, becomes the cascade's new ConversationID
//     (the cycle-3a rule; the registry.Send path applies it).
//   - The returned error follows the cycle-3a mapping
//     (ErrCascadeNotFound, ErrCascadeClosed, ErrTurnTimeout,
//     ErrTurnFailed).
type StreamingMessageBackend interface {
	MessageBackend
	SendStream(ctx context.Context, prompt MessagePrompt, sink EventSink, opts MessageSendOptions) (MessageResult, error)
}

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
//     LastReply; OnState(phase=done) calls recordReply to
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
	cascade *Cascade
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
func NewConnNotifierSink(d *Dispatcher, connID string, cascade *Cascade) *ConnNotifierSink {
	return &ConnNotifierSink{
		disp:      d,
		connID:    connID,
		cascade:   cascade,
		startedAt: time.Now().UTC(),
	}
}
