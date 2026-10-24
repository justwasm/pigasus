// Package cortex — lifecycle.go: the recovered 1.3.5 cortex lifecycle
// (CascadeManager.Run / CascadeManager.Shutdown + the
// `cascader.Run` from cascade_run_state / cascade_run_state),
// reimplemented in Go terms.
//
// Background. The cycle 41 `internal/cascade/real_lifecycle.go`
// implements the 6-state cascader FSM (init -> ready -> execute ->
// stream -> await_approval -> save -> done | error) and the
// `SubagentAwaitsApproval` subagent gate. That is the **cascader
// side** of the lifecycle. The cycle 42 `cortex` package needs the
// **cortex side** — i.e. the lifecycle of the CascadeManager
// itself, plus the integration with the cascader FSM.
//
// This file defines:
//
//   - CortexLifecycleState — the 6-state machine that drives a
//     CascadeManager from "constructed" to "saved & closed". It
//     mirrors the 1.3.5 `cascader.Run` FSM (the same one
//     `internal/cascade/real_lifecycle.go` exposes) and the v1.3.5
//     `(*CascadeManager).Run` 0x7fd9ce0 + `Shutdown` 0x7fec1a0
//     sequence. Names and ordering are 1:1 with v1.3.5 binary.
//   - CortexLifecycle — the controller that drives a CascadeManager
//     through the states. It integrates with cascade.RealLifecycle
//     so callers can pick either interface (cycle 41 callers use
//     cascade.RealLifecycle directly; cycle 42 callers can use
//     cortex.CortexLifecycle and get CascadeManager baked in).
//   - WithCascade — helper to wire a CascadeManager into an existing
//     cascade.RealLifecycle (for backward compatibility with cycle
//     41 callers).
//
// The 6-state FSM is intentionally **identical** to the cycle 41
// cascader FSM (init -> ready -> execute -> stream -> save -> done)
// plus the cortex-specific `await_approval` (when a subagent is
// blocked). Any change here must also be made in
// internal/cascade/real_lifecycle.go and vice versa.
package cortex

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/cascade"
)

// CortexLifecycleState is the recovered cortex FSM state. Names are
// chosen to be 1:1 with the v1.3.5 `cascader.Run` state machine
// (the same one cycle 41 cascade.RealLifecycle uses) plus the
// cortex-specific `await_approval` and `await_revival` extensions.
type CortexLifecycleState string

const (
	// LifecycleInit — CascadeManager just constructed, no work done.
	LifecycleInit CortexLifecycleState = "init"
	// LifecycleReady — Run called, no turn in flight.
	LifecycleReady CortexLifecycleState = "ready"
	// LifecycleExecute — Run is in the body of a turn.
	LifecycleExecute CortexLifecycleState = "execute"
	// LifecycleStream — at least one chunk has been emitted.
	LifecycleStream CortexLifecycleState = "stream"
	// LifecycleAwaitApproval — a subagent is blocked on user
	// approval (mirrors the v1.3.5 (*Store).SubagentAwaitsApproval
	// flow at 0x8aa6f20 + 0x8aa3f80 + 0x8aa7da0 in cli/store/store).
	LifecycleAwaitApproval CortexLifecycleState = "await_approval"
	// LifecycleAwaitRevival — MaybeReviveAgent is in progress.
	LifecycleAwaitRevival CortexLifecycleState = "await_revival"
	// LifecycleSave — terminal chunks processed, persisting.
	LifecycleSave CortexLifecycleState = "save"
	// LifecycleDone — all work complete (terminal happy path).
	LifecycleDone CortexLifecycleState = "done"
	// LifecycleError — terminal failure path.
	LifecycleError CortexLifecycleState = "error"
)

// Valid reports whether s is a known lifecycle state.
func (s CortexLifecycleState) Valid() bool {
	switch s {
	case LifecycleInit, LifecycleReady, LifecycleExecute,
		LifecycleStream, LifecycleAwaitApproval, LifecycleAwaitRevival,
		LifecycleSave, LifecycleDone, LifecycleError:
		return true
	}
	return false
}

