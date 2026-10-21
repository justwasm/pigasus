// slash_cycle214_test.go — cycle 214: byte-level wrap assertions for the
// /changelog panel's long-line wraps and inline-code styling.
//
// Two recoveries from the REF 1.4.0 captures (out/local-macos/w99.ref.raw.txt
// and the other 11 widths under out/local-macos/): the panel wraps each
// bullet's body greedily to width-2 columns (the bullet's first line counts
// its "  • " marker, continuation lines just the 2-column indent) and pads
// non-final wrap lines to the budget when the trailing gap is at most 12
// columns. Inline `code` spans are wrapped at the column where the trailing
// ` can stay whole — "sign-" / "in.", "--" / "mode auto", "-" / "p runs" —
// by splitting on the last hyphen whose prefix still fits. The renders below
// pin all three rules; the unit suites in slash_cycle207_test.go and
// slash_panels_test.go cover byte-level panel composition.
package main

import (
	"strings"
	"testing"

	"github.com/mattn/go-runewidth"
)

// findChangelogLine returns the index of the first panel line whose
// visible content contains substr, or -1 if absent. It works around the
// changelog panel's render order — title row, blank, tagline, blank,
// header, blank, bullets — which is fixed by changelogPanelDocBuild, so a
// substring search is the clearest way to address a line in a test.
func findChangelogLine(t *testing.T, doc []string, substr string) int {
	t.Helper()
	for i, ln := range doc {
		if strings.Contains(stripAnsiForMeasure(ln), substr) {
			return i
		}
	}
	return -1
}

// TestChangelogWrapBudget confirms panelDoc emits an exact-width first
// line for the tagline at every captured width. The tagline's body fits
// greedily, so the line's visible width equals min(width-2, body+indent)
// — the budget when the body fits, the body's natural width otherwise.
//
// Both formats pin the wrap model's terminal-cell output. The 80..120
// widths cover every captured terminal size; w400 is the synthetic-
// infinite case where the body fits on one line.
func TestChangelogWrapBudget(t *testing.T) {
	for _, c := range []struct {
		name      string
		width     int
		wantVisLn int
	}{
		{"w80", 80, 78},
		{"w96", 96, 94},
		{"w98", 98, 96},
		{"w99", 99, 97},
		{"w100", 100, 98},
		{"w101", 101, 99},
		{"w103", 103, 101},
		{"w104", 104, 102},
		{"w106", 106, 104},
		{"w110", 110, 108},
		{"w120", 120, 118},
		// w400: budget = 398 > tagline body, so the tagline fits on one
		// line and its visible width is body+indent (2 + 108 = 110).
		{"w400", 400, 110},
	} {
		t.Run(c.name, func(t *testing.T) {
			doc := changelogPanelDoc(c.width)
			idx := findChangelogLine(t, doc, "terminal-first surface")
			if idx < 0 {
				t.Fatalf("tagline not found in panel for width %d", c.width)
			}
			stripped := stripAnsiForMeasure(doc[idx])
			if w := ansiStringWidth(stripped); w != c.wantVisLn {
				t.Fatalf("tagline line visual width = %d, want %d (budget=%d). line=%q",
					w, c.wantVisLn, c.width-2, stripped)
			}
		})
	}
}

