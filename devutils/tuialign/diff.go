package main

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"
)

// DiffResult holds comparison metrics between reference (agy) and target (pigasus).
type DiffResult struct {
	TotalLines  int
	MatchLines  int
	DiffLines   int
	ColorDiffs  int
	ParityScore float64 // 0.0 - 100.0%
	Report      string
	HTMLReport  string
	Identical   bool
}

var (
	// Ignore variable version banners in header
	versionRe = regexp.MustCompile(`(Antigravity|Pigasus)\s+CLI\s+\d+\.\d+\.\d+`)
	// Ignore slight duration jitter: "Thought for 64ms" vs "Thought for 80ms"
	thoughtDurRe = regexp.MustCompile(`Thought for \d+(\.\d+)?(ms|s)`)
	ansiStripRe  = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
)

// StripANSI removes ANSI escape codes from string.
func StripANSI(s string) string {
	return ansiStripRe.ReplaceAllString(s, "")
}

// CompareSnapshots compares two captured terminal text buffers.
func CompareSnapshots(leftRaw, rightRaw string, label string) DiffResult {
	leftRawLines := strings.Split(strings.TrimRight(leftRaw, "\n"), "\n")
	rightRawLines := strings.Split(strings.TrimRight(rightRaw, "\n"), "\n")

	maxLines := len(leftRawLines)
	if len(rightRawLines) > maxLines {
		maxLines = len(rightRawLines)
	}

	matches := 0
	diffs := 0
	colorDiffs := 0
	columnWidth := reportColumnWidth(leftRawLines, rightRawLines)
	accountNote := ""

	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== PARITY SNAPSHOT: %s ===\n", label))
	b.WriteString(fmt.Sprintf("%-4s | %-*s | %-*s\n", "LN", columnWidth, "REFERENCE (agy)", columnWidth, "TARGET (pigasus)"))
	b.WriteString(strings.Repeat("-", columnWidth*2+9) + "\n")

	for i := 0; i < maxLines; i++ {
		var lRaw, rRaw string
		if i < len(leftRawLines) {
			lRaw = leftRawLines[i]
		}
		if i < len(rightRawLines) {
			rRaw = rightRawLines[i]
		}

		lStrip := strings.TrimRight(StripANSI(lRaw), " ")
		rStrip := strings.TrimRight(StripANSI(rRaw), " ")

		status := " "
		colorNote := ""
		// Banner rows: compare the text after the product's own art, and do
		// not compare the art's colours at all.
		lBanner, lIsBanner := bannerText(lStrip)
		rBanner, rIsBanner := bannerText(rStrip)
		if lIsBanner && rIsBanner {
			if lBanner == rBanner {
				matches++
				status = "✓"
			} else {
				diffs++
				status = "✗"
			}
			if le, re := bannerEmail(lStrip), bannerEmail(rStrip); le != "" && re != "" && le != re {
				if accountNote == "" {
					accountNote = fmt.Sprintf("      note: the account rows name different accounts (%s vs %s) — environment state, not rendering\n", le, re)
					b.WriteString(accountNote)
				}
			}
			lStrip, rStrip = lBanner, rBanner
			b.WriteString(fmt.Sprintf("%3d %s | %-*s | %-*s\n",
				i+1, status, columnWidth, truncateOrPad(lStrip, columnWidth), columnWidth, truncateOrPad(rStrip+colorNote, columnWidth)))
			continue
		}
		if isSemanticallyEqual(lStrip, rStrip) {
			// Text matches! Now check color & style parity
			lCells := ParseStyledLine(lRaw)
			rCells := ParseStyledLine(rRaw)
			mismatches := CompareStyledLines(lCells, rCells)
			if len(mismatches) > 0 {
				colorDiffs++
				status = "~" // Text match, but style/color differ
				colorNote = fmt.Sprintf(" (~ color: %s)", mismatches[0].Reason)
			} else {
				matches++
				status = "✓"
			}
		} else {
			diffs++
			status = "✗"
		}

		dispL := truncateOrPad(lStrip, columnWidth)
		dispR := truncateOrPad(rStrip+colorNote, columnWidth)

		b.WriteString(fmt.Sprintf("%3d %s | %-*s | %-*s\n", i+1, status, columnWidth, dispL, columnWidth, dispR))
	}

	score := 0.0
	if maxLines > 0 {
		score = float64(matches) / float64(maxLines) * 100.0
	}

	b.WriteString(strings.Repeat("-", columnWidth*2+9) + "\n")
	b.WriteString(fmt.Sprintf("RESULT: %d/%d exact matches (%.1f%% parity). Text diffs: %d, Color diffs: %d\n\n",
		matches, maxLines, score, diffs, colorDiffs))

	htmlContent := RenderHTMLComparison(leftRaw, rightRaw, label)

	return DiffResult{
		TotalLines:  maxLines,
		MatchLines:  matches,
		DiffLines:   diffs,
		ColorDiffs:  colorDiffs,
		ParityScore: score,
		Report:      b.String(),
		HTMLReport:  htmlContent,
		Identical:   diffs == 0 && colorDiffs == 0,
	}
}

