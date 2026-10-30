package lifecycle

import (
	"strings"
	"testing"
)

// TestClearApprovalsBySubagent_OnePending covers the simplest
// case: a single approval tied to sub-1, stop the subagent, the
// approval must be cleared from the queue.
func TestClearApprovalsBySubagent_OnePending(t *testing.T) {
	s := NewState()
	id, _ := s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "run_command", "")

	cleared := s.ClearApprovalsBySubagent("sub-1")
	if len(cleared) != 1 {
		t.Fatalf("expected 1 cleared, got %d", len(cleared))
	}
	if cleared[0].ID != id {
		t.Errorf("expected cleared approval id %s, got %s", id, cleared[0].ID)
	}
	if got := len(s.PendingApprovals()); got != 0 {
		t.Errorf("expected queue empty after clear, got %d pending", got)
	}
}

// TestClearApprovalsBySubagent_ThreePending covers the case named
// in the gap: a stopped subagent with multiple pending approvals
// must have ALL of them cleared in one call.
func TestClearApprovalsBySubagent_ThreePending(t *testing.T) {
	s := NewState()
	idA, _ := s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "run_command", "")
	idB, _ := s.SubagentAwaitsApproval("sub-1", ApprovalUserInput, "", "which env?")
	// Add a third with the same subagent id but a different kind,
	// to prove the clearing isn't bounded by approval kind.
	idC, _ := s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "edit_file", "")

	cleared := s.ClearApprovalsBySubagent("sub-1")
	if len(cleared) != 3 {
		t.Fatalf("expected 3 cleared, got %d", len(cleared))
	}
	// Returned slice must contain all three ids in queue order.
	gotIDs := []string{cleared[0].ID, cleared[1].ID, cleared[2].ID}
	for _, want := range []string{idA, idB, idC} {
		found := false
		for _, g := range gotIDs {
			if g == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected cleared approvals to include id %s, got %v", want, gotIDs)
		}
	}

	if got := len(s.PendingApprovals()); got != 0 {
		t.Errorf("expected queue empty after 3-clear, got %d pending", got)
	}
}

// TestClearApprovalsBySubagent_NoPending covers the no-op case:
// clearing a subagent with no pending approvals returns nil and
// does not mutate the cascader state (it cannot — there is no
// transition to record).
func TestClearApprovalsBySubagent_NoPending(t *testing.T) {
	s := NewState()
	cleared := s.ClearApprovalsBySubagent("sub-nobody")
	if cleared != nil {
		t.Errorf("expected nil for no-op, got %v", cleared)
	}
	// Cascader stays at CascaderInit.
	if got := s.Current(); got != CascaderInit {
		t.Errorf("expected cascader unchanged, got %s", got)
	}
}

// TestClearApprovalsBySubagent_OtherSubagentUntouched is the
// "other subagents' approvals untouched" invariant named in the
// gap. Clearing sub-A must not touch sub-B's pending approvals.
func TestClearApprovalsBySubagent_OtherSubagentUntouched(t *testing.T) {
	s := NewState()
	idA1, _ := s.SubagentAwaitsApproval("sub-A", ApprovalToolConfirm, "run_command", "")
	idB1, _ := s.SubagentAwaitsApproval("sub-B", ApprovalUserInput, "", "ok?")
	idB2, _ := s.SubagentAwaitsApproval("sub-B", ApprovalToolConfirm, "edit_file", "")

	cleared := s.ClearApprovalsBySubagent("sub-A")
	if len(cleared) != 1 {
		t.Fatalf("expected 1 cleared (sub-A only), got %d", len(cleared))
	}
	if cleared[0].ID != idA1 {
		t.Errorf("expected cleared id %s, got %s", idA1, cleared[0].ID)
	}

	// Queue must still hold sub-B's two approvals, in order.
	remaining := s.PendingApprovals()
	if len(remaining) != 2 {
		t.Fatalf("expected 2 remaining (sub-B), got %d", len(remaining))
	}
	if remaining[0].ID != idB1 || remaining[1].ID != idB2 {
		t.Errorf("expected sub-B approvals untouched in order, got [%s,%s]", remaining[0].ID, remaining[1].ID)
	}

	// Cascader is still CascaderAwaitApproval because sub-B's
	// approvals are still parked.
	if got := s.Current(); got != CascaderAwaitApproval {
		t.Errorf("expected cascader still await_approval (sub-B pending), got %s", got)
	}
}

// TestClearApprovalsBySubagent_EmptyID covers the empty-id
// guardrail: passing "" must be a strict no-op (don't sweep
// everything).
func TestClearApprovalsBySubagent_EmptyID(t *testing.T) {
	s := NewState()
	id1, _ := s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "run_command", "")
	id2, _ := s.SubagentAwaitsApproval("sub-2", ApprovalUserInput, "", "?")

	cleared := s.ClearApprovalsBySubagent("")
	if cleared != nil {
		t.Errorf("expected nil for empty id, got %v", cleared)
	}
	if got := len(s.PendingApprovals()); got != 2 {
		t.Errorf("expected 2 pending untouched, got %d", got)
	}
	_ = id1
	_ = id2
}

// TestClearApprovalsBySubagent_AwaitApprovalResetsToReady
// covers the cascader-state transition: when the queue was JUST
// emptied by this call AND the cascader was parked awaiting the
// cleared approvals, it transitions back to CascaderReady. The
// transition log records the cleanup.
func TestClearApprovalsBySubagent_AwaitApprovalResetsToReady(t *testing.T) {
	s := NewState()
	s.SubagentAwaitsApproval("sub-1", ApprovalToolConfirm, "run_command", "")
	if got := s.Current(); got != CascaderAwaitApproval {
		t.Fatalf("expected cascader await_approval after enqueue, got %s", got)
	}

	s.ClearApprovalsBySubagent("sub-1")
	if got := s.Current(); got != CascaderReady {
		t.Errorf("expected cascader Ready after clear-and-empty, got %s", got)
	}

	// Transition log should mention the cleanup.
	histStrs := make([]string, 0, len(s.History()))
	for _, h := range s.History() {
		histStrs = append(histStrs, h.Note)
	}
	joined := strings.Join(histStrs, "|")
	if !strings.Contains(joined, "ClearApprovalsBySubagent(sub-1)") {
		t.Errorf("expected history note to mention the cleanup, got %s", joined)
	}
}

// TestClearApprovalsBySubagent_KeepsAwaitWhenOtherSubagentPending
// covers the negative transition case: when sub-A is cleared but
// sub-B still has a pending approval, the cascader stays in
// CascaderAwaitApproval. The transition log does NOT receive a
// new entry for this call.
func TestClearApprovalsBySubagent_KeepsAwaitWhenOtherSubagentPending(t *testing.T) {
	s := NewState()
	s.SubagentAwaitsApproval("sub-A", ApprovalToolConfirm, "run_command", "")
	s.SubagentAwaitsApproval("sub-B", ApprovalToolConfirm, "edit_file", "")

	histBefore := len(s.History())

	s.ClearApprovalsBySubagent("sub-A")
	if got := s.Current(); got != CascaderAwaitApproval {
		t.Errorf("expected cascader STILL await_approval (sub-B pending), got %s", got)
	}
	// The cascader didn't transition; the history should not have
	// grown because the call only mutated the queue.
	if got := len(s.History()); got != histBefore {
		t.Errorf("expected history length unchanged when cascader did not transition, got %d (was %d)", got, histBefore)
	}
}
