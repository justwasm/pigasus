// Package remotecontrol — backend.go: the seam between the
// dispatcher's cascade surface and the model backend that
// actually runs a turn.
//
// Cycle rc-cascade-message adds the second JSON-RPC method
// that touches a registered cascade:
// SendRemoteAgentCascadeMessage. The handler parses params,
// resolves per-turn overrides against the cascade's pinned
// values, then asks a MessageBackend to run the turn. The
// backend returns the assistant's final reply, the tool calls
// (if any), and a stop reason. Errors are surfaced to the
// dashboard via the dispatcher's existing -32603 envelope for
// the generic path and three new custom codes (-32004 /
// -32005 / -32010) for the cascade and turn paths.
//
// Why a separate file:
//
//   - It groups the dispatcher-agnostic types (MessageBackend,
//     MessagePrompt, MessageResult, ToolCall, plus the three
//     custom error sentinels and their JSON-RPC code constants).
//     cascade.go stays the lifecycle-value / registry file;
//     dispatch.go stays the wire-format / method-handler file;
//     backend.go is the seam both of them read from and the
//     only file a future "rc-piagent-bind" cycle needs to
//     touch to install a real backend.
//
//   - The StubBackend is a fixture: a hand-rolled
//     `(*StubBackend).Send` that returns a fixed
//     MessageResult or a fixed error depending on which fields
//     the test populates. Production wiring never imports the
//     stub; tests exercise the registry / dispatcher paths
//     against it to keep the rc-cascade-message cycle free of
//     any real PiG dependency.
//
// File layout (cycle rc-filesize-internal-remotecontrol):
//
//   - backend.go         — package doc + types +
//     errors / sentinels /
//     StubBackend struct /
//     PendingInteractionConfig
//   - backend_stub.go    — StubBackend methods
//     (emitInteractions /
//     Send / SendStream)
//
// No third-party deps. Stdlib only.
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// MessageBackend is the seam the cascade sends a turn through.
//
// The cycle-rc-cascade-start cascade value is metadata only;
// this interface is the one a real PiG session (or any other
// turn runner) satisfies. The cycle ships StubBackend as the
// default so tests and the print-mode daemon can register a
// cascade, call registry.Send, and exercise the dispatcher's
// error mapping without any model client on the wire.
//
// The Send contract:
//
//   - ctx may carry a deadline. The implementation MUST honour
//     it (cancel any goroutine it owns and return promptly).
//   - The returned MessageResult.NewConversationID, when
//     non-empty, becomes the cascade's new ConversationID
//     (the "first turn ran" anchor; rc-list-conversations
//     reads this).
//   - The returned error falls into one of three buckets:
//   - a context.DeadlineExceeded (or a wrapped one) so the
//     registry can map it to -32010 Turn timeout;
//   - any other error, which the registry wraps in
//     ErrTurnFailed and surfaces as -32603;
//   - nil, which the registry treats as success.
type MessageBackend interface {
	Send(ctx context.Context, prompt MessagePrompt) (MessageResult, error)
}

// MessagePrompt is one user message turned into a backend turn.
// Fields are the resolved set (per-turn overrides applied
// already), so a backend implementing this interface does not
// need to know about a Cascade — it just sees the prompt it
// should run. BackendLabel is the resolved backend identifier
// ("agw" | "shim") so a future multi-backend router can pick
// without rereading the cascade value.
type MessagePrompt struct {
	// ConversationID is the cascade's current conversation id.
	// May be empty on the first turn (the registry will fill it
	// in from the result).
	ConversationID string
	// Text is the dashboard-supplied user message.
	Text string
	// Model is the resolved model label.
	Model string
	// Agent is the resolved agent name.
	Agent string
	// Effort is "low|medium|high|xhigh|max" or empty.
	Effort string
	// Scaffold mirrors --scaffold.
	Scaffold bool
	// SessionMode is opaque passthrough.
	SessionMode string
	// BackendLabel is "agw" / "shim" / "" — see Cascade.BackendLabel.
	BackendLabel string
}

