package permissionprompt

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func TestPermissionPrompt_RenderApprovalBox(t *testing.T) {
	req := Request{
		Tool:    "run_command",
		Command: "rm -rf dist",
	}

	m := New(req, WithWidth(62))
	plain := m.RenderPlain(62)

	// Check required banner elements from specification
	expectedSubstrings := []string{
		"┌─ Permission Required ──────────────────────────────────────┐",
		"│ Tool: run_command                                          │",
		"│ Command: rm -rf dist                                       │",
		"│ [y] Approve once  [a] Always allow  [n] Deny  [d] Feedback │",
		"└────────────────────────────────────────────────────────────┘",
	}

	for _, sub := range expectedSubstrings {
		if !strings.Contains(plain, sub) {
			t.Errorf("expected rendered box to contain %q, but got:\n%s", sub, plain)
		}
	}

	// Verify consistent width across all lines
	lines := m.RenderPlainLines(62)
	for i, l := range lines {
		// Each line should have length 62 (measured in runes/characters)
		runes := []rune(l)
		if len(runes) != 62 {
			t.Errorf("line %d: length in runes = %d, want 62 (line: %q)", i, len(runes), l)
		}
	}
}

func TestPermissionPrompt_RenderPathAndReason(t *testing.T) {
	req := Request{
		Tool:        "write_file",
		Path:        "/etc/nginx/nginx.conf",
		Description: "Update server configuration",
	}

	m := New(req, WithWidth(62))
	plain := m.RenderPlain(62)

	if !strings.Contains(plain, "Tool: write_file") {
		t.Errorf("missing tool name in output:\n%s", plain)
	}
	if !strings.Contains(plain, "Path: /etc/nginx/nginx.conf") {
		t.Errorf("missing path in output:\n%s", plain)
	}
	if !strings.Contains(plain, "Reason: Update server configuration") {
		t.Errorf("missing reason in output:\n%s", plain)
	}
}

func TestPermissionPrompt_KeyApproveOnce(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "make build"})

	res, handled := m.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if !handled {
		t.Fatalf("expected 'y' to be handled")
	}
	if res.Decision != DecisionApproveOnce {
		t.Errorf("res.Decision = %v, want %v", res.Decision, DecisionApproveOnce)
	}
	if !res.IsApproved() {
		t.Errorf("res.IsApproved() = false, want true")
	}
	if m.IsOpen() {
		t.Errorf("expected prompt to be closed after decision")
	}
}

func TestPermissionPrompt_KeyAlwaysAllow(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "git status"})

	res, handled := m.HandleKey(tea.Key{Code: 'a', Text: "a"})
	if !handled {
		t.Fatalf("expected 'a' to be handled")
	}
	if res.Decision != DecisionAlwaysAllow {
		t.Errorf("res.Decision = %v, want %v", res.Decision, DecisionAlwaysAllow)
	}
	if !res.IsApproved() {
		t.Errorf("res.IsApproved() = false, want true")
	}
	if m.IsOpen() {
		t.Errorf("expected prompt to be closed after decision")
	}
}

func TestPermissionPrompt_KeyDeny(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "rm -rf /"})

	res, handled := m.HandleKey(tea.Key{Code: 'n', Text: "n"})
	if !handled {
		t.Fatalf("expected 'n' to be handled")
	}
	if res.Decision != DecisionDeny {
		t.Errorf("res.Decision = %v, want %v", res.Decision, DecisionDeny)
	}
	if !res.IsDenied() {
		t.Errorf("res.IsDenied() = false, want true")
	}
	if m.IsOpen() {
		t.Errorf("expected prompt to be closed after decision")
	}
}

func TestPermissionPrompt_KeyEscDismiss(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "shutdown -h now"})

	res, handled := m.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatalf("expected Esc to be handled")
	}
	if res.Decision != DecisionDeny {
		t.Errorf("res.Decision = %v, want %v", res.Decision, DecisionDeny)
	}
	if !res.IsDenied() {
		t.Errorf("res.IsDenied() = false, want true")
	}
	if m.IsOpen() {
		t.Errorf("expected prompt to be closed after Esc")
	}
}

