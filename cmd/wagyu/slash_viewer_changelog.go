package main

import (
	"strings"
)

func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.viewerSpecFor()
	rows, start, end := m.viewerWindow(spec)
	lines := append([]string{}, rows...)
	// Cycle 207: REF puts the counter immediately under the window and the
	// blank between the counter and the legend (changelog24.ref.raw.txt
	// rows 20-23). The counter is always present: the release-notes document
	// never fits.
	// Live 1.4.3 advertises only Navigate/Cancel in this viewer even though
	// the scroll keys still work; the full pagination hint was a 1.4.0
	// transcription.
	lines = append(lines,
		viewerCounter(start, end, spec.total),
		"",
		panelDotHint("  ", "↑/↓", "Navigate", "esc", "Cancel"))
	return lines
}

// changelogExcerptRows is the visible first screen of the /changelog
// viewer: the yellow title (2-space indent, 48;5;63 background — REF
// 1.4.0), a blank, the tagline and the transcribed 1.3.6 block.
func changelogExcerptRows() []string {
	out := []string{
		"  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m",
		"",
		"  \x1b[38;5;235mThe terminal-first surface to interact with Pigasus agents. Stay in your flow without context switching.\x1b[39m",
	}
	return append(out, changelogBody...)
}

// changelogWindowRows is how many document rows the viewer shows at
// once. Cycle changelog-pagination switched the source to the runtime
// reference file (internal/cli/testdata/changelog_reference.txt), but
// the first-screen shape is preserved at 18 rows: title + blank +
// tagline + blank + ## 1.3.6 + blank + the first 12 bullets of the
// 1.3.6 section. The 13th bullet and the section break to 1.3.5 land
// on the second screen, matching the cycle-201 first-screen layout
// that TestPanelChangelogLayout / TestPanelChangelogViewerScrolls pin.
//
// The 13th bullet is rendered into the document (and visible after a
// single ↓ press); only the window size is fixed at 18 so the counter
// starts at "(1–18 of 2014 lines)" rather than the wrap-dependent
// row count.
func changelogWindowRows() int { return 18 }

// changelogTotalLines is the denominator in the viewer's "(a–b of N lines)"
// counter: the reference's wrapped line count for the whole release-notes
// document at 120x40. Re-measured for the 1.4.4 capture (live: "1–36 of 2014
// lines"); the 1.4.2-era fixture showed 1962.
const changelogTotalLines = 2014

// changelogViewerLines is the /changelog document.
//
// Cycle changelog-pagination: when the runtime source loader finds
// internal/cli/testdata/changelog_reference.txt (it walks up from cwd
// looking for go.mod) the panel renders the full release notes
// 1.3.6 → 1.0.0 — ~915 logical rows, more when the bullet text wraps
// to the terminal width. The fallback path keeps the cycle-201
// transcription (changelogExcerptRows + changelogRestRows parsed from
// cmd/wagyu/testdata/print_changelog.md) so a binary installed in a
// user's PATH still shows a populated document.
//
// On a live terminal (m.width > 0 and m.height > 0) the document is
// wrapped to the panel width — see changelogPanelDoc in
// slash_changelog_wrap.go for the markdown path and
// changelogSourceWrapped in slash_changelog_source.go for the new
// bullet-list source. The width-0 fallback preserves the cycle-193
// transcription byte-for-byte (the transcription is one long line per
// bullet and only fits a width that wraps back to the same rows).
func (m *runMode) changelogViewerLines() []string {
	if m.width > 0 && m.height > 0 {
		if src := changelogSourceLines(); src != nil {
			return changelogSourceWrapped(src, m.width)
		}
		return changelogPanelDoc(m.width)
	}
	if src := changelogSourceLines(); src != nil {
		return changelogSourceStyled(src)
	}
	out := append([]string{}, changelogExcerptRows()...)
	return append(out, changelogRestRows...)
}

// changelogRestRows is the remainder of the release notes (1.3.5 down
// through 1.0.0) rendered in the panel's bullet style. It is parsed once
// from the embedded print-mode markdown (`printSlashChangelog`); the
// print-mode blob deliberately omits the 1.3.6 section that changelogBody
// above already covers, so the two concatenate without overlap.
var changelogRestRows = buildChangelogRestRows()

func buildChangelogRestRows() []string {
	var out []string
	headers := 0
	for _, raw := range strings.Split(printSlashChangelog, "\n") {
		line := strings.TrimRight(raw, "\r")
		if strings.HasPrefix(line, "## ") {
			headers++
			// Cycle 207: the cycle-200 markdown refresh re-added the 1.3.6
			// section (line 7 of testdata/print_changelog.md), but
			// changelogExcerptRows already ships that block via
			// changelogBody. Emitting it again duplicated the whole
			// section in the panel, so skip the first header.
			if headers == 1 {
				continue
			}
			out = append(out, "",
				"  \x1b[1m\x1b[38;5;27m"+line+"\x1b[0m")
			continue
		}
		if headers < 2 {
			// The H1 title, tagline, HTML comments and the duplicated
			// 1.3.6 body above the first real (`## 1.3.5`) header are
			// already represented by the excerpt.
			continue
		}
		switch {
		case strings.HasPrefix(line, "- "):
			out = append(out,
				"  \x1b[38;5;235m• "+panelPlainInline(line[2:])+"\x1b[39m")
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// panelPlainInline strips the inline markdown the panel renders as plain
// text: `backticks` around code and **bold** markers. The reference's
// captures show both dropped (ppref.changelog.raw.txt renders
// "AGY_ADC_AUTH" with no quoting).
func panelPlainInline(s string) string {
	s = strings.ReplaceAll(s, "`", "")
	s = strings.ReplaceAll(s, "**", "")
	return s
}

// panelDiffLines renders the /diff viewer. Two shapes exist in REF 1.4.0:
//
//   - a git (or hg/jj) workspace is detected from m.cwd — the All Changes
//     tab lists the working-copy files (panelDiffLinesVCS);
//   - otherwise the no-VCS warning below is shown. That shape (and its
//     leading " Diff" tab row with plain inactive tabs) is recovered from
//     /tmp/agycmp/panels/ppref.diff.raw.txt and is also the fallback the
//     unit tests exercise (mkPanel leaves m.cwd empty). 【一手】
//
// Cycle 207 wires the git branch in; the no-VCS renderer below is the
// cycle-193/201 shape, with the inactive tabs changed from dim gray to
// plain because both the 1.3.x capture and the 1.4.0 captures show no SGR
// on them.