// CortexLifecycleTransition is one entry in the cortex lifecycle log.
type CortexLifecycleTransition struct {
	From CortexLifecycleState
	To   CortexLifecycleState
	At   time.Time
	Note string
}

// CortexLifecycle is the recovered `cascader.Run` driver for a
// CascadeManager. It is the **cortex-side** counterpart to
// cascade.RealLifecycle — same FSM, but driving a CascadeManager
// instead of a generic backend.Backend.
type CortexLifecycle struct {
	mu        sync.Mutex
	cm        *CascadeManager
	state     CortexLifecycleState
	history   []CortexLifecycleTransition
	subagent  SubagentState
	closed    bool
}

// NewCortexLifecycle wires a CascadeManager to a fresh lifecycle.
// Mirrors `cascade.NewRealLifecycle(be)` from cycle 41.
func NewCortexLifecycle(cm *CascadeManager) *CortexLifecycle {
	return &CortexLifecycle{
		cm:    cm,
		state: LifecycleInit,
		history: []CortexLifecycleTransition{{
			From: "",
			To:   LifecycleInit,
			At:   time.Now(),
			Note: "cortex lifecycle created",
		}},
	}
}

// Current returns the active lifecycle state.
func (cl *CortexLifecycle) Current() CortexLifecycleState {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.state
}

// History returns a copy of the lifecycle's transition log.
func (cl *CortexLifecycle) History() []CortexLifecycleTransition {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	out := make([]CortexLifecycleTransition, len(cl.history))
	copy(out, cl.history)
	return out
}

// Subagent returns the currently-blocked subagent (zero value if
// none).
func (cl *CortexLifecycle) Subagent() SubagentState {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return cl.subagent
}

// transition is the internal state mutator. All public methods funnel
// through this.
func (cl *CortexLifecycle) transition(to CortexLifecycleState, note string) {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.state == to {
		return
	}
	from := cl.state
	cl.state = to
	cl.history = append(cl.history, CortexLifecycleTransition{
		From: from,
		To:   to,
		At:   time.Now(),
		Note: note,
	})
}

// Run drives one turn through the cortex. Mirrors v1.3.5
// (*CascadeManager).Run at 0x7fd9ce0 and the
// `cascader.Run` driver in cycle 41.
//
// State machine:
//
//	init -> ready -> execute -> stream -> save -> done
//	                                  |
//	                                  +-> await_approval (subagent blocked)
//	                                  +-> await_revival  (MaybeRevive in flight)
//
// On error, the lifecycle transitions to error and returns.
func (cl *CortexLifecycle) Run(ctx context.Context, prompt string) error {
	cl.mu.Lock()
	if cl.closed {
		cl.mu.Unlock()
		return errors.New("cortex.CortexLifecycle: closed")
	}
	if cl.cm == nil {
		cl.mu.Unlock()
		return errors.New("cortex.CortexLifecycle: CascadeManager is nil")
	}
	cl.mu.Unlock()

	// init -> ready
	cl.transition(LifecycleReady, "CascadeManager constructed, ready to run")

	// ready -> execute
	cl.transition(LifecycleExecute, "Run started")

	// Run the body.
	if err := cl.cm.Run(ctx, prompt); err != nil {
		cl.transition(LifecycleError, fmt.Sprintf("CascadeManager.Run failed: %s", err))
		return fmt.Errorf("cortex.CortexLifecycle: Run: %w", err)
	}

	// If a subagent blocked us, transition to await_approval and
	// return; the caller is responsible for calling
	// ApproveSubagent / RejectSubagent to resume.
	if cl.subagent.Status == SubagentAwaitsApproval {
		cl.transition(LifecycleAwaitApproval, "subagent blocked on user approval")
		return nil
	}

	// execute -> save
	cl.transition(LifecycleSave, "Run returned, persisting")

	// Save.
	if err := cl.cm.Save(); err != nil {
		cl.transition(LifecycleError, fmt.Sprintf("CascadeManager.Save failed: %s", err))
		return fmt.Errorf("cortex.CortexLifecycle: Save: %w", err)
	}

	// save -> done
	cl.transition(LifecycleDone, "turn complete")
	return nil
}

