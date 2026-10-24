// types.go — provider-neutral chat and streaming interfaces.
package models

import (
	"context"
)

// ChatRequest is the provider-neutral input. We re-use the backend.Message
// shape so callers can pass the same []Message the shim built.
type ChatRequest struct {
	// ExternalModel overrides Model.ExternalModelName for this turn.
	// Empty means "use the registry value".
	ExternalModel string
	// SystemPrompt is prepended as a system role. Empty ⇒ no system msg.
	SystemPrompt string
	// Messages is the conversation tail (user / assistant only; the system
	// message has been hoisted out into SystemPrompt by the caller).
	Messages []ChatMessage
	// MaxTokens is optional. 0 ⇒ let the provider default.
	MaxTokens int
	// Temperature is optional. 0 ⇒ let the provider default.
	Temperature float64
	// Stream is set by Stream; the Chat method ignores it.
	Stream bool
	// ReasoningEffort is the OpenAI o-series hint. "" ⇒ don't send.
	ReasoningEffort string
	// Tools lists the functions the model may call (cycle 274). Nil means
	// "no tools", which keeps every pre-274 caller byte-identical on the wire.
	Tools []ToolSpec
}

// ChatMessage is a single conversation turn. Role is "user" or "assistant";
// "system" is hoisted out into ChatRequest.SystemPrompt.
//
// ToolCalls / ToolCallID are only meaningful on assistant / tool messages
// respectively and are omitted from the wire when empty.
type ChatMessage struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ChatResponse is the non-streaming reply. Content is the assembled text;
// Usage mirrors the OpenAI usage block (or its Anthropic / Gemini shape).
type ChatResponse struct {
	Content      string
	Model        string
	FinishReason string
	PromptTokens int
	OutputTokens int
	TotalTokens  int
	// ToolCalls holds any function calls the model asked for (cycle 274).
	ToolCalls []ToolCall
	Raw       []byte // raw upstream body, for debugging
}

// StreamEvent is one element of a streaming response. We mirror the
// backend.StreamChunk shape so a single bridge function can lift provider
// events into the existing agent.Loop machinery.
type StreamEvent struct {
	Text  string
	Done  bool
	Error error
	// ToolCalls is set when the model asked to call one or more functions.
	// Providers that stream partial arguments (OpenAI) accumulate them and
	// emit a single event with the finished calls just before Done.
	ToolCalls []ToolCall
	// Meta is set on the final chunk (Done=true) and mirrors the
	// backend.ChunkMeta fields we care about.
	Meta *StreamMeta
}

// StreamMeta is the metadata we forward on the terminal chunk.
type StreamMeta struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// Provider is the seven-impl interface. Chat is used by the CLI smoke test
// and by anything that doesn't need SSE; Stream is what the TUI / shims use.
type Provider interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
	Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error)
	// Name is the provider handle from the registry ("openai", "openrouter", ...).
	// Returned for log lines and `openagy-models test` output.
	Name() string
}