func TestPermissionPrompt_FeedbackMode(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "rm -rf dist"})

	// Press 'd' to enter feedback mode
	res, handled := m.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !handled {
		t.Fatalf("expected 'd' to be handled")
	}
	if !m.IsFeedbackMode() {
		t.Fatalf("expected prompt to be in feedback mode")
	}
	if res.Decision != DecisionNone {
		t.Errorf("res.Decision should be none while entering feedback, got %v", res.Decision)
	}

	// View should now display the feedback input banner
	plain := m.RenderPlain(62)
	if !strings.Contains(plain, "Feedback (Esc to cancel, Enter to submit):") {
		t.Errorf("expected feedback header in view, got:\n%s", plain)
	}

	// Type: "Use a safer command instead"
	input := "Use a safer command instead"
	for _, r := range input {
		m.HandleKey(tea.Key{Code: r, Text: string(r)})
	}

	if m.Feedback() != input {
		t.Errorf("m.Feedback() = %q, want %q", m.Feedback(), input)
	}

	// Test backspace
	m.HandleKey(tea.Key{Code: tea.KeyBackspace})
	if m.Feedback() != "Use a safer command instea" {
		t.Errorf("after backspace: %q", m.Feedback())
	}
	// Re-add 'd'
	m.HandleKey(tea.Key{Code: 'd', Text: "d"})

	// Press Enter to submit rejection with feedback
	res, handled = m.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatalf("expected Enter to be handled")
	}
	if res.Decision != DecisionFeedback {
		t.Errorf("res.Decision = %v, want %v", res.Decision, DecisionFeedback)
	}
	if res.Feedback != input {
		t.Errorf("res.Feedback = %q, want %q", res.Feedback, input)
	}
	if !res.IsDenied() {
		t.Errorf("res.IsDenied() = false, want true")
	}
	if m.IsOpen() {
		t.Errorf("expected prompt to be closed after submitting feedback")
	}
}

func TestPermissionPrompt_FeedbackEscCancels(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "rm -rf dist"})

	// Press 'd' to enter feedback mode
	m.HandleKey(tea.Key{Code: 'd', Text: "d"})
	if !m.IsFeedbackMode() {
		t.Fatalf("expected prompt to be in feedback mode")
	}

	// Press Esc to cancel feedback mode
	_, handled := m.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatalf("expected Esc to be handled")
	}
	if m.IsFeedbackMode() {
		t.Errorf("expected Esc to exit feedback mode")
	}
	if !m.IsOpen() {
		t.Errorf("expected prompt to remain open after cancelling feedback mode")
	}
}

func TestPermissionPrompt_ArrowNavigationAndEnter(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "touch myfile"})

	if m.SelectedOption() != OptionApproveOnce {
		t.Fatalf("initial option = %v, want OptionApproveOnce", m.SelectedOption())
	}

	// Move right -> OptionAlwaysAllow
	m.HandleKey(tea.Key{Code: tea.KeyRight})
	if m.SelectedOption() != OptionAlwaysAllow {
		t.Errorf("after Right: selected = %v, want OptionAlwaysAllow", m.SelectedOption())
	}

	// Move right -> OptionDeny
	m.HandleKey(tea.Key{Code: tea.KeyRight})
	if m.SelectedOption() != OptionDeny {
		t.Errorf("after Right: selected = %v, want OptionDeny", m.SelectedOption())
	}

	// Press Enter on Deny
	res, handled := m.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || res.Decision != DecisionDeny {
		t.Errorf("expected Enter on Deny to return DecisionDeny, got %v", res.Decision)
	}
}

func TestPermissionPrompt_UpdateAndView(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "cargo test"})

	// Update with tea.KeyPressMsg
	upd, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Errorf("cmd != nil")
	}
	model, ok := upd.(*Model)
	if !ok {
		t.Fatalf("upd is not *Model")
	}
	res, hasRes := model.Result()
	if !hasRes || res.Decision != DecisionApproveOnce {
		t.Errorf("expected DecisionApproveOnce, got %v", res)
	}

	// View after close
	view := model.View()
	if view.Content != "" {
		t.Errorf("expected empty view when closed, got %q", view.Content)
	}
	if str := model.ViewString(); str != "" {
		t.Errorf("expected empty ViewString when closed, got %q", str)
	}
}

