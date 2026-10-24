// elig_inline.go — cycle 262: eligibility errors rendered inline in the
// input loop, not stacked above the screen.
//
// Changelog 1.1.3 (internal/cli/testdata/changelog_reference.txt:686):
//
//	"Improved eligibility error handling by showing errors with a
//	 verification URL inline in the input loop instead of stacking
//	 them above the screen."
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
//   - EligibilityError (this file) is the typed shape an upstream
//     backend / agent loop wraps the eligibility payload in. Reason +
//     VerificationURL are both optional; a Reason-less eligibility
//     error falls back to "your account is not eligible for this
//     request" so the inline row always renders something the user
//     can act on. The Unwrap method exposes Cause so errors.As walks
//     back to the original *backend.HTTPError.
//
//   - (m *runMode).eligibilityNotice (cmd/wagyu/tui_run.go, added by
//     this cycle) holds the active inline notice. The chunk handler
//     in streamPrompt posts an eligibilityNoticeMsg to the program
//     when chunk.Error carries an *EligibilityError; Update() routes
//     the message into setEligibilityNotice, which writes the row
//     AND emits a one-line diagnostic to stderr so an operator
//     tailing the file can see why the inline row is showing. The
//     inline row is rendered by viewBody (just below where
//     announcementRow goes — both slots stack between the top
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
//     github.com/justwasm/pigasus/internal/render. The internal/render
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
package cliapp

import (
	"fmt"
	"os"

	"github.com/justwasm/pigasus/internal/announce"
	"github.com/justwasm/pigasus/internal/apierror"
	"github.com/justwasm/pigasus/internal/eligibility"
)

// EligibilityError is aliased to eligibility.Error.
type EligibilityError = eligibility.Error

// AsEligibilityError walks err's chain looking for an *EligibilityError.
func AsEligibilityError(err error) *EligibilityError {
	return eligibility.AsError(err)
}

// eligibilityNoticeMsg is the tea.Msg the streamPrompt chunk
// handler posts to the program when chunk.Error carries an
// *EligibilityError. The Update() handler routes it into
// setEligibilityNotice, which writes the inline notice onto
// m.eligibilityNotice and emits the diagnostic to stderr.
//
// The struct is a value type (Reason / VerificationURL) rather
// than a pointer-to-EligibilityError so the post path is a
// simple copy — chunk handlers fire from a streaming goroutine
// and we don't want to share the source error's lifecycle with
// the program goroutine.
type eligibilityNoticeMsg struct {
	Reason          string
	VerificationURL string
}

// eligibilityNotice is the value stored on runMode that drives
// the viewBody render. The struct is a value (not a pointer) so
// the zero value is "no notice" — viewBody checks the Reason
// field to decide whether to render.
//
// The struct deliberately has no method that mutates itself;
// setEligibilityNotice / clearEligibilityNotice on runMode are the
// only mutators so the lifecycle is easy to reason about
// (one place sets, one place clears, both are reached from a
// small number of Update() branches).
type eligibilityNotice struct {
	Reason          string
	VerificationURL string
}

// eligibilityNoticeRow renders the inline notice row for
// m.eligibilityNotice. Returns "" when no notice is set so the
// caller can write the result unconditionally and the no-notice
// case is a free no-op.
//
// Layout: a single styled line placed between the top separator
// and the prompt input, exactly where announcementRow() goes.
//
//	ⓘ <Reason> <render.Hyperlink("Verify here", URL)>
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
// one line + a one-line hyperlink) and the cycle's tests pin
// that the row stays single-line under any terminal width. A
// future cycle that needs to wrap long Reasons can add a width
// clamp here without changing the call site.
func (m *runMode) eligibilityNoticeRow() string {
	return eligibility.RenderNoticeRow(m.eligibilityNotice.Reason, m.eligibilityNotice.VerificationURL, m.hyperlinkCaps)
}

// setEligibilityNotice stores a notice on the runMode AND logs
// it for diagnostics. The diagnostic-log path is the only place
// the user-facing message reaches stderr; the inline notice is
// what the user sees on the screen. The split mirrors the
// `error: <msg>` / `AGY_ERROR: {...}` print-mode contract:
// the diagnostic line is operator-facing, the inline row is
// user-facing, neither leaks into the conversation transcript.
//
// reason is the user-facing message (falls back to a generic
// message when empty). url is the verification URL (empty when
// the upstream did not supply one — the inline row still
// renders in that case).
//
// The function does NOT clear the notice first; if a caller
// wants to overwrite an active notice with a new one, they
// should call clearEligibilityNotice first. Cycle 262's call
// site (the Update() handler for eligibilityNoticeMsg) is the
// only producer, and each eligibility error from the chunk
// handler produces a fresh message — overwrites are an
// emergent property, not an explicit branch.
func (m *runMode) setEligibilityNotice(reason, url string) {
	m.eligibilityNotice = eligibilityNotice{
		Reason:          reason,
		VerificationURL: url,
	}
	if reason == "" {
		reason = "your account is not eligible for this request"
	}
	if url != "" {
		fmt.Fprintf(os.Stderr, "diagnostic: eligibility error: %s (verify at %s)\n", reason, url)
	} else {
		fmt.Fprintf(os.Stderr, "diagnostic: eligibility error: %s\n", reason)
	}
}

