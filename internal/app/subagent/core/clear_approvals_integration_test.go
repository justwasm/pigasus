package core

// clear_approvals_integration_test.go — cycle
// feat/subagent-stop-clear-approvals integration seam.
//
// Wires a subagent.Manager + cascade.State through the OnStop
// hook documented in the gap:
//
//	m.subagentManager.OnStop(id) → clearApprovalsBySubagent(id)
//
// The test lives in the subagent package because the seam is
// expressed in the manager's terms: the manager owns the tracked
// subagents and fires the hook, and the cascade owns the queue the
// hook clears. The manager's package depends on the cascade's, so the
// test follows the dependency — it used to live in the cascade's
// package, where the dependency pointed the other way and the two
// formed a test-only import cycle.

import (
	"testing"

	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// TestStopSubagent_ClearsApprovals_Single is the integration
// shape of the AGY 1.3.3 line-52 gap: a subagent has exactly one
// pending approval and is then stopped. After Stop, both halves
// reflect the cleared state: the subagent is gone from the
// active-agents list AND the pending approval is gone from the
// queue.
func TestStopSubagent_ClearsApprovals_Single(t *testing.T) {
	mgr := NewManager()
	st := lifecycle.NewState()

	mgr.SetOnStop(func(id string) {
		st.ClearApprovalsBySubagent(id)
	})

	// Register a subagent, give it a pending approval.
	sa := NewSubagent("echo", "hi")
	mgr.Add(sa)
	id, _ := st.SubagentAwaitsApproval(sa.ID, lifecycle.ApprovalToolConfirm, "run_command", "")

	// Sanity: both halves reflect the pre-stop state.
	if got := len(mgr.List()); got != 1 {
		t.Fatalf("expected 1 active agent, got %d", got)
	}
	if got := len(st.PendingApprovals()); got != 1 {
		t.Fatalf("expected 1 pending, got %d", got)
	}

	// Stop the subagent. The hook fires; the cascade queue is
	// cleared; the active-agents list is purged.
	killed, removed := mgr.Stop(sa.ID)
	if !killed || !removed {
		t.Errorf("expected (true,true) from Stop, got (%v,%v)", killed, removed)
	}

	if got := len(mgr.List()); got != 0 {
		t.Errorf("expected 0 active agents after stop, got %d (AGY 1.3.3 stop-list bug)", got)
	}
	if got := len(st.PendingApprovals()); got != 0 {
		t.Errorf("expected 0 pending after stop, got %d (AGY 1.3.3 stop-approvals bug)", got)
	}
	_ = id // unused; only id-style assertion is the count
}

// TestStopSubagent_ClearsApprovals_Three covers the multi-
// approval case: one subagent has three pending approvals,
// stopping it clears all three.
func TestStopSubagent_ClearsApprovals_Three(t *testing.T) {
	mgr := NewManager()
	st := lifecycle.NewState()
	mgr.SetOnStop(func(id string) {
		st.ClearApprovalsBySubagent(id)
	})

	sa := NewSubagent("echo", "hi")
	mgr.Add(sa)
	st.SubagentAwaitsApproval(sa.ID, lifecycle.ApprovalToolConfirm, "run_command", "")
	st.SubagentAwaitsApproval(sa.ID, lifecycle.ApprovalUserInput, "", "ok?")
	st.SubagentAwaitsApproval(sa.ID, lifecycle.ApprovalToolConfirm, "edit_file", "")

	if got := len(st.PendingApprovals()); got != 3 {
		t.Fatalf("expected 3 pending pre-stop, got %d", got)
	}

	mgr.Stop(sa.ID)

	if got := len(st.PendingApprovals()); got != 0 {
		t.Errorf("expected 0 pending after stop, got %d (the AGY 1.3.3 bug)", got)
	}
}

// TestStopSubagent_LeavesOthersAlone covers the
// "other subagents' approvals untouched" invariant across the
// full wiring: stopping sub-A clears sub-A's approvals while
// sub-B's approvals (queued by a different subagent) remain.
func TestStopSubagent_LeavesOthersAlone(t *testing.T) {
	mgr := NewManager()
	st := lifecycle.NewState()
	mgr.SetOnStop(func(id string) {
		st.ClearApprovalsBySubagent(id)
	})

	sa := NewSubagent("echo", "a")
	sa.ID = "sub-A"
	sb := NewSubagent("echo", "b")
	sb.ID = "sub-B"
	mgr.Add(sa)
	mgr.Add(sb)
	st.SubagentAwaitsApproval("sub-A", lifecycle.ApprovalToolConfirm, "run_command", "")
	st.SubagentAwaitsApproval("sub-B", lifecycle.ApprovalUserInput, "", "ok?")

	mgr.Stop("sub-A")

	pending := st.PendingApprovals()
	if len(pending) != 1 {
		t.Fatalf("expected 1 remaining (sub-B's), got %d", len(pending))
	}
	if pending[0].SubagentID != "sub-B" {
		t.Errorf("expected sub-B untouched, got subagent=%s", pending[0].SubagentID)
	}
	// sub-A gone from active list, sub-B still present.
	if _, ok := mgr.Get("sub-A"); ok {
		t.Errorf("expected sub-A gone from active list, still present")
	}
	if _, ok := mgr.Get("sub-B"); !ok {
		t.Errorf("expected sub-B still present, missing")
	}
}
