// tui_task_notify_test.go — cycle 309: the delivery of a background task's
// notification into the turn loop.
//
// The reference tells the model that a background task is over: the
// notification is an agent message, and the agent picks it up and carries
// on. These tests pin the clone's side of that: the notification joins the
// session, reaches the model in the user-content position, and is stored
// as the reference's own system-message step.
package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
)

// TestTheNotifierPutsTheNotificationInTheInbox pins the seam between the
// tools layer and the session: the runtime fires the notification, the
// session's inbox holds it until the next turn.
func TestTheNotifierPutsTheNotificationInTheInbox(t *testing.T) {
	prev := shell.SetTaskNotifier(nil)
	defer shell.SetTaskNotifier(prev)

	m := &runMode{engine: engineState{taskCtrl: taskregistry.NewController(nil)}}
	m.installTaskNotifier()

	shell.NotifyTask(shell.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	if m.engine.taskCtrl.Inbox().Len() != 1 {
		t.Fatalf("the inbox holds %d notifications, want 1", m.engine.taskCtrl.Inbox().Len())
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
	m := &runMode{engine: engineState{taskCtrl: taskregistry.NewController(nil)}}
	m.engine.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
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
	if len(m.engine.session) != 1 || m.engine.session[0].Role != "system" {
		t.Fatalf("the session does not keep the notification as a system message: %+v", m.engine.session)
	}
}

// TestTheNotificationIsStoredAsASystemNotice pins the trajectory: the
// reference stores a system message under its own step type,
// CORTEX_STEP_TYPE_SYSTEM_MESSAGE = 101.
func TestTheNotificationIsStoredAsASystemNotice(t *testing.T) {
	m := &runMode{engine: engineState{taskInbox: agentturn.NewTaskInbox()}}
	m.engine.taskInbox.Add(shell.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	m.drainTaskNotifications()

	steps := convstore.BuildSteps(m.trajectoryFacts())
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
	m := &runMode{engine: engineState{taskCtrl: taskregistry.NewController(nil)}}
	m.engine.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})

	cmd := m.updateTaskNotificationMsg()
	if cmd == nil {
		t.Fatal("an idle agent did not start a turn for the notification")
	}
	if !m.engine.streaming {
		t.Error("the turn was not marked as running")
	}
	if len(m.engine.session) != 1 {
		t.Fatalf("the session holds %d messages, want 1", len(m.engine.session))
	}
}

// TestANotificationWaitsWhileATurnIsRunning: a turn that is already in
// flight is not interrupted; the notification is delivered by the next one.
func TestANotificationWaitsWhileATurnIsRunning(t *testing.T) {
	m := &runMode{engine: engineState{taskCtrl: taskregistry.NewController(nil), streaming: true}}
	m.engine.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})

	if cmd := m.updateTaskNotificationMsg(); cmd != nil {
		t.Error("a running turn was interrupted by the notification")
	}
	if m.engine.taskCtrl.Inbox().Len() != 1 {
		t.Errorf("the inbox holds %d notifications, want 1", m.engine.taskCtrl.Inbox().Len())
	}
}

// TestNotificationDeliveredBeforeUserTurn verifies that task notifications
// are delivered to the session before the user's turn prompt, ensuring the
// model treats the user prompt as the active instruction.
func TestNotificationDeliveredBeforeUserTurn(t *testing.T) {
	m := newSlashModel(t)
	if m.engine.taskCtrl == nil {
		m.engine.taskCtrl = taskregistry.NewController(nil)
	}
	m.engine.taskCtrl.Inbox().Add(taskregistry.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:\nok",
	})

	m.startModelTurn("what is the current status?")

	if len(m.engine.session) != 2 {
		t.Fatalf("expected 2 session messages (system notification then user prompt), got %d", len(m.engine.session))
	}
	if m.engine.session[0].Role != "system" || !strings.Contains(m.engine.session[0].Content, "conv-1/task-1") {
		t.Errorf("expected session[0] to be task notification system message, got: %+v", m.engine.session[0])
	}
	if m.engine.session[1].Role != "user" || !strings.Contains(m.engine.session[1].Content, "what is the current status?") {
		t.Errorf("expected session[1] to be user prompt, got: %+v", m.engine.session[1])
	}
}

// TestUnappliedSteerPromotedToQueue verifies that mid-stream prompts that were
// buffered as steer instructions but unapplied by turn completion are promoted
// to StreamQueue and executed in the next turn instead of being lost.
func TestUnappliedSteerPromotedToQueue(t *testing.T) {
	m := newSlashModel(t)
	m.session.store.SendingMessages = []string{"unapplied user instruction"}

	m.updateTurnDoneMsg(toolsteps.TurnDoneMsg{Reply: "finished step"})

	if len(m.session.store.SendingMessages) != 0 {
		t.Errorf("expected SendingMessages to be cleared, got: %v", m.session.store.SendingMessages)
	}
	if len(m.session.store.StreamQueue) != 0 {
		t.Errorf("expected StreamQueue to be dequeued for next turn, got: %v", m.session.store.StreamQueue)
	}
	if !m.engine.streaming {
		t.Errorf("expected next turn to start streaming for the unapplied instruction")
	}
	if len(m.engine.session) == 0 || m.engine.session[len(m.engine.session)-1].Content != "unapplied user instruction" {
		t.Errorf("expected unapplied instruction to be active turn prompt, got session: %+v", m.engine.session)
	}
}
