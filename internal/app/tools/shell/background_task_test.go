package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/tools"
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
		got := TaskLaunchResult(at, refConvID, tc.task, taskLogPath(refHome, refConvID, tc.task))
		if got != want {
			t.Errorf("%s:\nlaunch acknowledgement =\n%q\nwant\n%q", tc.section, got, want)
		}
	}
}

// TestTaskLaunchResultCarriesTheReferenceTaskID pins the shape of the task
// id the acknowledgement names.
func TestTaskLaunchResultCarriesTheReferenceTaskID(t *testing.T) {
	at := time.Now()
	got := TaskLaunchResult(at, refConvID, "task-56", taskLogPath(refHome, refConvID, "task-56"))
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
			_, _ = task.Kill()
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

// TestRunCommandDaemonRegistersInSharedTaskRegistry pins that daemon/background commands
// register into tools.SharedTaskRegistry with KindShell and can be killed via the registry handle.
func TestRunCommandDaemonRegistersInSharedTaskRegistry(t *testing.T) {
	home := t.TempDir()
	conv := "conv-daemon-reg"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"sleep 30","is_daemon":true}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Tool is running as a background task") {
		t.Fatalf("expected launch result, got %s", text)
	}

	fullID := conv + "/task-1"
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		t.Fatal("expected SharedTaskRegistry to be non-nil")
	}

	t.Logf("cmd output: %s", text)
	for _, item := range reg.List() {
		t.Logf("reg item: id=%q name=%q", item.ID, item.Name)
	}

	task := reg.Get(fullID)
	if task == nil {
		t.Fatalf("expected task %q to be registered in SharedTaskRegistry", fullID)
	}
	if task.Kind != "shell" {
		t.Errorf("task.Kind = %v, want shell", task.Kind)
	}
	if task.State != "running" {
		t.Errorf("task.State = %v, want running", task.State)
	}
	if task.Command != "sleep 30" {
		t.Errorf("task.Command = %q, want 'sleep 30'", task.Command)
	}
	if task.OutputFunc == nil {
		t.Error("expected OutputFunc to be non-nil")
	}

	// Kill via task registry handle
	killed := reg.Kill(fullID)
	if !killed {
		t.Errorf("expected reg.Kill(%q) to succeed", fullID)
	}

	// Verify task is marked killed
	taskAfter := reg.Get(fullID)
	if taskAfter.State != "killed" {
		t.Errorf("expected task to be killed, got state %v", taskAfter.State)
	}
}
