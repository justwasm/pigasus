// Package remotecontrol — piagentbackend.go: the production
// PiG-backed MessageBackend that replaces StubBackend at the
// cascade seam.
//
// Cycle rc-piagent-bind ships the production `PiagentBackend`
// that turns a `MessagePrompt` into a real PiG `coding.Session`
// turn and re-emits the agent events as `EventSink` callbacks.
// The dispatcher routes both `Send` (cycle-3a fallback) and
// `SendStream` (cycle-5 streaming path) through this single
// type. StubBackend stays in `backend.go` for tests that don't
// want a real runtime.
//
// Why this file:
//
//   - The previous cycles wired the cascade surface as a seam
//     (cycle 4: `MessageBackend`, cycle 5: `EventSink` +
//     `StreamingMessageBackend`); they explicitly parked a
//     real backend for a future cycle. This file is that
//     future cycle.
//
//   - `internal/piagentbridge` already speaks PiG on the
//     print path; the cycle's factory function lives there
//     (`piagent_remote.go`) so the production wiring stays
//     in one place. The factory returns a `Session` (the
//     abstract seam this file defines) and the host wires
//     a `PiagentBackend{SessionFactory: factory}` into
//     `(*Cascade).SetBackend`. Tests substitute a fake
//     factory.
//
//   - `StubBackend` is preserved (cycle-3a/5/6 tests still
//     need it) but no longer the production default; the
//     dispatcher's `Config.MessageBackend` is what the
//     cascade seeds itself from on first `Backend()` access.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - piagentbackend.go        — Session / SessionFactory / PiagentBackend /
//     Send / ErrNilSessionFactory / ErrSessionClosed
//   - piagentbackend_send.go   — SendStream + drainEvents / startSession /
//     mapStreamFailure /
//     assembleStreamResult
//   - piagentbackend_events.go — piagentToolCall /
//     stopReasonFromPiG / piagentTurnFailure
//
// How the bridge works:
//
//	prompt → SessionFactory → Session
//	Session.Send(ctx, prompt) drives the turn, emitting
//	agent.AgentEvents; PiagentBackend.SendStream drains
//	the session's event channel and translates each
//	event into one of:
//
//	  Start                → OnState(Phase=starting)
//	  TextDelta            → OnTextDelta(delta)
//	  ToolCallStart/End    → OnToolCall(call)
//	  Done                 → OnState(Phase=done, StopReason=…)
//	  Error                → OnError(err)
//
//	The end-of-turn MessageResult carries the joined
//	`Reply` text, the `ToolCalls` slice, the
//	`StopReason`, and any `NewConversationID` the backend
//	returned. The registry's success path then stamps
//	the cascade's per-cascade state (ConversationID,
//	LastReply, LastReplyAt).
//
// No third-party deps beyond `coder/websocket` and the
// existing set. The package-internal import is
// `internal/piagentbridge` for the `Session` interface
// type only — keep imports here minimal so the
// dispatcher doesn't pull the cycle's full runtime build
// path.
package remotecontrol

import (
	"context"
	"errors"

	"github.com/MichaelKinsy/PiG/agent"
)

// Session is the abstract seam `PiagentBackend.SessionFactory`
// produces. The production factory
// (`piagentbridge.NewRemoteCascadeSession`) wraps a real
// `*pigrt.Runtime` and exposes only the surface this backend
// drives a turn through; tests substitute a fake
// implementation that replays canned event sequences.
//
// Method shapes:
//
//   - Send(ctx, prompt) drives one turn. The implementation
//     owns the lifetime of any PiG session it created; this
//     backend does not hold a reference between calls. Each
//     Send is one fresh `coding.Session`.
//
//   - Close releases any resources the session holds. Called
//     unconditionally on the success path; called on the
//     error path too (errors don't release the defer early).
//
//   - The session is NOT required to be safe for concurrent
//     Send calls — the registry already serialises per-
//     cascade turns through the per-cascade mu. (Today's
//     PiG runtime is single-turn anyway, so the constraint
//     is implicit.)
type Session interface {
	// Send drives one turn. The returned Messages are
	// PiG's assistant messages (cycle-209/210 vocabulary);
	// the backend walks them once to assemble the final
	// MessageResult and discards them on return.
	//
	// `events` is the channel the session emits its
	// agent.AgentEvent frames on; the backend drains it
	// until the channel closes. The channel is buffered
	// (production uses 128), so a slow sink does not
	// stall the session.
	Send(ctx context.Context, prompt string, events chan<- agent.AgentEvent) ([]agent.AgentMessage, error)

	// Close releases the session. Idempotent. May be a
	// no-op for fake implementations.
	Close() error
}