// MessageResult is one turn's outcome. NewConversationID may
// equal MessagePrompt.ConversationID when the backend reuses
// the existing conversation (the common case for follow-up
// turns). It is set on the cascade verbatim when non-empty.
type MessageResult struct {
	// Reply is the assistant's final text. Empty if the
	// backend returned no textual response (e.g. the model
	// hit a tool_use loop and surfaced a structured result).
	Reply string
	// ToolCalls is the list of tool invocations the
	// assistant produced during the turn. May be empty.
	// The Cycle rc-cascade-stream (next) will stream these
	// via Dispatcher.Notify as the model emits them; the
	// rc-cascade-message cycle serialises them in the
	// result envelope as a faithful snapshot at end-of-turn.
	ToolCalls []ToolCall
	// StopReason is "end_turn" | "tool_use" | "max_tokens"
	// | "error" — see the StopReason* constants.
	StopReason string
	// NewConversationID becomes the cascade's ConversationID
	// after a successful turn (when non-empty). May equal
	// MessagePrompt.ConversationID (the backend reused the
	// existing conversation) or be a fresh id (first turn).
	NewConversationID string
}

// ToolCall is one tool invocation the model produced. Args is
// left as json.RawMessage so the dispatcher can round-trip the
// raw JSON object the model emitted (OpenAI-shape arguments
// arrive as a JSON object, not a Go struct). OK=false flags a
// tool that returned an error so the dispatcher can render it
// differently in the dashboard UI.
type ToolCall struct {
	Name   string
	Args   []byte // JSON-encoded argument object (or null)
	Result string
	OK     bool
}

// MessageSendOptions is the per-call knob. Timeout, when > 0,
// is applied as a context.WithTimeout around the backend Send.
// A zero Timeout means the call inherits the caller's ctx as
// is.
type MessageSendOptions struct {
	// Timeout bounds the backend Send call. Zero means no
	// extra timeout (the dispatcher's ctx is the only
	// budget).
	Timeout time.Duration
}

// StopReason constants — the assistant's high-level stop reason
// the dispatcher's ResultEnvelope surfaces as `stopReason`.
// Cycle rc-cascade-stream will reuse these when emitting
// mid-turn notifications; the canonical mapping lives here so
// the dispatcher and the backend agree on the same vocabulary.
const (
	StopReasonEndTurn   = "end_turn"
	StopReasonToolUse   = "tool_use"
	StopReasonMaxTokens = "max_tokens"
	StopReasonError     = "error"
)

// DefaultEffortValues is the closed set of effort strings the
// dispatcher accepts. Empty string is also valid (use the
// cascade's pinned effort) but is not in the set — callers
// MUST check IsValidEffort against the resolved value (after
// per-turn override is applied), not the raw param value.
var DefaultEffortValues = []string{"low", "medium", "high", "xhigh", "max"}

// IsValidEffort reports whether s is one of the closed set
// of effort values the dashboard is allowed to send. Empty is
// always valid (treated as "use the cascade default"), so
// callers should check `s == "" || IsValidEffort(s)`.
func IsValidEffort(s string) bool {
	for _, v := range DefaultEffortValues {
		if s == v {
			return true
		}
	}
	return false
}

// ErrCascadeClosed is returned by registry.Send when the
// cascade has been closed out-of-band (future
// rc-cancel-cascade cycle wires an RPC for this; for now the
// cascade's MarkClosed is the only path to it). The
// dispatcher surfaces it as JSON-RPC -32005 "Cascade closed".
var ErrCascadeClosed = errors.New("remotecontrol: cascade closed")

// ErrTurnTimeout is returned by registry.Send when the
// configured timeout (passed via MessageSendOptions) elapses
// before the backend Send returns. The dispatcher surfaces it
// as JSON-RPC -32010 "Turn timeout".
var ErrTurnTimeout = errors.New("remotecontrol: turn timeout")

