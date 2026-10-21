// Package cortex — approval.go: the recovered 1.3.5 approval flow
// for the cortex side, plus the adapter that keeps the cortex-side
// gate (this package) and the store-side gate (cycle 41's
// internal/cascade/real_lifecycle.go) in sync.
//
// Background. The v1.3.5 binary has TWO approval surfaces:
//
//  1. Store-side: `(*Store).SubagentAwaitsApproval` +
//     `updatePendingApprovalsQueue` +
//     `HandleSubagentUserInput` +
//     `commitSubagentStates` +
//     `RespondToSubagentToolConfirmation`. Implemented in
//     internal/cascade/real_lifecycle.go (cycle 41).
//  2. Cortex-side: `(*CascadeManager).SubagentHub` +
//     `gatedSubagentHub` + `KillAgent` + `MaybeReviveAgent`.
//     Implemented in subagent.go (cycle 42) and exposed via the
//     SubagentAwaitsApprovalGate type.
//
// This file defines the **adapter** that keeps the two in sync —
// the cycle 41 printmode path uses the store-side gate, the cycle
// 42 TUI path uses the cortex-side gate, and the adapter makes
// sure they don't diverge.
//
// It also defines the high-level ApproveSubagent / RejectSubagent
// API that the TUI uses (mirrors the v1.3.5
// (*Store).RespondToSubagentToolConfirmation at 0x8aa3240 +
// (*Store).HandleSubagentUserInput at 0x8aa7da0).
package cortex

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ApprovalDriver drives the approval flow. It is the cortex-side
// counterpart to the cycle 41 cascade.RealLifecycle.ApproveSubagent
// / RejectSubagent API.
type ApprovalDriver struct {
	gate *SubagentAwaitsApprovalGate
	hub  *subagentHub
	cm   *CascadeManager
	mu   sync.Mutex
}

// NewApprovalDriver wires a gate + hub + CascadeManager.
func NewApprovalDriver(cm *CascadeManager, hub *subagentHub, gate *SubagentAwaitsApprovalGate) *ApprovalDriver {
	return &ApprovalDriver{cm: cm, hub: hub, gate: gate}
}

// Approve approves a pending approval. The v1.3.5 binary's
// (*Store).RespondToSubagentToolConfirmation at 0x8aa3240
// deques the approval, marks the subagent as `done`, and resumes
// the cascader. We do the same.
//
// Returns the resolved approval and the SubagentState after the
// approval.
func (d *ApprovalDriver) Approve(id string) (PendingApproval, SubagentState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gate == nil {
		return PendingApproval{}, SubagentState{}, errors.New("cortex: gate is nil")
	}
	appr, err := d.gate.ApproveSubagent(id)
	if err != nil {
		return PendingApproval{}, SubagentState{}, err
	}
	if d.hub == nil {
		return appr, SubagentState{}, nil
	}
	if err := d.hub.markDone(appr.SubagentID); err != nil {
		return appr, SubagentState{}, fmt.Errorf("cortex: mark done: %w", err)
	}
	st, _ := d.hub.Lookup(appr.SubagentID)
	return appr, st, nil
}

// Reject rejects a pending approval. The v1.3.5 binary cancels
// the subagent via commitSubagentStates (failed) and surfaces a
// "user declined" message.
func (d *ApprovalDriver) Reject(id string) (PendingApproval, SubagentState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.gate == nil {
		return PendingApproval{}, SubagentState{}, errors.New("cortex: gate is nil")
	}
	appr, err := d.gate.RejectSubagent(id)
	if err != nil {
		return PendingApproval{}, SubagentState{}, err
	}
	if d.hub == nil {
		return appr, SubagentState{}, nil
	}
	if err := d.hub.markFailed(appr.SubagentID, errors.New("user declined")); err != nil {
		return appr, SubagentState{}, fmt.Errorf("cortex: mark failed: %w", err)
	}
	st, _ := d.hub.Lookup(appr.SubagentID)
	return appr, st, nil
}

// Kill kills a subagent by id. Mirrors v1.3.5
// (*CascadeManager).KillAgent at 0x7feb060.
func (d *ApprovalDriver) Kill(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hub == nil {
		return errors.New("cortex: hub is nil")
	}
	if err := d.hub.markKilled(id); err != nil {
		return err
	}
	if d.cm != nil {
		_ = d.cm.KillAgent(id)
	}
	return nil
}

