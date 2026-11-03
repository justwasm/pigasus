package codesearch

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/justwasm/pigasus/palette"
)

// AppendFileHits appends one Hit per matching line of content, up to maxHits.
func AppendFileHits(hits []Hit, rel, content string, re *regexp.Regexp, maxHits int) []Hit {
	lines := strings.Split(content, "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	for i, line := range lines {
		loc := re.FindStringIndex(line)
		if loc == nil {
			continue
		}
		line = strings.TrimRight(line, "\r")
		if loc[1] > len(line) {
			loc[1] = len(line)
		}
		h := Hit{
			File: rel,
			Line: i + 1,
			Col:  utf8.RuneCountInString(line[:loc[0]]) + 1,
			Text: line,
			MS:   utf8.RuneCountInString(line[:loc[0]]),
			ME:   utf8.RuneCountInString(line[:loc[1]]),
		}
		if i > 0 {
			h.Prev = strings.TrimRight(lines[i-1], "\r")
			h.HasPrev = true
		}
		if i+1 < len(lines) {
			h.Next = strings.TrimRight(lines[i+1], "\r")
			h.HasNext = true
		}
		hits = append(hits, h)
		if maxHits > 0 && len(hits) >= maxHits {
			break
		}
	}
	return hits
}

// BlockRows renders context lines around the matching line.
func BlockRows(h Hit, cursor, width int) []string {
	type ctxLine struct {
		n    int
		text string
	}
	body := make([]ctxLine, 0, 3)
	if h.HasPrev {
		body = append(body, ctxLine{h.Line - 1, h.Prev})
	}
	body = append(body, ctxLine{h.Line, h.Text})
	if h.HasNext {
		body = append(body, ctxLine{h.Line + 1, h.Next})
	}
	out := make([]string, 0, len(body))
	for _, l := range body {
		isMatch := 0
		if l.n == h.Line {
			isMatch = 1
		}
		out = append(out, Row(l.n, l.text, h.MS, h.ME, isMatch, cursor&isMatch, width))
	}
	return out
}

// ContentWidth computes content cells available after the line-number gutter.
func ContentWidth(width int, gutter string) int {
	if width <= WidthInset {
		return 0
	}
	w := width - WidthInset - utf8.RuneCountInString(gutter)
	if w < 1 {
		w = 1
	}
	return w
}

// markerCells is the width of the gutter marker column: the marker
// itself and the space behind it, or three spaces when the line carries
// no marker.
const markerCells = 3

// Row renders one numbered line (context or match with cursor/highlighting).
//
// The cursor arrow is the cursor's own: a line that carries no match can
// still be the cursor's line, and the arrow marks it.
func Row(n int, line string, ms, me int, isMatch, isCursor, width int) string {
	return renderRow(n, line, ms, me, isMatch, isCursor, width, "", 0)
}

// MarkedRow renders one numbered line with a marker column between the
// cursor arrow and the line number.
//
// The marker column is three cells wide: the marker and a space behind
// it, or three spaces when the line carries no marker. The marker is the
// reference's 💬 for a line that carries a comment
// (official-docs/cli/commands/codesearch.md:86).
func MarkedRow(n int, line string, ms, me int, isMatch, isCursor int, width int, marker string) string {
	return renderRow(n, line, ms, me, isMatch, isCursor, width, marker, markerCells)
}

// renderRow renders one numbered line.
//
// The cells are the width the marker column takes out of the row's own
// width; a row without a marker column spends none.
func renderRow(n int, line string, ms, me int, isMatch, isCursor int, width int, marker string, cells int) string {
	gutter := fmt.Sprintf("L%d: ", n)
	cw := ContentWidth(width-cells, gutter)
	column := ""
	if cells > 0 {
		column = "   "
		if marker != "" {
			column = marker + " "
		}
	}
	var b strings.Builder
	if isCursor != 0 {
		b.WriteString(palette.BlueOpen)
		b.WriteString("> ")
	} else {
		b.WriteString("  ")
	}
	b.WriteString(column)
	if isMatch == 0 {
		body, _, _ := Fit(line, 0, 0, cw)
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(gutter)
		b.WriteString(body)
		b.WriteString(palette.DimGrayClose)
		return b.String()
	}
	body, s, e := Fit(line, ms, me, cw)
	r := []rune(body)
	b.WriteString(gutter)
	if s > 0 {
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(string(r[:s]))
	}
	b.WriteString(palette.BlueOpen)
	b.WriteString(string(r[s:e]))
	if e < len(r) {
		b.WriteString(palette.DimGrayOpen)
		b.WriteString(string(r[e:]))
	}
	b.WriteString(palette.DimGrayClose)
	return b.String()
}

// Fit shortens a line to w cells around its match, marking elided sides with ellipsis.
func Fit(text string, ms, me, w int) (string, int, int) {
	if w <= 0 {
		return text, ms, me
	}
	runes := []rune(text)
	if utf8.RuneCountInString(text) <= w {
		return text, ms, me
	}
	matchLen := me - ms
	if matchLen > w {
		body := Truncate(text, w)
		return body, 0, 0
	}
	avail := w - 2 - matchLen
	if avail < 0 {
		avail = 0
	}
	left := avail / 2
	right := avail - left
	lo := ms - left
	if lo < 0 {
		lo = 0
	}
	hi := me + right
	if hi > len(runes) {
		hi = len(runes)
	}
	prefix, suffix := "", ""
	if lo > 0 {
		prefix = "…"
	}
	if hi < len(runes) {
		suffix = "…"
	}
	return prefix + string(runes[lo:hi]) + suffix,
		utf8.RuneCountInString(prefix) + (ms - lo),
		utf8.RuneCountInString(prefix) + (me - lo)
}
