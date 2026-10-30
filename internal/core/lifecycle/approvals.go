package lifecycle

import (
	"fmt"
	"time"
)

// PendingApprovals returns a copy of the queue.
func (s *State) PendingApprovals() []PendingApproval {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]PendingApproval, len(s.approvals))
	copy(out, s.approvals)
	return out
}

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
