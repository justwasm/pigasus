package cascade

// clear_approvals.go — cycle feat/subagent-stop-clear-approvals:
// State.ClearApprovalsBySubagent.
//
// AGY 1.3.3 changelog line 52:
//
//	Fixed a stopped subagent staying in the active agents list while it
//	still had an approval pending; stopping it now clears its pending
//	approvals right away.
//
// The two halves of this fix live in separate packages so the seam
// the gap names is exercised directly:
//
//	subagent.Manager.Stop(id)
//	  → fires OnStopFunc(id)
//	    → cascade.State.ClearApprovalsBySubagent(id)
//
// This file owns the cascade half. The shape:
//
//   - State.ClearApprovalsBySubagent(id) removes every entry in
//     `approvals` whose SubagentID matches id. The returned slice
//     is the cleared entries (in queue order) so a caller can
//     log / audit / surface a "cleared N approvals for X" message;
//     nil when nothing matched.
//   - The cascader state is restored to CascaderReady when the
//     queue was just emptied AND the cascader was parked in
//     CascaderAwaitApproval ONLY because of the cleared approvals.
//     We do not transition when other subagents still hold pending
//     approvals — the cascader is correctly parked waiting on the
//     remaining ones.
//   - The transition log records the cleanup so a future /tasks or
//     /agents audit shows what happened.
//
// Concurrency: holds s.mu (the State lock) throughout. The
// subagent package's onStop hook is fired outside any subagent
// lock, so a long-running ClearApprovalsBySubagent cannot block
// Manager.List() / Manager.Add() / Manager.Get() — important for
// the cycle-feat panel live-update path (the panel re-renders on
// the next subscriber tick after the hook returns).

import (
	"fmt"
	"time"
)

// ClearApprovalsBySubagent removes every approval in the queue
// whose SubagentID matches id. It returns the cleared entries in
// queue order (oldest first). The slice is empty when id is "" or
// no approvals matched.
//
// The cleared slice is a defensive copy: callers may mutate or
// retain the result without affecting State. PendingApproval is a
// value type, so a slice header copy is sufficient.
//
// Cascader transition: when the queue had at least one matching
// approval AND that approval was the last one keeping the
// cascader parked (i.e. the queue is now empty AND
// s.cascader == CascaderAwaitApproval), the state transitions to
// CascaderReady. The transition is appended to s.history so a
// later audit / log rendering surface knows what happened.
//
// Returning nil and leaving the cascader untouched when id is ""
// is deliberate: a blank subagent id is treated as "not me" to
// avoid accidentally sweeping every approval when a buggy caller
// forgets to pass an id.
func (s *State) ClearApprovalsBySubagent(id string) []PendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		return nil
	}
	if len(s.approvals) == 0 {
		return nil
	}

	cleared := make([]PendingApproval, 0)
	remaining := make([]PendingApproval, 0, len(s.approvals))
	for _, p := range s.approvals {
		if p.SubagentID == id {
			cleared = append(cleared, p)
			continue
		}
		remaining = append(remaining, p)
	}
	if len(cleared) == 0 {
		return nil
	}

	s.approvals = remaining

	// Reset the cascader only when the queue was emptied solely by
	// this call. The 1.3.5 decompiled `commitSubagentStates` at
	// 0x8aa0b20 surfaces an "unanswered" failure when the cascader
	// is still awaiting at the end of a turn; since this is the
	// "user stopped the subagent" path, transitioning back to
	// Ready lets the next turn re-enter CascaderExecute cleanly.
	// (The stop itself is reported to the user by the picker's
	// "stopped" message, NOT by the cascader.)
	if len(s.approvals) == 0 && s.cascader == CascaderAwaitApproval {
		s.cascader = CascaderReady
		s.history = append(s.history, Transition{
			From: CascaderAwaitApproval,
			To:   CascaderReady,
			At:   time.Now(),
			Note: fmt.Sprintf("ClearApprovalsBySubagent(%s): %d cleared", id, len(cleared)),
		})
	}

	// Return the slice in queue order (already preserved — the
	// loop above appends in iteration order).
	return cleared
}
