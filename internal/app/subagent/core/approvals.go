// approvals.go — the manager's pending authorizations.
//
// A tracked subagent can be parked on a protected operation: it waits
// for the user to allow it or to refuse it. The reference's queue is
// the recovered `updatePendingApprovalsQueue` (0x8aa3f80) — the
// pending tool confirmations the 1.3.5 binary parks a subagent on,
// answered through `RespondToSubagentToolConfirmation` (0x8aa3240).
// The recovered call graph survives at
// docs/legacy-re/extracts/analysis-v1.3.5-official/backend-call-graph.md
// (§3, the Store Manager table). The clone keeps that queue in
// internal/core/lifecycle: it owns the payload (`PendingApproval`) and
// the queue itself.
//
// The manager owns the *association*: which authorization belongs to
// which tracked subagent. That is the fact the /agents panel reads —
// the authorization a subagent's row carries — and the fact it answers
// when the user presses A (allow) or D (refuse).
//
// 【strings】 the reference's own wording for the two answers is in its
// string table: "Yes, allow tool call" and "No, deny tool call", beside
// the keybinding names `KeySubagentApproveFast` and
// `KeySubagentApproveFastNo`. The deny half and the "No" keybinding
// name survive in the pruned string index
// (out/local-macos-1.4.11/strings-index.txt, the A-段 windows around
// lines 66329 and 66521).

package core

import (
	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// approvalQueue is the manager's pending-authorization queue.
//
// The queue is created on first use, so a zero Manager still answers.
func (m *Manager) approvalQueue() *lifecycle.State {
	if m == nil {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.approvals == nil {
		m.approvals = lifecycle.NewState()
	}
	return m.approvals
}

// PendingAuthorization returns the authorization the named subagent is
// waiting on.
//
// It answers false when the subagent waits for nothing, and when the
// manager has no queue at all (a zero Manager).
func (m *Manager) PendingAuthorization(subagentID string) (lifecycle.PendingApproval, bool) {
	if m == nil || subagentID == "" {
		return lifecycle.PendingApproval{}, false
	}
	queue := m.approvalQueue()
	if queue == nil {
		return lifecycle.PendingApproval{}, false
	}
	for _, appr := range queue.PendingApprovals() {
		if appr.SubagentID == subagentID {
			return appr, true
		}
	}
	return lifecycle.PendingApproval{}, false
}

// AnswerAuthorization answers a pending authorization.
//
// approve allows the protected operation, refuse denies it. It answers
// whether an authorization was answered: an id that is not in the
// queue is refused (nothing happened).
//
// The two answers are the queue's own recovered operations:
// `ApproveSubagent` (the tool-confirmation answer, the recovered
// `RespondToSubagentToolConfirmation` at 0x8aa3240) and its dual
// `RejectSubagent` (the recovered `HandleSubagentUserInput` at
// 0x8aa7da0 — both in
// docs/legacy-re/extracts/analysis-v1.3.5-official/backend-call-graph.md,
// §3).
func (m *Manager) AnswerAuthorization(approvalID string, approve bool) bool {
	if m == nil || approvalID == "" {
		return false
	}
	queue := m.approvalQueue()
	if queue == nil {
		return false
	}
	var err error
	if approve {
		_, err = queue.ApproveSubagent(approvalID)
	} else {
		_, err = queue.RejectSubagent(approvalID)
	}
	return err == nil
}

// RequestAuthorization parks a subagent on a protected operation and
// returns the authorization that was queued.
//
// It is the recovered `TriggerSubagentApprovalFast` entry point
// (0x8aa6f20 — the recovered call graph's §3 table): the subagent asks
// for a tool confirmation, the request joins the pending queue, and
// the caller answers it later with AnswerAuthorization.
//
// 【反编译】 the recovered call shape (0x8aa6f20 + 0x8aa3f80): the
// approval is enqueued and the caller keeps the returned id to match
// the answer by.
//
// The clone's tracked subagents are child *processes* and do not ask
// for a protected operation yet, so nothing in the tree calls this
// today — it is staged here because it is the reference's own entry
// point, and the /agents panel's A/D keys are the half that answers
// what it queues.
func (m *Manager) RequestAuthorization(subagentID string, kind lifecycle.ApprovalKind, toolName, question string) lifecycle.PendingApproval {
	if m == nil {
		return lifecycle.PendingApproval{}
	}
	queue := m.approvalQueue()
	if queue == nil {
		return lifecycle.PendingApproval{}
	}
	_, appr := queue.SubagentAwaitsApproval(subagentID, kind, toolName, question)
	return appr
}

// dropAuthorizations removes the authorizations of one subagent.
//
// The killed subagent will never answer them, so they go with it — the
// same rule the AGY 1.3.3 changelog states for a stopped subagent
// ("stopping it now clears its pending approvals right away").
func (m *Manager) dropAuthorizations(subagentID string) {
	if m == nil || subagentID == "" {
		return
	}
	queue := m.approvalQueue()
	if queue == nil {
		return
	}
	queue.ClearApprovalsBySubagent(subagentID)
}
