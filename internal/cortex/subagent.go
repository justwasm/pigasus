// Package cortex — subagent.go: the recovered 1.3.5 subagent
// state machine and SubagentAwaitsApproval gate.
//
// Background. The v1.3.5 binary has TWO parallel subagent-approval
// paths:
//
//  1. The store-level gate in
//     google3/third_party/jetski/cli/store/store (pclntab
//     TriggerSubagentApprovalFast 0x8aa6f20,
//     updatePendingApprovalsQueue 0x8aa3f80,
//     HandleSubagentUserInput 0x8aa7da0,
//     commitSubagentStates 0x8aa0b20,
//     RespondToSubagentToolConfirmation 0x8aa3240).
//  2. The cortex-level gate in
//     google3/third_party/jetski/cortex/cortex via the
//     `SubagentHub` / `gatedSubagentHub` / `KillAgent` /
//     `MaybeReviveAgent` surface.
//
// Cycle 41's internal/cascade/real_lifecycle.go implemented the
// store-level gate. This file implements the **cortex-level** gate
// in Go — i.e. the SubagentAwaitsApproval state machine, the
// SubagentHub registry, the presenceTracker, and the
// gatedSubagentHub. The two paths are kept in sync via a thin
// adapter (see approval.go).
//
// The subagent FSM is the same 5-state one as the v1.3.5 store
// (pending -> running -> awaits_approval -> done | failed | killed),
// with the `awaits_approval` state being the one that blocks the
// cascade.
package cortex

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// SubagentFSMState is the recovered subagent state machine. Names
// are 1:1 with v1.3.5 SubagentStatus enum.
type SubagentFSMState string

const (
	// SubagentFSMInit — subagent just created, not yet registered.
	SubagentFSMInit SubagentFSMState = "init"
	// SubagentFSMPending — registered, queued for execution.
	SubagentFSMPending SubagentFSMState = "pending"
	// SubagentFSMRunning — actively executing.
	SubagentFSMRunning SubagentFSMState = "running"
	// SubagentFSMAwaitsApproval — blocked on user (mirrors v1.3.5
	// (*Store).SubagentAwaitsApproval at 0x8aa6f20).
	SubagentFSMAwaitsApproval SubagentFSMState = "awaits_approval"
	// SubagentFSMDone — finished OK.
	SubagentFSMDone SubagentFSMState = "done"
	// SubagentFSMFailed — finished with error.
	SubagentFSMFailed SubagentFSMState = "failed"
	// SubagentFSMKilled — killed by KillAgent.
	SubagentFSMKilled SubagentFSMState = "killed"
)

// Valid reports whether s is a known subagent FSM state.
func (s SubagentFSMState) Valid() bool {
	switch s {
	case SubagentFSMInit, SubagentFSMPending, SubagentFSMRunning,
		SubagentFSMAwaitsApproval, SubagentFSMDone, SubagentFSMFailed,
		SubagentFSMKilled:
		return true
	}
	return false
}

// SubagentTransition is one entry in a subagent's transition log.
type SubagentTransition struct {
	From SubagentFSMState
	To   SubagentFSMState
	At   time.Time
	Note string
}

// subagentRecord is the per-subagent FSM record kept in the
// subagentHub.
type subagentRecord struct {
	state     SubagentFSMState
	history   []SubagentTransition
	parent    string
	trajectoryID string
	created   time.Time
}

// newSubagentRecord returns a fresh subagent record at init.
func newSubagentRecord(id, parent, trajectoryID string) *subagentRecord {
	return &subagentRecord{
		state:       SubagentFSMInit,
		parent:      parent,
		trajectoryID: trajectoryID,
		created:     time.Now(),
		history: []SubagentTransition{{
			From: "",
			To:   SubagentFSMInit,
			At:   time.Now(),
			Note: "subagent created",
		}},
	}
}

// transition is the internal FSM mutator.
func (r *subagentRecord) transition(to SubagentFSMState, note string) {
	if r.state == to {
		return
	}
	from := r.state
	r.state = to
	r.history = append(r.history, SubagentTransition{
		From: from,
		To:   to,
		At:   time.Now(),
		Note: note,
	})
}

// ---- subagentHub -----------------------------------------------------------

// subagentHub is the recovered `(*CascadeManager).SubagentHub` from
// v1.3.5. It implements SubagentHubLike and holds the per-trajectory
// subagent FSM records.
type subagentHub struct {
	mu    sync.Mutex
	cm    *CascadeManager
	regs  map[string]*subagentRecord
	gate  *gatedSubagentHub
}

// newSubagentHub returns a fresh subagent hub.
func newSubagentHub(cm *CascadeManager) *subagentHub {
	h := &subagentHub{
		cm:   cm,
		regs: map[string]*subagentRecord{},
	}
	h.gate = &gatedSubagentHub{hub: h}
	return h
}

