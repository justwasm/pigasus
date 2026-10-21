// Package piagentbridge — recovery.go: the user-facing copy and
// renderers for the stall-recovery prompt.
//
// AGY 1.3.6 changelog line 5: "a recovery prompt is shown offering to
// retry once or continue". The clone surfaces the same prompt in two
// places (TUI and print mode) so the prompt body is one canonical
// string and each mode wraps it with its own chrome.
//
// The TUI renderer returns plain text the TUI styler (lipgloss) will
// apply itself; the print renderer returns a plain-text banner with
// enough spacing that a tail-of-stdout user can read it.
//
// The functions are pure: no state, no globals, no I/O. Tests can pin
// every byte of the output without races.
package piagentbridge

import (
	"fmt"
	"strings"
	"time"
)

// RecoveryPromptBody is the canonical copy the TUI and print renderers
// build on. It is exported so cmd/wagyu tests can pin it and so the
// localisation track (cycle 217+) finds a single source.
const RecoveryPromptBody = "The request appears stuck. Retry once / Continue waiting / Cancel"

// RecoveryPromptTUIRow is the line the TUI inserts into the chat view.
// lipgloss wraps it before drawing; the row is one visual line so a
// user pasting the prompt into a bug report sees the same shape.
func RecoveryPromptTUIRow() string {
	return "⚠  " + RecoveryPromptBody
}

// RecoveryPromptPrintBlock is the multi-line block the print path
// writes to stderr. It frames the prompt with the stall timeout the
// watcher waited before cancelling, so a tail of `-p` output tells the
// user why the line is there.
//
//	fmt.Fprintln(os.Stderr, piagentbridge.RecoveryPromptPrintBlock(
//	    piagentbridge.StallTimeout,
//	    "30 seconds between attempts"))
func RecoveryPromptPrintBlock(timeout time.Duration, backoffHint string) string {
	var builder strings.Builder
	builder.WriteString("[stall-recovery] ")
	builder.WriteString(RecoveryPromptBody)
	builder.WriteString("\n[stall-recovery] idle window: ")
	builder.WriteString(timeout.String())
	builder.WriteString("; retry backoff: ")
	builder.WriteString(backoffHint)
	builder.WriteString(" (capped from 4 minutes)")
	return builder.String()
}

// RecoveryPromptPrint is the simple form: only the canonical body,
// no framing. Used by the print path when it just wants the prompt
// without the surrounding context.
func RecoveryPromptPrint() string {
	return RecoveryPromptBody
}

// FormatStallElapsed is a tiny helper that pins the "stalled after X"
// copy. TUI uses it as the chat footer; print uses it on the
// recovery line.
func FormatStallElapsed(elapsed time.Duration) string {
	return fmt.Sprintf("[stalled after %s — request cancelled, recovery prompt above]", elapsed.String())
}
