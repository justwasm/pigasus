// login_prompt.go renders the OAuth 2.0 device-flow prompt (verification
// URL + one-time user code) through internal/mdrender so the user sees
// a properly formatted Markdown block instead of fmt.Fprintf lines.
//
// Cycle 171. The Markdown renderer added in cycle 168 was wired into
// the package surface but had no caller yet — the OAuth device flow
// prompt is the natural first one because the reference binary itself
// shows the URL and code in a structured panel rather than as a
// sequence of `--key=value` lines, and our prior runLogin output looked
// visibly worse than the rebuilt one.
//
// Future cycles can route other prompt-shaped surfaces through the
// same helper (e.g. the TUI /login modal in cycle 173+).
package cli

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// loginPromptMarkdown is the Markdown source fed to the renderer. Kept
// as a small helper so tests can pin its shape without re-running the
// renderer.
func loginPromptMarkdown(verificationURL, userCode string) string {
	return fmt.Sprintf(`# Sign in to Pigasus CLI

To authenticate, open this URL in **any** browser:

  %s

Then enter this one-time code:

`+"```"+`
%s
`+"```"+`

Waiting for you to approve…
`, verificationURL, userCode)
}

// RenderLoginPrompt returns the rendered Markdown source for the OAuth
// device-flow prompt. Plain text is used here (not the ANSI variant) so
// the output is readable both on a TTY and when redirected to a file
// or a pipe; the structural cues (`# heading`, fenced code block, the
// bold on "any") survive in either case.
//
// Cycle glamour-migration: routes through mdrender.Quiet (glamour
// v2.0.1 in plain-text mode with no colour) instead of the cycle 171
// mdrender.Render walker. The Quiet variant is the AGY 1.4.2
// glamour-based markdown rendering, kept colour-free so non-TTY
// consumers (CI logs, redirected captures) read identically.
//
// Cycle 268: the wrapper now asks the mdrender package for a
// non-zero terminal width via mdrender.CurrentWidth() (which reads
// $COLUMNS / x/term) so the prompt renders at the actual terminal
// width rather than glamour's hard default of 80 columns. width=0
// is left as the safety fallback path (the prompt is short — the
// change is invisible in normal use, but it keeps the API
// consistent with cycle 268's contract for every other mdrender
// caller).
func RenderLoginPrompt(verificationURL, userCode string) (string, error) {
	width := mdrender.CurrentWidth()
	return mdrender.Quiet(loginPromptMarkdown(verificationURL, userCode), width)
}
