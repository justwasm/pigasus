// notifications_test.go — the background task's notification rows.
//
// The rows are pinned by their exact byte sequence: the notification's
// text is one dim-gray row per line, and the blank line between the title
// and the body is a row of its own.

package rows

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

// TestRows_NotificationRows pins the notification's rows: the title, the
// body, and the blank line between them.
func TestRows_NotificationRows(t *testing.T) {
	want := []string{
		goldenDimGrayOpen + "Notification timeout reached" + goldenClose39,
		goldenDimGrayOpen + "" + goldenClose39,
		goldenDimGrayOpen + "the command ran for too long" + goldenClose39,
	}
	got := NotificationRows(shell.TaskNotification{
		Title: "Notification timeout reached",
		Body:  "the command ran for too long",
	})
	if len(got) != len(want) {
		t.Fatalf("NotificationRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_NotificationRowsTitleOnly pins the notification with a title
// and no body: the title is the row.
func TestRows_NotificationRowsTitleOnly(t *testing.T) {
	want := []string{
		goldenDimGrayOpen + "Notification timeout reached" + goldenClose39,
	}
	got := NotificationRows(shell.TaskNotification{Title: "Notification timeout reached"})
	if len(got) != len(want) {
		t.Fatalf("NotificationRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_NotificationRowsBodyOnly pins the notification with a body and
// no title: the body's own lines are the rows, and the trailing newline
// is not one of them.
func TestRows_NotificationRowsBodyOnly(t *testing.T) {
	want := []string{
		goldenDimGrayOpen + "body only" + goldenClose39,
		goldenDimGrayOpen + "second line" + goldenClose39,
	}
	got := NotificationRows(shell.TaskNotification{Body: "body only\nsecond line\n"})
	if len(got) != len(want) {
		t.Fatalf("NotificationRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_NotificationRowsEmpty pins that a notification with no text at
// all has no rows.
func TestRows_NotificationRowsEmpty(t *testing.T) {
	if got := NotificationRows(shell.TaskNotification{}); len(got) != 0 {
		t.Errorf("NotificationRows = %v, want none", got)
	}
	if got := NotificationRows(shell.TaskNotification{Body: "\n\n"}); len(got) != 0 {
		t.Errorf("NotificationRows = %v, want none", got)
	}
}

func TestRows_NotificationRows_StripsCommandOutput(t *testing.T) {
	body := "Task id \"schedule-37049/task-23\" finished with result:\n" +
		"Command exited with code 0.\n\n" +
		"Command output:\n" +
		"=== RUN   TestSetupCLIParser_SubcommandsRegistered\n" +
		"--- PASS: TestSetupCLIParser_SubcommandsRegistered (0.00s)\n" +
		"PASS\n" +
		"ok      github.com/justwasm/pigasus/internal/clirunner    1.413s\n\n" +
		"Log: file:///path/to/task-23.log\n"

	got := NotificationRows(shell.TaskNotification{Body: body})
	want := []string{
		goldenDimGrayOpen + "Task id \"schedule-37049/task-23\" finished with result:" + goldenClose39,
		goldenDimGrayOpen + "Command exited with code 0." + goldenClose39,
		goldenDimGrayOpen + "Log: file:///path/to/task-23.log" + goldenClose39,
	}
	if len(got) != len(want) {
		t.Fatalf("NotificationRows = %d rows, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}
