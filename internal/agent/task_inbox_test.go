// task_inbox_test.go — cycle 309: the reference's SYSTEM_MESSAGE envelope.
//
// 【一手】 The reference's own captured completion notification is the
// fixture these tests are pinned against. It lives in the tools package,
// next to the code that reads the task's output:
//
//	internal/tools/testdata/reference_background_task_1.4.7.txt
//
// The capture is the reference's own text: the lead-in, the SYSTEM_MESSAGE
// tags and the rendered message between them.
package agent

import (
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/tools"
)

// referenceNotification returns the reference's captured SYSTEM_MESSAGE —
// the completion notification it wrote into its own transcript.
func referenceNotification(t *testing.T) string {
	t.Helper()
	raw, err := os.ReadFile("../tools/testdata/reference_background_task_1.4.7.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	// The capture holds the command's own line endings (the reference ran
	// it under a pty); the fixture's other test normalises them the same
	// way.
	body := strings.ReplaceAll(string(raw), "\r\n", "\n")
	const marker = "---- completion notification (the SYSTEM_MESSAGE) ----\n"
	i := strings.Index(body, marker)
	if i < 0 {
		t.Fatal("the fixture has no completion notification section")
	}
	return strings.TrimRight(body[i+len(marker):], "\n")
}

// firstLine returns the text up to the first newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestTheEnvelopeReproducesTheReferencesNotification renders the
// reference's captured notification from the parts the capture carries —
// its timestamp, its sender, its priority and its content — and pins the
// result to the capture, byte for byte.
func TestTheEnvelopeReproducesTheReferencesNotification(t *testing.T) {
	captured := referenceNotification(t)

	const openTag = "<SYSTEM_MESSAGE>\n"
	i := strings.Index(captured, openTag)
	if i < 0 {
		t.Fatalf("the capture has no %q tag:\n%s", "<SYSTEM_MESSAGE>", captured)
	}
	inner := captured[i+len(openTag):]
	const closeTag = "\n</SYSTEM_MESSAGE>"
	j := strings.LastIndex(inner, closeTag)
	if j < 0 {
		t.Fatalf("the capture has no %q tag:\n%s", "</SYSTEM_MESSAGE>", captured)
	}
	inner = inner[:j]

	fields := regexp.MustCompile(`^\[Message\] timestamp=(\S+) sender=(\S+) priority=(\S+) content=(.*)$`).
		FindStringSubmatch(firstLine(inner))
	if fields == nil {
		t.Fatalf("the captured message does not have the reference's shape:\n%s", inner)
	}
	at, err := time.Parse(time.RFC3339, fields[1])
	if err != nil {
		t.Fatalf("the captured timestamp %q does not parse: %v", fields[1], err)
	}
	// The content runs over several lines; the first one ends the
	// [Message] line.
	content := fields[4]
	if _, rest, found := strings.Cut(inner, "\n"); found {
		content += "\n" + rest
	}

	got := RenderTaskNotification(tools.TaskNotification{
		TaskID:   fields[2],
		Priority: fields[3],
		Body:     content,
		At:       at,
	})
	if got != captured {
		t.Errorf("the envelope is\n%q\nwant\n%q", got, captured)
	}
}

// TestTheEnvelopeCarriesTheReferencesLeadIn pins the two halves of the
// reference's wrapper: the lead-in that tells the model the message was
// not sent by the user, and the tags around the message.
func TestTheEnvelopeCarriesTheReferencesLeadIn(t *testing.T) {
	got := RenderTaskNotification(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	const lead = "The following is a <SYSTEM_MESSAGE> not actually sent by the user. " +
		"It is provided by the system as important information to pay attention to."
	if !strings.HasPrefix(got, lead+"\n\n<SYSTEM_MESSAGE>\n") {
		t.Errorf("the envelope does not open with the reference's lead-in:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n</SYSTEM_MESSAGE>") {
		t.Errorf("the envelope does not close with the reference's tag:\n%s", got)
	}
	if !strings.Contains(got, "sender=conv-1/task-1") {
		t.Errorf("the envelope does not name the sender:\n%s", got)
	}
	if !strings.Contains(got, "priority="+tools.TaskPriorityHigh) {
		t.Errorf("the envelope does not carry the priority:\n%s", got)
	}
}

// TestTheNotificationIsASystemMessage pins the role the notification
// carries in the session: it is not something the user typed, and the
// reference stores it as its own step type (CORTEX_STEP_TYPE_SYSTEM_MESSAGE
// = 101).
func TestTheNotificationIsASystemMessage(t *testing.T) {
	msg := TaskNotificationMessage(tools.TaskNotification{
		TaskID: "conv-1/task-1",
		Body:   "Task id \"conv-1/task-1\" finished with result:",
	})
	if msg.Role != "system" {
		t.Errorf("the notification's role is %q, want \"system\"", msg.Role)
	}
	if !strings.Contains(msg.Content, "<SYSTEM_MESSAGE>") {
		t.Errorf("the message does not carry the envelope:\n%s", msg.Content)
	}
}

// TestTheInboxHoldsTheNotificationsUntilTheTurnAsksForThem pins the
// queue: the notification arrives from the task's own goroutine and is
// delivered by the next turn, so it has to survive in between.
func TestTheInboxHoldsTheNotificationsUntilTheTurnAsksForThem(t *testing.T) {
	inbox := NewTaskInbox()
	if inbox.Len() != 0 {
		t.Fatalf("a new inbox holds %d notifications", inbox.Len())
	}
	inbox.Add(tools.TaskNotification{TaskID: "conv-1/task-1", Body: "one"})
	inbox.Add(tools.TaskNotification{TaskID: "conv-1/task-2", Body: "two"})
	if inbox.Len() != 2 {
		t.Fatalf("the inbox holds %d notifications, want 2", inbox.Len())
	}

	msgs := inbox.DrainMessages()
	if len(msgs) != 2 {
		t.Fatalf("the drain returned %d messages, want 2", len(msgs))
	}
	if !strings.Contains(msgs[0].Content, "content=one") {
		t.Errorf("the first message does not carry its notification:\n%s", msgs[0].Content)
	}
	if !strings.Contains(msgs[1].Content, "content=two") {
		t.Errorf("the second message does not carry its notification:\n%s", msgs[1].Content)
	}
	if inbox.Len() != 0 {
		t.Errorf("the inbox still holds %d notifications after the drain", inbox.Len())
	}
	if again := inbox.DrainMessages(); len(again) != 0 {
		t.Errorf("a second drain returned %d messages", len(again))
	}
}

// TestTheInboxDropsAnEmptyNotification: a notification with neither a task
// id nor a body would render as an empty message.
func TestTheInboxDropsAnEmptyNotification(t *testing.T) {
	inbox := NewTaskInbox()
	inbox.Add(tools.TaskNotification{})
	if inbox.Len() != 0 {
		t.Errorf("the inbox holds %d empty notifications", inbox.Len())
	}
}
