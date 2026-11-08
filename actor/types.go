// Package actor provides a serialized turn runtime for autonomous AI agent
// orchestration.
//
// One goroutine owns the turn loop: submitted turns queue in a mailbox and run
// one at a time. There is no message vocabulary and no broadcast — a turn is a
// TurnRequest, and its result is a TurnHandle.
//   - State Machine: the turn lifecycle (Idle, Running, WaitingApproval,
//     Closed) is guarded so no illegal transition is silent.
//   - Event Stream: every turn has a lossless, per-turn event channel. A slow
//     reader applies backpressure instead of losing the completion event.
//   - Per-turn configuration: model, system prompt, provider and tools travel
//     with the request, so a queued turn cannot be rewritten by the next one.
//
// Cancellation, steering and approval are methods, not messages: they must
// take effect while the loop is busy, which is exactly when a queued message
// would not be read. Cancellation is scoped by the TurnHandle the caller holds.
//
// Package actor has zero dependencies on internal/ packages and is safe for
// direct consumption by external applications.
package actor

import (
	"context"
	"errors"
	"time"
)

var (
	// ErrActorClosed is returned when submitting a turn to a closed Actor.
	ErrActorClosed = errors.New("actor: actor is closed")
	// ErrTurnCancelled is returned when a turn is cancelled by signal or context.
	ErrTurnCancelled = errors.New("actor: turn was cancelled")
	// ErrTurnNotFound is returned when a turn-scoped operation names a turn
	// that is neither running nor queued.
	ErrTurnNotFound = errors.New("actor: turn is not active")
	// ErrAutoContinueLimit is returned when a turn has recovered from
	// truncation or transport interruption AutoContMaxDepth times and still
	// cannot finish; the next step is the user's.
	ErrAutoContinueLimit = errors.New("actor: auto-continue limit reached; user input required")
)

// mailboxSize is the number of turns that may wait behind the running one.
const mailboxSize = 128

// Config configures a new Actor instance.
type Config struct {
	// ID is the unique identifier for the actor. If empty, a random ID is generated.
	ID string

	// Default is the configuration a turn inherits when its TurnRequest does
	// not override a field. Provider is required.
	Default TurnConfig

	// EventBufferSize is the buffer capacity of a turn's event channel.
	// Defaults to 256 if <= 0.
	EventBufferSize int

	// AutoContinue enables automatic continuation when model output is truncated by length cap.
	// Defaults to true when nil.
	AutoContinue *bool

	// TimeNow is the clock the actor stamps its own facts with: event
	// timestamps, turn records, state snapshots. If nil, time.Now is used.
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
	if c.EventBufferSize <= 0 {
		c.EventBufferSize = 256
	}
	if c.TimeNow == nil {
		c.TimeNow = time.Now
	}
}

// TurnConfig is the configuration a turn runs under.
//
// It appears twice: as Config.Default (the actor's defaults) and embedded in
// TurnRequest (per-turn overrides). A zero field inherits the default for that
// turn; Tools is the exception — nil inherits, an empty non-nil slice means the
// turn runs with no tools.
type TurnConfig struct {
	Model        string
	SystemPrompt string
	Provider     Provider
	Tools        []Tool
	Temperature  *float32
	MaxTokens    int

	// Checkpoint is asked before every model call. A nil callback means the
	// host does not checkpoint. The callback only inspects the probe and
	// returns a decision; it must not mutate the actor or cancel the turn.
	Checkpoint CheckpointFunc

	// tools is the resolved snapshot a turn executes against. It is set when
	// the request is submitted, never by a caller.
	tools *ToolRegistry
}

// TurnRequest is one turn: its prompt and the configuration it runs under.
//
// The configuration travels with the request, not with the actor, so two
// queued turns keep their own model, provider and tools.
type TurnRequest struct {
	TurnConfig
	Prompt string
}

// CheckpointProbe is the state the actor reports at a step boundary.
//
// It is deliberately cheap: the host can decide from LastStep/TurnUsage
// whether it needs the full transcript, which it can read through
// Actor.Trajectory().CheckpointView() without the actor copying it.
type CheckpointProbe struct {
	TurnID          string
	StepIndex       int
	GlobalStepIndex int
	Model           string
	LastStep        *Step
	TurnUsage       TokenUsage
}

// CheckpointDecision tells the actor how to replace the model context before
// the next call.
type CheckpointDecision struct {
	// Apply reports whether the actor should record the checkpoint and use
	// ResumeText for the next call.
	Apply bool
	// ResumeText is the reference's "# Resuming from a compaction" block.
	ResumeText string
	// Summary is the model's summary, kept for hosts and persistence.
	Summary string
	// Model is the model that wrote the summary.
	Model string
	// Strategy is CHECKPOINT_STRATEGY_SAME_MODEL / SINGLE_PROMPT / ...
	Strategy string
	// IncludedThroughStep is the global step index folded into ResumeText.
	IncludedThroughStep int
	// KeepRecent is the number of most recent steps left outside the summary.
	KeepRecent int
	// KeepInput reports that the current turn's input is not folded into the
	// summary and must still be sent to the model.
	KeepInput bool
	// TokensBefore and TokensAfter are the checkpoint's own accounting.
	TokensBefore int
	TokensAfter  int
	// StepsSummarized is the number of steps folded into this checkpoint.
	StepsSummarized int
	// Sync reports whether the checkpoint blocked the turn.
	Sync bool
}

// CheckpointFunc is asked before each model call. Returning Apply=false keeps
// the turn's full context; an error that is not context.Canceled is treated as
// a checkpoint failure and leaves the full context in place. The host decides
// whether a failure is fatal by returning ErrCheckpointRequired.
type CheckpointFunc func(ctx context.Context, probe CheckpointProbe) (CheckpointDecision, error)

// ErrCheckpointRequired reports that the context cannot fit another model call
// and the checkpoint did not produce a replacement.
var ErrCheckpointRequired = errors.New("actor: checkpoint required before the next model call")