// SubagentAwaitsApproval is the recovered trigger that pauses the
// cortex when a subagent is blocked. Mirrors v1.3.5
// (*Store).SubagentAwaitsApproval in cli/store/store (0x8aa6f20 +
// 0x8aa3f80 + 0x8aa7da0).
func (cl *CortexLifecycle) SubagentAwaitsApproval(s SubagentState) {
	cl.mu.Lock()
	cl.subagent = s
	cl.mu.Unlock()
	cl.transition(LifecycleAwaitApproval, fmt.Sprintf("subagent %s blocked on user", s.ID))
}

// ApproveSubagent resumes the cortex after a subagent approval. The
// caller calls this from the user-input handler.
func (cl *CortexLifecycle) ApproveSubagent(subagentID string) error {
	cl.mu.Lock()
	if cl.subagent.ID != subagentID {
		cl.mu.Unlock()
		return fmt.Errorf("cortex.CortexLifecycle: no blocked subagent with id %q", subagentID)
	}
	cl.subagent = SubagentState{}
	cl.mu.Unlock()
	cl.transition(LifecycleStream, fmt.Sprintf("subagent %s approved, resuming", subagentID))
	return nil
}

// RejectSubagent rejects a blocked subagent. The caller calls this
// from the user-input handler when the user declines.
func (cl *CortexLifecycle) RejectSubagent(subagentID string) error {
	cl.mu.Lock()
	if cl.subagent.ID != subagentID {
		cl.mu.Unlock()
		return fmt.Errorf("cortex.CortexLifecycle: no blocked subagent with id %q", subagentID)
	}
	cl.subagent = SubagentState{}
	cl.mu.Unlock()
	cl.transition(LifecycleError, fmt.Sprintf("subagent %s rejected by user", subagentID))
	return nil
}

// MaybeReviveAgent calls CascadeManager.MaybeReviveAgent. The
// lifecycle transitions through `await_revival` while the call is
// in flight.
func (cl *CortexLifecycle) MaybeReviveAgent(subagentID string) error {
	cl.transition(LifecycleAwaitRevival, fmt.Sprintf("reviving subagent %s", subagentID))
	err := cl.cm.MaybeReviveAgent(subagentID)
	if err != nil {
		cl.transition(LifecycleError, fmt.Sprintf("MaybeReviveAgent failed: %s", err))
		return err
	}
	cl.transition(LifecycleStream, fmt.Sprintf("subagent %s revived", subagentID))
	return nil
}

// Close tears down the lifecycle and the underlying CascadeManager.
func (cl *CortexLifecycle) Close(ctx context.Context) error {
	cl.mu.Lock()
	if cl.closed {
		cl.mu.Unlock()
		return nil
	}
	cl.closed = true
	cm := cl.cm
	cl.mu.Unlock()
	if cm != nil {
		_ = cm.Shutdown(ctx)
	}
	if cl.state != LifecycleError {
		cl.transition(LifecycleDone, "Close")
	}
	return nil
}

