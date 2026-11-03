package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
)

// tui_task_notify.go — cycle 309: the delivery of a background task's
// notification into the turn loop.
//
// The reference tells the model that a background task is over: the
// notification is an agent message, and the agent picks it up and carries
// on with its work. The clone's seam is the same, in three pieces:
//
//   - the tools layer fires the notification when the command is gone
//     (internal/tools/support.go);
//   - the inbox holds it until the next turn (internal/agent/task_inbox.go);
//   - the TUI drains the inbox into the session and starts a turn of its
//     own when the agent is idle.
//
// The inbox lives on the run mode, not on the agent.Loop: the TUI builds a
// fresh Loop for every turn, so a per-loop queue would drop every
// notification that arrives between turns — which is when a background task
// finishes.
//
// The transcript's own rows for a notification are the ui tier's
// (internal/ui/rows.NotificationRows); this file owns the delivery, not
// the shape of the rows that show it.

// taskNotificationMsg carries the fact that a task notification is waiting.
//
// The payload is not in the message: the notification is in the inbox, and
// the handler drains it. One message per notification would race the
// inbox's own ordering.
type taskNotificationMsg struct{}

// installTaskNotifier wires the tools layer's notification hook to the
// session's inbox.
//
// The hook is process-wide (internal/shell.SetTaskNotifier), so the last
// run mode to install it owns the notifications. That is the TUI's case:
// there is one run mode per process.
func (m *runMode) installTaskNotifier() {
	if m.taskInbox == nil {
		m.taskInbox = agent.NewTaskInbox()
	}
	shell.SetTaskNotifier(func(n shell.TaskNotification) {
		m.taskInbox.Add(n)
		if m.prog != nil {
			m.prog.Send(taskNotificationMsg{})
		}
	})
}

// updateTaskNotificationMsg handles a task notification that arrived.
//
// The notification is drained into the session by the next turn's message
// builder. When the agent is idle, the TUI starts a turn right away: the
// reference's agent picks the message up and carries on with its work
// without waiting for the user to type something.
func (m *runMode) updateTaskNotificationMsg() tea.Cmd {
	if m.taskInbox == nil || m.taskInbox.Len() == 0 {
		return nil
	}
	if m.streaming {
		// A turn is in flight: the notification waits for the next one.
		// The turn's end starts a turn of its own for it.
		return nil
	}
	return m.startNotificationTurn()
}

// startNotificationTurn starts a turn that hands the pending task
// notifications to the model.
func (m *runMode) startNotificationTurn() tea.Cmd {
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.streaming = true
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(""), tickCmd)
}

// takeTaskNotifications removes the pending notifications from the inbox and
// returns them, oldest first.
func (m *runMode) takeTaskNotifications() []shell.TaskNotification {
	if m.taskInbox == nil {
		return nil
	}
	return m.taskInbox.Drain()
}

// drainTaskNotifications moves the pending notifications into the session
// and renders them in the transcript.
//
// The rows go in before the turn's live block: the drain happens while the
// turn is being set up, so the notification belongs to the conversation
// before the model's answer to it.
func (m *runMode) drainTaskNotifications() {
	items := m.takeTaskNotifications()
	if len(items) == 0 {
		return
	}
	rows := 0
	for _, item := range items {
		m.session = append(m.session, agent.TaskNotificationMessage(item))
		lines := uirows.NotificationRows(item)
		m.response = append(m.response, lines...)
		rows += len(lines)
	}
	// The turn's live block starts after the notification's rows.
	if m.liveStart >= 0 && m.liveStart+rows <= len(m.response) {
		m.liveStart += rows
	}
}
