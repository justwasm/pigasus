package actor

import (
	"context"
)

// TurnRunner defines the standard turn invocation capabilities of an Actor
// via dedicated, turn-scoped event streams.
type TurnRunner interface {
	// Ask sends a message and returns an event stream dedicated to that turn.
	Ask(ctx context.Context, msg Message) (<-chan Event, error)
	// Prompt sends a text prompt as a user message and returns a dedicated event stream.
	Prompt(ctx context.Context, text string) (<-chan Event, error)
	// Run executes a text prompt synchronously to completion and returns the final assistant text.
	Run(ctx context.Context, text string) (string, error)
}

// Steerer defines the capability to inject dynamic guidance into an in-flight turn.
type Steerer interface {
	// Steer injects dynamic guidance into an in-flight turn without breaking the active step.
	Steer(instruction string) error
}

// TurnController defines dynamic out-of-band intervention for in-flight turns.
// These methods operate concurrently and out-of-band without being blocked by mailbox processing.
type TurnController interface {
	Steerer
	// CancelTurn aborts the active turn if one is running.
	CancelTurn(reason string) error
	// Approve resolves a pending tool execution approval.
	Approve(toolCallID string, approved bool, feedback string) error
}

// SteerTracker defines observation and management of pending steering instructions.
type SteerTracker interface {
	Steerer
	// PendingSteer returns a copy of pending steer instructions waiting to be processed.
	PendingSteer() []string
	// PopSteer removes and returns the most recent pending steer instruction.
	PopSteer() (string, bool)
	// ClearSteer clears all pending steer instructions for the active turn.
	ClearSteer() []string
}

// EventStreamer defines subscription to the observable events emitted by an actor.
type EventStreamer interface {
	// Subscribe returns a channel of events and an unsubscribe function.
	Subscribe() (<-chan Event, func())
	// Events returns a persistent subscription channel.
	Events() <-chan Event
}

// ActorHandle composes the core execution and control interfaces of an Actor.
type ActorHandle interface {
	TurnRunner
	TurnController
	SteerTracker
	EventStreamer

	ID() string
	Name() string
	Model() string
	State() StateSnapshot
	Close() error
}
