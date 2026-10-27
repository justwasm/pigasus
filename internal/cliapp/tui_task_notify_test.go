// tui_task_notify_test.go — cycle 309: the delivery of a background task's
// notification into the turn loop.
//
// The reference tells the model that a background task is over: the
// notification is an agent message, and the agent picks it up and carries
// on. These tests pin the clone's side of that: the notification joins the
// session, reaches the model in the user-content position, and is stored
// as the reference's own system-message step.
package cliapp

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestTheNotifierPutsTheNotificationInTheInbox pins the seam between the
// tools layer and the session: the runtime fires the notification, the
// session's inbox holds it until the next turn.
func TestTheNotifierPutsTheNotificationInTheInbox(t *testing.T) {
	prev := tools.SetTaskNotifier(nil)
	defer tools.SetTaskNotifier(prev)

	m := &runMode{taskInbox: agent.NewTaskInbox()}
	m.installTaskNotifier()

	tools.NotifyTask(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	if m.taskInbox.Len() != 1 {
		t.Fatalf("the inbox holds %d notifications, want 1", m.taskInbox.Len())
	}
}

// TestTheNotificationReachesTheModelAsAUserTurn pins the delivery: the
// notification joins the session as a system message and reaches the model
// in the user-content position, without the <USER_REQUEST> envelope the
// reference wraps around what the user typed.
//
// The reference's own lead-in says the message "was not actually sent by
// the user" — which is why it travels where a user message travels.
func TestTheNotificationReachesTheModelAsAUserTurn(t *testing.T) {
	m := &runMode{taskInbox: agent.NewTaskInbox()}
	m.taskInbox.Add(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})

	msgs := m.prepareStreamTurnMessages()
	if len(msgs) != 1 {
		t.Fatalf("the turn carries %d messages, want 1", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("the message's role is %q, want \"user\"", msgs[0].Role)
	}
	if !strings.Contains(msgs[0].Content, "<SYSTEM_MESSAGE>") {
		t.Errorf("the message does not carry the envelope:\n%s", msgs[0].Content)
	}
	if strings.Contains(msgs[0].Content, "<USER_REQUEST>") {
		t.Errorf("the message carries the user envelope:\n%s", msgs[0].Content)
	}
	// The session keeps the notification as a system message: the
	// reference stores it as its own step type.
	if len(m.session) != 1 || m.session[0].Role != "system" {
		t.Fatalf("the session does not keep the notification as a system message: %+v", m.session)
	}
}

// TestTheNotificationIsStoredAsASystemNotice pins the trajectory: the
// reference stores a system message under its own step type,
// CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101.
func TestTheNotificationIsStoredAsASystemNotice(t *testing.T) {
	m := &runMode{taskInbox: agent.NewTaskInbox()}
	m.taskInbox.Add(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	m.drainTaskNotifications()

	steps := m.trajectorySteps()
	if len(steps) != 1 {
		t.Fatalf("the trajectory holds %d steps, want 1", len(steps))
	}
	if steps[0].Type != convstore.StepSystemNotice {
		t.Errorf("the step's type is %d, want %d", steps[0].Type, convstore.StepSystemNotice)
	}
	if !strings.Contains(steps[0].Text, "<SYSTEM_MESSAGE>") {
		t.Errorf("the step does not carry the envelope:\n%s", steps[0].Text)
	}
}

// TestANotificationStartsATurnWhenTheAgentIsIdle: the reference's agent
// picks the message up and carries on with its work — the clone starts a
// turn of its own rather than waiting for the user to type something.
func TestANotificationStartsATurnWhenTheAgentIsIdle(t *testing.T) {
	m := &runMode{taskInbox: agent.NewTaskInbox()}
	m.taskInbox.Add(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})

	cmd := m.updateTaskNotificationMsg()
	if cmd == nil {
		t.Fatal("an idle agent did not start a turn for the notification")
	}
	if !m.streaming {
		t.Error("the turn was not marked as running")
	}
	if len(m.session) != 1 {
		t.Fatalf("the session holds %d messages, want 1", len(m.session))
	}
}

// TestANotificationWaitsWhileATurnIsRunning: a turn that is already in
// flight is not interrupted; the notification is delivered by the next one.
func TestANotificationWaitsWhileATurnIsRunning(t *testing.T) {
	m := &runMode{taskInbox: agent.NewTaskInbox(), streaming: true}
	m.taskInbox.Add(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})

	if cmd := m.updateTaskNotificationMsg(); cmd != nil {
		t.Error("a running turn was interrupted by the notification")
	}
	if m.taskInbox.Len() != 1 {
		t.Errorf("the inbox holds %d notifications, want 1", m.taskInbox.Len())
	}
}
