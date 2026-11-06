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
//
// The command output block of background task completion notifications is omitted
// so stdout/stderr does not flood the main transcript panel; the full output is
// preserved in the referenced task log file.
func NotificationRows(n shell.TaskNotification) []string {
	body := stripCommandOutput(n.Body)
	text := body
	if n.Title != "" {
		if text == "" {
			text = n.Title
		} else {
			text = n.Title + "\n\n" + body
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

// stripCommandOutput removes the raw "Command output:\n..." body from a task notification,
// keeping only the status header, exit code, and log reference so the transcript remains clean.
func stripCommandOutput(text string) string {
	idx := strings.Index(text, "Command output:")
	if idx == -1 {
		return text
	}
	prefix := strings.TrimRight(text[:idx], "\n")

	// Look for trailing "Log: file://" line after Command output:
	logIdx := strings.LastIndex(text, "Log: file://")
	if logIdx != -1 && logIdx > idx {
		logLine := strings.TrimSpace(text[logIdx:])
		if prefix == "" {
			return logLine
		}
		return prefix + "\n" + logLine
	}
	return prefix
}
