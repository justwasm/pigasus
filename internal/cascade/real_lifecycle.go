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

// ApprovalKind is the recovered `(*Store).updatePendingApprovalsQueue`
// payload type. The 1.3.5 binary only ever enqueues two kinds:
// tool-confirmation and side-question; we model both so the cascader
// can route them to the right handler.
type ApprovalKind string

const (
	// ApprovalToolConfirm — subagent wants to run a tool that needs
	// user permission (recovered from `RespondToSubagentToolConfirmation`
	// at 0x8aa3240 in cli/store/store).
	ApprovalToolConfirm ApprovalKind = "tool_confirm"
	// ApprovalUserInput — subagent is asking the user a question
	// (recovered from `HandleSubagentUserInput` at 0x8aa7da0).
	ApprovalUserInput ApprovalKind = "user_input"
)

// PendingApproval is one entry in the pending-approvals queue. The
// recovered 1.3.5 code models it as a linked-list node
// (next-pointer + payload); we use a slice for the same shape since
// the typical queue depth is 1..3.
type PendingApproval struct {
	ID         string       // unique id (uuid-ish; we use timestamp+counter)
	SubagentID string       // id of the subagent that requested approval
	Kind       ApprovalKind // tool_confirm or user_input
	ToolName   string       // populated for ApprovalToolConfirm
	Question   string       // populated for ApprovalUserInput
	CreatedAt  time.Time
}

// String makes PendingApproval printable. Used by the `--print` path
// when it surfaces a blocked cascader to the user.
func (p PendingApproval) String() string {
	switch p.Kind {
	case ApprovalToolConfirm:
		return fmt.Sprintf("[%s] subagent=%s tool=%s (created %s)",
			p.ID, p.SubagentID, p.ToolName, p.CreatedAt.Format(time.RFC3339))
	case ApprovalUserInput:
		return fmt.Sprintf("[%s] subagent=%s question=%q (created %s)",
			p.ID, p.SubagentID, p.Question, p.CreatedAt.Format(time.RFC3339))
	default:
		return fmt.Sprintf("[%s] subagent=%s (created %s)",
			p.ID, p.SubagentID, p.CreatedAt.Format(time.RFC3339))
	}
}

// State is the recovered cascading state machine. It is intentionally
// small: CascaderInit / Ready / Execute / Stream / AwaitApproval /
// Save / Done / Error, matching the 1.3.5 `cascader.Run` lifecycle.
type State struct {
	mu         sync.Mutex
	cascader   CascaderState
	history    []Transition
	approvals  []PendingApproval
	counter    int64
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

// PendingApprovals returns a copy of the queue.
func (s *State) PendingApprovals() []PendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingApproval, len(s.approvals))
	copy(out, s.approvals)
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

// enqueueApproval is the internal queue mutator. Caller holds s.mu.
func (s *State) enqueueApprovalLocked(p PendingApproval) {
	s.approvals = append(s.approvals, p)
}

// dequeueApprovalByID returns the approval with id `id` and removes it
// from the queue. Returns false if no such approval.
func (s *State) dequeueApprovalByID(id string) (PendingApproval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, p := range s.approvals {
		if p.ID == id {
			// Remove at i (preserves order for the rest).
			s.approvals = append(s.approvals[:i], s.approvals[i+1:]...)
			return p, true
		}
	}
	return PendingApproval{}, false
}

// nextID generates a unique approval id.
func (s *State) nextID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.counter++
	return fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), s.counter)
}

// ---- SubagentAwaitsApproval (real lifecycle gate) ----------------------

