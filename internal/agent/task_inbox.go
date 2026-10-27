// Package agent — task_inbox.go: cycle 309, the delivery of a background
// task's notification into the turn loop.
//
// 【一手】 The reference hands a background task's completion notification to
// the model as an agent message, wrapped in the SYSTEM_MESSAGE envelope.
// The reference's own captured notification:
//
//	The following is a <SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.
//
//	<SYSTEM_MESSAGE>
//	[Message] timestamp=2026-10-24T19:15:22Z sender=45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56 priority=MESSAGE_PRIORITY_HIGH content=Task id "45fde9e1-76d5-4b87-bd23-1b2c626bbf26/task-56" finished with result:
//	Command exited with code 0.
//
//	Command output:
//	...
//
//	Log: file:///Users/gear/.gemini/antigravity-cli/brain/45fde9e1-76d5-4b87-bd23-1b2c626bbf26/.system_generated/tasks/task-56.log
//	</SYSTEM_MESSAGE>
//
// The capture lives in internal/tools/testdata/reference_background_task_1.4.7.txt
// (the "completion notification (the SYSTEM_MESSAGE)" section) and is what
// the renderer below is pinned against.
//
// 【反编译】 The 1.4.7 binary holds the three format strings the envelope is
// built from, one hit each:
//
//	"The following is a "
//	"<SYSTEM_MESSAGE> not actually sent by the user. It is provided by the system as important information to pay attention to.\n\n<SYSTEM_MESSAGE>\n%s\n</SYSTEM_MESSAGE>"
//	"[Message] timestamp=%s sender=%s priority=%s content=%s"
//
// The wrapper is the reference's way of telling the model that the message
// is *not* something the user typed — which is why it travels in the
// user-content position of the model's conversation rather than in the
// system prompt.
//
// 【推测】 The reference's message title ("Notification timeout reached" for
// the "still running" message) is a render detail — the reference's own
// AgentMessage carries it in render_details.message_title, and the format
// string above has no slot for it. The clone therefore puts a titled
// notification's title in front of its body, so the model reads the
// reference's own wording; the captured completion notification has no
// title and is unaffected.
package agent

import (
	"fmt"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/agentmessage"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tools"
)

const (
	// systemMessagePreamble is the reference's own lead-in: it tells the
	// model that what follows was not sent by the user.
	systemMessagePreamble = agentmessage.Preamble

	// systemMessageWrapper wraps the rendered message.
	systemMessageWrapper = agentmessage.Wrapper

	// taskMessageFormat is the reference's own rendering of one agent
	// message: the timestamp, the sender, the priority and the content.
	taskMessageFormat = agentmessage.LineFormat
)

// RenderTaskNotification renders the reference's SYSTEM_MESSAGE envelope for
// one task notification.
//
// The result is the text the model reads: the reference's lead-in, the
// SYSTEM_MESSAGE tags, and the rendered message between them.
func RenderTaskNotification(n tools.TaskNotification) string {
	at := n.At
	if at.IsZero() {
		at = time.Now()
	}
	priority := n.Priority
	if priority == "" {
		priority = tools.TaskPriorityHigh
	}
	content := n.Body
	if n.Title != "" {
		if content == "" {
			content = n.Title
		} else {
			content = n.Title + "\n\n" + content
		}
	}
	msg := fmt.Sprintf(taskMessageFormat, at.UTC().Format(time.RFC3339), n.TaskID, priority, content)
	return systemMessagePreamble + "\n\n" + fmt.Sprintf(systemMessageWrapper, msg)
}

// TaskNotificationMessage renders one task notification into the message the
// model's conversation carries.
//
// The role is "system": the notification is not something the user typed,
// and the reference stores it as its own step type (CORTEX_STEP_TYPE
// SYSTEM_MESSAGE = 101, see internal/convstore). The turn's message builder
// turns it into a user-content message on the way to the model — the
// reference's own lead-in says the message travels where a user message
// would.
func TaskNotificationMessage(n tools.TaskNotification) backend.Message {
	return backend.Message{Role: "system", Content: RenderTaskNotification(n)}
}

// TaskInbox is the session-level queue of task notifications that have not
// reached the model yet.
//
// The queue belongs to the session, not to a turn: the TUI builds a fresh
// agent.Loop for every turn (internal/cliapp/tui_stream_prompt.go), so a
// per-turn queue would drop every notification that arrives between turns —
// which is exactly when a background task finishes.
//
// All methods are safe for concurrent use: the notification is fired from
// the task's own goroutine, the drain happens on the UI goroutine.
type TaskInbox struct {
	mu    sync.Mutex
	items []tools.TaskNotification
}

// NewTaskInbox returns an empty inbox.
func NewTaskInbox() *TaskInbox {
	return &TaskInbox{}
}

// Add queues one notification. A notification with neither a task id nor a
// body is dropped: it would render as an empty message.
func (i *TaskInbox) Add(n tools.TaskNotification) {
	if i == nil {
		return
	}
	if n.TaskID == "" && n.Body == "" {
		return
	}
	i.mu.Lock()
	i.items = append(i.items, n)
	i.mu.Unlock()
}

// Len reports how many notifications are waiting.
func (i *TaskInbox) Len() int {
	if i == nil {
		return 0
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	return len(i.items)
}

// Drain removes the queued notifications and returns them in the order they
// arrived. The slice is fresh: the caller may keep it.
func (i *TaskInbox) Drain() []tools.TaskNotification {
	if i == nil {
		return nil
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if len(i.items) == 0 {
		return nil
	}
	out := i.items
	i.items = nil
	return out
}

// DrainMessages removes the queued notifications and returns them as the
// messages the model's conversation carries.
func (i *TaskInbox) DrainMessages() []backend.Message {
	items := i.Drain()
	if len(items) == 0 {
		return nil
	}
	out := make([]backend.Message, 0, len(items))
	for _, item := range items {
		out = append(out, TaskNotificationMessage(item))
	}
	return out
}
