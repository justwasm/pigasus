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