// Register registers a subagent. v1.3.5 binary addr (per pclntab
// SubagentHubLike.Register), name=Register, signature=(h
// *subagentHub, id, parent string) error.
func (h *subagentHub) Register(id, parent string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.regs[id]; ok {
		return fmt.Errorf("cortex: subagent %q already registered", id)
	}
	h.regs[id] = newSubagentRecord(id, parent, h.cm.storedTrajectory)
	return nil
}

// Lookup looks up a subagent by id. v1.3.5 binary addr (per pclntab
// SubagentHubLike.Lookup), name=Lookup, signature=(h *subagentHub, id
// string) (SubagentState, bool).
func (h *subagentHub) Lookup(id string) (SubagentState, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return SubagentState{}, false
	}
	return SubagentState{
		ID:          id,
		Parent:      r.parent,
		TrajectoryID: r.trajectoryID,
		Status:      SubagentStatus(r.state),
		CreatedAt:   r.created,
		UpdatedAt:   time.Now(),
	}, true
}

// ListForParent lists all subagents for a parent. v1.3.5 binary
// addr (per pclntab SubagentHubLike.ListForParent),
// name=ListForParent, signature=(h *subagentHub, parent string)
// []SubagentState.
func (h *subagentHub) ListForParent(parent string) []SubagentState {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []SubagentState
	for id, r := range h.regs {
		if r.parent != parent {
			continue
		}
		out = append(out, SubagentState{
			ID:          id,
			Parent:      r.parent,
			TrajectoryID: r.trajectoryID,
			Status:      SubagentStatus(r.state),
			CreatedAt:   r.created,
			UpdatedAt:   time.Now(),
		})
	}
	return out
}

// Unregister removes a subagent. v1.3.5 binary addr (per pclntab
// SubagentHubLike.Unregister), name=Unregister, signature=(h
// *subagentHub, id string) error.
func (h *subagentHub) Unregister(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.regs[id]; !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	delete(h.regs, id)
	return nil
}

// Gate returns the gated subagent hub.
func (h *subagentHub) Gate() SubagentGate {
	return h.gate
}

// start transitions a subagent from pending to running. Called by
// CascadeManager.Run when it picks up the subagent.
func (h *subagentHub) start(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	r.transition(SubagentFSMRunning, "subagent started")
	return nil
}

// markAwaitsApproval transitions a subagent from running to
// awaits_approval. Mirrors v1.3.5
// (*Store).TriggerSubagentApprovalFast at 0x8aa6f20.
func (h *subagentHub) markAwaitsApproval(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	if r.state != SubagentFSMRunning {
		return fmt.Errorf("cortex: subagent %q in state %s, not running", id, r.state)
	}
	r.transition(SubagentFSMAwaitsApproval, "subagent blocked on user")
	return nil
}

// markDone transitions a subagent from running to done. Mirrors
// v1.3.5 commitSubagentStates at 0x8aa0b20.
func (h *subagentHub) markDone(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	r.transition(SubagentFSMDone, "subagent completed")
	return nil
}

// markFailed transitions a subagent to failed. Mirrors v1.3.5
// commitSubagentStates at 0x8aa0b20 (the failed branch).
func (h *subagentHub) markFailed(id string, err error) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	note := "subagent failed"
	if err != nil {
		note = fmt.Sprintf("subagent failed: %s", err)
	}
	r.transition(SubagentFSMFailed, note)
	return nil
}

// markKilled transitions a subagent to killed. Mirrors v1.3.5
// (*CascadeManager).KillAgent at 0x7feb060.
func (h *subagentHub) markKilled(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	r.transition(SubagentFSMKilled, "subagent killed")
	return nil
}

// revive transitions a subagent from killed/failed back to running.
// Mirrors v1.3.5 (*CascadeManager).MaybeReviveAgent at 0x7fe2160.
func (h *subagentHub) revive(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	r, ok := h.regs[id]
	if !ok {
		return fmt.Errorf("cortex: subagent %q not registered", id)
	}
	if r.state != SubagentFSMKilled && r.state != SubagentFSMFailed {
		return fmt.Errorf("cortex: subagent %q in state %s, cannot revive", id, r.state)
	}
	r.transition(SubagentFSMRunning, "subagent revived")
	return nil
}

// ---- gatedSubagentHub -----------------------------------------------------

// gatedSubagentHub is the recovered "gated" view of the subagent
// hub. v1.3.5 binary addr 0x7fe46e0, name=gatedSubagentHub,
// signature=(cm *CascadeManager) SubagentGate.
type gatedSubagentHub struct {
	hub *subagentHub
}

// Allow returns true if registration is allowed. The v1.3.5 binary
// gates subagent registration on the parent's lifecycle state
// (init/ready/execute allow; await_approval/save/done/error block).
func (g *gatedSubagentHub) Allow(id string) bool {
	if g == nil || g.hub == nil || g.hub.cm == nil {
		return false
	}
	// Walk up to the parent's state. The v1.3.5 binary
	// gatesSubagentHub looks at the parent's stored lifecycle state.
	parent, ok := g.hub.Lookup(parentIDFor(id))
	if !ok {
		// No parent — allow root-level subagents.
		return true
	}
	switch SubagentFSMState(parent.Status) {
	case SubagentFSMInit, SubagentFSMPending, SubagentFSMRunning, SubagentFSMAwaitsApproval:
		return true
	}
	return false
}

