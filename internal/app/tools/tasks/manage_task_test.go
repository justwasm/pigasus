package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

// launchBackgroundTask starts a background command through the shell
// capability's own door and returns the task id the reference mints.
//
// The task id is the reference's own shape — "<conversation-id>/task-<n>" —
// and it is the id the task tools address, so the test never has to guess
// it.
func launchBackgroundTask(t *testing.T, conversationID, command string) string {
	t.Helper()
	r := &shell.RunCommand{
		Workspace:      t.TempDir(),
		ConversationID: conversationID,
		Home:           t.TempDir(),
	}
	out, err := r.Run(context.Background(), json.RawMessage(`{"command":`+quoteJSON(command)+`,"background":true}`))
	if err != nil {
		t.Fatalf("run_command(background): %v", err)
	}
	text, _ := out.(string)
	const marker = "task id: "
	i := strings.Index(text, marker)
	if i < 0 {
		t.Fatalf("the launch result names no task: %q", text)
	}
	id := text[i+len(marker):]
	if j := strings.IndexAny(id, " \n"); j >= 0 {
		id = id[:j]
	}
	t.Cleanup(func() { _ = shell.KillBackgroundTask(id) })
	return id
}

// quoteJSON renders a string as a JSON literal.
func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestManageTaskImplementsTheReferenceActions pins the four actions the
// reference's own manage_task declaration advertises: list, kill, status
// and send_input.
func TestManageTaskImplementsTheReferenceActions(t *testing.T) {
	id := launchBackgroundTask(t, "conv-manage", "cat")

	m := &ManageTask{}
	// list
	out, err := m.Run(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, id) {
		t.Errorf("list does not name the task:\n%s", s)
	}
	// status with id
	out, err = m.Run(context.Background(), json.RawMessage(`{"action":"status","id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, id) {
		t.Errorf("status does not name the task:\n%s", s)
	}
	// status with wire TaskId (PascalCase)
	out, err = m.Run(context.Background(), json.RawMessage(`{"Action":"status","TaskId":"`+id+`"}`))
	if err != nil {
		t.Fatalf("status with TaskId: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, id) {
		t.Errorf("status with TaskId does not name the task:\n%s", s)
	}
	// status with snake_case task_id
	out, err = m.Run(context.Background(), json.RawMessage(`{"action":"status","task_id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("status with task_id: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, id) {
		t.Errorf("status with task_id does not name the task:\n%s", s)
	}
	// send_input
	out, err = m.Run(context.Background(), json.RawMessage(
		`{"action":"send_input","id":"`+id+`","input":"hello"}`))
	if err != nil {
		t.Fatalf("send_input: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "Input sent successfully.") {
		t.Errorf("send_input does not confirm:\n%s", s)
	}
	// kill
	out, err = m.Run(context.Background(), json.RawMessage(
		`{"action":"kill","id":"`+id+`"}`))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, id) {
		t.Errorf("kill does not name the task:\n%s", s)
	}
	// an unknown action is an error
	if _, err := m.Run(context.Background(), json.RawMessage(`{"action":"nope"}`)); err == nil {
		t.Error("an unknown action was accepted")
	}
}

// TestSendCommandInputUsesTheReferenceStepString pins the input tool's
// result text to the reference's own step string.
//
// "Input sent successfully.", "The command is still running." and "The
// command exited with code N." each appear exactly once in the reference's
// binary — inside step_strings/send_command_input.tmpl. That template is
// therefore the reference's wording for this result.
func TestSendCommandInputUsesTheReferenceStepString(t *testing.T) {
	id := launchBackgroundTask(t, "conv-input", "cat")

	out, err := (&SendCommandInput{}).Run(context.Background(), json.RawMessage(
		`{"id":"`+id+`","input":"hello"}`))
	if err != nil {
		t.Fatalf("SendCommandInput: %v", err)
	}
	s, _ := out.(string)
	if !strings.HasPrefix(s, "Input sent successfully.\n") {
		t.Errorf("the result does not confirm the input:\n%q", s)
	}
	if !strings.Contains(s, "The command is still running.") {
		t.Errorf("the result does not report the running command:\n%q", s)
	}
	if strings.HasPrefix(s, "\n") {
		t.Error("the result starts with the template's leading newline")
	}
}
