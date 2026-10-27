package tools

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The reference's own schema field, read out of the 1.4.8 binary.
//
// 【反编译】 oneharness/internal/tools/tools.(*RunCommandTool).
// withTimeoutNotificationField
// (0x101dfa4b0) calls utils.AugmentSchemaField with:
//
//	name        "NotificationTimeoutSeconds"  26 B  vm 0x1035712d0
//	type        "integer"                      7 B  vm 0x1033f047f
//	description <386 bytes>                 0x182  vm 0x1036c2c0d
const refNotificationTimeoutDescription = "Optional. If the command runs for longer than this duration (in seconds) after the wait time (WaitMsBeforeAsync), the system will notify you that it is still running. Use this instead of setting a manual timer for checking status of commands. Highly recommended if the command is expected to terminate at all. Do not set this for daemon commands, which are expected to run indefinitely."

// TestTheSchemaCarriesTheReferenceNotificationTimeout pins the advertised
// parameter to the reference's own name, type and description — byte for
// byte.
func TestTheSchemaCarriesTheReferenceNotificationTimeout(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(runCommandSchema, &schema); err != nil {
		t.Fatalf("the run_command schema does not parse: %v", err)
	}
	field, ok := schema.Properties["NotificationTimeoutSeconds"]
	if !ok {
		t.Fatal("the run_command schema has no NotificationTimeoutSeconds")
	}
	if field.Type != "integer" {
		t.Errorf("NotificationTimeoutSeconds is a %q, want \"integer\"", field.Type)
	}
	if field.Description != refNotificationTimeoutDescription {
		t.Errorf("the description is\n%q\nwant\n%q", field.Description, refNotificationTimeoutDescription)
	}
}

// TestTheReferenceDescriptionIs386Bytes guards the length the reference's
// binary carries: the literal at vm 0x1036c2c0d is 0x182 bytes long.
func TestTheReferenceDescriptionIs386Bytes(t *testing.T) {
	if n := len(refNotificationTimeoutDescription); n != 386 {
		t.Errorf("the description is %d bytes, the reference's is 386", n)
	}
}

// TestTheNotificationTextIsTheReferences pins the two literals the
// reference uses for the "the command is still running" message.
func TestTheNotificationTextIsTheReferences(t *testing.T) {
	if notificationTimeoutTitle != "Notification timeout reached" {
		t.Errorf("the title is %q", notificationTimeoutTitle)
	}
	got := notificationTimeoutBody(300, "sleep 600")
	want := "Background command is still running after 300s: sleep 600"
	if got != want {
		t.Errorf("the body is\n%q\nwant\n%q", got, want)
	}
}

// TestTheTimeoutNotifiesWhileTheCommandRuns drives the reference's timer:
// the command is still running when it fires, so the model is told.
func TestTheTimeoutNotifiesWhileTheCommandRuns(t *testing.T) {
	notifications := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	task := &bgTask{
		id:       "conv-1/task-1",
		command:  "sleep 600",
		logPath:  "/tmp/task-1.log",
		finished: make(chan struct{}),
	}
	armNotificationTimeoutAfter(task, 10*time.Millisecond, 300)

	select {
	case n := <-notifications:
		if n.TaskID != "conv-1/task-1" {
			t.Errorf("the notification names the task %q", n.TaskID)
		}
		if n.Title != "Notification timeout reached" {
			t.Errorf("the notification's title is %q", n.Title)
		}
		if n.Body != "Background command is still running after 300s: sleep 600" {
			t.Errorf("the notification's body is %q", n.Body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the timer did not fire")
	}
}

// TestNoTimeoutNotificationWhenTheCommandFinishesFirst is the other half:
// a command that finishes before the timeout gets no such notification —
// the completion notification is the one it gets.
func TestNoTimeoutNotificationWhenTheCommandFinishesFirst(t *testing.T) {
	notifications := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	task := &bgTask{
		id:       "conv-1/task-2",
		command:  "true",
		logPath:  "/tmp/task-2.log",
		finished: make(chan struct{}),
	}
	armNotificationTimeoutAfter(task, 500*time.Millisecond, 300)
	close(task.finished)

	select {
	case n := <-notifications:
		t.Errorf("a finished command still produced the notification %q", n.Body)
	case <-time.After(800 * time.Millisecond):
	}
}

// TestNoTimeoutWithoutTheParameter: a command the model did not ask to be
// notified about never notifies.
func TestNoTimeoutWithoutTheParameter(t *testing.T) {
	notifications := make(chan TaskNotification, 1)
	prev := SetTaskNotifier(func(n TaskNotification) { notifications <- n })
	defer SetTaskNotifier(prev)

	task := &bgTask{
		id:       "conv-1/task-3",
		command:  "sleep 600",
		finished: make(chan struct{}),
	}
	armNotificationTimeout(task, 0)

	select {
	case n := <-notifications:
		t.Errorf("a command without the parameter produced the notification %q", n.Body)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestTheParameterIsParsed: the reference's own spelling of the parameter
// (CamelCase) reaches the executor through the clone's canonicaliser.
func TestTheParameterIsParsed(t *testing.T) {
	var p runCommandParams
	args := json.RawMessage(`{"CommandLine":"sleep 1","WaitMsBeforeAsync":5000,"NotificationTimeoutSeconds":300}`)
	if err := argsOf(args, &p); err != nil {
		t.Fatalf("argsOf: %v", err)
	}
	if p.NotificationTimeoutSeconds != 300 {
		t.Errorf("NotificationTimeoutSeconds = %d, want 300", p.NotificationTimeoutSeconds)
	}
	if p.WaitMsBeforeAsync != 5000 {
		t.Errorf("WaitMsBeforeAsync = %d, want 5000", p.WaitMsBeforeAsync)
	}
}

// TestTheWireDeclarationDoesNotCarryIt: the reference's *default*
// declaration — the one the clone ships on the wire — does not have the
// parameter, and the clone must not pretend otherwise.
func TestTheWireDeclarationDoesNotCarryIt(t *testing.T) {
	for _, decl := range ModelDeclarations() {
		if decl.Name != "run_command" {
			continue
		}
		if strings.Contains(string(decl.Parameters), "NotificationTimeoutSeconds") {
			t.Error("the wire declaration carries NotificationTimeoutSeconds; the reference's default does not")
		}
		return
	}
	t.Fatal("the wire declarations have no run_command")
}