// clearEligibilityNotice wipes the inline notice when the user
// starts typing or submits a new prompt. Called from the
// tea.KeyMsg handler (the cycle 262 "user starts typing"
// signal) and from submitPrompt (a new prompt implicitly
// acknowledges the prior notice).
//
// The function is idempotent — clearing an already-empty
// notice is a no-op so the call sites don't have to guard.
func (m *runMode) clearEligibilityNotice() {
	m.eligibilityNotice = eligibilityNotice{}
}

// classifyEligibilityError produces the AGY_ERROR JSON envelope
// the print path emits on stderr when a turn terminates on an
// *EligibilityError. Returns nil, false when err is not an
// eligibility error so the caller can fall through to the
// ordinary classifyAPIError path.
//
// The envelope shape is identical to the existing apiError
// (short_error / status / error_code / code_kind / retryable /
// error_id) — cycle 262's spec "prints the error to stderr in
// the same AGY_ERROR JSON shape" is the print-mode contract.
// Status is "ELIGIBILITY_DENIED" so an automation script
// branching on AGY_ERROR.status can route eligibility failures
// to a "show user the verification URL" handler instead of a
// generic "retry the API call" path. error_code stays at 0
// (no canonical HTTP status code) so the field does not lie
// about the cause — the cycle's design uses status as the
// distinguishing field.
//
// retryable stays false: a "verify your account" failure does
// not get better with a retry. The reference's quota case
// (the only other cycle-262-adjacent structured error)
// returns retryable=true; cycle 262 deliberately diverges
// here.
// classifyEligibilityError delegates to eligibility.ClassifyError.
func classifyEligibilityError(err error) (*apierror.APIError, bool) {
	return eligibility.ClassifyError(err)
}

// eligibilityHTTPStatus is aliased to eligibility.HTTPStatus.
const eligibilityHTTPStatus = eligibility.HTTPStatus

// announcementRow renders one announcement card as rows between separator and prompt input.
func (m *runMode) announcementRow() string {
	return announce.RenderCard(m.activeAnnouncement())
}

// activeAnnouncement returns the head of m.announceVisible that the TUI should render.
func (m *runMode) activeAnnouncement() *announce.Card {
	if m == nil || len(m.announceVisible) == 0 {
		return nil
	}
	c := m.announceVisible[0]
	return &c
}

// announcementRowsCount returns the number of screen rows announcementRow() will emit.
func (m *runMode) announcementRowsCount() int {
	if m.activeAnnouncement() == nil {
		return 0
	}
	return 5
}

// dismissCurrentAnnouncement removes the head of the visible queue and persists dismissal.
func (m *runMode) dismissCurrentAnnouncement() {
	card := m.activeAnnouncement()
	if card == nil {
		return
	}
	m.announceVisible = m.announceVisible[1:]
	if err := announce.SaveDismissed(card.ID); err != nil {
		m.announceSaveError = err
	}
	m.announceSessionHiddenID = ""
}

// hideCurrentAnnouncementForSession advances the visible queue head without persisting.
func (m *runMode) hideCurrentAnnouncementForSession() {
	if len(m.announceVisible) == 0 {
		return
	}
	m.announceSessionHiddenID = m.announceVisible[0].ID
	m.announceVisible = m.announceVisible[1:]
}

// announcementSaveErrorMessage returns a human-readable warning when persistence failed.
func (m *runMode) announcementSaveErrorMessage() string {
	if m == nil || m.announceSaveError == nil {
		return ""
	}
	return "  ⓘ Could not save announcement dismissal: " + m.announceSaveError.Error()
}

// loadAnnouncements seeds the in-memory visible queue from the persisted dismiss list.
func (m *runMode) loadAnnouncements() {
	dismissed, err := announce.LoadDismissed()
	if err != nil {
		m.announceSaveError = err
	}
	m.announceVisible = announce.VisibleCards(dismissed)
	m.announceSessionHiddenID = ""
}