func TestPermissionPrompt_FileEditAndCreate(t *testing.T) {
	// B-9: File Edit Confirmation
	editReq := Request{
		Kind: KindFileEdit,
		Path: "internal/main.go",
	}
	mEdit := New(editReq, WithWidth(62))
	plainEdit := mEdit.RenderPlain(62)
	if !strings.Contains(plainEdit, "Accept this file edit?") {
		t.Errorf("expected prompt to contain 'Accept this file edit?', got:\n%s", plainEdit)
	}
	if !strings.Contains(plainEdit, "[1] Yes, accept this change") {
		t.Errorf("expected option '[1] Yes, accept this change', got:\n%s", plainEdit)
	}
	if !strings.Contains(plainEdit, "[2] No, reject this change") {
		t.Errorf("expected option '[2] No, reject this change', got:\n%s", plainEdit)
	}

	// Esc does not dismiss a file edit
	_, handled := mEdit.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !mEdit.IsOpen() {
		t.Errorf("expected Esc not to dismiss pending file edit")
	}

	// Pressing '1' accepts file edit
	res, handled := mEdit.HandleKey(tea.Key{Code: '1', Text: "1"})
	if !handled || res.Decision != DecisionApproveOnce {
		t.Errorf("expected '1' to approve file edit, got %v", res.Decision)
	}

	// B-9: File Create Confirmation
	createReq := Request{
		Kind:      KindFileCreate,
		Path:      "src/utils/formatter.py",
		OutsideWS: true,
	}
	mCreate := New(createReq, WithWidth(62))
	plainCreate := mCreate.RenderPlain(62)
	if !strings.Contains(plainCreate, "Create file") {
		t.Errorf("expected header 'Create file', got:\n%s", plainCreate)
	}
	if !strings.Contains(plainCreate, "Allow creation of this file?") {
		t.Errorf("expected question 'Allow creation of this file?', got:\n%s", plainCreate)
	}
	if !strings.Contains(plainCreate, "Reason: outside workspace") {
		t.Errorf("expected 'Reason: outside workspace', got:\n%s", plainCreate)
	}
	if !strings.Contains(plainCreate, "[a] Yes, and always allow non-workspace access") {
		t.Errorf("expected non-workspace option, got:\n%s", plainCreate)
	}

	// Esc dismisses/denies file creation
	resCreate, handled := mCreate.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || resCreate.Decision != DecisionDeny {
		t.Errorf("expected Esc to deny file creation, got %v", resCreate.Decision)
	}
}

func TestPermissionPrompt_MatchOptionID(t *testing.T) {
	req := Request{
		Tool: "run_command",
		Options: []PermissionOption{
			{ID: "opt_allow", Kind: "allow_once", Title: "Allow once"},
			{ID: "opt_always", Kind: "allow_always", Title: "Always allow"},
			{ID: "opt_reject", Kind: "reject", Title: "Deny"},
			{ID: "opt_feedback", Kind: "reject_with_feedback", Title: "Reject with feedback"},
		},
	}

	m := New(req)
	res, _ := m.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if res.OptionID != "opt_allow" {
		t.Errorf("res.OptionID = %q, want opt_allow", res.OptionID)
	}
}

func TestPermissionPrompt_StyledRender(t *testing.T) {
	req := Request{
		Tool:    "run_command",
		Command: "git commit -m 'feat: interactive approval'",
	}
	m := New(req, WithWidth(70))
	lines := m.Lines(70)
	if len(lines) == 0 {
		t.Fatalf("expected non-empty styled lines")
	}

	rendered := m.Render(70)
	if !strings.Contains(rendered, "run_command") {
		t.Errorf("expected rendered text to contain tool name")
	}
}

func TestPermissionPrompt_ArgsMapFallback(t *testing.T) {
	req := Request{
		Tool: "run_command",
		Args: map[string]any{
			"CommandLine": "ls -la /tmp",
		},
	}
	m := New(req)
	plain := m.RenderPlain(62)
	if !strings.Contains(plain, "Command: ls -la /tmp") {
		t.Errorf("expected CommandLine from Args map to be rendered as Command, got:\n%s", plain)
	}
}

func TestPermissionPrompt_ReopenModel(t *testing.T) {
	m := New(Request{Tool: "run_command", Command: "first"})
	m.HandleKey(tea.Key{Code: 'n', Text: "n"})
	if m.IsOpen() {
		t.Fatalf("expected prompt to be closed")
	}

	// Reopen with new request
	m.Open(Request{Tool: "write_file", Path: "/tmp/foo.txt"})
	if !m.IsOpen() {
		t.Fatalf("expected prompt to be reopened")
	}
	plain := m.RenderPlain(62)
	if !strings.Contains(plain, "Tool: write_file") || !strings.Contains(plain, "Path: /tmp/foo.txt") {
		t.Errorf("reopened view did not reflect new request:\n%s", plain)
	}
}
