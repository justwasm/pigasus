// slash_changelog_wrap.go — cycle 214: the /changelog viewer's long-line
// wrapping and inline-code styling.
//
// Cycle 207 shipped the /changelog document as one pre-rendered row per
// bullet; on a real terminal the reference wraps each bullet to the panel
// width, and that wrapping is the bulk of the terminal-cell divergence the
// cycle-214 parity run measured. Every rule below was recovered by
// probing the closed-source reference `agy` 1.4.0 under tmux socket
// `agy214` at widths 80/96/98/99/100/101/103/104/106/110/120/400 (see
// docs/cycles/cycle-214/deliverable-changelog-wrap.md for the captures):
//
//   - The document wraps greedily to a display budget of `width-2`
//     columns. The row's own indent counts toward the budget, and a
//     bullet's first line counts its "  • " marker (4 columns) instead of
//     the 2-column continuation indent.
//   - A word whose body fits but whose trailing comma would cross the
//     budget is still placed, hanging one column past it (REF's first
//     "…AGY_ADC_AUTH) sign-in." line reaches width-1).
//   - A word that does not fit at all is split after the last hyphen whose
//     prefix still fits; the remainder opens the next line ("sign-" /
//     "in.", "--" / "mode auto", "-" / "p runs").
//   - Every wrapped line except the last of its paragraph is padded with
//     literal spaces out to the budget when the gap is at most 12 columns.
//     The padding sits inside the line's colour run, before the closing
//     \x1b[39m. We emit literal 0x20 bytes (NOT CSI 'b' REP, NOT
//     underscores, NOT NBSP) — ultraviolet's printString accumulates
//     every non-printable sequence into a Width=0 placeholder cell that
//     the renderer drops, so REP leaves only the leading literal space
//     in the cell buffer; tmux's `capture-pane -e` then renders that
//     single trailing cell as one visible space, while REF's 8 literal
//     cells become 8 visible spaces.
//
// Inline `code` spans are styled: the reference emits
//
//	\x1b[38;5;69m\x1b[48;5;254m CODE \x1b[38;5;235m\x1b[49m
//
// (256-colour 69 foreground on a 256-colour 254 background, restored to
// the 235 body foreground and the default background). A span split
// across a wrap boundary is closed at the end of the first line and
// re-opened on the next (`--` / `mode auto`).
//
// Bold (**…**) appears exactly once in the whole document (the 1.0.x
// "terminal" scheme bullet, far below every captured window) and could
// not be measured, so it is stripped to plain text like cycle 207 did and
// flagged 存疑 in the report.
package main

import (
	"strings"

	"github.com/mattn/go-runewidth"
)

// The /changelog panel's palette. These are the byte sequences the
// reference emits in this panel; they are deliberately local rather than
// reusing cInlineCodeOpen, whose byte order (background first) is the one
// the *transcript* renderer uses, not this panel's.
const (
	chgTextOpen   = "\x1b[38;5;235m"
	chgTextClose  = "\x1b[39m"
	chgCodeOpen   = "\x1b[38;5;69m\x1b[48;5;254m"
	chgCodeClose  = "\x1b[38;5;235m\x1b[49m"
	chgTitleRow   = "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m"
	chgHeaderOpen = "  \x1b[1m\x1b[38;5;27m"
	chgHeaderEnd  = "\x1b[0m"
)

// changelogPadLimit is the largest ragged gap (in columns) that the
// reference fills with padding spaces. Measured across the twelve
// captures: 498 of 501 non-final wrapped lines follow it.
const changelogPadLimit = 12

// changelogWrapBudget is the column budget the document wraps to: the
// terminal width minus two.
func changelogWrapBudget(width int) int {
	b := width - 2
	if b < 1 {
		b = 1
	}
	return b
}

// changelogPanelDoc renders the whole /changelog document at a terminal
// width — the badge title, a blank, the tagline, then every `## ` section
// and `- ` bullet of the embedded print-mode markdown, wrapped and styled
// the way the reference lays them out.
//
// The document is rebuilt from printSlashChangelog rather than from the
// cycle-193 changelogBody transcription because the transcription (a)
// dropped the 1.3.6 "Fixed plugin names … case-sensitive" bullet, (b)
// dropped the `�` in the CJK bullet, and (c) had already stripped the
// backticks the inline-code styling needs.
func changelogPanelDoc(width int) []string {
	budget := changelogWrapBudget(width)
	return changelogPanelDocBuild(width, budget)
}

