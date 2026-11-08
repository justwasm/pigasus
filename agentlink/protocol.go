// Package agentlink is the wire between a pigasus client and a pigasus
// server.
//
// The client sends a prompt; the server runs the turn and sends the turn's
// events back. One JSON object per WebSocket text frame, and one Frame type
// for the whole protocol: a frame's Kind says what it carries, and the other
// members are only meaningful for the kinds that use them.
//
// The protocol is deliberately small. A turn is a prompt in and a stream of
// events out, and the events are the same ones the in-process TUI already
// produces — so a client that speaks this protocol renders a turn exactly
// the way the local TUI does.
//
// The package has no dependency on the clone's internals: a third party
// implementing the client side imports it directly.
package agentlink

import "encoding/json"

// Kind names one frame on the wire.
type Kind string

const (
	// KindHello is the handshake. The client sends one to open the
	// session; the server answers with the same kind, carrying its own
	// version, the workspace it serves and the model it resolved.
	KindHello Kind = "hello"

	// KindPrompt asks the server to run a turn. It carries the model,
	// the session and the transcript the turn replays.
	KindPrompt Kind = "prompt"

	// KindCancel asks the server to stop the turn with the given ID.
	KindCancel Kind = "cancel"

	// KindState reports that the turn's phase changed.
	KindState Kind = "state"

	// KindText carries a text delta — the model's answer as it arrives.
	KindText Kind = "text"

	// KindThought carries a reasoning delta.
	KindThought Kind = "thought"

	// KindTool reports a tool call that started or ended.
	KindTool Kind = "tool"

	// KindCompact reports a compaction that started or ended.
	KindCompact Kind = "compact"

	// KindNotice carries an out-of-band notice: the auto-continue
	// banner, an account-eligibility warning.
	KindNotice Kind = "notice"

	// KindResult ends the turn. It carries the turn's outcome.
	KindResult Kind = "result"
)

// Phase is the turn's phase.
type Phase string

const (
	// PhaseStarting is the phase before the model produced anything.
	PhaseStarting Phase = "starting"
	// PhaseWorking is the phase while the turn runs.
	PhaseWorking Phase = "working"
	// PhaseDone is the phase after the turn ended normally.
	PhaseDone Phase = "done"
	// PhaseError is the phase after the turn failed.
	PhaseError Phase = "error"
	// PhaseCancelled is the phase after the turn was cancelled.
	PhaseCancelled Phase = "cancelled"
)

// ToolPhase says whether a tool frame announces a call or its outcome.
type ToolPhase string

const (
	// ToolStart announces that the call is running.
	ToolStart ToolPhase = "start"
	// ToolEnd announces that the call finished.
	ToolEnd ToolPhase = "end"
)

// CompactPhase says whether a compaction frame announces the start of the
// compaction or its outcome.
type CompactPhase string

const (
	// CompactStart announces that the compaction began.
	CompactStart CompactPhase = "start"
	// CompactEnd announces that the compaction finished.
	CompactEnd CompactPhase = "end"
)

// NoticeKind names the sort of notice a notice frame carries.
type NoticeKind string

const (
	// NoticeAutoCont is the banner that shows the auto-continue depth.
	NoticeAutoCont NoticeKind = "auto_cont"
	// NoticeEligibility is the account-eligibility warning: the
	// account cannot run the model until it is verified.
	NoticeEligibility NoticeKind = "eligibility"
	// NoticeError is a failure the turn reported: the transport
	// failed, the model answered with an error.
	NoticeError NoticeKind = "error"
	// NoticeSteerApplied reports that pending steering guidance was applied at step boundary.
	NoticeSteerApplied NoticeKind = "steer_applied"
)

// Frame is one message on the wire.
//
// Only the members that the frame's Kind uses are meaningful; the rest keep
// their zero value.
type Frame struct {
	// Kind says what the frame carries.
	Kind Kind `json:"kind"`

	// ID correlates the frames of one turn. The client mints it when it
	// asks for a turn, and every frame the server sends for that turn
	// carries it back.
	ID string `json:"id,omitempty"`

	// Session names the conversation the turn belongs to.
	Session string `json:"session,omitempty"`

	// Version is the peer's version. It rides on the hello frames.
	Version string `json:"version,omitempty"`

	// Workspace is the workspace the server serves. It rides on the
	// server's hello.
	Workspace string `json:"workspace,omitempty"`

	// Model is the model the turn runs under.
	Model string `json:"model,omitempty"`

	// Text is the prompt on a prompt frame, and the delta on a text or
	// thought frame.
	Text string `json:"text,omitempty"`

	// History is the transcript the turn replays. It rides on a prompt
	// frame.
	History []Message `json:"history,omitempty"`

	// Phase is the turn's phase. It rides on a state frame.
	Phase Phase `json:"phase,omitempty"`

	// Tool is the tool call. It rides on a tool frame.
	Tool *Tool `json:"tool,omitempty"`

	// Compact is the compaction. It rides on a compact frame.
	Compact *Compact `json:"compact,omitempty"`

	// Notice is the notice. It rides on a notice frame.
	Notice *Notice `json:"notice,omitempty"`

	// Usage is the turn's token accounting. It rides on a result frame.
	Usage *Usage `json:"usage,omitempty"`

	// Error is the failure message. It rides on a result frame.
	Error string `json:"error,omitempty"`

	// Result is the turn's outcome. It rides on a result frame.
	Result *Result `json:"result,omitempty"`
}

