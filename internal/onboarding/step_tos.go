package onboarding

import (
	"fmt"
	"io"
)

// stepNameTerms is the literal header line shown at the top of the ToS screen.
const stepNameTerms = "Terms of Service & Data Use"

// TermsWarning is the 3-line warning text shown above the ToS
// checkbox. The v1.3.5 binary's exact wording (from the recovered
// onboarding screen capture, cycle 42 tmux + render_agy.py).
var TermsWarning = []string{
	"By accepting, you agree to the Terms of Service and Privacy Policy.",
	"Google may collect usage data and crash reports to help improve the CLI.",
	"You can change this at any time in /settings.",
}

// TermsCheckboxLabel is the full text rendered next to the checkbox
// when checked. The binary's format string is:
//
//	%s[%s] Yes, I agree to help improve %s by allowing
//	  Google to collect usage data and crash reports.
//
// (recovered from "Yes, I agree to help improve %s by allowing" in
// the v1.3.5 binary's onboarding string table).
const TermsCheckboxLabel = "Yes, I agree to help improve Pigasus CLI by allowing Google to collect usage data and crash reports."

// TermsLinkTerms is the URL rendered under the warning text for
// "Terms of Service". The binary's onboarding screen renders this
// as a hyperlink with the URL underlined; the parity test asserts the
// URL string is present.
const TermsLinkTerms = "https://antigravity.google/terms"

// TermsLinkPrivacy is the URL rendered for "Privacy Policy".
const TermsLinkPrivacy = "https://policies.google.com/privacy"

// termsCheckboxOn / termsCheckboxOff are the two glyph states. The
// v1.3.5 binary uses a Unicode ballot box with check mark on / off.
// (v1.3.5 binary: "the spacebar not toggling an option in multi-
// select prompts" release note implies a checkbox-style UI.)
const (
	termsCheckboxOn  = "☑"
	termsCheckboxOff = "☐"
)

// renderTerms writes Step 2 of the wizard. The ToS screen:
//
//	┌──────────────────────────────────────────────────────────────────┐
//	│ Terms of Service & Data Use                                       │
//	│                                                                  │
//	│ By accepting, you agree to the Terms of Service and Privacy ...  │
//	│ Google may collect usage data and crash reports to help impr...  │
//	│ You can change this at any time in /settings.                    │
//	│                                                                  │
//	│ ☑ Yes, I agree to help improve Antigravity CLI by allowing...   │
//	│                                                                  │
//	│ Terms of Service: https://antigravity.google/terms              │
//	│ Privacy Policy:  https://policies.google.com/privacy            │
//	│                                                                  │
//	│                       [ Previous ]    [ Done ]                   │
//	└──────────────────────────────────────────────────────────────────┘
func renderTerms(w io.Writer, agreed bool) error {
	if _, err := fmt.Fprintln(w, stepNameTerms); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	for _, line := range TermsWarning {
		if _, err := fmt.Fprintln(w, "  ", line); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	check := termsCheckboxOff
	if agreed {
		check = termsCheckboxOn
	}
	if _, err := fmt.Fprintf(w, "  %s %s\n", check, TermsCheckboxLabel); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Terms of Service: %s%s%s%s\n", ansiUnder, TermsLinkTerms, ansiReset, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "  Privacy Policy:  %s%s%s%s\n", ansiUnder, TermsLinkPrivacy, ansiReset, ""); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	if _, err := fmt.Fprintln(w, "                       [ Previous ]    [ Done ]"); err != nil {
		return err
	}
	return nil
}