// SubagentAwaitsApproval is the recovered `TriggerSubagentApprovalFast`
// entry point. The 1.3.5 binary calls it whenever a subagent needs
// user permission (tool confirmation) or has a question. The cascader
// is blocked until the user calls `ApproveSubagent` or
// `RejectSubagent` with the returned approval id.
//
// Behavior mirrors the decompiled call shape (0x8aa6f20 +
// 0x8aa3f80 + 0x8aa7da0):
//
//   - enqueue the approval
//   - transition the cascader to CascaderAwaitApproval
//   - return the approval id so the caller can later match the
//     response (mirrors how the store tracks approval state by id)
//
// Returns the new approval's id and the pending-approvals entry. The
// caller (cmd/wagyu, the TUI, …) is responsible for surfacing
// the entry to the user and then calling ApproveSubagent /
// RejectSubagent.
func (s *State) SubagentAwaitsApproval(subagentID string, kind ApprovalKind, toolName, question string) (string, PendingApproval) {
	s.mu.Lock()
	defer s.mu.Unlock()
	appr := PendingApproval{
		ID:         s.nextIDLocked(),
		SubagentID: subagentID,
		Kind:       kind,
		ToolName:   toolName,
		Question:   question,
		CreatedAt:  time.Now(),
	}
	s.enqueueApprovalLocked(appr)
	s.cascader = CascaderAwaitApproval
	s.history = append(s.history, Transition{
		From: s.cascader,
		To:   CascaderAwaitApproval,
		At:   time.Now(),
		Note: fmt.Sprintf("SubagentAwaitsApproval: %s", appr.String()),
	})
	return appr.ID, appr
}

// nextIDLocked is the lockless variant; caller holds s.mu.
func (s *State) nextIDLocked() string {
	s.counter++
	return fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), s.counter)
}

// ApproveSubagent marks the given approval as approved and resumes
// the cascader. Mirrors `(*Store).RespondToSubagentToolConfirmation`
// at 0x8aa3240 (for tool_confirm) and `HandleSubagentUserInput` at
// 0x8aa7da0 (for user_input).
//
// Returns the resolved approval, or an error if no such approval is
// in the queue.
func (s *State) ApproveSubagent(id string) (PendingApproval, error) {
	appr, ok := s.dequeueApprovalByID(id)
	if !ok {
		return PendingApproval{}, fmt.Errorf("cascade: no pending approval with id %q", id)
	}
	s.transition(CascaderStream, fmt.Sprintf("ApproveSubagent: %s", appr.String()))
	return appr, nil
}

// RejectSubagent is the dual of ApproveSubagent. The recovered 1.3.5
// code doesn't have an explicit "reject" — it cancels the subagent
// (commitSubagentStates sets it to failed) and surfaces a "user
// declined" message. We model it as a separate method so callers can
// distinguish yes / no in the audit log.
func (s *State) RejectSubagent(id string) (PendingApproval, error) {
	appr, ok := s.dequeueApprovalByID(id)
	if !ok {
		return PendingApproval{}, fmt.Errorf("cascade: no pending approval with id %q", id)
	}
	s.transition(CascaderError, fmt.Sprintf("RejectSubagent: %s", appr.String()))
	return appr, nil
}

// ---- RealLifecycle (cascader.Run) --------------------------------------

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
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return errors.New("cascade.RealLifecycle: closed")
	}
	r.mu.Unlock()

	if r.be == nil {
		return errors.New("cascade.RealLifecycle: backend is nil")
	}

	// Init -> Ready
	if _, err := r.be.Init(ctx, model); err != nil {
		r.State.transition(CascaderError, fmt.Sprintf("Init failed: %s", err))
		r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "init", Title: stepTitle("init"), Error: err.Error()})
		return fmt.Errorf("cascade.RealLifecycle: Init: %w", err)
	}
	r.State.transition(CascaderReady, "Init completed")

	// Ready -> Execute (cycle 45 emit).
	r.State.transition(CascaderExecute, "Run started")
	r.emitEvent(CascadeEvent{Type: EventStepStart, StepID: "execute", Title: stepTitle("execute")})

	// Run the turn body. The backend wraps the stream and pushes
	// chunks through the Sink; we just observe the FSM.
	runErr := r.be.Run(ctx, prompt)
	if runErr != nil {
		r.State.transition(CascaderError, fmt.Sprintf("Run failed: %s", runErr))
		r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "execute", Title: stepTitle("execute"), Error: runErr.Error()})
		return fmt.Errorf("cascade.RealLifecycle: Run: %w", runErr)
	}
	// Execute -> Stream (cycle 45 emit). The "stream" step starts
	// the moment the backend's body returns (chunks are now
	// committed); the step ends when Save begins.
	r.State.transition(CascaderStream, "Backend.Run returned; streaming")
	r.emitEvent(CascadeEvent{Type: EventStepEnd, StepID: "execute", Title: stepTitle("execute")})
	r.emitEvent(CascadeEvent{Type: EventStepStart, StepID: "stream", Title: stepTitle("stream")})

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

	// Execute -> Save -> Done
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

