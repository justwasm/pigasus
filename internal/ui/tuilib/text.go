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

// asciiANSIWidth computes the visible cell width of s in a single zero-alloc
// byte pass when s contains only printable ASCII (0x20..0x7e) and CSI escape
// sequences (\x1b[...<0x40..0x7e>). Returns (0, false) for UTF-8 multibyte
// runes, control characters, or non-CSI escapes (e.g. OSC 8 hyperlinks).
func asciiANSIWidth(s string) (int, bool) {
	w := 0
	n := len(s)
	for i := 0; i < n; i++ {
		b := s[i]
		if b == 0x1b {
			if i+1 >= n || s[i+1] != '[' {
				return 0, false
			}
			i += 2
			for i < n && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			if i >= n {
				return 0, false
			}
			continue
		}
		if b < 0x20 || b >= 0x7f {
			return 0, false
		}
		w++
	}
	return w, true
}

// FitWidth clamps one rendered line to w visible cells, padding with spaces
// when it is short. ANSI escape sequences are preserved.
func FitWidth(s string, w int) string {
	if strings.IndexByte(s, '\r') >= 0 {
		s = strings.ReplaceAll(s, "\r", "")
	}
	if w <= 0 {
		return s
	}
	sw, ok := asciiANSIWidth(s)
	if !ok {
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
		// Fast-path: for non-ANSI lines with byte-length <= width, visual width
		// can never exceed byte length (all UTF-8 multibyte runes have width <= byte length).
		if strings.IndexByte(sub, 0x1b) == -1 && len(sub) <= width {
			out = append(out, sub)
			continue
		}
		if sw, ok := asciiANSIWidth(sub); ok {
			if sw <= width {
				out = append(out, sub)
				continue
			}
		} else if ansi.StringWidth(sub) <= width {
			out = append(out, sub)
			continue
		}
		out = append(out, strings.Split(ansi.Hardwrap(sub, width, true), "\n")...)
	}
	return out
}

// PadTo pads or truncates s to w cells.
func PadTo(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if s == "" {
		return strings.Repeat(" ", w)
	}
	sw, ok := asciiANSIWidth(s)
	if !ok {
		sw = ansi.StringWidth(s)
	}
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
	if sw, ok := asciiANSIWidth(s); ok {
		if sw <= w {
			return s
		}
	} else if ansi.StringWidth(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, tail)
}
