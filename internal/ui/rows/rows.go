// rows.go — the transcript's own notice rows and its queued rows.
//
// The host used to spell these rows out at every call site, which meant
// the same bytes lived in six places for the compact shape alone. This
// package owns them: the "⎿" (U+23BF) notice rows the conversation
// buffer carries, and the "▸" (U+25B8) rows the queued messages and
// the queued shell commands are listed with.
//
// Three notice shapes exist and all three are the reference's own:
//
//   - the compact shape, "⎿  " at the left margin with the muted body
//     (the compaction rows, the compact-failure row);
//   - the indented shape, "  ⎿  " with the muted body (the slash
//     command's echo row);
//   - the dim shape, "  ⎿  " with the dim-gray body (the keybinding
//     echo rows).
//
// They are named by their shape and not by their purpose: the very same
// bytes serve different callers (the remote-control card's echo row and
// the slash command's echo row are the same indented shape), so a name
// that promised one purpose would be wrong for the other.
//
// Two more shapes live here: the attachment indicator above the composer
// (attachments.go) and the background task's notification rows
// (notifications.go). The tab-separated content block's own column
// layout lives here as well (tabaligned.go).
//
// The colours are read from the palette when a row is drawn, so they
// follow the selected colour scheme. The muted body's closing span has
// no palette export of its own, so it is declared here — the same
// literal the ui-tier panels declare (see
// internal/ui/views/providerpanel/panel_style.go).

package rows

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/palette"
)

// The notice row's own spans.
const (
	// noticeGlyph is the reference's "⎿" bullet (U+23BF).
	noticeGlyph = "⎿"

	// subtleClose ends the muted body's span. The palette has no
	// "SubtleClose", so the default-foreground reset is declared here.
	subtleClose = "\x1b[39m"
)

// NoticeCompact renders the compact notice row: the bold-blue "⎿  "
// bullet at the left margin, with the muted body.
func NoticeCompact(text string) string {
	return palette.WarningBlueOpen + noticeGlyph + "  " + palette.WarningBlueClose +
		palette.SubtleOpen + text + subtleClose
}

// CompactingLine renders the in-progress compaction row: the Braille
// spinner, the phrase, the elapsed clock and the cancel hint.
//
//	⣾  Compacting conversation… (10s · esc to cancel)
//
// It is the *only* place a running compaction is drawn: the reference puts
// this row where "Generating…" and "Working…" appear, inside the
// conversation, and puts nothing in the status row. A compaction shown in
// both places would read as two compactions.
//
// wantsTokens is the "before → after" pair the reference prints once the
// summary has reported its sizes; it is empty while the summary is still
// running, because there is no "after" to name yet.
func CompactingLine(frame string, elapsed time.Duration, cancellable bool) string {
	text := "Compacting conversation\u2026 (" + compactElapsed(elapsed)
	if cancellable {
		text += " \u00b7 esc to cancel"
	}
	text += ")"
	return compactSpinnerSpan(frame) + palette.SubtleOpen + text + subtleClose
}

// compactElapsed renders the compaction clock the way the reference's own
// labels do: whole seconds, and minutes past a minute.
func compactElapsed(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	secs := int(d.Seconds())
	if secs < 60 {
		return fmt.Sprintf("%ds", secs)
	}
	return fmt.Sprintf("%dm %02ds", secs/60, secs%60)
}

// compactSpinnerSpan renders the spinner glyph in the row's own dim grey,
// the way the status row draws it.
func compactSpinnerSpan(frame string) string {
	if frame == "" {
		frame = "\u28be"
	}
	return palette.DimGrayOpen + frame + "  " + palette.DimGrayClose
}

// NoticeIndented renders the indented notice row: the bold-blue
// "  ⎿  " bullet, with the muted body.
func NoticeIndented(text string) string {
	return palette.WarningBlueOpen + "  " + noticeGlyph + "  " + palette.WarningBlueClose +
		palette.SubtleOpen + text + subtleClose
}

// NoticeDim renders the indented notice row whose body is dim gray: the
// bold-blue "  ⎿  " bullet, with the dim-gray body.
func NoticeDim(text string) string {
	return palette.WarningBlueOpen + "  " + noticeGlyph + "  " + palette.WarningBlueClose +
		palette.DimGrayOpen + text + palette.DimGrayClose
}

// QueuedPromptRow renders a queued prompt: "▸ <prompt>". When withHint is true,
// QueuedPromptRow renders a queued prompt: "▸ <prompt> (alt+m to send
// immediately)". The suffix names the gesture that flushes the queue,
// and it sits inside the dim span.
func QueuedPromptRow(prompt string) string {
	return queuedRow(prompt) + " (alt+m to send immediately)" + palette.DimGrayClose
}

// QueuedPromptRowWithHint renders a queued prompt with or without the gesture hint.
// When prompt contains newlines, it truncates to the first line with "..."
// to match the live reference TUI rendering.
func QueuedPromptRowWithHint(prompt string, withHint bool) string {
	head := prompt
	if idx := strings.Index(prompt, "\n"); idx >= 0 {
		head = prompt[:idx] + "..."
	}
	if withHint {
		return queuedRow(head) + " (alt+m to send immediately)" + palette.DimGrayClose
	}
	return queuedRow(head) + palette.DimGrayClose
}

// SendingMessageRow renders an in-flight steer prompt waiting for the active step to finish:
// "▸ <prompt> (Sending message...)".
func SendingMessageRow(prompt string) string {
	return queuedRow(prompt) + " (Sending message...)" + palette.DimGrayClose
}

// QueuedCommandRow renders a queued shell command: "▸ <command>". A
// queued command carries no gesture suffix.
func QueuedCommandRow(command string) string {
	return queuedRow(command) + palette.DimGrayClose
}

// queuedRow is the "▸ <text>" head both queued rows are built on.
func queuedRow(text string) string {
	return palette.DimGrayOpen + "▸ " + text
}