// WithState returns a RealLifecycle that uses the given State. Useful
// when a caller (e.g. a test) wants to share a single State across
// multiple cascaders, or wants to pre-seed approvals.
func WithState(be backend.Backend, st *State) *RealLifecycle {
	return &RealLifecycle{State: st, be: be}
}

// ---- CascadeEvent (cycle 45 Issue #7 integration) -----------------------
//
// The 1.3.5 binary's decompiled `cascader.Run` loop emits a
// sequence of step events that the recovered TUI subscribes to
// in order to render the live task list (cli_printmode.tui.tasksPanel).
// Cycle 45 wires the recovered `cascade.RealLifecycle` to that same
// event vocabulary: a real emit surface that any consumer
// (notably internal/tui.TasksPanel.OnCascadeEvent) can hook into.

// CascadeEventType is the event discriminator emitted by
// RealLifecycle to consumers (TasksPanel, etc).
type CascadeEventType string

const (
	// EventStepStart — a logical step just began. RealLifecycle
	// emits this when the cascader transitions from one of
	// {CascaderInit, CascaderReady, CascaderExecute, CascaderStream}
	// into CascaderExecute / CascaderStream / CascaderSave.
	EventStepStart CascadeEventType = "start"
	// EventStepEnd — a logical step just completed. RealLifecycle
	// emits this when the cascader leaves one of
	// {CascaderExecute, CascaderStream, CascaderSave, CascaderDone}
	// for CascaderSave / CascaderDone / CascaderError.
	EventStepEnd CascadeEventType = "end"
)

// CascadeEvent is the recovered 1.3.5 step event. It carries
// just enough information for a consumer (e.g. the TUI's
// TasksPanel) to render the live step list without depending
// on the cascade package's internal State.
//
// StepID is a stable identifier for the step (currently
// one of "init", "execute", "stream", "save"); consumers use
// it to match end events to start events. Title is the
// human-readable label. Error is non-empty only for end
// events when the step failed.
type CascadeEvent struct {
	Type   CascadeEventType
	StepID string
	Title  string
	Error  string
}

// SetEventEmitter wires an optional event callback. The
// callback is invoked synchronously from CascaderRun at every
// step boundary; pass nil to silence. The cycle 45
// integration test (internal/tui/tasks_integration_test.go)
// uses this hook to drive a TasksPanel without a full
// backend.Backend round-trip.
func (r *RealLifecycle) SetEventEmitter(fn func(CascadeEvent)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onEvent = fn
}

// emitEvent fires the optional event callback. Safe to call
// when no callback is wired.
func (r *RealLifecycle) emitEvent(ev CascadeEvent) {
	r.mu.Lock()
	fn := r.onEvent
	r.mu.Unlock()
	if fn != nil {
		fn(ev)
	}
}

// stepTitle is the human-readable label for a known step.
// Keeps the event-emission code (CascaderRun) free of magic
// strings.
func stepTitle(stepID string) string {
	switch stepID {
	case "init":
		return "Read user request"
	case "execute":
		return "Execute tools"
	case "stream":
		return "Stream response"
	case "save":
		return "Save session"
	default:
		return stepID
	}
}

// stepIDForState maps a CascaderState transition to the step
// it represents. Returns "" if the transition is not a step
// boundary (e.g. init -> ready, or save -> done).
func stepIDForState(to CascaderState) string {
	switch to {
	case CascaderExecute:
		return "execute"
	case CascaderStream:
		return "stream"
	case CascaderSave:
		return "save"
	case CascaderDone:
		return "save"
	default:
		return ""
	}
}
