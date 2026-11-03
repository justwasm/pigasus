// notifications.go — the background task's notification rows.
//
// A task notification is a title and a body. The transcript keeps the
// notification's own text — one dim-gray row per line — the way the
// reference's own step blocks keep a summary and hide the detail behind
// ctrl+o.
//
// The shape is the notification's text, line by line, in the dim-gray
// span. The colour is read from the palette when the row is drawn, so the
// rows follow the selected colour scheme.

package rows

import (
	"strings"

	"github.com/justwasm/pigasus/internal/app/tools/shell"
)

// NotificationRows renders a background task's notification as the
// transcript's dim rows.
//
// The title comes first, then the body, separated by a blank line — the
// shape the reference's own notification has. A notification with no text
// at all has no rows.
func NotificationRows(n shell.TaskNotification) []string {
	text := n.Body
	if n.Title != "" {
		if text == "" {
			text = n.Title
		} else {
			text = n.Title + "\n\n" + n.Body
		}
	}
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return nil
	}
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, dimRow(line))
	}
	return out
}
