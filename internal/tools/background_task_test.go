package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The reference's own captured background-task texts. The fixture holds the
// step contents of three backgrounded commands and the SYSTEM_MESSAGE that
// carries a completion notification; both come from the reference's own
// transcript (see the file's provenance header).
const (
	refConvID = "45fde9e1-76d5-4b87-bd23-1b2c626bbf26"
	refHome   = "/Users/gear/.gemini/antigravity-cli"
)

// fixtureSection returns the body of the "---- <name> ----" section of the
// reference fixture.
func fixtureSection(t *testing.T, name string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "reference_background_task_1.4.7.txt"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	marker := "---- " + name + " ----\n"
	i := strings.Index(string(body), marker)
	if i < 0 {
		t.Fatalf("fixture has no section %q", name)
	}
	rest := string(body)[i+len(marker):]
	if j := strings.Index(rest, "\n---- "); j >= 0 {
		rest = rest[:j+1]
	}
	return rest
}

// TestTaskLaunchResultMatchesTheReference pins the launch acknowledgement
// the clone sends for a backgrounded task to the reference's own text.
//
// The reference's step content carries two extras the model does not see
// (a "Task Description:" line and, when the command has produced output, a
// "Log output:" section); the function response — the text the model
// receives — is the acknowledgement without them. Steps 56 and 114 are
// two backgrounded commands that had produced no output yet, so the
// reference's step content minus the description line must equal the
// clone's launch acknowledgement byte for byte.
func TestTaskLaunchResultMatchesTheReference(t *testing.T) {
	for _, tc := range []struct {
		section string
		task    string
		at      string
	}{
		{"launch acknowledgement (the step content, step 56)", "task-56", "2026-10-25T03:15:13+08:00"},
		{"launch acknowledgement (the step content, step 114)", "task-114", "2026-10-25T03:18:32+08:00"},
	} {
		step := fixtureSection(t, tc.section)
		// The step content without the "Task Description:" line is the
		// reference's function response.
		var kept []string
		for _, line := range strings.Split(strings.TrimRight(step, "\n"), "\n") {
			if strings.HasPrefix(line, "Task Description: ") {
				continue
			}
			kept = append(kept, line)
		}
		want := strings.Join(kept, "\n")

		at, err := time.Parse(time.RFC3339, tc.at)
		if err != nil {
			t.Fatalf("parse %q: %v", tc.at, err)
		}
		got := taskLaunchResult(at, refConvID, tc.task, taskLogPath(refHome, refConvID, tc.task))
		if got != want {
			t.Errorf("%s:\nlaunch acknowledgement =\n%q\nwant\n%q", tc.section, got, want)
		}
	}
}

// TestTaskLaunchResultCarriesTheReferenceTaskID pins the shape of the task
// id the acknowledgement names.
func TestTaskLaunchResultCarriesTheReferenceTaskID(t *testing.T) {
	at := time.Now()
	got := taskLaunchResult(at, refConvID, "task-56", taskLogPath(refHome, refConvID, "task-56"))
	if !strings.Contains(got, "task id: "+refConvID+"/task-56") {
		t.Errorf("the acknowledgement does not name the reference-shaped task id:\n%s", got)
	}
	if !strings.Contains(got, "Task logs are available at: file://"+refHome+"/brain/"+refConvID+
		"/.system_generated/tasks/task-56.log") {
		t.Errorf("the acknowledgement does not name the reference-shaped task log:\n%s", got)
	}
}

// TestTaskCompletionNotificationMatchesTheReference pins the completion
// notification to the reference's own text.
//
// The reference delivers it as a SYSTEM_MESSAGE whose content is the
// notification; the clone's taskCompletionNotification renders the same
// content (the delivery itself is a separate seam — see
// internal/agent/bg_finish.go).
func TestTaskCompletionNotificationMatchesTheReference(t *testing.T) {
	msg := fixtureSection(t, "completion notification (the SYSTEM_MESSAGE)")
	prefix := "[Message] timestamp=2026-10-24T19:15:22Z sender=" + refConvID +
		"/task-56 priority=MESSAGE_PRIORITY_HIGH content="
	i := strings.Index(msg, prefix)
	if i < 0 {
		t.Fatalf("the fixture has no message header:\n%s", msg)
	}
	content := msg[i+len(prefix):]
	content = strings.TrimSuffix(strings.TrimRight(content, "\n"), "</SYSTEM_MESSAGE>")
	content = strings.TrimRight(content, "\n")
	// The reference's command output passes through a pty, so its line
	// endings are CRLF (the pty's onlcr); the clone's pipes produce LF.
	// The notification's own text is compared; the output bytes are not.
	content = strings.ReplaceAll(content, "\r\n", "\n")

	result := "Command exited with code 0.\n\nCommand output:\n" +
		"=== RUN   TestAccountsPanelIsInvocableButNotListed\n" +
		"--- PASS: TestAccountsPanelIsInvocableButNotListed (0.00s)\n" +
		"PASS\n" +
		"ok  \tgithub.com/justwasm/pigasus/internal/cliapp\t1.579s\n"
	log := taskLogPath(refHome, refConvID, "task-56")
	got := taskCompletionNotification(refConvID+"/task-56", result, log)
	if got != content {
		t.Errorf("completion notification =\n%q\nwant\n%q", got, content)
	}
}

