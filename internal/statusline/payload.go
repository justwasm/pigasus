package statusline

import "time"

// Payload is the JSON object the CLI pipes to a custom status-line script's
// stdin (and to a custom window-title script's stdin — the reference sends
// the very same document to both).
//
// The shape is the reference's `types.StatusLineData`. Field order, JSON
// names and omit rules are part of the wire contract: a script reads the
// fields by name, and the official examples published at
// github.com/google-antigravity/antigravity-cli/examples depend on the exact
// spelling (`context_window.used_percentage`, `model.display_name`,
// `vcs.branch`, `sandbox.enabled`, `artifact_count`, `subagents`,
// `task_count`, `workspace.current_dir`, `terminal_width`).
//
// Evidence:
//   - 【一手】 agy 1.4.7 was run with a capture script bound to
//     `statusLine.command`; the payloads it produced are the fixture in
//     payload_test.go.
//   - 【反编译】 the JSON tags were read out of the binary's string table
//     (`types.StatusLine*`, `types.StatusLineConfig`).
//
// The order below is the reference's declaration order. It was confirmed
// field by field against the live payloads: `conversation_title` sits
// between `conversation_id` and `transcript_path`, and every field that
// was populated appears in exactly this order.
type Payload struct {
	// Cwd is the directory the CLI was started in.
	Cwd string `json:"cwd"`
	// SessionID mirrors ConversationID; the reference keeps it for
	// compatibility with scripts written for the earlier shape.
	SessionID string `json:"session_id"`
	// ConversationID is the active conversation's ID (empty before the
	// first one starts).
	ConversationID string `json:"conversation_id"`
	// ConversationTitle is the active conversation's name. Omitted until
	// it has one.
	ConversationTitle string `json:"conversation_title,omitempty"`
	// TranscriptPath is where the conversation transcript is written. The
	// file may not exist yet.
	TranscriptPath string `json:"transcript_path,omitempty"`
	// Model describes the selected model. Nil until a model is selected —
	// and then the member is still written, as JSON null.
	//
	// Evidence 【一手】: agy 1.4.7 wrote `"model":null` while the model
	// was still being resolved.
	Model *ModelInfo `json:"model"`
	// Workspace holds the two directory paths a script cares about.
	Workspace Workspace `json:"workspace"`
	// Version is the CLI version string.
	Version string `json:"version"`
	// ContextWindow holds the token accounting.
	ContextWindow ContextWindow `json:"context_window"`
	// Exceeds200kTokens reports whether the conversation context crossed
	// 200,000 tokens. Nil (JSON null) before the first model response.
	Exceeds200kTokens *bool `json:"exceeds_200k_tokens"`
	// Agent names the active agent. Omitted for the default agent.
	Agent *AgentInfo `json:"agent,omitempty"`
	// Product is the application name ("antigravity"). Omitted when the
	// CLI does not know it.
	//
	// Evidence 【一手】: agy 1.4.7 omitted the member while it was still
	// starting up and wrote "antigravity" afterwards.
	Product string `json:"product,omitempty"`
	// Quota maps a quota bucket ID to its status. Omitted when empty.
	Quota map[string]QuotaBucket `json:"quota,omitempty"`
	// AgentState is the current state: "idle", "working", "tool_use",
	// "reviewing", "authenticating", "initializing" or "error".
	AgentState string `json:"agent_state,omitempty"`
	// VCS describes the version control system of the workspace. Omitted
	// outside a repository.
	VCS *VCSInfo `json:"vcs,omitempty"`
	// CycleMode is the mode selected with Shift+Tab: "accept-edits",
	// "auto" or "plan". Omitted in the default mode.
	CycleMode string `json:"cycle_mode,omitempty"`
	// Sandbox reports whether the terminal sandbox is on.
	Sandbox *SandboxInfo `json:"sandbox,omitempty"`
	// Subagents lists the running and finished subagents.
	Subagents []SubagentInfo `json:"subagents,omitempty"`
	// ArtifactCount is the number of artifacts produced in the
	// conversation. Omitted when 0.
	ArtifactCount int `json:"artifact_count,omitempty"`
	// PlanTier is the subscription tier of the signed-in account.
	PlanTier string `json:"plan_tier,omitempty"`
	// Email is the signed-in account's email address.
	Email string `json:"email,omitempty"`
	// PendingInputCount is the number of queued user messages. Omitted
	// when 0.
	PendingInputCount int `json:"pending_input_count,omitempty"`
	// ToolConfirmationPending is true while a tool confirmation dialog is
	// showing. Omitted otherwise.
	ToolConfirmationPending bool `json:"tool_confirmation_pending,omitempty"`
	// TaskCount is the number of running background tasks. Omitted when 0.
	TaskCount int `json:"task_count,omitempty"`
	// TerminalWidth is the live width of the interactive terminal.
	TerminalWidth int `json:"terminal_width,omitempty"`
	// Vim holds the Vim editing state. Present only when Vim editor mode
	// is enabled.
	Vim *VimInfo `json:"vim,omitempty"`
	// Cost holds the estimated spend. Omitted until a cost is reported.
	Cost *CostInfo `json:"cost,omitempty"`
}