// SessionFactory is the seam `PiagentBackend` uses to mint a
// fresh `Session` per `SendStream` call. The factory pattern
// is the cycle's deliberate choice over constructing the
// session directly on the struct:
//
//   - Tests pass a factory that returns a fake Session
//     emitting canned events. The fake owns its own
//     goroutine + channel lifetime, and the production
//     code path is exercised end-to-end through the same
//     `SendStream` driver.
//
//   - Production wiring in `cmd/wagyu/remote_control.go`
//     passes `piagentbridge.NewRemoteCascadeSession` (a
//     closure that mints a real `*pigrt.Runtime`).
//
//   - The factory is responsible for: choosing the model,
//     setting up the runtime, supplying the EventCh.
//     `PiagentBackend.SendStream` itself only consumes.
//
// Signature mirrors `MessageSendOptions` so a future
// per-call knob (timeout, retry budget, …) lands without
// another signature change.
type SessionFactory func(ctx context.Context, prompt MessagePrompt, opts MessageSendOptions) (Session, error)

// PiagentBackend is the production `MessageBackend` +
// `StreamingMessageBackend`. It satisfies both interfaces
// (Send delegates to SendStream with a nil sink).
//
// The struct holds a single field — the SessionFactory seam
// — because that's all it needs. Per-turn state (the freshly-
// minted Session, the drained messages, the joined Reply)
// lives in SendStream's stack frame, not on the struct; a
// `PiagentBackend` is therefore reusable across turns and
// threads without synchronisation.
//
// Construction:
//
//	be := &PiagentBackend{SessionFactory: piagentbridge.NewRemoteCascadeSession}
//
// Tests pass a factory that emits canned events; production
// passes the closure the host wires up at daemon start.
type PiagentBackend struct {
	// SessionFactory is the seam. Must be non-nil for a
	// `SendStream` call to succeed; `SendStream` returns
	// `errors.New("piagent: nil SessionFactory")` when
	// the field is nil so a host that forgot to wire it
	// sees a clear failure instead of a nil-deref panic.
	SessionFactory SessionFactory
}

// Ensure PiagentBackend satisfies both interfaces the cascade
// dispatcher type-asserts against. Compile-time check, so
// future cycle changes that drift the interface trip the
// build instead of a wire test.
var (
	_ MessageBackend          = (*PiagentBackend)(nil)
	_ StreamingMessageBackend = (*PiagentBackend)(nil)
)

// Send implements MessageBackend. It is a thin shim around
// SendStream with a nil sink — the dispatcher calls
// `(*CascadeRegistry).SendStream` and the type assertion
// inside the registry routes through SendStream when the
// backend implements `StreamingMessageBackend` (which
// PiagentBackend does), so a non-streaming caller is rare in
// practice. We still implement Send because the cycle-3a
// type-assertion fallback path expects it; `nil sink` means
// "no EventSink is in scope; turn the events into
// MessageResult fields directly".
//
// Behaviour matches `StubBackend.Send`: ctx deadline → ctx.Err();
// SessionFactory returns an error → that error verbatim;
// turn completes → MessageResult with the joined reply /
// tool calls / stop reason / new conversation id.
//
// PiagentBackend's Send does NOT emit OnState/OnTextDelta
// notifications — the sink is nil. A streaming dashboard
// subscribes through SendStream instead.
func (b *PiagentBackend) Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error) {
	return b.SendStream(ctx, prompt, nil, MessageSendOptions{})
}

// ErrNilSessionFactory is returned by PiagentBackend when
// SessionFactory is nil. The dispatcher surfaces it as -32603
// via the existing ErrTurnFailed wrap; the message is
// distinctive enough that an operator can spot a missed
// wiring quickly. The brief: "the cycle reserves the
// sentinel so a future cycle can map it to a distinct
// JSON-RPC code without renaming".
var ErrNilSessionFactory = errors.New("piagent: SessionFactory is nil")

// ErrSessionClosed is returned by SendStream when the
// session's event channel closes without a Done or Error
// event. PiG promises a Done, but the dispatcher doesn't
// crash on the absence — it just sees a typed error.
//
// Today the dispatcher's switch wraps this in ErrTurnFailed
// for -32603; a future cycle that wants a distinct JSON-RPC
// code can add a switch arm without renaming the sentinel.
var ErrSessionClosed = errors.New("piagent: session event channel closed without done")
