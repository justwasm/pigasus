// Package cascade — real_lifecycle.go: the recovered 1.3.5 cascader
// lifecycle (cascader.Run + SubagentAwaitsApproval) reimplemented in
// Go terms.
//
// The cycle 40 recovery of `agent.Cascade` in internal/agent/cascade.go
// was a thin observer — it watched the agent.Loop drive its 6-state
// state machine (init -> read -> plan -> execute -> stream -> save ->
// done) and fired OnTransition callbacks. That was fine for the `--print`
// path, but the recovered 1.3.5 binary has a much richer lifecycle
// driven by `cascader.Run` (in
// google3/third_party/jetski/cortex/cascade_run_state/cascade_run_state)
// plus a parallel subagent approval flow in
// google3/third_party/jetski/cli/store/store
// (TriggerSubagentApprovalFast at 0x8aa6f20,
//  updatePendingApprovalsQueue at 0x8aa3f80,
//  HandleSubagentUserInput at 0x8aa7da0,
//  commitSubagentStates at 0x8aa0b20).
//
// This file reconstructs the cascader side of that lifecycle in Go:
//
//   - RealLifecycle owns the FSM (init -> execute -> stream -> save ->
//     done), the subagent approval gate, and the pending-approvals
//     queue. It is the Go-side counterpart to the decompiled
//     `cascader.Run` loop and the store's subagent helpers.
//   - PendingApprovals is the recovered `updatePendingApprovalsQueue`
//     state (linked list of pending tool-confirmation items keyed by
//     subagent id).
//   - SubagentAwaitsApproval is the gate that pauses the cascader on
//     a subagent that needs user input. The recovered 1.3.5 code
//     routes the request through `TriggerSubagentApprovalFast` and
//     blocks until `RespondToSubagentToolConfirmation` is called.
//
// Why a new package. The cycle 40 `agent.Cascade` is an observer
// pattern owned by agent.Loop; this new `cascade.RealLifecycle` is a
// controller pattern owned by whoever drives the turn (cmd/wagyu
// runPrintMode, or the cycle 42 TUI). Splitting them keeps
// `agent.Loop` transport-agnostic and lets us swap the cascader out
// without touching the loop.
package cascade

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// CascaderState is the recovered cascader state. It mirrors
// `(*CascadeRunState)` in
// google3/third_party/jetski/cortex/cascade_run_state/cascade_run_state
// (the only recovered function in that package is
// `closeSubagentForwarder` at 0x76caa60, which is the cleanup
// transition; the rest of the FSM is reconstructed here from the
// call graph).
type CascaderState string

const (
	// CascaderInit — RealLifecycle just constructed.
	CascaderInit CascaderState = "init"
	// CascaderReady — Run() has been called at least once, no turn in flight.
	CascaderReady CascaderState = "ready"
	// CascaderExecute — Run is in the body of a turn.
	CascaderExecute CascaderState = "execute"
	// CascaderStream — at least one chunk has been emitted.
	CascaderStream CascaderState = "stream"
	// CascaderAwaitApproval — a subagent asked for tool confirmation;
	// the cascader is blocked on the user.
	CascaderAwaitApproval CascaderState = "await_approval"
	// CascaderSave — terminal chunks processed, persisting to store.
	CascaderSave CascaderState = "save"
	// CascaderDone — all work complete (terminal happy path).
	CascaderDone CascaderState = "done"
	// CascaderError — terminal failure path.
	CascaderError CascaderState = "error"
)

// State is the recovered cascading state machine. It is intentionally
// small: CascaderInit / Ready / Execute / Stream / AwaitApproval /
// Save / Done / Error, matching the 1.3.5 `cascader.Run` lifecycle.
type State struct {
	mu        sync.Mutex
	cascader  CascaderState
	history   []Transition
	approvals []PendingApproval
	counter   int64
}

// Transition is one entry in the cascader's transition log. Modeled
// after agent.CascadeTransition but lives in the cascade package to
// avoid a back-dependency from cascade to agent (agent is the loop
// driver; cascade is the cascader / subagent-approval controller).
type Transition struct {
	From CascaderState
	To   CascaderState
	At   time.Time
	Note string
}

// NewState returns a fresh cascader state, rooted at CascaderInit.
func NewState() *State {
	return &State{
		cascader: CascaderInit,
		history: []Transition{{
			From: "",
			To:   CascaderInit,
			At:   time.Now(),
			Note: "cascader created",
		}},
	}
}

// Current returns the active cascader state.
func (s *State) Current() CascaderState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cascader
}

// History returns a copy of the cascader's transition log.
func (s *State) History() []Transition {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Transition, len(s.history))
	copy(out, s.history)
	return out
}

// transition is the internal state-mutator. All public methods
// (CascaderRun, SubagentAwaitsApproval, ApproveSubagent, RejectSubagent)
// funnel through this.
func (s *State) transition(to CascaderState, note string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cascader == to {
		return
	}
	from := s.cascader
	s.cascader = to
	s.history = append(s.history, Transition{
		From: from,
		To:   to,
		At:   time.Now(),
		Note: note,
	})
}