// ErrTurnFailed is the generic catch-all registry.Send wraps
// any non-timeout backend error in. The dispatcher surfaces
// it as JSON-RPC -32603 "Internal error" (with the wrapped
// message preserved in `data`).
var ErrTurnFailed = errors.New("remotecontrol: turn failed")

// ErrCascadeInteractionTimeout is returned by StubBackend
// (and any future real backend) when the
// HandleRemoteAgentCascadeUserInteraction RPC never arrives
// for an emitted user-interaction prompt. The cycle
// brief: "the stub backend pauses for up to 200 ms waiting
// for the intr.ResultCh; if no response arrives, the stub
// errors with ErrCascadeInteractionTimeout". A real PiG-
// backed backend in rc-piagent-bind would also use this
// sentinel so the dispatcher's switch can map it to a
// distinct error envelope (rather than the generic
// ErrTurnFailed → -32603).
//
// Today no dispatcher handler maps this sentinel to a
// distinct JSON-RPC code; the stub returns it as a
// turn-failed error which the dispatcher surfaces as
// -32603 with the inner message preserved. The cycle
// reserves the sentinel so future cycles can add a custom
// code without renaming.
var ErrCascadeInteractionTimeout = errors.New("remotecontrol: cascade interaction timeout")

// ErrTurnCancelled is returned by
// (*CascadeRegistry).SendStream when the cascade's
// per-turn context was cancelled (c.Cascade.Cancel fired).
// The dispatcher surfaces it as JSON-RPC -32603 with the
// inner message preserved (the cancel handler already
// emitted a cascade/state{phase:cancelled} notification
// before this error arrives, so the dashboard's UI sees
// the cancel signal twice — once as a notification, once
// as the SendMessage error — which is the contract the
// rc-cancel-cascade brief pins).
//
// ErrTurnCancelled is only returned when the cascade was
// cancelled via (*Cascade).Cancel — a plain
// context.Canceled that landed via the dispatcher's ctx
// (e.g. the ws connection closed) still surfaces as the
// generic ErrTurnFailed → -32603 so dashboards can
// distinguish "user wanted to stop" from "connection
// died".
//
// Cycle rc-cancel-cascade adds this sentinel. Reserved
// here (in backend.go alongside the other sentinels)
// rather than in cascade.go so the dispatcher's switch
// keeps a single import for the error vocabulary.
var ErrTurnCancelled = errors.New("remotecontrol: turn cancelled")

// Custom JSON-RPC error codes reserved for the
// rc-cascade-message surface. The -32000 .. -32099 range is
// the JSON-RPC 2.0 "custom error" space; the brief's
// reservation is:
//
//	-32004 Cascade not found
//	-32005 Cascade closed
//	-32006 Interaction not found       (rc-handle-user-interaction)
//	-32010 Turn timeout
//
// Anything else from the cascade-message handler stays on
// the standard -32600 Invalid Request / -32602 Invalid params
// / -32603 Internal error codes.
const (
	ErrCodeCascadeNotFound     = -32004
	ErrCodeCascadeClosed       = -32005
	ErrCodeInteractionNotFound = -32006
	ErrCodeTurnTimeout         = -32010
)

