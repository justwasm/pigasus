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

func isPureASCIIPrintable(s string) bool {
	for i := 0; i < len(s); i++ {
		b := s[i]
		if b < 0x20 || b >= 0x7f {
			return false
		}
	}
	return true
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
	var sw int
	if isPureASCIIPrintable(s) {
		sw = len(s)
	} else {
		sw = ansi.StringWidth(s)
	}
	switch {
	case sw > w:
		return ansi.Truncate(s, w, "")
	case sw < w:
		return s + "\x1b[38;5;235m" + strings.Repeat(" ", w-sw) + "\x1b[39m"
	}
	return s
}

// FitLines clamps every line of a rendered block to w cells.
func FitLines(s string, w int) string {
	if w <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	last := len(lines) - 1
	changed := false
	for i, line := range lines {
		if i == last && line == "" {
			continue
		}
		fitted := FitWidth(line, w)
		if fitted != line {
			lines[i] = fitted
			changed = true
		}
	}
	if !changed {
		return s
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
	if lo > hi {
		lo, hi = hi, lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// Truncate clamps s to at most w visible cells, appending tail if truncated.
// Preserves ANSI escapes and grapheme clusters.
func Truncate(s string, w int, tail string) string {
	if w <= 0 {
		return ""
	}
	if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, tail)
}