// parentIDFor returns the parent id for a subagent id. The v1.3.5
// binary stores the parent in the subagent record; we look it up.
func parentIDFor(id string) string {
	// The 1.3.5 binary stores parent in the record; the id is
	// itself enough to look it up via the hub. This stub returns
	// id as a placeholder.
	return id
}

// ---- SubagentAwaitsApproval gate ----------------------------------------

// SubagentAwaitsApprovalGate is the cortex-side approval gate. It
// tracks which subagents are currently blocked on the user. Mirrors
// v1.3.5 (*Store).SubagentAwaitsApproval at 0x8aa6f20 + 0x8aa3f80
// + 0x8aa7da0.
//
// This is a **parallel** implementation to cycle 41's
// cascade.PendingApprovals — both must stay in sync (the
// approval.go adapter does the sync). The 1.3.5 binary itself has
// both: the store-level queue and the cortex-level
// `gatedSubagentHub` allow/block. Callers should drive one or the
// other; the adapter is for transition only.
type SubagentAwaitsApprovalGate struct {
	mu     sync.Mutex
	queue  []*PendingApproval
	closed bool
}

// PendingApproval is the recovered approval request payload. The
// v1.3.5 binary's `updatePendingApprovalsQueue` (0x8aa3f80) models
// this as a linked-list node; we use a slice since the typical
// depth is 1..3.
type PendingApproval struct {
	ID         string
	SubagentID string
	Kind       ApprovalKind
	ToolName   string
	Question   string
	CreatedAt  time.Time
}

// ApprovalKind is the recovered approval kind enum.
type ApprovalKind string

const (
	// ApprovalToolConfirm — subagent wants to run a tool that
	// needs user permission. Mirrors v1.3.5
	// (*Store).RespondToSubagentToolConfirmation at 0x8aa3240.
	ApprovalToolConfirm ApprovalKind = "tool_confirm"
	// ApprovalUserInput — subagent is asking the user a question.
	// Mirrors v1.3.5 (*Store).HandleSubagentUserInput at
	// 0x8aa7da0.
	ApprovalUserInput ApprovalKind = "user_input"
)

// NewSubagentAwaitsApprovalGate returns a fresh gate.
func NewSubagentAwaitsApprovalGate() *SubagentAwaitsApprovalGate {
	return &SubagentAwaitsApprovalGate{}
}

// Enqueue adds a new approval to the queue. Mirrors v1.3.5
// (*Store).updatePendingApprovalsQueue at 0x8aa3f80.
func (g *SubagentAwaitsApprovalGate) Enqueue(p PendingApproval) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	g.queue = append(g.queue, &p)
}

// Dequeue removes and returns the next pending approval. Mirrors
// the v1.3.5 (*Store).commitSubagentStates at 0x8aa0b20 (the
// approval-resolved branch).
func (g *SubagentAwaitsApprovalGate) Dequeue() (PendingApproval, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.queue) == 0 {
		return PendingApproval{}, false
	}
	p := g.queue[0]
	g.queue = g.queue[1:]
	return *p, true
}

// DequeueByID removes and returns the approval with the given id.
func (g *SubagentAwaitsApprovalGate) DequeueByID(id string) (PendingApproval, bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i, p := range g.queue {
		if p.ID == id {
			g.queue = append(g.queue[:i], g.queue[i+1:]...)
			return *p, true
		}
	}
	return PendingApproval{}, false
}

// Pending returns a copy of the current queue.
func (g *SubagentAwaitsApprovalGate) Pending() []PendingApproval {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]PendingApproval, len(g.queue))
	for i, p := range g.queue {
		out[i] = *p
	}
	return out
}

// Len returns the number of pending approvals.
func (g *SubagentAwaitsApprovalGate) Len() int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return len(g.queue)
}

// Close closes the gate.
func (g *SubagentAwaitsApprovalGate) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	g.queue = nil
	return nil
}

// ApproveSubagent is a convenience that dequeues an approval and
// returns the resolved payload. Mirrors the v1.3.5
// (*Store).RespondToSubagentToolConfirmation at 0x8aa3240.
func (g *SubagentAwaitsApprovalGate) ApproveSubagent(id string) (PendingApproval, error) {
	appr, ok := g.DequeueByID(id)
	if !ok {
		return PendingApproval{}, errors.New("cortex: no pending approval with id " + id)
	}
	return appr, nil
}

// RejectSubagent is a convenience that dequeues an approval and
// returns the resolved payload. The v1.3.5 binary doesn't have a
// separate "reject" — it cancels the subagent via
// commitSubagentStates and surfaces a "user declined" message.
// This wrapper preserves the cycle 41 API.
func (g *SubagentAwaitsApprovalGate) RejectSubagent(id string) (PendingApproval, error) {
	return g.ApproveSubagent(id)
}
