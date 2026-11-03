// error_row.go — the transcript row for a failed turn.
//
// The row is the reference's own: "Error: " followed by the message
// the turn's classifier produced. The message itself is the host's
// fact (the turn failed with *this*), the row is this package's —
// the same split the step model makes between what a step means and
// how it is painted.
package toolsteps

import "strings"

// ErrorRow renders the transcript row for a failed turn.
//
// The message is trimmed, so a failure whose text carries a trailing
// newline still renders one clean row.
func ErrorRow(message string) string {
	return "Error: " + strings.TrimSpace(message)
}