// Revive revives a subagent by id. Mirrors v1.3.5
// (*CascadeManager).MaybeReviveAgent at 0x7fe2160.
func (d *ApprovalDriver) Revive(id string) (SubagentState, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.hub == nil {
		return SubagentState{}, errors.New("cortex: hub is nil")
	}
	if err := d.hub.revive(id); err != nil {
		return SubagentState{}, err
	}
	if d.cm != nil {
		if err := d.cm.MaybeReviveAgent(id); err != nil {
			return SubagentState{}, err
		}
	}
	st, _ := d.hub.Lookup(id)
	return st, nil
}

// ---- Adapter to cycle 41 cascade.RealLifecycle -----------------------------

// CascadeAdapter keeps the cortex-side gate (this package) and the
// store-side gate (cycle 41 internal/cascade/real_lifecycle.go) in
// sync.
//
// The v1.3.5 binary has both — the cortex-side
// `SubagentAwaitsApprovalGate` is what the TUI watches, and the
// store-side `cascade.PendingApprovals` is what the printmode path
// drives. The adapter makes sure that when a subagent is enqueued
// on one side, it shows up on the other (and vice versa).
//
// The adapter is **best-effort**: it does not guarantee that the
// two gates are in lockstep at all times (they share no lock).
// For test purposes, the adapter is fine; for production, drive
// either cortex or cascade, not both.
type CascadeAdapter struct {
	cm       *CascadeManager
	gate     *SubagentAwaitsApprovalGate
	cascade  CascadeAdapterCascadeAPI
	mu       sync.Mutex
}

// CascadeAdapterCascadeAPI is the slice of the cycle 41
// cascade.RealLifecycle that the adapter needs. We don't import
// the cycle 41 package directly to avoid a circular import (cortex
// is more foundational than cascade); instead we declare the
// interface here and let callers wire it in.
type CascadeAdapterCascadeAPI interface {
	// EnqueueCascadeApproval enqueues an approval on the cascade
	// side (mirrors cascade.State.SubagentAwaitsApproval).
	EnqueueCascadeApproval(id, subagentID, toolName, question string, kind string) (string, error)
	// ResolveCascadeApproval dequeues an approval by id (mirrors
	// cascade.State.ApproveSubagent / RejectSubagent).
	ResolveCascadeApproval(id string) error
}

// NewCascadeAdapter wires the cortex + cascade sides.
func NewCascadeAdapter(cm *CascadeManager, gate *SubagentAwaitsApprovalGate, c CascadeAdapterCascadeAPI) *CascadeAdapter {
	return &CascadeAdapter{cm: cm, gate: gate, cascade: c}
}

// ForwardFromCortex pushes a cortex-side approval to the cascade
// side. Called when a subagent enqueues on the cortex gate.
func (a *CascadeAdapter) ForwardFromCortex(p PendingApproval) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cascade == nil {
		return nil
	}
	_, err := a.cascade.EnqueueCascadeApproval(p.ID, p.SubagentID, p.ToolName, p.Question, string(p.Kind))
	return err
}

// ForwardFromCascade pushes a cascade-side approval to the cortex
// side. Called when a subagent enqueues on the cascade gate.
func (a *CascadeAdapter) ForwardFromCascade(id, subagentID, toolName, question, kind string) (PendingApproval, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	p := PendingApproval{
		ID:         id,
		SubagentID: subagentID,
		ToolName:   toolName,
		Question:   question,
		Kind:       ApprovalKind(kind),
		CreatedAt:  time.Now(),
	}
	if a.gate == nil {
		return p, nil
	}
	a.gate.Enqueue(p)
	return p, nil
}

// ResolveCascade resolves a cascade-side approval by id and
// mirrors it to the cortex side.
func (a *CascadeAdapter) ResolveCascade(id string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.gate == nil {
		return nil
	}
	if _, ok := a.gate.DequeueByID(id); !ok {
		return errors.New("cortex: no pending approval with id " + id)
	}
	if a.cascade == nil {
		return nil
	}
	return a.cascade.ResolveCascadeApproval(id)
}