// Message is one round of a conversation.
//
// It mirrors the shape the model transports use: the role, the text, the
// tool calls an assistant message requested, and the images a user message
// carries.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
	Images     []Image    `json:"images,omitempty"`
}

// ToolCall is a model-initiated tool invocation.
type ToolCall struct {
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// Image is a picture a user message carries.
type Image struct {
	MIMEType string `json:"mime_type,omitempty"`
	Data     []byte `json:"data,omitempty"`
}

// Tool is a tool call on a tool frame.
type Tool struct {
	// Phase says whether the call started or ended.
	Phase ToolPhase `json:"phase,omitempty"`
	// Name is the tool's name.
	Name string `json:"name"`
	// Args is the raw JSON the model produced for the call.
	Args string `json:"args,omitempty"`
	// Result is the tool's output. It is set on an end frame.
	Result string `json:"result,omitempty"`
	// Err marks a failed call. It is set on an end frame.
	Err bool `json:"err,omitempty"`
}

// Compact is a compaction on a compact frame.
type Compact struct {
	// Phase says whether the compaction started or ended.
	Phase CompactPhase `json:"phase,omitempty"`
	// Reason is why the compaction happened.
	Reason string `json:"reason,omitempty"`
	// Summary is the compaction's summary. It is set on an end frame.
	Summary string `json:"summary,omitempty"`
	// Resume is the exact "# Resuming from a compaction" block the actor
	// installed. The client uses it as the new context verbatim.
	Resume string `json:"resume,omitempty"`
	// StepsSummarized is how many steps this checkpoint folded.
	StepsSummarized int `json:"steps_summarized,omitempty"`
	// Strategy is CHECKPOINT_STRATEGY_SAME_MODEL / SINGLE_PROMPT / ...
	Strategy string `json:"strategy,omitempty"`
	// SummaryModel is the model that wrote the summary.
	SummaryModel string `json:"summary_model,omitempty"`
	// Sync reports whether the checkpoint blocked the turn.
	Sync bool `json:"sync,omitempty"`
	// IncludedStepEnd is the global step index folded into Resume.
	IncludedStepEnd int `json:"included_step_end,omitempty"`
	// KeepRecent is the number of recent steps left outside the summary.
	KeepRecent int `json:"keep_recent,omitempty"`
	// TokensBefore is the token count before the compaction.
	TokensBefore int `json:"tokens_before,omitempty"`
	// TokensAfter is the estimated token count after it.
	TokensAfter int `json:"tokens_after,omitempty"`
	// Aborted marks a compaction that did not finish.
	Aborted bool `json:"aborted,omitempty"`
	// ElapsedMS is how long the compaction took, in milliseconds. It is
	// set on an end frame.
	//
	// The duration travels with the frame because the client cannot
	// measure it: the compaction ran on the server, and the client's own
	// clock says nothing about how long it took there.
	ElapsedMS int64 `json:"elapsed_ms,omitempty"`
}

// Notice is an out-of-band notice on a notice frame.
type Notice struct {
	// Kind names the sort of notice.
	Kind NoticeKind `json:"kind,omitempty"`
	// Text is the notice's text.
	Text string `json:"text,omitempty"`
	// URL is where the user can resolve the notice.
	URL string `json:"url,omitempty"`
	// Depth is the auto-continue attempt the banner reports.
	Depth int `json:"depth,omitempty"`
	// MaxDepth is how many auto-continue attempts the turn allows.
	MaxDepth int `json:"max_depth,omitempty"`
}

// Usage is a turn's token accounting.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens,omitempty"`
	CompletionTokens int `json:"completion_tokens,omitempty"`
	TotalTokens      int `json:"total_tokens,omitempty"`
	ReasoningTokens  int `json:"reasoning_tokens,omitempty"`
}

// Result is a turn's outcome, on a result frame.
type Result struct {
	// Reply is the turn's answer.
	Reply string `json:"reply,omitempty"`
	// Reasoning is the turn's thinking.
	Reasoning string `json:"reasoning,omitempty"`
	// Steps are the tool calls the turn made, in order.
	Steps []Step `json:"steps,omitempty"`
}

// Step is one finished tool call.
type Step struct {
	Name   string `json:"name"`
	Args   string `json:"args,omitempty"`
	Result string `json:"result,omitempty"`
	Err    bool   `json:"err,omitempty"`
}