// ModelInfo describes the selected model.
type ModelInfo struct {
	// ID is the model's identifier.
	ID string `json:"id"`
	// DisplayName is the model's human-readable label.
	DisplayName string `json:"display_name"`
	// Effort is the reasoning effort ("low" through "max"). Only models
	// with several effort levels carry one.
	Effort string `json:"effort,omitempty"`
}

// Workspace holds the workspace paths.
type Workspace struct {
	// CurrentDir is the directory the CLI is running in.
	CurrentDir string `json:"current_dir"`
	// ProjectDir is the root of the workspace project.
	ProjectDir string `json:"project_dir"`
}

// ContextWindow holds the context token accounting.
type ContextWindow struct {
	// TotalInputTokens is the prompt tokens sent so far.
	TotalInputTokens int `json:"total_input_tokens"`
	// TotalOutputTokens is the completion tokens received so far.
	TotalOutputTokens int `json:"total_output_tokens"`
	// ContextWindowSize is the model's context size, 0 when unknown.
	ContextWindowSize int `json:"context_window_size"`
	// UsedPercentage is the share of the context window in use.
	UsedPercentage float64 `json:"used_percentage"`
	// RemainingPercentage is the share still free.
	RemainingPercentage float64 `json:"remaining_percentage"`
	// CurrentUsage is the last turn's usage. Nil (JSON null) before the
	// first model response.
	CurrentUsage *CurrentUsage `json:"current_usage"`
}

// CurrentUsage is one turn's token usage.
type CurrentUsage struct {
	// InputTokens is the turn's prompt tokens.
	InputTokens int `json:"input_tokens"`
	// OutputTokens is the turn's completion tokens.
	OutputTokens int `json:"output_tokens"`
	// CacheCreationInputTokens is the turn's cache-write tokens.
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	// CacheReadInputTokens is the turn's cache-read tokens.
	CacheReadInputTokens int `json:"cache_read_input_tokens"`
}

// QuotaBucket is one quota bucket's status.
type QuotaBucket struct {
	// RemainingFraction is the share of the bucket left.
	RemainingFraction float64 `json:"remaining_fraction,omitempty"`
	// RemainingAmount is the absolute amount of the bucket left, for
	// buckets that are metered in credits rather than in a fraction.
	RemainingAmount float64 `json:"remaining_amount,omitempty"`
	// ResetTime is when the bucket refills.
	ResetTime *time.Time `json:"reset_time,omitempty"`
	// ResetInSeconds is how long until the bucket refills.
	ResetInSeconds int `json:"reset_in_seconds,omitempty"`
	// Disabled reports whether the bucket is switched off.
	Disabled bool `json:"disabled,omitempty"`
}

// VCSInfo describes the workspace's version control system.
type VCSInfo struct {
	// Type is "git", "jj" or "hg".
	Type string `json:"type,omitempty"`
	// Branch is the checked-out branch.
	Branch string `json:"branch,omitempty"`
	// Dirty reports whether the working tree has uncommitted changes.
	Dirty bool `json:"dirty,omitempty"`
}

// SandboxInfo reports the terminal sandbox's state.
type SandboxInfo struct {
	// Enabled is true while the terminal sandbox is on.
	Enabled bool `json:"enabled"`
}

// SubagentInfo describes one subagent.
type SubagentInfo struct {
	// Name is the subagent's name.
	Name string `json:"name,omitempty"`
	// Role is the subagent's role.
	Role string `json:"role,omitempty"`
	// Status is "running" or "completed".
	Status string `json:"status,omitempty"`
}

// AgentInfo names the active agent.
type AgentInfo struct {
	// Name is the agent's name.
	Name string `json:"name,omitempty"`
}

// VimInfo holds the Vim editing state.
type VimInfo struct {
	// Mode is "NORMAL", "INSERT", "VISUAL" or "VISUAL LINE".
	Mode string `json:"mode"`
}

// CostInfo is the estimated spend of the session.
type CostInfo struct {
	// TotalUSD is the estimated total spend, subagents included.
	TotalUSD float64 `json:"total_usd"`
	// SubagentUSD is the estimated spend of the subagents alone.
	SubagentUSD float64 `json:"subagent_usd"`
	// Estimated reports whether the number is an estimate rather than a
	// billed amount.
	Estimated bool `json:"estimated"`
}
