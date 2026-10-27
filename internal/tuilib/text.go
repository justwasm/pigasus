package tuilib

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// ViewRows counts the displayed rows in a string.
func ViewRows(s string) int {
	if s == "" {
		return 0
	}
	n := strings.Count(s, "\n")
	if !strings.HasSuffix(s, "\n") {
		n++
	}
	return n
}

// FitWidth clamps one rendered line to w visible cells, padding with spaces
// when it is short. ANSI escape sequences are preserved.
func FitWidth(s string, w int) string {
	if strings.ContainsRune(s, '\r') {
		s = strings.ReplaceAll(s, "\r", "")
	}
	if w <= 0 {
		return s
	}
	switch sw := ansi.StringWidth(s); {
	case sw > w:
		return ansi.Truncate(s, w, "")
	case sw < w:
		return s + "\x1b[38;5;235m" + strings.Repeat(" ", w-sw) + "\x1b[39m"
	}
	return s
}

// FitLines clamps every line of a rendered block to w cells.
func FitLines(s string, w int) string {
	if w <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	last := len(lines) - 1
	for i, line := range lines {
		if i == last && line == "" {
			continue
		}
		lines[i] = FitWidth(line, w)
	}
	return strings.Join(lines, "\n")
}

// WrapRows soft-wraps one rendered line into terminal rows no wider than width.
//
// The wrap is charmbracelet/x/ansi's hard wrap — the library the
// reference's own binary embeds. Two things it does that the
// hand-rolled cell index did not:
//
//   - it never cuts a grapheme cluster in half. The cell index cut at
//     the cell boundary, so a wide character that straddled it lost
//     half of itself.
//   - it keeps the ANSI escape sequences where they are, and it counts
//     the cells the way the rest of this package counts them (the
//     library's grapheme width, not the rune width).
func WrapRows(line string, width int) []string {
	if width <= 0 {
		if line == "" {
			return []string{""}
		}
		return strings.Split(line, "\n")
	}
	out := make([]string, 0, strings.Count(line, "\n")+1)
	for _, sub := range strings.Split(line, "\n") {
		if sub == "" {
			out = append(out, "")
			continue
		}
		if ansi.StringWidth(sub) <= width {
			out = append(out, sub)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(sub, width, true), "\n")...)
	}
	return out
}

// CellByteIndex returns idx where idx[c] is the byte offset of the byte just
// past the c-th visible cell.
//
// The cells are the library's: charmbracelet/x/ansi measures a string in
// grapheme clusters, so a combining mark or an emoji sequence counts as the
// one cluster it renders as.
func CellByteIndex(s string) []int {
	idx := []int{0}
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			i = SkipEscape(s, i)
			continue
		}
		cluster, width := ansi.FirstGraphemeCluster(s[i:], ansi.GraphemeWidth)
		if len(cluster) == 0 {
			i++
			continue
		}
		i += len(cluster)
		for k := 0; k < width; k++ {
			idx = append(idx, i)
		}
	}
	if last := idx[len(idx)-1]; last != len(s) {
		idx = append(idx, len(s))
	}
	return idx
}

// PadTo pads or truncates s to w cells.
func PadTo(s string, w int) string {
	sw := ansi.StringWidth(s)
	if sw > w {
		return ansi.Truncate(s, w, "")
	}
	if sw < w {
		return s + strings.Repeat(" ", w-sw)
	}
	return s
}

// ClampInt clamps v between lo and hi.
func ClampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