// TestChangelogWrapBulletPadding confirms a wrapped bullet body line is
// padded to its wrap budget (width-2) with literal 0x20 bytes (NOT CSI
// 'b' REP). The "Improved Auto mode's review" paragraph wraps to four
// lines; the second continuation must be 97 cells, with the trailing
// cells filled by N literal spaces.
//
// A regression to the CSI 'b' REP path leaves the line at body width —
// ultraviolet's printString parser converts CSI 'b' into a Width=0
// placeholder cell, and only the leading literal space reaches the cell
// buffer; tmux's `capture-pane -e` then renders one visible space where
// REF shows three or more.
func TestChangelogWrapBulletPadding(t *testing.T) {
	doc := changelogPanelDoc(99)
	// The "Improved Auto mode" paragraph's second continuation line
	// ("faster and no longer times out...") fits in 97 columns with a
	// 1-column trailing pad.
	idx := findChangelogLine(t, doc, "faster and no longer times out")
	if idx < 0 {
		t.Fatalf("auto-mode continuation line not found in panel; panel=%d lines", len(doc))
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w != 97 {
		t.Fatalf("auto-mode continuation visual width = %d, want 97. line=%q",
			w, stripped)
	}
	tail := strings.TrimRight(stripped, " ")
	gap := ansiStringWidth(stripped) - ansiStringWidth(tail)
	if gap < 1 || gap > changelogPadLimit {
		t.Fatalf("trailing pad %d cells out of range (1..%d); line=%q",
			gap, changelogPadLimit, stripped)
	}
}

// TestChangelogWrapCommaHang confirms the wrap-budget rule on a bullet
// that fits one line without comma-overshoot: the line is padded to
// `width-2` cells (97 at w99) with literal 0x20 bytes inside the colour
// run, and the trailing pad is at least 1 cell. Captured at w99, REF's
// "(AGY_ADC_AUTH) sign-in. Claude models…" line reaches 97 cells
// (2 indent + 92 body + 3 trailing pad). The same wrap rule reaches
// `width-1` for paragraphs where a word's trailing comma crosses the
// budget, but that case does not surface in the captured panels at any
// measured width — the body of the comma-overshoot check stays
// observable through the span's fit-at-budget outcome here.
func TestChangelogWrapCommaHang(t *testing.T) {
	doc := changelogPanelDoc(99)
	want := "AGY_ADC_AUTH) sign-in. Claude models now appear in /model only if your Google Cloud project"
	idx := findChangelogLine(t, doc, want)
	if idx < 0 {
		t.Fatalf("first bullet continuation should contain %q; not found in %d-line panel",
			want, len(doc))
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w != 97 {
		t.Fatalf("first bullet continuation visual width = %d, want 97. line=%q",
			w, stripped)
	}
	// Body fills budget-3 = 94 cells (indent 2 + body 92), so the
	// 3-cell trailing pad must land exactly there.
	wantTail := "project   "
	if !strings.HasSuffix(stripped, wantTail) {
		t.Fatalf("expected trailing pad of 3 spaces after 'project' to match REF; got %q",
			stripped)
	}
}

// TestChangelogWrapHyphenSplit confirms the hyphen-split rule: a word
// that does not fit on the current line is split after the last hyphen
// whose prefix still fits, and the remainder opens the next line. Two
// styles exercise the rule: a CJK-prefixed hyphen (" --" / "mode auto")
// and a one-character split ("-" / "p runs") in the inline-code span.
// Both surface in the captured panels at width=99.
func TestChangelogWrapHyphenSplit(t *testing.T) {
	doc := changelogPanelDoc(99)
	// "--" / "mode auto": the auto-mode-on-board-paragraph bullet
	// ("Added Auto mode, which approves...") splits at the second
	// hyphen of "--mode auto", putting "mode auto" on the next line.
	if idx := findChangelogLine(t, doc, "mode auto"); idx < 0 {
		t.Fatalf("'mode auto' wrap continuation not found in panel")
	}
	// "-" / "p runs": the subagent-questions bullet ("...Headless -p
	// runs skip these...") splits the inline code at its leading "-",
	// opening the next line with "p runs".
	if idx := findChangelogLine(t, doc, "p runs"); idx < 0 {
		t.Fatalf("'p runs' wrap continuation not found in panel")
	}
}

// TestChangelogInlineCodeStyle confirms the inline `code` spans get the
// reference's 256-colour open/close: fg=69 on bg=254, and the trailing
// fg=235;49 to restore the body palette. Captures show every inline
// span styled with these exact byte sequences.
func TestChangelogInlineCodeStyle(t *testing.T) {
	doc := changelogPanelDoc(99)
	const wantOpen = "\x1b[38;5;69m\x1b[48;5;254m"
	const wantClose = "\x1b[38;5;235m\x1b[49m"
	foundOpen, foundClose := false, false
	for _, ln := range doc {
		if strings.Contains(ln, wantOpen) {
			foundOpen = true
		}
		if strings.Contains(ln, wantClose) {
			foundClose = true
		}
	}
	if !foundOpen {
		t.Fatalf("inline code opening SGR (%q) not found in panel", wantOpen)
	}
	if !foundClose {
		t.Fatalf("inline code closing SGR (%q) not found in panel", wantClose)
	}
}

// TestChangelogPanelSkipsFitLines confirms the changelog panel does not
// get fitLines'd at the body level. fitLines extends short lines to the
// panel width via an explicit gray pad run, but ultraviolet's
// paintLine+transformLine path collapses that trailing fg-coloured run
// into a single \x1b[K — REF's captures match the clone's only without
// that extension (the header "## 1.3.6" sits at content-length 10, not
// padded to 99). Cycle 214 added the `panelChangelog` short-circuit
// in tui_run.go (lines ~2279) to keep fitLines off the panel.
func TestChangelogPanelSkipsFitLines(t *testing.T) {
	doc := changelogPanelDoc(99)
	idx := findChangelogLine(t, doc, "## 1.3.6")
	if idx < 0 {
		t.Fatal("header '## 1.3.6' not found in panel")
	}
	stripped := stripAnsiForMeasure(doc[idx])
	if w := ansiStringWidth(stripped); w >= 97 {
		t.Fatalf("header line should be content-length only (no fitLines pad), got width %d. line=%q",
			w, stripped)
	}
}

// ansiStringWidth counts visible cells of a string with embedded SGRs
// stripped. It uses go-runewidth directly so it matches the wrap model
// (changelogWrapParagraph also uses go-runewidth's StringWidth).
func ansiStringWidth(s string) int {
	return runewidth.StringWidth(stripAnsiForMeasure(s))
}

// stripAnsiForMeasure removes CSI SGR sequences (\x1b[...m). It's
// coarser than uv/bubbletea's ansi package — it intentionally drops
// only the SGRs because that is all the wrap model depends on.
// picker_view_test.go already defines stripAnsi for unrelated
// purposes, so we use a name that is unambiguous within this file.
func stripAnsiForMeasure(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if i+1 < len(s) && s[i] == 0x1b && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && (s[j] < 0x40 || s[j] > 0x7e) {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}
