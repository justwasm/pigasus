// approvals_test.go — the manager's pending authorizations.
//
// The manager owns the association between an authorization and the
// tracked subagent it belongs to: the /agents panel reads it (the
// authorization a subagent's row carries) and answers it (the A and D
// keys).

package core

import (
	"testing"

	lifecycle "github.com/justwasm/pigasus/internal/core/lifecycle"
)

// TestManagerPendingAuthorization — the queue answers for the subagent
// that asked, and for nobody else.
func TestManagerPendingAuthorization(t *testing.T) {
	mgr := NewManager()

	if _, ok := mgr.PendingAuthorization("sub-1"); ok {
		t.Error("a subagent with no authorization has one")
	}

	appr := mgr.RequestAuthorization("sub-1", lifecycle.ApprovalToolConfirm, "write_file", "")
	if appr.ID == "" {
		t.Fatal("the queued authorization has no id")
	}
	if appr.SubagentID != "sub-1" {
		t.Errorf("the authorization's subagent = %q, want %q", appr.SubagentID, "sub-1")
	}

	got, ok := mgr.PendingAuthorization("sub-1")
	if !ok {
		t.Fatal("the queued authorization was not found")
	}
	if got.ID != appr.ID {
		t.Errorf("the authorization's id = %q, want %q", got.ID, appr.ID)
	}
	if got.ToolName != "write_file" {
		t.Errorf("the authorization's tool = %q, want %q", got.ToolName, "write_file")
	}

	if _, ok := mgr.PendingAuthorization("sub-2"); ok {
		t.Error("another subagent found the authorization")
	}
}

// TestManagerAnswerAuthorizationApproves — the "yes" answer empties the
// queue entry.
func TestManagerAnswerAuthorizationApproves(t *testing.T) {
	mgr := NewManager()
	appr := mgr.RequestAuthorization("sub-1", lifecycle.ApprovalToolConfirm, "write_file", "")

	if !mgr.AnswerAuthorization(appr.ID, true) {
		t.Fatal("the approval was refused")
	}
	if _, ok := mgr.PendingAuthorization("sub-1"); ok {
		t.Error("the authorization is still pending after the approval")
	}
	// The same authorization cannot be answered twice.
	if mgr.AnswerAuthorization(appr.ID, true) {
		t.Error("the same authorization was answered twice")
	}
}

// TestManagerAnswerAuthorizationDenies — the "no" answer empties the
// queue entry too.
func TestManagerAnswerAuthorizationDenies(t *testing.T) {
	mgr := NewManager()
	appr := mgr.RequestAuthorization("sub-1", lifecycle.ApprovalUserInput, "", "proceed?")

	if !mgr.AnswerAuthorization(appr.ID, false) {
		t.Fatal("the refusal was refused")
	}
	if _, ok := mgr.PendingAuthorization("sub-1"); ok {
		t.Error("the authorization is still pending after the refusal")
	}
}

// TestManagerAnswerAuthorizationRefusesAnUnknownID — an id that is not
// in the queue answers nothing.
func TestManagerAnswerAuthorizationRefusesAnUnknownID(t *testing.T) {
	mgr := NewManager()
	if mgr.AnswerAuthorization("no-such-id", true) {
		t.Error("an unknown id was approved")
	}
	if mgr.AnswerAuthorization("", true) {
		t.Error("an empty id was approved")
	}
	if mgr.AnswerAuthorization("", false) {
		t.Error("an empty id was refused")
	}
}
