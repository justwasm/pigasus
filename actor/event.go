package actor

import "time"

// EventType classifies observable occurrences emitted by a turn.
type EventType string

const (
	EventTurnStarted      EventType = "turn_started"
	EventThought          EventType = "thought"
	EventDelta            EventType = "delta"
	EventToolCallStarted  EventType = "tool_call_started"
	EventToolCallFinished EventType = "tool_call_finished"
	EventApprovalRequired EventType = "approval_required"
	EventSteerApplied     EventType = "steer_applied"
	EventTurnCompleted    EventType = "turn_completed"
	EventCancelled        EventType = "cancelled"
	EventError            EventType = "error"
	EventUsage            EventType = "usage"
	EventCheckpoint       EventType = "checkpoint"
)

// Event represents an individual event frame emitted by a turn.
type Event struct {
	Type      EventType `json:"type"`
	ActorID   string    `json:"actor_id"`
	Timestamp time.Time `json:"timestamp"`

	// Specific event payload fields
	Delta            string         `json:"delta,omitempty"`
	Thought          string         `json:"thought,omitempty"`
	SteerInstruction string         `json:"steer_instruction,omitempty"`
	ToolCall         *ToolCall      `json:"tool_call,omitempty"`
	ToolResult       *ToolResult    `json:"tool_result,omitempty"`
	Usage            *TokenUsage    `json:"usage,omitempty"`
	Error            string         `json:"error,omitempty"`
	Err              error          `json:"-"`
	TurnSummary      *TurnSummary   `json:"turn_summary,omitempty"`
	Checkpoint       *Checkpoint    `json:"checkpoint,omitempty"`
	Metadata         map[string]any `json:"metadata,omitempty"`
}

// TurnSummary provides the final outcome of an executed turn.
type TurnSummary struct {
	Status         string        `json:"status"` // "done", "error", "cancelled"
	Output         string        `json:"output"`
	StepsCount     int           `json:"steps_count"`
	Duration       time.Duration `json:"duration"`
	TotalTokens    int           `json:"total_tokens,omitempty"`
	PromptTokens   int           `json:"prompt_tokens,omitempty"`
	OutputTokens   int           `json:"output_tokens,omitempty"`
	ThoughtsTokens int           `json:"thoughts_tokens,omitempty"`
}
