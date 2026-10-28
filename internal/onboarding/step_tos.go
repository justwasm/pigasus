package onboarding

import (
	"fmt"
	"io"
	"strings"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/textwrap"
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
//
// It is the compiled-in default. The live link is
// endpoints.Current().Terms — the configuration file may point it at a
// local service.
const TermsLinkTerms = endpoints.DefaultTerms

// TermsLinkPrivacy is the URL rendered for "Privacy Policy".
//
// It is the compiled-in default. The live link is
// endpoints.Current().Privacy.
const TermsLinkPrivacy = endpoints.DefaultPrivacy

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
	return renderTermsWithDims(w, agreed, 0, 0, 0)
}

func renderTermsLines(agreed bool, width int) []string {
	var lines []string
	lines = append(lines, stepNameTerms, "")
	for _, line := range TermsWarning {
		if width > 0 {
			lines = append(lines, wrapProseLines(line, width, "  ")...)
		} else {
			lines = append(lines, "  "+line)
		}
	}
	lines = append(lines, "")
	check := termsCheckboxOff
	if agreed {
		check = termsCheckboxOn
	}
	if width > 0 {
		lines = append(lines, wrapCheckboxLines(TermsCheckboxLabel, width, "  "+check+" ")...)
	} else {
		lines = append(lines, fmt.Sprintf("  %s %s", check, TermsCheckboxLabel))
	}
	lines = append(lines, "")
	tosLink := fmt.Sprintf("Terms of Service: %s%s%s", ansiUnder, endpoints.Current().Terms, ansiReset)
	privLink := fmt.Sprintf("Privacy Policy:  %s%s%s", ansiUnder, endpoints.Current().Privacy, ansiReset)
	if width > 0 {
		lines = append(lines, wrapLinkLines("Terms of Service: ", endpoints.Current().Terms, width)...)
		lines = append(lines, wrapLinkLines("Privacy Policy:  ", endpoints.Current().Privacy, width)...)
	} else {
		lines = append(lines, "  "+tosLink, "  "+privLink)
	}
	return lines
}

func renderTermsWithDims(w io.Writer, agreed bool, width, height, scroll int) error {
	lines := renderTermsLines(agreed, width)
	footer := "                       [ Previous ]    [ Done ]"
	if width > 0 && len(footer) > width {
		btn := "[ Previous ]  [ Done ]"
		if len(btn) <= width {
			pad := (width - len(btn)) / 2
			footer = strings.Repeat(" ", pad) + btn
		} else {
			footer = btn
		}
	}
	if height > 0 {
		area := height - 3
		if area < 1 {
			area = 1
		}
		if len(lines) > area {
			start := scroll
			if start > len(lines)-area {
				start = len(lines) - area
			}
			if start < 0 {
				start = 0
			}
			end := start + area
			if end > len(lines) {
				end = len(lines)
			}
			lines = lines[start:end]
		}
	}
	for _, l := range lines {
		if _, err := fmt.Fprintln(w, l); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w); err != nil {
		return err
	}
	_, err := fmt.Fprintln(w, footer)
	return err
}

func wrapProseLines(text string, width int, indent string) []string {
	if width <= 0 || len(indent)+len(text) <= width {
		return []string{indent + text}
	}
	limit := width - len(indent)
	if limit < 1 {
		limit = 1
	}
	chunks := textwrap.Words(text, limit)
	out := make([]string, len(chunks))
	for i, c := range chunks {
		out[i] = indent + c
	}
	return out
}

func wrapCheckboxLines(text string, width int, prefix string) []string {
	if width <= 0 || len(prefix)+len(text) <= width {
		return []string{prefix + text}
	}
	limit := width - len(prefix)
	if limit < 1 {
		limit = 1
	}
	chunks := textwrap.Words(text, limit)
	if len(chunks) == 0 {
		return []string{prefix}
	}
	out := make([]string, len(chunks))
	out[0] = prefix + chunks[0]
	contIndent := strings.Repeat(" ", len(prefix))
	for i := 1; i < len(chunks); i++ {
		out[i] = contIndent + chunks[i]
	}
	return out
}

func wrapLinkLines(label, url string, width int) []string {
	fullLine := "  " + label + ansiUnder + url + ansiReset
	if width <= 0 || len("  "+label)+len(url) <= width {
		return []string{fullLine}
	}
	limit := width - len("  "+label)
	if limit < 10 {
		return []string{
			"  " + label,
			"    " + ansiUnder + url + ansiReset,
		}
	}
	chunks := textwrap.Break(url, limit)
	if len(chunks) == 0 {
		return []string{fullLine}
	}
	out := make([]string, len(chunks))
	out[0] = "  " + label + ansiUnder + chunks[0] + ansiReset
	contIndent := "  " + strings.Repeat(" ", len(label))
	for i := 1; i < len(chunks); i++ {
		out[i] = contIndent + ansiUnder + chunks[i] + ansiReset
	}
	return out
}