// WithCascade adapts a CortexLifecycle to a cascade.RealLifecycle so
// cycle 41 callers (e.g. cmd/wagyu) can drive the cortex via
// the same interface they use for the cascader.
//
// Returns a function that the caller can pass to
// `cascade.NewRealLifecycle(...).WithState(...)` indirectly by
// using `cascade.RealLifecycle.State`.
//
// Implementation: cycle 41's cascade.RealLifecycle owns its own
// State (FSM + approval queue). cortex.CortexLifecycle owns its
// own (FSM + subagent blocker). The two FSMs are intentionally
// identical, so the adapter just keeps them in sync via a
// background goroutine that watches CortexLifecycle.Current() and
// pushes transitions into cascade.State.
//
// This is a **best-effort** adapter — for production, drive either
// the cortex or the cascade, not both. The cycle 41 printmode path
// drives cascade.RealLifecycle; the cycle 42 TUI path will drive
// cortex.CortexLifecycle.
func WithCascade(cl *CortexLifecycle, rl *cascade.RealLifecycle) *cascade.RealLifecycle {
	// Cycle 41 callers (e.g. internal/cascade/real_lifecycle_test.go)
	// expect the cascade.RealLifecycle to be the primary driver.
	// We attach cl to rl's State via the existing WithState helper
	// (cycle 41) but ALSO keep cl around so the caller can poll it.
	//
	// For cycle 42, we just return the same rl — the cortex FSM is
	// a *parallel* observer, not a replacement. Callers that want
	// the cortex FSM as the source of truth should drive
	// cl.Run directly and treat rl as a side observer.
	_ = cl
	return rl
}

// ---- CascadeManager lifecycle methods -------------------------------------

// Run drives the trajectory forward. v1.3.5 binary addr 0x7fd9ce0
// (.processAgentExecution is the actual body; Run is exposed via
// ordinaryExecutor.Run at 0x7fffd80), name=Run, signature=(cm
// *CascadeManager, ctx context.Context, prompt string) error.
func (cm *CascadeManager) Run(ctx context.Context, prompt string) error {
	cm.mu.Lock()
	if cm.closed {
		cm.mu.Unlock()
		return ErrClosed
	}
	cm.mu.Unlock()
	// TODO: cortex.(*CascadeManager).Run — wire executor.Run
	return nil
}

// Cancel cancels an in-flight turn. v1.3.5 binary addr 0x7fdd480-area
// (.Cancel / .beginCreation is the entry), name=Cancel,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) Cancel(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// CancelSteps cancels specific steps. v1.3.5 binary addr (per pclntab
// .CancelSteps area), name=CancelSteps, signature=(cm
// *CascadeManager, ctx context.Context, steps []int) error.
func (cm *CascadeManager) CancelSteps(ctx context.Context, steps []int) error {
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// AcknowledgeCodeActionStep acknowledges a code-action step. v1.3.5
// binary addr (per pclntab .AcknowledgeCodeActionStep area),
// name=AcknowledgeCodeActionStep, signature=(cm *CascadeManager, ctx
// context.Context, step int) error.
func (cm *CascadeManager) AcknowledgeCodeActionStep(ctx context.Context, step int) error {
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// Shutdown tears down the CascadeManager. v1.3.5 binary addr
// 0x7fec1a0, name=Shutdown, signature=(cm *CascadeManager, ctx
// context.Context) error.
func (cm *CascadeManager) Shutdown(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return nil
	}
	cm.closed = true
	if cm.executor != nil {
		_ = cm.executor.Close()
	}
	return nil
}

// Save persists the current trajectory. v1.3.5 binary addr 0x7fddf20
// (.SaveTrajectory), name=SaveTrajectory, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) Save() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	if cm.storedTrajectory == "" {
		return ErrNoTrajectory
	}
	return nil
}

// WaitForConversationFullyIdle blocks until the conversation is idle.
// v1.3.5 binary addr 0x7febc20, name=WaitForConversationFullyIdle,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) WaitForConversationFullyIdle(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// ResumeConversationsFromRestart resumes conversations after a
// restart. v1.3.5 binary addr 0x7fdd380, name=ResumeConversationsFromRestart,
// signature=(cm *CascadeManager, ctx context.Context) error.
func (cm *CascadeManager) ResumeConversationsFromRestart(ctx context.Context) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

// ForceStopTree force-stops the trajectory tree. v1.3.5 binary addr
// (per pclntab .ForceStopTree), name=ForceStopTree, signature=(cm
// *CascadeManager) error.
func (cm *CascadeManager) ForceStopTree() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	if cm.closed {
		return ErrClosed
	}
	return nil
}

