package changelog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/mattn/go-runewidth"
)

// PadLimit is the largest ragged gap (in columns) that the reference fills
// with padding spaces.
const PadLimit = 12

// WrapBudget is the column budget the document wraps to: width - 2.
func WrapBudget(width int) int {
	b := width - 2
	if b < 1 {
		b = 1
	}
	return b
}

// PanelDoc renders the whole /changelog document at a terminal width.
func PanelDoc(width int) []string {
	budget := WrapBudget(width)
	return PanelDocBuild(width, budget)
}

// PanelDocBuild builds the changelog document for a specific budget.
func PanelDocBuild(width, budget int) []string {
	src := strings.Split(printmode.PrintSlashChangelog, "\n")
	out := []string{TitleRow, ""}
	if len(src) > 4 {
		out = append(out, WrapParagraph(strings.TrimRight(src[4], "\r"), false, budget)...)
	}
	for i := 5; i < len(src); i++ {
		line := strings.TrimRight(src[i], "\r")
		switch {
		case IsHeader(line):
			out = append(out, HeaderOpen+strings.TrimLeft(line, " ")+HeaderEnd)
		case strings.HasPrefix(line, "- "):
			out = append(out, WrapParagraph(line[2:], true, budget)...)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// BuildRestRows renders the remainder of the release notes (1.3.5 down
// through 1.0.0) in the panel's bullet style. It is parsed once from the
// embedded print-mode markdown (printmode.PrintSlashChangelog); the
// print-mode blob deliberately omits the 1.3.6 section that the excerpt
// already covers, so the two concatenate without overlap.
//
// The rows are the *unwrapped* transcription: one row per bullet, which
// is the width-0 fallback shape.
func BuildRestRows() []string {
	var out []string
	headers := 0
	for _, raw := range strings.Split(printmode.PrintSlashChangelog, "\n") {
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
				"  \x1b[38;5;235m• "+PlainInline(line[2:])+"\x1b[39m")
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// IsHeader reports whether a markdown line is a level-2 heading.
func IsHeader(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " "), "## ")
}

type span struct{ lo, hi int }

func findWords(plain []rune) [][2]int {
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
	return words
}

func layoutLines(plain []rune, words [][2]int, firstWidth, contWidth, budget int) []span {
	var lines []span
	curLo, curHi := -1, -1
	curWidth := 0
	for wi := 0; wi < len(words); wi++ {
		wlo, whi := words[wi][0], words[wi][1]
		if curLo < 0 {
			curLo, curHi = wlo, whi
			curWidth = firstWidth + RunesWidth(plain[wlo:whi])
			continue
		}
		coreHi := whi
		if coreHi > wlo && plain[coreHi-1] == ',' {
			coreHi--
		}
		if curWidth+1+RunesWidth(plain[wlo:coreHi]) > budget {
			split := -1
			for k := wlo; k < whi; k++ {
				if plain[k] == '-' && curWidth+1+RunesWidth(plain[wlo:k+1]) <= budget {
					split = k + 1
				}
			}
			if split > wlo && split < whi {
				lines = append(lines, span{curLo, split})
				words[wi][0] = split
				curLo, curHi = -1, -1
				continue
			}
			lines = append(lines, span{curLo, curHi})
			curLo, curHi = wlo, whi
			curWidth = contWidth + RunesWidth(plain[wlo:whi])
			continue
		}
		curHi = whi
		curWidth += 1 + RunesWidth(plain[wlo:whi])
	}
	if curLo >= 0 {
		lines = append(lines, span{curLo, curHi})
	}
	return lines
}

func renderLine(plain []rune, code []bool, ln span, idx int, bullet bool, firstWidth, contWidth, budget int) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(TextOpen)
	if idx == 0 && bullet {
		b.WriteString("\u2022 ")
	}
	inCode := false
	for k := ln.lo; k < ln.hi; k++ {
		if code[k] != inCode {
			if code[k] {
				b.WriteString(CodeOpen)
			} else {
				b.WriteString(CodeClose)
			}
			inCode = code[k]
		}
		b.WriteRune(plain[k])
	}
	if inCode {
		b.WriteString(CodeClose)
	}
	width := contWidth
	if idx == 0 {
		width = firstWidth
	}
	if gap := budget - (width + RunesWidth(plain[ln.lo:ln.hi])); gap > 0 && gap <= PadLimit {
		for i := 0; i < gap; i++ {
			b.WriteByte(' ')
		}
	}
	b.WriteString(TextClose)
	return b.String()
}

// WrapParagraph wraps one paragraph's markdown text to budget.
func WrapParagraph(text string, bullet bool, budget int) []string {
	plain, code := inline(text)
	words := findWords(plain)

	firstWidth, contWidth := 2, 2
	if bullet {
		firstWidth = 4
	}

	lines := layoutLines(plain, words, firstWidth, contWidth, budget)
	out := make([]string, 0, len(lines))
	for idx, ln := range lines {
		out = append(out, renderLine(plain, code, ln, idx, bullet, firstWidth, contWidth, budget))
	}
	return out
}

func inline(text string) ([]rune, []bool) {
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

// RunesWidth is the terminal column width of a run of runes.
func RunesWidth(rs []rune) int {
	return runewidth.StringWidth(string(rs))
}
