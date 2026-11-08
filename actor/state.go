package actor

import (
	"fmt"
	"sync"
	"time"
)

// LifecycleState indicates the current operational phase of the Actor.
type LifecycleState string

const (
	StateIdle            LifecycleState = "idle"
	StateRunning         LifecycleState = "running"
	StateWaitingApproval LifecycleState = "waiting_approval"
	StateClosed          LifecycleState = "closed"
)

// IsValid reports whether s is a recognized state.
func (s LifecycleState) IsValid() bool {
	switch s {
	case StateIdle, StateRunning, StateWaitingApproval, StateClosed:
		return true
	}
	return false
}

// StateSnapshot captures an immutable point-in-time view of the actor's state.
type StateSnapshot struct {
	ActorID              string         `json:"actor_id"`
	State                LifecycleState `json:"state"`
	WaitingToolCallID    string         `json:"waiting_tool_call_id,omitempty"`
	PendingMessagesCount int            `json:"pending_messages_count"`
	TotalTurns           int            `json:"total_turns"`
	TotalSteps           int            `json:"total_steps"`
	UpdatedAt            time.Time      `json:"updated_at"`
}

// actorStateMachine guards transitions between LifecycleStates.
type actorStateMachine struct {
	mu                sync.RWMutex
	current           LifecycleState
	waitingToolCallID string
}

func newActorStateMachine() *actorStateMachine {
	return &actorStateMachine{current: StateIdle}
}

func (m *actorStateMachine) State() LifecycleState {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current
}

func (m *actorStateMachine) WaitingToolCallID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.waitingToolCallID
}

// Transition changes state if the requested transition is valid.
func (m *actorStateMachine) Transition(to LifecycleState, toolCallID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	from := m.current
	if from == StateClosed {
		return fmt.Errorf("cannot transition from closed state to %s: %w", to, ErrActorClosed)
	}
	if from == to {
		return nil
	}

	valid := false
	switch from {
	case StateIdle:
		valid = (to == StateRunning || to == StateClosed)
	case StateRunning:
		valid = (to == StateIdle || to == StateWaitingApproval || to == StateClosed)
	case StateWaitingApproval:
		valid = (to == StateRunning || to == StateIdle || to == StateClosed)
	}
	if !valid {
		return fmt.Errorf("illegal state transition from %s to %s", from, to)
	}

	m.current = to
	m.waitingToolCallID = toolCallID
	return nil
}
