package tools

import (
	"fmt"
	"sync"
	"time"
)

// The reference's "the command is still running" notification.
//
// 【反编译】 The 1.4.8 binary holds three literals that belong to this
// feature, all in cortex/handlers:
//
//	"Notification timeout reached"                      28 B  vm 0x10358b6d5
//	"Background command is still running after %ds: %s"  49 B  vm 0x10361ddb7
//	"failed to send timeout notification via task: %v"   48 B  vm 0x103618e21
//
// 【反编译】 oneharness/internal/tools/tools.(*RunCommandTool).
// withTimeoutNotificationField (0x101dfa4b0) adds the parameter to the
// tool's schema *after* the
// reflection step, through utils.AugmentSchemaField:
//
//	AugmentSchemaField(schema,
//	    "NotificationTimeoutSeconds", 26,     <- vm 0x1035712d0
//	    "integer",                    7,      <- vm 0x1033f047f
//	    <the 386-byte description>,   0x182)  <- vm 0x1036c2c0d
//
// so the parameter is not part of the reflected struct — it is bolted on.
// The reference adds it only when the tool carries the notification
// capability (a flag byte on the tool and a bool behind one of its
// pointers); the clone has no such gate and always advertises it.
//
// 【反编译】 cortex/handlers.(*RunCommandHandler).runTaskListeners (0x101dc7bf0)
// arms a timer of that many seconds (time_NewTimer(seconds * 1e9)) and, when
// the timer fires while the command is still running, sends the model a
// message titled "Notification timeout reached" whose body is the literal
// above. The reference sends it *via the task* — the message goes into the
// task's own message channel, which is what the agent's pump reads.
//
// 【一手】 The parameter is the proto field
// `CortexStepRunCommand.notification_timeout_seconds = 30` (uint64) in the
// reference's own descriptor (out/local-macos/protos/cortex_1.4.6.proto).
const (
	// notificationTimeoutTitle is the reference's own title for the
	// "the command is still running" message.
	notificationTimeoutTitle = "Notification timeout reached"

	// notificationTimeoutBodyFormat is the reference's own body. The
	// first argument is the timeout in seconds, the second the command.
	notificationTimeoutBodyFormat = "Background command is still running after %ds: %s"
)

// notificationTimeoutBody renders the reference's body text.
//
// 【推测】 The reference's second %s is a string it reads out of the
// handler's frame; the decompile does not say whether it is the command
// line or the task id. The command line is what the model needs — it may
// have several commands running at once — so the clone puts the command
// there.
func notificationTimeoutBody(seconds int, command string) string {
	return fmt.Sprintf(notificationTimeoutBodyFormat, seconds, command)
}

// TaskPriorityHigh is the reference's own name for the priority of a message
// the model has to pay attention to.
//
// 【一手】 The reference's MessagePriority enum spells its members
// MESSAGE_PRIORITY_UNSPECIFIED / _LOW / _NORMAL / _HIGH (the recovered
// .proto, out/local-macos/protos/cortex_1.4.6.proto, "enum MessagePriority"),
// and the completion notification the reference wrote into its own
// transcript carries MESSAGE_PRIORITY_HIGH — the captured text is in
// internal/tools/testdata/reference_background_task_1.4.7.txt.
const TaskPriorityHigh = "MESSAGE_PRIORITY_HIGH"

// TaskNotification is one message about a background task that the runtime
// hands to the model.
//
// The reference sends these into the task's message channel (【反编译】
// runTaskListeners: "failed to send timeout notification via task: %v"), so
// they arrive at the agent as a message rather than as a tool result. The
// clone keeps the same shape: Title is the message's title, Body is its
// text.
type TaskNotification struct {
	// TaskID is the task's id: "<conversation-id>/task-<n>".
	TaskID string

	// Title is the message's title — the reference's own, e.g.
	// "Notification timeout reached".
	Title string

	// Body is the message's text.
	Body string

	// LogPath is the task's log file, when the task has one.
	LogPath string

	// Priority is the priority the reference puts on the message, spelled
	// the way the reference's own enum spells it ("MESSAGE_PRIORITY_HIGH").
	// Empty means TaskPriorityHigh: a task notification is information the
	// model has to pay attention to.
	Priority string

	// At is the moment the notification was created. Zero means "now" — the
	// reference's own message carries the moment it was written.
	At time.Time
}

var (
	taskNotifierMu sync.RWMutex
	taskNotifier   func(TaskNotification)
)

// SetTaskNotifier installs the hook the runtime uses to deliver task
// notifications to the model, and returns the previous one.
//
// The hook is called from the task's own goroutine, so it must be safe for
// concurrent use and must not block for long. Passing nil removes the
// hook: notifications are then dropped, which is what the clone did before
// this cycle.
func SetTaskNotifier(fn func(TaskNotification)) func(TaskNotification) {
	taskNotifierMu.Lock()
	prev := taskNotifier
	taskNotifier = fn
	taskNotifierMu.Unlock()
	return prev
}

// notifyTask hands a notification to the installed hook, if any.
// NotifyTask delivers one task notification to the installed notifier.
//
// The runtime fires the notifications — the completion notification when a
// background task is done, the "the command is still running" one when the
// timeout elapses — and this is the seam a host uses to hand one in from
// outside the package.
func NotifyTask(n TaskNotification) {
	notifyTask(n)
}

func notifyTask(n TaskNotification) {
	taskNotifierMu.RLock()
	fn := taskNotifier
	taskNotifierMu.RUnlock()
	if fn == nil {
		return
	}
	fn(n)
}

// armNotificationTimeout starts the reference's "the command is still
// running" timer for a background task.
//
// 【反编译】 The reference arms the timer in the handler's listener loop,
// after the command has gone to the background: the timer runs for
// NotificationTimeoutSeconds, and when it fires while the command is still
// running, the model is told. A command that finishes first gets no
// notification — the completion notification is the one it gets then.
func armNotificationTimeout(t *bgTask, seconds int) {
	if t == nil || seconds <= 0 {
		return
	}
	armNotificationTimeoutAfter(t, time.Duration(seconds)*time.Second, seconds)
}

// armNotificationTimeoutAfter is armNotificationTimeout with the wait
// spelled as a duration, so a test can drive it in milliseconds. The
// seconds count is what the notification text carries.
func armNotificationTimeoutAfter(t *bgTask, wait time.Duration, seconds int) {
	if t == nil || wait <= 0 {
		return
	}
	timer := time.NewTimer(wait)
	go func() {
		defer timer.Stop()
		select {
		case <-t.finished:
			// The command finished before the timeout: the completion
			// notification is the one that matters.
		case <-timer.C:
			if t.hasCompleted() {
				// The command finished while the timer was running: the
				// completion notification is the one that matters, and a
				// "still running" message would be a lie.
				return
			}
			notifyTask(TaskNotification{
				TaskID:   t.id,
				Title:    notificationTimeoutTitle,
				Body:     notificationTimeoutBody(seconds, t.command),
				LogPath:  t.logPath,
				Priority: TaskPriorityHigh,
				At:       time.Now(),
			})
		}
	}()
}
