package announce

// notice.go — the inline eligibility notice and its row.
//
// Changelog 1.1.3 (internal/cli/testdata/changelog_reference.txt:686):
//
//	"Improved eligibility error handling by showing errors with a
//	 verification URL inline in the input loop instead of stacking
//	 them above the screen."
//
// 【一手】 the changelog line above is the reference's own wording.
//
// Root cause (cycle 262): the v1.3.x binary surfaces
// `(*AuthStatus).EligibilityError` / `backend.IneligibilityFromResult`
// errors that carry a Reason + VerificationUrl (mirrored on the
// upstream proto's `IneligibleTier.ValidationErrorMessage` /
// `ValidationUrl` getters and on `VerificationRequired.GetVerificationUrl`).
// Before cycle 262 those errors landed in the TUI the same way any
// other chunk error did: nothing — the chunk handler in streamPrompt
// silently dropped `chunk.Error != nil` (`tui_run.go:4058`). On a
// long-running interactive session the user never learned the
// account / tier was ineligible; on a session that DID bubble the
// error out, the message + URL stacked above the input loop and
// polluted the conversation transcript (m.response / m.session).
//
// Cycle 262 fix:
//
//   - eligibility.Error (internal/features/auth/eligibility) is the
//     typed shape an upstream backend / agent loop wraps the
//     eligibility payload in. Reason +
//     VerificationURL are both optional; a Reason-less eligibility
//     error falls back to "your account is not eligible for this
//     request" so the inline row always renders something the user
//     can act on. The Unwrap method exposes Cause so errors.As walks
//     back to the original *apierror.HTTPError.
//
//   - (m *runMode).eligibilityNotice (cmd/wagyu/tui_run.go, added by
//     this cycle) holds the active inline notice. The chunk handler
//     in streamPrompt posts an eligibilityNoticeMsg to the program
//     when chunk.Error carries an *EligibilityError; Update() routes
//     the message into setEligibilityNotice, which writes the row
//     AND emits a one-line diagnostic to stderr so an operator
//     tailing the file can see why the inline row is showing. The
//     inline row is rendered by viewBody (just below where
//     announcement card goes — both slots stack between the top
//     separator and the prompt input).
//
//   - User keystrokes clear the inline notice. The tea.KeyMsg
//     handler in Update() calls clearEligibilityNotice once per
//     non-modifier keystroke; the resubmit path also clears it.
//     The "user starts typing" signal that cycle 262's spec calls
//     out is satisfied by the keystroke branch — once any printable
//     input arrives, the inline row goes away and the user's prompt
//     is unobstructed.
//
//   - Print mode (`-p`) keeps the existing AGY_ERROR JSON envelope
//     path on stderr (main.go's classifyAPIError chain). It has no
//     input loop to render an inline row into, so the cycle does
//     not introduce a new render surface there. The envelope for
//     an *EligibilityError is the same shape api_error.go emits for
//     any other model API failure — short_error / status /
//     error_code / code_kind / retryable / error_id — with the
//     status normalised to "ELIGIBILITY_DENIED" so an automation
//     script parsing AGY_ERROR can branch on it. cycle 262's spec
//     "prints the error to stderr in the same AGY_ERROR JSON shape
//     but does NOT add an inline notice" is the print-mode contract.
//
// Hard constraints this cycle ships:
//
//   - No third-party dependencies added. The file imports only
//     errors, fmt, os, strings, github.com/justwasm/pigasus/internal/backend,
//     github.com/justwasm/pigasus/render. The render
//     import is the cycle-240 OSC 8 hyperlink encoder — same import
//     slash_skills.go and others already use, so this cycle does not
//     extend the dependency graph.
//   - No internal/convstore/sqlite.go edit. The cycle touches only
//     cmd/wagyu/elig_inline.go (new), cmd/wagyu/elig_inline_test.go
//     (new), cmd/wagyu/tui_run.go (a field on runMode, one Update()
//     case, one KeyMsg branch, one viewBody() render call, one
//     streamPrompt() chunk-handler branch), cmd/wagyu/api_error.go
//     (a single new classifyEligibilityError function), cmd/wagyu/main.go
//     (a one-line call to classifyEligibilityError in the print path),
//     and internal/cli/testdata/feature_diff_table.json (one row +
//     clone_version bump).
//   - Tabs, lowercase error strings. gofmt -l stays clean for the
//     cycle files; the existing 50 gofmt-flagged files under internal/
//     are untouched.
//   - The inline notice MUST NOT be appended to m.response or
//     m.session. The viewBody render writes the row directly into
//     the screen buffer; nothing in the chunk / message path
//     mutates the transcript slices.
//
// Cycle 381 (tranche 53): the notice's state and its row moved here
// from internal/cliapp/elig_inline.go and
// internal/features/auth/eligibility. The eligibility *facts* — the
// error type, the Reason, the VerificationUrl, the tier gates — stay
// in internal/features/auth/eligibility (tier 2); the row is a UI row
// and a UI row does not belong in a features package.

import (
	"strings"

	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/render"
)

// Notice is the inline eligibility notice the TUI renders between the
// top separator and the prompt input.
//
// The value carries the upstream's Reason (the user-facing message)
// and the optional VerificationURL (the link the user opens to fix
// the account). Both are optional: a Reason-less notice renders no
// row at all.
type Notice struct {
	Reason          string
	VerificationURL string
}

// RenderNoticeRow renders the single styled line placed between the top separator
// and the prompt input:
//
//	ⓘ <Reason> <render.Hyperlink("Verify here", URL)>
//
// Layout: a single styled line placed between the top separator
// and the prompt input, exactly where the announcement card goes.
//
// The "ⓘ" glyph + dim gray framing matches the announcement
// card's visual language so the inline notice reads as a
// transient, low-priority row rather than an error banner.
// The verification URL is rendered through render.Hyperlink so
// the cycle-240 OSC 8 ';' percent encoding applies (URLs that
// carry query strings ending in `;something` are still rendered
// correctly), and the cycle-213 / cycle-240 fallback ("label
// (url)" plain text on terminals that don't implement OSC 8) is
// inherited for free.
//
// maxWidth is 0 (no clamp) — the inline row is short (Reason is
// one line + a one-line hyperlink) and cycle 262's tests pin
// that the row stays single-line under any terminal width. A
// future cycle that needs to wrap long Reasons can add a width
// clamp here without changing the call site.
func RenderNoticeRow(reason, verificationURL string, caps render.HyperlinkCaps) string {
	if reason == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(palette.DimGrayOpen)
	b.WriteString("  ⓘ ")
	b.WriteString(palette.WarningBodyOpen)
	b.WriteString(reason)
	if verificationURL != "" {
		b.WriteString(" ")
		b.WriteString(render.Hyperlink(verificationURL, "Verify here", 0, caps))
	}
	b.WriteString("\x1b[0m\n")
	return b.String()
}
