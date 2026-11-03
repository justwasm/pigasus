package statusline

import (
	"path/filepath"

	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/core/version"
)

// Product is the `product` member of the payload. The reference writes the
// application name.
const Product = "antigravity"

// exceeds200kTokens is the context size above which the reference reports
// `exceeds_200k_tokens`.
const exceeds200kTokens = 200000

// The `agent_state` member's vocabulary.
//
// The reference names seven states (official-docs/cli/statusline.md:67);
// the clone can tell four of them apart, and the three it cannot reach are
// recorded as 存疑 in the cycle report.
const (
	// StateIdle is the state between turns.
	StateIdle = "idle"
	// StateWorking is the state while the agent runs.
	StateWorking = "working"
	// StateToolUse is the state while a tool call is in flight.
	StateToolUse = "tool_use"
	// StateError is the state after a turn failed.
	StateError = "error"
)

// PayloadState is the session's facts the payload document is rendered
// from.
//
// The facts are the host's: the workspace, the conversation, the selected
// model, the signed-in account, the token counters and the quota. The
// document is the package's: it is the reference's `types.StatusLineData`,
// and every member name, its order and its omit rules are a wire
// contract — a script reads the members by name.
//
// Evidence:
//   - 【一手】 agy 1.4.7 was run with a capture script bound to
//     `statusLine.command`; the payloads it produced are the fixture in
//     payload_test.go.
//   - 【反编译】 the JSON tags were read out of the binary's string table
//     (`types.StatusLineData` and its member structs).
type PayloadState struct {
	// Cwd is the directory the CLI was started in.
	Cwd string
	// ConversationID is the active conversation's id.
	ConversationID string
	// ConversationTitle is the active conversation's name.
	ConversationTitle string
	// Model is the selected model's label.
	Model string
	// Effort is the selected model's reasoning effort.
	Effort string
	// AgentName is the active agent's name.
	AgentName string
	// CycleMode is the mode selected with Shift+Tab.
	CycleMode string
	// PlanTier is the signed-in account's tier.
	PlanTier string
	// Email is the signed-in account's email address.
	Email string
	// TerminalWidth is the live width of the interactive terminal.
	TerminalWidth int
	// Streaming reports whether the agent is running.
	Streaming bool
	// ToolInFlight reports whether a tool call is running inside the
	// streaming turn.
	ToolInFlight bool
	// TurnFailed reports whether the last turn ended in an error.
	TurnFailed bool
	// SandboxEnabled reports whether the terminal sandbox is on.
	SandboxEnabled bool
	// Quota maps a quota bucket id to its status.
	Quota map[string]QuotaBucket
	// TaskCount is the number of running background tasks.
	TaskCount int
	// PendingInputCount is the number of queued user messages.
	PendingInputCount int
	// Subagents lists the conversation's subagents.
	Subagents []SubagentInfo
	// VimMode is the vim editing mode's name ("NORMAL", "INSERT",
	// "VISUAL" or "VISUAL LINE"). Empty when vim editor mode is off.
	VimMode string
	// SessionPromptTokens is the prompt tokens sent so far.
	SessionPromptTokens int
	// SessionCompletionTokens is the completion tokens received so far.
	SessionCompletionTokens int
	// LastTurnPromptTokens is the last turn's prompt tokens.
	LastTurnPromptTokens int
	// LastTurnCompletionTokens is the last turn's completion tokens.
	LastTurnCompletionTokens int
	// ContextWindowSize is the model's context window, 0 when unknown.
	ContextWindowSize int
	// PricingModel is the model the session's tokens are billed by. It is
	// the status row's model label, falling back to the selected model.
	PricingModel string
}

// PayloadFrom renders the document the scripts read.
//
// The document is the reference's `types.StatusLineData`, so the shape is
// fixed: the member names, their order and their omit rules are part of
// the wire contract. See [Payload] and payload_test.go.
func PayloadFrom(s PayloadState) Payload {
	payload := Payload{
		Cwd:            s.Cwd,
		SessionID:      s.ConversationID,
		ConversationID: s.ConversationID,
		TranscriptPath: transcriptPath(s.ConversationID),
		Version:        version.Reference,
		Product:        Product,
		Workspace: Workspace{
			CurrentDir: s.Cwd,
			ProjectDir: s.Cwd,
		},
		ContextWindow: s.contextWindow(),
		AgentState:    AgentState(s.Streaming, s.ToolInFlight, s.TurnFailed),
		CycleMode:     s.CycleMode,
		PlanTier:      s.PlanTier,
		Email:         s.Email,
		TerminalWidth: s.TerminalWidth,
	}
	if s.ConversationTitle != "" {
		payload.ConversationTitle = s.ConversationTitle
	}
	if s.Model != "" {
		payload.Model = &ModelInfo{
			ID:          s.Model,
			DisplayName: s.Model,
			Effort:      s.Effort,
		}
	}
	if s.AgentName != "" {
		payload.Agent = &AgentInfo{Name: s.AgentName}
	}
	if vcs := DetectVCS(s.Cwd); vcs != nil {
		payload.VCS = vcs
	}
	payload.Sandbox = &SandboxInfo{Enabled: s.SandboxEnabled}
	if len(s.Quota) > 0 {
		payload.Quota = s.Quota
	}
	if s.TaskCount > 0 {
		payload.TaskCount = s.TaskCount
	}
	if s.PendingInputCount > 0 {
		payload.PendingInputCount = s.PendingInputCount
	}
	if len(s.Subagents) > 0 {
		payload.Subagents = s.Subagents
	}
	if s.VimMode != "" {
		payload.Vim = &VimInfo{Mode: s.VimMode}
	}
	if cost := PricingForModel(s.PricingModel, s.SessionPromptTokens, s.SessionCompletionTokens); cost > 0 {
		payload.Cost = &CostInfo{TotalUSD: cost, Estimated: true}
	}
	payload.Exceeds200kTokens = s.exceeds200kTokens()
	return payload
}

