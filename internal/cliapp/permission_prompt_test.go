package cliapp

import (
	"context"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/views/permissionprompt"
)

func TestPermissionPrompt_HostBasicLifecycle(t *testing.T) {
	req := permissionprompt.Request{
		Tool:    "run_command",
		Command: "cargo build --release",
	}

	prompt := NewPrompt(req)
	if !prompt.IsOpen() {
		t.Fatalf("expected prompt to be open initially")
	}

	// Verify request retrieval
	if prompt.Request().CommandStr() != "cargo build --release" {
		t.Errorf("prompt.Request().CommandStr() = %q", prompt.Request().CommandStr())
	}

	// Render view
	view := prompt.View()
	if !strings.Contains(view, "run_command") {
		t.Errorf("view missing tool name:\n%s", view)
	}

	// Resolve with ApproveOnce
	res := permissionprompt.Result{
		Decision: permissionprompt.DecisionApproveOnce,
	}
	prompt.Resolve(res)

	if prompt.IsOpen() {
		t.Errorf("expected prompt to be closed after Resolve")
	}

	// Verify Done channel
	select {
	case got := <-prompt.Done():
		if got.Decision != permissionprompt.DecisionApproveOnce {
			t.Errorf("got decision %v, want ApproveOnce", got.Decision)
		}
	default:
		t.Fatalf("expected result on Done channel")
	}
}

func TestPermissionPrompt_HostWaitContextCancel(t *testing.T) {
	prompt := NewPrompt(permissionprompt.Request{
		Tool: "run_command",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	res, err := prompt.Wait(ctx)
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
	if res.Decision != permissionprompt.DecisionDeny {
		t.Errorf("expected DecisionDeny on context cancellation, got %v", res.Decision)
	}
	if prompt.IsOpen() {
		t.Errorf("expected prompt to be closed after cancellation")
	}
}

func TestPermissionPrompt_HostKeyHandling(t *testing.T) {
	prompt := NewPrompt(permissionprompt.Request{
		Tool:    "run_command",
		Command: "git reset --hard",
	})

	// Send 'y'
	res, handled := prompt.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if !handled {
		t.Fatalf("expected 'y' to be handled")
	}
	if res.Decision != permissionprompt.DecisionApproveOnce {
		t.Errorf("res.Decision = %v, want ApproveOnce", res.Decision)
	}

	// Wait should return immediately with the result
	waitRes, err := prompt.Wait(context.Background())
	if err != nil {
		t.Fatalf("unexpected Wait error: %v", err)
	}
	if waitRes.Decision != permissionprompt.DecisionApproveOnce {
		t.Errorf("waitRes = %v, want ApproveOnce", waitRes)
	}
}

func TestPermissionPrompt_StateManagerQueue(t *testing.T) {
	state := NewPermissionPromptState()
	defer state.Reset()

	req1 := permissionprompt.Request{Tool: "tool1", Command: "cmd1"}
	req2 := permissionprompt.Request{Tool: "tool2", Command: "cmd2"}

	p1 := state.Raise(req1)
	p2 := state.Raise(req2)

	if state.Active() != p1 {
		t.Fatalf("expected p1 to be active initially")
	}
	if state.QueueLen() != 1 {
		t.Errorf("expected queue length 1, got %d", state.QueueLen())
	}

	// Approve p1
	state.HandleKey(tea.Key{Code: 'y', Text: "y"})

	if p1.IsOpen() {
		t.Errorf("expected p1 to be closed")
	}

	// p2 should now automatically become active!
	if state.Active() != p2 {
		t.Fatalf("expected p2 to be active after p1 resolved")
	}
	if state.QueueLen() != 0 {
		t.Errorf("expected queue to be empty, got %d", state.QueueLen())
	}

	// Reject p2 with feedback 'd'
	state.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !p2.Model().IsFeedbackMode() {
		t.Fatalf("expected p2 to be in feedback mode")
	}

	// Type reason
	state.HandleKey(tea.Key{Code: 'o', Text: "o"})
	state.HandleKey(tea.Key{Code: 'k', Text: "k"})
	state.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p2.IsOpen() {
		t.Errorf("expected p2 to be closed after feedback submit")
	}

	res, _ := p2.Model().Result()
	if res.Decision != permissionprompt.DecisionFeedback || res.Feedback != "ok" {
		t.Errorf("p2 result = %v, want feedback 'ok'", res)
	}

	if state.Active() != nil {
		t.Errorf("expected no active prompt after queue empty")
	}
}

func TestPermissionPrompt_RunModeSeams(t *testing.T) {
	m := &runMode{}

	DefaultPermissionPrompts.Reset()
	defer DefaultPermissionPrompts.Reset()

	if m.hasActivePermissionPrompt() {
		t.Fatalf("expected no active prompt initially")
	}

	p := m.raisePermissionPrompt(permissionprompt.Request{
		Tool:    "write_file",
		Path:    "/tmp/test.go",
		Command: "",
	})

	if !m.hasActivePermissionPrompt() {
		t.Fatalf("expected active prompt after raise")
	}
	if m.activePermissionPrompt() != p {
		t.Errorf("m.activePermissionPrompt() != p")
	}

	// Handle 'n' (deny)
	res, handled := m.handlePermissionKey(tea.Key{Code: 'n', Text: "n"})
	if !handled || res.Decision != permissionprompt.DecisionDeny {
		t.Errorf("res = %v, handled = %v", res, handled)
	}

	if m.hasActivePermissionPrompt() {
		t.Errorf("expected prompt to be closed after deny")
	}
}