// StubBackend is the default backend installed on a fresh
// Cascade. It returns a fixed MessageResult (Reply +
// ToolCalls + StopReason + NewConversationID) when Err is nil
// and Sleep is zero. Tests configure it to mimic the surface
// they need:
//
//   - happy path: set Reply/StopReason/NewConversationID
//     and leave Err/Sleep at zero;
//   - error path: set Err and the test asserts the
//     dispatcher's -32603 envelope;
//   - timeout path: set Sleep > configured timeout; the stub
//     blocks on the context and Send returns ctx.Err(); the
//     registry maps it to -32010.
//   - user-interaction path: set Interactions + (optionally)
//     Answers; SendStream walks each configured interaction
//     in order, calling sink.OnUserInteraction with a fresh
//     PendingInteraction, then parks on the interaction's
//     ResultCh for up to InteractionWait (200ms default).
//     If no answer arrives, SendStream returns
//     ErrCascadeInteractionTimeout (TestUserInteraction_TimeoutInBackend
//     pins this). If Answers[id] is pre-populated, the stub
//     consumes that answer verbatim (TestHandle…UserInteraction_Ok
//     pins this).
//
// A nil *StubBackend is a usable backend (Send returns the
// zero MessageResult with no error); the cycle's tests use
// the explicit form for readability.
type StubBackend struct {
	// Reply is the assistant's final text.
	Reply string
	// ConversationID is the conversation id the
	// MessageResult carries back. Empty means
	// "leave the cascade's ConversationID untouched"
	// (the registry's contract: only update when
	// non-empty).
	ConversationID string
	// ToolCalls is the slice of tool invocations the
	// MessageResult carries back.
	ToolCalls []ToolCall
	// StopReason is the StopReason* constant the
	// MessageResult carries back. Defaults to
	// StopReasonEndTurn when empty AND Err is nil.
	StopReason string
	// Err, when non-nil, is what Send returns verbatim.
	// Takes precedence over the reply-shaped fields.
	Err error
	// Sleep, when > 0, makes Send wait that long before
	// returning. Honours ctx — used by the timeout test.
	Sleep time.Duration

	// Interactions is the cycle-6 user-interaction prompt
	// sequence the stub drives. Each entry yields one
	// sink.OnUserInteraction call followed by a ResultCh
	// receive. Order is preserved (the stub walks the slice
	// left-to-right so a test can drive a multi-prompt
	// scenario in a deterministic order).
	//
	// An interaction's ID is minted by the stub from
	// "int_<6 bytes hex>" — matching mintInteractionID's
	// output — so the cascade's Pending().Get(intr.ID) call
	// in the dispatcher's handler resolves the same entry.
	//
	// The cycle brief says the stub pauses "for up to
	// InteractionWait" (default 200 ms) per interaction. A
	// test that pre-populates Answers[id] makes the
	// receive fire immediately; a test that leaves
	// Answers empty drives the timeout path.
	Interactions []PendingInteractionConfig

	// Answers is the test-side input: the stub's ResultCh
	// receive completes against Answers[interactionID]
	// (raw JSON), or against the zero InteractionResult
	// (an empty Response) when the id is absent. Tests that
	// pre-populate this map and call SendStream on a
	// goroutine get the cycle-6 happy-path behaviour
	// without needing the dispatcher's RPC handler to be
	// involved at all (TestHandle…UserInteraction_Ok uses
	// this shape).
	Answers map[string]json.RawMessage

	// InteractionWait bounds the per-interaction park on
	// ResultCh. Zero falls back to 200ms per the cycle brief.
	// The dispatcher's handler is not affected (its send on
	// the channel is what actually unblocks the stub; this
	// timeout is the "the dashboard never answered" path).
	InteractionWait time.Duration
}

// PendingInteractionConfig is one configured user-interaction
// prompt in the StubBackend.Interactions slice. The stub
// mints a fresh PendingInteraction at SendStream time
// (filling ID via mintInteractionID + CreatedAt via
// time.Now()) and emits it through the sink.
//
// Options, ExpiresAt, and Dangerous are optional — zero
// values produce a notification with no options /
// expires_at / dangerous fields. Kind / Title / Prompt are
// required (a test that wants an empty prompt just sets
// Prompt = ""; the brief doesn't require any field).
type PendingInteractionConfig struct {
	Kind      string
	Title     string
	Prompt    string
	Options   []InteractionOption
	Dangerous bool
	ExpiresAt time.Time
}
