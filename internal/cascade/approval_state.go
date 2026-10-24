package cascade

import (
	"fmt"
	"time"
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

// enqueueApprovalLocked is the internal queue mutator. Caller holds s.mu.
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
	return s.nextIDLocked()
}

// nextIDLocked is the lockless variant; caller holds s.mu.
func (s *State) nextIDLocked() string {
	s.counter++
	return fmt.Sprintf("appr-%d-%d", time.Now().UnixNano(), s.counter)
}