func changelogPanelDocBuild(width, budget int) []string {
	src := strings.Split(printSlashChangelog, "\n")
	out := []string{chgTitleRow, ""}
	if len(src) > 4 {
		out = append(out, changelogWrapParagraph(strings.TrimRight(src[4], "\r"), false, budget)...)
	}
	for i := 5; i < len(src); i++ {
		line := strings.TrimRight(src[i], "\r")
		switch {
		case changelogIsHeader(line):
			out = append(out, chgHeaderOpen+strings.TrimLeft(line, " ")+chgHeaderEnd)
		case strings.HasPrefix(line, "- "):
			out = append(out, changelogWrapParagraph(line[2:], true, budget)...)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// changelogIsHeader reports whether a markdown line is a level-2 heading.
// One line in the embedded changelog (" ## 1.1.4") carries a leading
// space; the reference still renders it as a heading.
func changelogIsHeader(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), "## ")
}

// changelogWrapParagraph wraps one paragraph's markdown text (a bullet's
// body or the tagline) to `budget` and returns the styled visual rows.
func changelogWrapParagraph(text string, bullet bool, budget int) []string {
	plain, code := changelogInline(text)

	type span struct{ lo, hi int }
	var words [][2]int
	for i := 0; i < len(plain); {
		if plain[i] == ' ' {
			i++
			continue
		}
		j := i
		for j < len(plain) && plain[j] != ' ' {
			j++
		}
		words = append(words, [2]int{i, j})
		i = j
	}

	firstWidth, contWidth := 2, 2
	if bullet {
		firstWidth = 4
	}

	var lines []span
	curLo, curHi := -1, -1
	curWidth := 0
	for wi := 0; wi < len(words); wi++ {
		wlo, whi := words[wi][0], words[wi][1]
		if curLo < 0 {
			curLo, curHi = wlo, whi
			curWidth = firstWidth + runesWidth(plain[wlo:whi])
			continue
		}
		coreHi := whi
		if coreHi > wlo && plain[coreHi-1] == ',' {
			coreHi-- // a trailing comma may hang one column past the budget
		}
		if curWidth+1+runesWidth(plain[wlo:coreHi]) > budget {
			split := -1
			for k := wlo; k < whi; k++ {
				if plain[k] == '-' && curWidth+1+runesWidth(plain[wlo:k+1]) <= budget {
					split = k + 1
				}
			}
			if split > wlo && split < whi {
				lines = append(lines, span{curLo, split})
				words[wi][0] = split
				curLo, curHi = -1, -1
				continue
			}
			// The word does not fit and there is no hyphen split point:
			// commit the line so far and start the next line with this
			// word — REF never drops a word, it just pushes the breaker
			// forward to the next whitespace.
			lines = append(lines, span{curLo, curHi})
			curLo, curHi = wlo, whi
			curWidth = contWidth + runesWidth(plain[wlo:whi])
			continue
		}
		curHi = whi
		curWidth += 1 + runesWidth(plain[wlo:whi])
	}
	if curLo >= 0 {
		lines = append(lines, span{curLo, curHi})
	}

	out := make([]string, 0, len(lines))
	for idx, ln := range lines {
		var b strings.Builder
		if idx == 0 {
			b.WriteString("  ")
			b.WriteString(chgTextOpen)
			if bullet {
				b.WriteString("\u2022 ")
			}
		} else {
			b.WriteString("  ")
			b.WriteString(chgTextOpen)
		}
		inCode := false
		for k := ln.lo; k < ln.hi; k++ {
			if code[k] != inCode {
				if code[k] {
					b.WriteString(chgCodeOpen)
				} else {
					b.WriteString(chgCodeClose)
				}
				inCode = code[k]
			}
			b.WriteRune(plain[k])
		}
		if inCode {
			b.WriteString(chgCodeClose)
		}
		// REF pads every non-final paragraph line to the budget when the
		// gap is at most changelogPadLimit columns; single-line paragraphs
		// (the tagline, section heads) are also padded when the body fits
		// the budget and the trailing gap stays under the limit. The pad
		// lives inside the line's colour run, before chgTextClose, and is
		// emitted as N literal 0x20 bytes — uv's diff render clears the
		// trailing fg-coloured spaces with a single \x1b[NX but the cells
		// stay in the curbuf at the new Style, so `capture-pane -e`
		// re-renders them as visible spaces (verified w99 vs REF: 8-cell
		// pad → 8 visible spaces; with the previous CSI-REP path only the
		// leading 1 cell survived the bubbletea→uv paint).
		width := contWidth
		if idx == 0 {
			width = firstWidth
		}
		if gap := budget - (width + runesWidth(plain[ln.lo:ln.hi])); gap > 0 && gap <= changelogPadLimit {
			for i := 0; i < gap; i++ {
				b.WriteByte(' ')
			}
		}
		b.WriteString(chgTextClose)
		out = append(out, b.String())
	}
	return out
}

// changelogInline strips markdown inline markers from a paragraph and
// reports, for each surviving rune, whether it came from inside a
// `code` span. Backticks toggle the span; a `**` bold pair is dropped
// (the one occurrence is off-screen and unmeasured — see the file
// header). A stray single `*` is kept as literal text.
func changelogInline(text string) ([]rune, []bool) {
	runes := []rune(text)
	plain := make([]rune, 0, len(runes))
	code := make([]bool, 0, len(runes))
	inCode := false
	for i := 0; i < len(runes); i++ {
		switch runes[i] {
		case '`':
			inCode = !inCode
		case '*':
			if i+1 < len(runes) && runes[i+1] == '*' {
				i++
				continue
			}
			plain = append(plain, runes[i])
			code = append(code, inCode)
		default:
			plain = append(plain, runes[i])
			code = append(code, inCode)
		}
	}
	return plain, code
}

// runesWidth is the terminal column width of a run of runes.
func runesWidth(rs []rune) int {
	return runewidth.StringWidth(string(rs))
}
