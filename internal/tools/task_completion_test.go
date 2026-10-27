// task_completion_test.go — cycle 309: the reference's completion
// notification for a background task.
//
// 【一手】 The reference tells the model that a background task is over: the
// notification carries the command's result and names the task log, and it
// reaches the model as the SYSTEM_MESSAGE the reference wraps its agent
// messages in. The captured notification is the reference's own text — see
// internal/tools/testdata/reference_background_task_1.4.7.txt, the
// "completion notification (the SYSTEM_MESSAGE)" section.
//
// These tests cover the delivery: the tools layer fires the notification
// when the command is gone, and it fires it once — a command that finished
// inside the wait window never became a task and owes no notification.
package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// TestTheCompletionNotificationFiresWhenTheTaskIsGone drives a backgrounded
// command to its end and pins the notification the model gets.
func TestTheCompletionNotificationFiresWhenTheTaskIsGone(t *testing.T) {
	notifications := make(chan TaskNotification, 4)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	home := t.TempDir()
	conv := "conv-completion"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"sleep 0.2; printf hello","wait_ms_before_async":1}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Tool is running as a background task with task id: "+conv+"/task-1") {
		t.Fatalf("the command did not go to the background:\n%s", text)
	}

	select {
	case n := <-notifications:
		if n.TaskID != conv+"/task-1" {
			t.Errorf("the notification names the task %q", n.TaskID)
		}
		if n.Priority != TaskPriorityHigh {
			t.Errorf("the notification's priority is %q, want %q", n.Priority, TaskPriorityHigh)
		}
		if n.At.IsZero() {
			t.Error("the notification carries no timestamp")
		}
		want := "Task id \"" + conv + "/task-1\" finished with result:\n" +
			"Command exited with code 0.\n\nCommand output:\nhello" +
			"\nLog: file://" + taskLogPath(home, conv, "task-1")
		if n.Body != want {
			t.Errorf("the notification's body is\n%q\nwant\n%q", n.Body, want)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the completion notification did not arrive")
	}
}

// TestNoCompletionNotificationForAForegroundCommand is the other half: a
// command that finishes inside the wait window never became a task, so the
// model reads the foreground result and gets no notification.
func TestNoCompletionNotificationForAForegroundCommand(t *testing.T) {
	notifications := make(chan TaskNotification, 4)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	home := t.TempDir()
	conv := "conv-foreground"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"printf hello","wait_ms_before_async":5000}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Command exited with code 0.") {
		t.Fatalf("the result is not the foreground result:\n%s", text)
	}

	select {
	case n := <-notifications:
		t.Errorf("a foreground command produced the notification %q", n.Body)
	case <-time.After(500 * time.Millisecond):
	}
}

// TestTheCompletionNotificationCarriesTheExitCode pins the non-zero exit:
// it is data, not a failure (the repo's own invariant), so the task
// "finished" and the result carries the code the model has to read.
func TestTheCompletionNotificationCarriesTheExitCode(t *testing.T) {
	notifications := make(chan TaskNotification, 4)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	home := t.TempDir()
	conv := "conv-exit-code"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"sleep 0.2; exit 3","wait_ms_before_async":1}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Tool is running as a background task") {
		t.Fatalf("the command did not go to the background:\n%s", text)
	}

	select {
	case n := <-notifications:
		if !strings.Contains(n.Body, "Command exited with code 3.") {
			t.Errorf("the notification does not carry the exit code:\n%s", n.Body)
		}
		if !strings.Contains(n.Body, "finished with result:") {
			t.Errorf("the notification does not report a finished task:\n%s", n.Body)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the completion notification did not arrive")
	}
}

// TestTheCompletionNotificationReportsACanceledTask: a task the user
// killed has no result — the reference's own status word for it is
// "was canceled".
func TestTheCompletionNotificationReportsACanceledTask(t *testing.T) {
	notifications := make(chan TaskNotification, 4)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	home := t.TempDir()
	conv := "conv-canceled"
	cmd := &RunCommand{Workspace: t.TempDir(), ConversationID: conv, Home: home}

	out, err := cmd.Run(context.Background(), json.RawMessage(
		`{"command":"sleep 30","wait_ms_before_async":1}`))
	if err != nil {
		t.Fatalf("run_command: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "Tool is running as a background task") {
		t.Fatalf("the command did not go to the background:\n%s", text)
	}

	task, ok := backgroundTasks.get(conv + "/task-1")
	if !ok {
		t.Fatal("the backgrounded command is not in the task table")
	}
	if _, err := task.kill(); err != nil {
		t.Fatalf("kill: %v", err)
	}

	select {
	case n := <-notifications:
		if !strings.Contains(n.Body, "was canceled") {
			t.Errorf("the notification does not report a canceled task:\n%s", n.Body)
		}
		if strings.Contains(n.Body, "with result:") {
			t.Errorf("a canceled task carries a result:\n%s", n.Body)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the completion notification did not arrive")
	}
}
