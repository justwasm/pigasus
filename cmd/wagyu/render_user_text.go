// render_user_text.go — render user-supplied text as Markdown for the
// TUI transcript.
//
// Cycle 174. The /learn and /goal slash commands echo the user's
// typed text back into the transcript. Before cycle 174 the echo was
// a plain `+` of strings; a user typing
//
//	/learn **Important**: the API endpoint is *down*
//
// would see the literal markdown markers in the transcript. After
// cycle 174 the same echo goes through internal/mdrender, so the
// transcript shows bold and italic emphasised rather than the raw
// `**` / `*` markers — matching what the reference binary does for
// its model-reply rendering (cycle 155's renderMarkdown path) and
// bringing user-typed text in line with that treatment.
//
// We use mdrender.RenderANSI rather than mdrender.Render so the TUI
// transcript picks up the bold/italic escapes (the TTY honours them);
// the package's plain-text Render would lose the visual cue.
//
// Failure mode: if goldmark errors (which only happens for a true
// parser-level problem; the lib is permissive for normal input),
// renderUserText falls back to s unchanged so the echo never
// disappears. The user always sees *something*.
package main

import (
	"strings"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// renderUserText returns the Markdown-rendered form of s suitable for
// appending to m.response. The trailing newline mdrender appends is
// stripped because the TUI transcript is a line-oriented []string and
// a phantom empty row would shift all subsequent lines down.
//
// Plain text (no markdown markers) round-trips identically through
// goldmark — confirmed by the cycle 174 unit tests — so callers do
// NOT need to special-case "the user typed plain text".
//
// Cycle glamour-migration: routes through mdrender.User (glamour
// v2.0.1 with WithPreservedNewLines) instead of the cycle 174
// mdrender.RenderANSI walker. The new path uses AGY 1.4.2's
// glamour-based markdown rendering, the same renderer the model-
// reply path uses, so user-typed echoes visually mirror model
// replies. The legacy walker remains in place as the glamour
// fallback path on any parser error.
//
// Cycle 268: the helper now takes a width argument so the TUI can
// plumb m.width through to glamour; before this, every user-typed
// markdown echo capped word-wrap at glamour's hard default of 80
// columns regardless of the terminal width (the same cycle-228
// cap that renderMarkdown's chat-transcript path hit).
func renderUserText(s string, width int) string {
	if s == "" {
		return s
	}
	pre := mdrender.PreprocessFileLinks(s)
	// Cycle 268: same fallback rule as renderMarkdown. Width <= 0
	// means "no caller-provided width — let the package resolve
	// via $COLUMNS / x/term so a test that never wired m.width
	// still produces something readable."
	if width <= 0 {
		width = mdrender.CurrentWidth()
	}
	rendered, err := mdrender.User(pre, width)
	if err != nil || rendered == "" {
		return s
	}
	return strings.TrimRight(rendered, "\n")
}
