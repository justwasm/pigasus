// Package cascades — events.go: the streaming seam a turn's
// backend emits its mid-turn events through.
//
// The vocabulary here is the cascade domain's own: a turn's
// backend does not know about the dispatcher, the transport or
// the dashboard — it pushes events into a sink, and whoever
// installed the sink decides what happens to them.
//
// File layout:
//
//   - events.go — StateFrame, the phase + stop-reason
//     vocabulary, the notification names, the EventSink
//     interface, the StreamingBackend capability and the
//     InteractionHostInstaller seam.
//
// Stdlib-only. No third-party deps.
package cascades

import (
	"context"
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
// otherwise — see the dispatcher's sink.
//
// DurationMs is NOT in StateFrame: the sink computes it
// from its construction-time stamp so a stub backend
// doesn't have to track its own timer. Future real
// backends can populate the synchronous
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
// uses the cascade parameter (and the sink's captured cascade)
// to populate the per-event params.
//
// Concurrency: the dispatcher invokes the sink from a single
// goroutine (the goroutine driving SendStream). The brief's
// "Optional async flush" comment anticipates a future
// backpressure cycle where the sink buffers writes; today the
// implementation is synchronous.
//
// Nil-safety: a sink that wants to be safe across calls from a
// nil cascade must implement its own nil-check (every method
// below takes a *Cascade; the dispatcher's sink uses its
// captured one, so the cascade param is reserved for
// multi-cascade sinks that aggregate events across cascades —
// not our current use case).
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
	// cycle-4 Result carries.
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
	// Backends that pre-date this cycle (the cycle-3a
	// non-streaming backends) are unaffected —
	// they don't implement OnUserInteraction and the
	// dispatcher's streaming type-assertion path doesn't
	// invoke it. The dispatcher's sink is the production
	// implementation: it adds intr to the cascade's
	// PendingInteractions registry (lazily allocated on
	// first call) and emits the cascade/user_interaction_required
	// notification.
	OnUserInteraction(cascade *Cascade, intr *PendingInteraction)
}

// StreamingBackend is the optional capability the
// dispatcher type-asserts to enable mid-turn notifications.
// A backend that implements only the cycle-3a Backend
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
//   - The returned Result.NewConversationID, when
//     non-empty, becomes the cascade's new ConversationID
//     (the cycle-3a rule; the registry.Send path applies it).
//   - The returned error follows the cycle-3a mapping
//     (ErrNotFound, ErrClosed, ErrTurnTimeout,
//     ErrTurnFailed).
type StreamingBackend interface {
	Backend
	SendStream(ctx context.Context, prompt Prompt, sink EventSink, opts SendOptions) (Result, error)
}

// InteractionHostInstaller is the optional capability a sink
// implements when the turn's context should carry an
// interaction host.
//
// The host is what the interactive tools ask: it publishes the
// waiting step the Web UI's card reads and blocks until the
// user answered. It belongs to the Web UI's surface, not to
// the cascade domain — so the domain asks the sink whether it
// can install one instead of importing that surface. The
// dependency runs from the domain to the host, never the other
// way round.
//
// A sink that does not implement it gets a turn with no
// interaction host, and the tools keep their headless
// behaviour — which is exactly what the legacy JSON-RPC
// surface wants.
type InteractionHostInstaller interface {
	// InstallInteractionHost puts the turn's interaction host
	// on ctx and returns the derived context. A sink with no
	// host to install returns ctx unchanged.
	InstallInteractionHost(ctx context.Context, c *Cascade, sink EventSink) context.Context
}