// RealLifecycle is the recovered `cascader.Run` driver. It owns a
// State (the FSM) and drives a backend.Backend through one turn:
//
//   - CascaderRun calls backend.Init + backend.Run (which itself
//     streams chunks through the Sink)
//   - for each emitted chunk the cascader transitions to
//     CascaderStream
//   - on the terminal chunk the cascader transitions to CascaderSave
//     then CascaderDone
//   - if a subagent asks for approval mid-stream, the cascader pauses
//     in CascaderAwaitApproval; the caller is expected to call
//     ApproveSubagent / RejectSubagent to resume
//
// Cycle 45 — RealLifecycle additionally exposes an optional
// event callback (SetEventEmitter) that fires CascadeEvent
// values at every step boundary. This is the surface the
// TUI's TasksPanel subscribes to for live step rendering
// (Issue #7 fix).
type RealLifecycle struct {
	State   *State
	be      backend.Backend
	mu      sync.Mutex
	closed  bool
	onEvent func(CascadeEvent) // optional; cycle 45 step-event sink
}

// NewRealLifecycle wires a backend.Backend to a fresh State.
func NewRealLifecycle(be backend.Backend) *RealLifecycle {
	return &RealLifecycle{
		State: NewState(),
		be:    be,
	}
}

// WithState returns a RealLifecycle that uses the given State. Useful
// when a caller (e.g. a test) wants to share a single State across
// multiple cascaders, or wants to pre-seed approvals.
func WithState(be backend.Backend, st *State) *RealLifecycle {
	return &RealLifecycle{State: st, be: be}
}

// Close tears down the lifecycle. Mirrors the decompiled
// `(*CascadeRunState).closeSubagentForwarder` at 0x76caa60 — any
// pending-approval items in the queue are dropped (the recovered
// binary surfaces them as a fatal error on next Run; we return nil
// for cycle 41 since the queue is best-effort in this recovery).
func (r *RealLifecycle) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil
	}
	r.closed = true
	if r.be != nil {
		_ = r.be.Close()
	}
	if r.State.Current() != CascaderError {
		r.State.transition(CascaderDone, "Close")
	}
	return nil
}

func (r *RealLifecycle) checkCanRun() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("cascade.RealLifecycle: closed")
	}
	if r.be == nil {
		return errors.New("cascade.RealLifecycle: backend is nil")
	}
	return nil
}

func (r *RealLifecycle) runInit(ctx context.Context, model string) error {
	if _, err := r.be.Init(ctx, model); err != nil {
		r.State.transition(CascaderError, fmt.Sprintf("Init failed: %s", err))
		r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "init", Title: stepTitle("init"), Error: err.Error()})
		return fmt.Errorf("cascade.RealLifecycle: Init: %w", err)
	}
	r.State.transition(CascaderReady, "Init completed")
	return nil
}

func (r *RealLifecycle) runExecute(ctx context.Context, prompt string) error {
	r.State.transition(CascaderExecute, "Run started")
	r.emitEvent(CascadeEvent{Type: EventStepStart, StepID: "execute", Title: stepTitle("execute")})

	runErr := r.be.Run(ctx, prompt)
	if runErr != nil {
		r.State.transition(CascaderError, fmt.Sprintf("Run failed: %s", runErr))
		r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "execute", Title: stepTitle("execute"), Error: runErr.Error()})
		return fmt.Errorf("cascade.RealLifecycle: Run: %w", runErr)
	}

	r.State.transition(CascaderStream, "Backend.Run returned; streaming")
	r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "execute", Title: stepTitle("execute")})
	r.emitEvent(CascadeEvent{Type: EventStepStart, StepID: "stream", Title: stepTitle("stream")})
	return nil
}

func (r *RealLifecycle) runSave() error {
	r.State.transition(CascaderSave, "Backend.Run returned; persisting")
	r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "stream", Title: stepTitle("stream")})
	r.emitEvent(CascadeEvent{Type: EventStepStart, StepID: "save", Title: stepTitle("save")})
	if err := r.be.Save(); err != nil {
		r.State.transition(CascaderError, fmt.Sprintf("Save failed: %s", err))
		r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "save", Title: stepTitle("save"), Error: err.Error()})
		return fmt.Errorf("cascade.RealLifecycle: Save: %w", err)
	}
	r.State.transition(CascaderDone, "turn complete")
	r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "save", Title: stepTitle("save")})
	return nil
}

// CascaderRun drives one turn. It mirrors `cascader.Run` in
// google3/third_party/jetski/cortex/cascade_run_state/cascade_run_state:
// open the conversation, run the body, await the terminal frame,
// persist. Errors at any point funnel through to CascaderError.
//
// The Run() call is synchronous: it returns once the backend has
// finished the turn (or once the cascader hits an unrecoverable
// error). For subagent-approval flows the backend itself would have
// called SubagentAwaitsApproval on this State (see the `WithState`
// helper below) before Run() returns.
func (r *RealLifecycle) CascaderRun(ctx context.Context, model, prompt string) error {
	if err := r.checkCanRun(); err != nil {
		return err
	}
	if err := r.runInit(ctx, model); err != nil {
		return err
	}
	if err := r.runExecute(ctx, prompt); err != nil {
		return err
	}

	// If a subagent blocked us mid-stream, the State is in
	// CascaderAwaitApproval; we don't proceed to Save until the user
	// resolves it. (The cycle 41 caller is responsible for calling
	// ApproveSubagent or RejectSubagent before invoking Save
	// again.)
	if r.State.Current() == CascaderAwaitApproval {
		// Stay in await_approval; caller will resume. The
		// "stream" step is left open — the resume path
		// re-emits its end below.
		return nil
	}

	return r.runSave()
}