// contextWindow renders the token accounting.
//
// The reference's `used_percentage` is the share of the context window the
// *prompt* tokens take: 19136 prompt tokens in a 1048576-token window is
// 1.824951171875%, which is what the live payload carried.
func (s PayloadState) contextWindow() ContextWindow {
	used := ContextUsedPercent(s.SessionPromptTokens, s.ContextWindowSize)
	remaining := 0.0
	if s.ContextWindowSize > 0 {
		remaining = 100 - used
	}
	cw := ContextWindow{
		TotalInputTokens:    s.SessionPromptTokens,
		TotalOutputTokens:   s.SessionCompletionTokens,
		ContextWindowSize:   s.ContextWindowSize,
		UsedPercentage:      used,
		RemainingPercentage: remaining,
	}
	if s.hasUsage() {
		cw.CurrentUsage = &CurrentUsage{
			InputTokens:  s.LastTurnPromptTokens,
			OutputTokens: s.LastTurnCompletionTokens,
			// The clone's transports do not surface the provider's cache
			// counters, so the two cache members stay zero.
		}
	}
	return cw
}

// hasUsage reports whether the session has seen a model response.
func (s PayloadState) hasUsage() bool {
	return s.LastTurnPromptTokens != 0 || s.LastTurnCompletionTokens != 0
}

// AgentState is the reference's `agent_state` value for a session's facts.
//
// The reference names seven states (official-docs/cli/statusline.md:67):
// "idle" between turns, "working" while the agent runs, "tool_use" while a
// tool call is in flight, "reviewing" while a review is pending,
// "authenticating" while it signs in, "initializing" while it starts up
// and "error" after a failed turn.
//
// The clone can tell four of the seven apart: it has no start-up phase it
// renders a status line for, no separate sign-in phase, and no pending
// review. The three it cannot reach are recorded as 存疑 in the cycle
// report.
//
// The vocabulary is the payload's (the reference's own names), and the
// facts are the host's: a streaming turn, a tool call inside it and the
// last turn's failure.
//
// The precedence is the reference's: the tool call inside a running turn
// is the more precise state, and a failed turn is the state the session
// rests in until the next turn starts.
func AgentState(streaming, toolInFlight, turnFailed bool) string {
	switch {
	case streaming && toolInFlight:
		return StateToolUse
	case streaming:
		return StateWorking
	case turnFailed:
		return StateError
	default:
		return StateIdle
	}
}

// ContextUsedPercent is the share of the context window the prompt tokens
// take.
//
// It is the reference's own arithmetic — the payload's `used_percentage`
// member is the same number — and the status row's context cell shows the
// same value, so the formula has one owner.
func ContextUsedPercent(promptTokens, windowSize int) float64 {
	if windowSize <= 0 {
		return 0
	}
	return float64(promptTokens) / float64(windowSize) * 100
}

// exceeds200kTokens reports whether the conversation crossed the 200k
// context threshold. The reference reports null until the first response.
func (s PayloadState) exceeds200kTokens() *bool {
	if !s.hasUsage() {
		return nil
	}
	exceeds := s.SessionPromptTokens > exceeds200kTokens
	return &exceeds
}

// transcriptPath is where the reference keeps the conversation's
// transcript. It is the brain directory's `.system_generated/logs`
// subdirectory.
//
// Evidence 【一手】: agy 1.4.7's payload carried
// `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`,
// and the id-less form `<appDataDir>/brain/.system_generated/logs/transcript.jsonl`
// before the conversation started.
func transcriptPath(conversationID string) string {
	return filepath.Join(confhome.Antigravity(), "brain", conversationID,
		".system_generated", "logs", "transcript.jsonl")
}
