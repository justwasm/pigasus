// elig_inline.go — cycle 262: the eligibility notice's host wiring.
//
// Changelog 1.1.3 (internal/cli/testdata/changelog_reference.txt:686):
//
//	"Improved eligibility error handling by showing errors with a
//	 verification URL inline in the input loop instead of stacking
//	 them above the screen."
//
// The notice itself — its state (announce.Notice) and its row
// (announce.RenderNoticeRow) — lives in
// internal/ui/views/announce: a UI row does not belong in a features
// package, and the eligibility *facts* (the typed error, the Reason,
// the VerificationURL, the tier gates) stay in
// internal/features/auth/eligibility (tier 2). Cycle 381, tranche 53.
//
// What stays here is the wiring the host owns:
//
//   - eligibilityNoticeMsg is the tea.Msg the streamPrompt chunk
//     handler posts to the program when chunk.Error carries an
//     *EligibilityError. The Update() handler routes it into
//     setEligibilityNotice, which writes the notice onto
//     m.eligibilityNotice and emits the diagnostic to stderr.
//
//   - setEligibilityNotice stores the notice AND logs it for
//     diagnostics. The diagnostic-log path is the only place the
//     user-facing message reaches stderr; the inline notice is what
//     the user sees on the screen. The split mirrors the
//     `error: <msg>` / `AGY_ERROR: {...}` print-mode contract:
//     the diagnostic line is operator-facing, the inline row is
//     user-facing, neither leaks into the conversation transcript.
//
//   - clearEligibilityNotice wipes the inline notice when the user
//     starts typing or submits a new prompt. Called from the
//     tea.KeyMsg handler (the cycle 262 "user starts typing"
//     signal) and from submitPrompt (a new prompt implicitly
//     acknowledges the prior notice).
//
// The root-cause analysis of cycle 262 — why the chunk handler used
// to drop the error, and why the notice renders inline instead of
// stacking above the transcript — moved with the row into
// internal/ui/views/announce/notice.go.
package cliapp

import (
	"fmt"
	"os"

	"github.com/justwasm/pigasus/internal/ui/views/announce"
)

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

// setEligibilityNotice stores a notice on the runMode AND logs
// it for diagnostics.
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
	m.eligibilityNotice = announce.Notice{
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
	m.eligibilityNotice = announce.Notice{}
}