func isSemanticallyEqual(l, r string) bool {
	if l == r {
		return true
	}
	// Normalize version headers
	normL := versionRe.ReplaceAllString(l, "[CLI BANNER]")
	normR := versionRe.ReplaceAllString(r, "[CLI BANNER]")

	// Normalize thought duration timestamps
	normL = thoughtDurRe.ReplaceAllString(normL, "Thought for [DUR]")
	normR = thoughtDurRe.ReplaceAllString(normR, "Thought for [DUR]")

	// And the product's own name: the clone is a different product, so a
	// row that says "Shared with Pigasus" where the reference says
	// "Shared with Antigravity" is the intended rename, not a rendering
	// difference. See branding.Config.
	normL = productRe.ReplaceAllString(normL, "[PRODUCT]")
	normR = productRe.ReplaceAllString(normR, "[PRODUCT]")

	return strings.TrimSpace(normL) == strings.TrimSpace(normR)
}

// productRe matches the two products' names where they stand in for each
// other. "Antigravity" is the reference's, "Pigasus" the clone's; the brand
// table (internal/core/branding) is what decides which word a surface shows,
// so the difference is deliberate.
var productRe = regexp.MustCompile(`\b(Antigravity|Pigasus)\b`)

// logoArtRe is the block-element range the startup banner's logo is drawn
// with (U+2580..U+259F, plus the full block).
var logoArtRe = regexp.MustCompile(`[\x{2580}-\x{259F}]`)

// bannerText returns the informational text of a startup-banner row and
// whether the row is one.
//
// The banner's first five rows are "<logo art>  <fact>": a product glyph
// then the CLI name, the account, the model, the workspace. The art is the
// product's own — two binaries are not expected to draw the same picture —
// so comparing it makes every scenario start at 40% and buries the rows that
// actually disagree. The text is what the two are supposed to agree on.
//
// The row is recognised by *starting* with art after optional indentation,
// which no other row on screen does; the product name and version are
// normalised by versionRe, exactly as in isSemanticallyEqual.
func bannerText(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if trimmed == "" {
		return "", false
	}
	first, _ := utf8.DecodeRuneInString(trimmed)
	if !logoArtRe.MatchString(string(first)) {
		return "", false
	}
	text := logoArtRe.ReplaceAllString(trimmed, "")
	text = versionRe.ReplaceAllString(text, "[CLI BANNER]")
	// The account is environment state, not rendering: the two binaries
	// resolve the signed-in email from different local stores (the clone's
	// account registry, the reference's keychain state), and on a machine
	// where those disagree the banner differs for a reason no renderer can
	// fix. The difference is reported separately instead of counted.
	text = emailRe.ReplaceAllString(text, "[EMAIL]")
	return strings.TrimSpace(text), true
}

// emailRe matches the account row's address.
var emailRe = regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`)

// bannerEmail returns the address a banner row names, or "".
func bannerEmail(line string) string {
	return emailRe.FindString(line)
}

// reportColumnWidth is the width of one pane column in the text report.
//
// It used to be a literal 48, which cut every scenario's rows three cells
// into the text — the actual disagreement ("(Antigravity Pro)", a stray
// "auto · ", one column of a rule) was always past the cut, so a report was
// unreadable without opening the HTML file. The report now uses the pane
// width the capture was taken at: the two sides were rendered at the same
// geometry, so that is exactly the width a row can occupy.
func reportColumnWidth(left, right []string) int {
	width := 0
	for _, lines := range [][]string{left, right} {
		for _, l := range lines {
			if n := utf8.RuneCountInString(l); n > width {
				width = n
			}
		}
	}
	if width < 40 {
		width = 40
	}
	if width > 200 {
		width = 200
	}
	return width
}

func truncateOrPad(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width-3]) + "..."
	}
	return s + strings.Repeat(" ", width-len(runes))
}