// TestRunCommandWaitWindowBackgroundsTheCommand pins the reference's
// WaitMsBeforeAsync behaviour: a command that outlives the wait window is
// sent to the background as a task and the tool returns the reference's
// launch acknowledgement.
func TestRunCommandWaitWindowBackgroundsTheCommand(t *testing.T) {
	home := t.TempDir()
	conv := "conv-wait-window"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"sleep 30","wait_ms_before_async":50}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Tool is running as a background task with task id: "+conv+"/task-1") {
		t.Fatalf("the result is not the reference's launch acknowledgement:\n%s", text)
	}
	if strings.Contains(text, "Background command ID") {
		t.Errorf("the result still carries the template's step string:\n%s", text)
	}
	// The task log is created when the task starts, and it is empty while
	// the command runs (【一手】 the reference's own task-114.log).
	log := taskLogPath(home, conv, "task-1")
	body, err := os.ReadFile(log)
	if err != nil {
		t.Fatalf("the task log was not created: %v", err)
	}
	if len(body) != 0 {
		t.Errorf("the task log of a running task is not empty: %q", body)
	}
	// The task is addressable by its id.
	if _, ok := backgroundTasks.get(conv + "/task-1"); !ok {
		t.Error("the backgrounded command is not in the task table")
	}
	t.Cleanup(func() {
		if task, ok := backgroundTasks.get(conv + "/task-1"); ok {
			_, _ = task.kill()
			// The task log is written when the command is gone; wait for
			// it, so the temp dir cleanup does not race the write.
			select {
			case <-task.finished:
			case <-time.After(5 * time.Second):
			}
		}
	})
}

// TestRunCommandFinishingInsideTheWaitWindowIsForeground pins the other
// half: a command that finishes inside the wait window returns the
// foreground result, not the launch acknowledgement.
func TestRunCommandFinishingInsideTheWaitWindowIsForeground(t *testing.T) {
	home := t.TempDir()
	conv := "conv-wait-foreground"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"printf hello","wait_ms_before_async":5000}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Command exited with code 0.") {
		t.Errorf("the result is not the foreground result:\n%s", text)
	}
	if !strings.Contains(text, "hello") {
		t.Errorf("the result does not carry the command's output:\n%s", text)
	}
	if strings.Contains(text, "Tool is running as a background task") {
		t.Errorf("a command that finished in the wait window was backgrounded:\n%s", text)
	}
}

// TestManageTaskImplementsTheReferenceActions pins the four actions the
// reference's own manage_task declaration advertises: list, kill, status
// and send_input.
func TestManageTaskImplementsTheReferenceActions(t *testing.T) {
	dir := t.TempDir()
	task, err := backgroundTasks.start(dir, "conv-manage/task-1", filepath.Join(dir, "task-1.log"),
		"cat", DefaultShellArgv("cat"), 0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _, _ = task.kill() })

	m := &ManageTask{}
	// list
	out, err := m.Run(context.Background(), json.RawMessage(`{"action":"list"}`))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "conv-manage/task-1") {
		t.Errorf("list does not name the task:\n%s", s)
	}
	// status
	out, err = m.Run(context.Background(), json.RawMessage(`{"action":"status","id":"conv-manage/task-1"}`))
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "conv-manage/task-1") {
		t.Errorf("status does not name the task:\n%s", s)
	}
	// send_input
	out, err = m.Run(context.Background(), json.RawMessage(
		`{"action":"send_input","id":"conv-manage/task-1","input":"hello"}`))
	if err != nil {
		t.Fatalf("send_input: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "Input sent successfully.") {
		t.Errorf("send_input does not confirm:\n%s", s)
	}
	// kill
	out, err = m.Run(context.Background(), json.RawMessage(
		`{"action":"kill","id":"conv-manage/task-1"}`))
	if err != nil {
		t.Fatalf("kill: %v", err)
	}
	if s, _ := out.(string); !strings.Contains(s, "conv-manage/task-1") {
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
	dir := t.TempDir()
	task, err := backgroundTasks.start(dir, "conv-input/task-1", filepath.Join(dir, "task-1.log"),
		"cat", DefaultShellArgv("cat"), 0)
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _, _ = task.kill() })
	if err := task.writeStdin("hello\n"); err != nil {
		t.Fatalf("writeStdin: %v", err)
	}
	out, err := (&SendCommandInput{}).Run(context.Background(), json.RawMessage(
		`{"id":"`+task.id+`","input":"hello"}`))
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
