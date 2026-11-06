// Package actor provides an Actor-model runtime for autonomous AI agent orchestration.
//
// In this architecture, an agent is an Actor:
//   - Mailbox: Receives asynchronous input messages (user prompts, scheduled wakeups,
//     approval decisions, steering instructions, cancellation signals).
//   - State Machine: Manages the turn lifecycle (Idle, Running, WaitingApproval,
//     Paused, Closed) with zero external concurrent state mutation.
//   - Event Stream: Non-blocking broadcast of observable events (thought deltas,
//     text deltas, tool executions, approval requests, turn completion).
//   - Decoupled Provider & Tools: Clean dependency injection for any LLM backend
//     and tool definition without coupling to internal host machinery.
//   - Built-in Scheduling: Reactive timer and cron capabilities that wake the
//     actor by dispatching self-addressed messages into its mailbox.
//
// Package actor has zero dependencies on internal/ packages and is safe for
// direct consumption by external applications.
package actor

import (
	"errors"
	"time"
)

var (
	// ErrActorClosed is returned when sending or operating on a closed Actor.
	ErrActorClosed = errors.New("actor: actor is closed")
	// ErrMailboxFull is returned when sending to a full, non-blocking mailbox.
	ErrMailboxFull = errors.New("actor: mailbox is full")
	// ErrTurnCancelled is returned when a turn is cancelled by signal or context.
	ErrTurnCancelled = errors.New("actor: turn was cancelled")
	// ErrToolNotFound is returned when the model calls an unregistered tool.
	ErrToolNotFound = errors.New("actor: tool not found")
	// ErrApprovalDenied is returned when human-in-the-loop approval is rejected.
	ErrApprovalDenied = errors.New("actor: tool approval was denied")
	// ErrInvalidSchedule is returned when scheduling parameters are invalid.
	ErrInvalidSchedule = errors.New("actor: invalid schedule specification")
	// ErrMaxStepsReached is returned when consecutive tool steps reach the maximum iteration limit.
	ErrMaxStepsReached = errors.New("actor: maximum steps per turn reached")
)

// Config configures a new Actor instance.
type Config struct {
	// ID is the unique identifier for the actor. If empty, a random ID is generated.
	ID string

	// Name is an optional human-readable name for the actor.
	Name string

	// SystemPrompt is the base instructions for the agent.
	SystemPrompt string

	// Model specifies the default model identifier to use (e.g. "gemini-2.5-pro").
	Model string

	// Provider is the LLM completion engine used by the actor. Required.
	Provider Provider

	// Tools is the initial set of tools registered with the actor.
	Tools []Tool

	// MailboxSize is the buffer capacity of the actor's incoming message queue.
	// Defaults to 128 if <= 0.
	MailboxSize int

	// EventBufferSize is the buffer capacity for event subscriber channels.
	// Defaults to 256 if <= 0.
	EventBufferSize int

	// MaxStepsPerTurn limits consecutive tool execution loops within a single turn.
	// Defaults to 0 (unlimited).
	MaxStepsPerTurn int

	// AutoCompactThreshold triggers trajectory compaction when history steps
	// exceed this count. Set to 0 to disable auto-compaction.
	AutoCompactThreshold int

	// Temperature controls LLM generation sampling.
	Temperature *float32

	// AutoContinue enables automatic continuation when model output is truncated by length cap.
	// Defaults to true when nil.
	AutoContinue *bool

	// MaxTokens is the maximum tokens for model generations.
	MaxTokens int

	// TimeNow allows overriding clock in tests. If nil, time.Now is used.
	TimeNow func() time.Time

	// InitialHistory allows pre-populating conversation messages into the actor's trajectory upon spawn.
	InitialHistory []ConversationMessage

	// InitialTurns allows pre-populating completed turn records directly into the actor's trajectory upon spawn.
	InitialTurns []*TurnRecord
}

// AutoContinueEnabled returns whether automatic continuation on length truncation is enabled.
func (c *Config) AutoContinueEnabled() bool {
	if c.AutoContinue == nil {
		return true
	}
	return *c.AutoContinue
}

// EnsureDefaults fills in default configuration values where omitted.
func (c *Config) EnsureDefaults() {
	if c.MailboxSize <= 0 {
		c.MailboxSize = 128
	}
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 256
	}
	// MaxStepsPerTurn defaults to 0 (unlimited)
	if c.TimeNow == nil {
		c.TimeNow = time.Now
	}
}
