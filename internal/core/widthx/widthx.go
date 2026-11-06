// Package widthx provides ANSI-aware and Unicode-aware display width, line wrapping,
// and truncation operations for terminal user interfaces.
package widthx

import (
	"strings"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// ExtractAnsiCode returns the ANSI escape code starting at byte offset i in s,
// and its byte length. Returns ("", 0) if no escape code begins at i.
//
// Supports:
//   - CSI sequences: ESC [ ... [A-Za-z]
//   - OSC sequences: ESC ] ... (BEL or ESC \)
//   - APC / PM / SOS sequences: ESC _ / ESC ^ / ESC X ... (BEL or ESC \)
//   - 2-byte escape sequences: ESC [A-Za-z0-9]
func ExtractAnsiCode(s string, i int) (string, int) {
	if i < 0 || i >= len(s) || s[i] != 0x1B {
		return "", 0
	}
	if i+1 >= len(s) {
		return s[i:], 1
	}

	c2 := s[i+1]
	// 1. CSI: ESC [ ...
	if c2 == '[' {
		j := i + 2
		for j < len(s) {
			b := s[j]
			// Final byte of CSI is in range 0x40-0x7E (@ through ~)
			if b >= 0x40 && b <= 0x7E {
				return s[i : j+1], j + 1 - i
			}
			j++
		}
		return s[i:], len(s) - i
	}

	// 2. OSC: ESC ] or APC: ESC _ or PM: ESC ^
	if c2 == ']' || c2 == '_' || c2 == '^' || c2 == 'X' {
		j := i + 2
		for j < len(s) {
			if s[j] == 0x07 { // BEL terminator
				return s[i : j+1], j + 1 - i
			}
			if s[j] == 0x1B && j+1 < len(s) && s[j+1] == '\\' { // ST (String Terminator)
				return s[i : j+2], j + 2 - i
			}
			j++
		}
		return s[i:], len(s) - i
	}

	// 3. Simple two-byte escapes (e.g. ESC M, ESC E, etc.)
	return s[i : i+2], 2
}

// ExtractAnsi returns the byte length of the ANSI escape code starting at byte offset i in s,
// or 0 if none begins there.
func ExtractAnsi(s string, i int) int {
	_, n := ExtractAnsiCode(s, i)
	return n
}

// StripAnsi removes recognized ANSI, CSI, OSC, and APC escape sequences from s.
func StripAnsi(s string) string {
	if !strings.ContainsRune(s, 0x1B) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if n := ExtractAnsi(s, i); n > 0 {
			i += n
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// RuneWidth returns the display column width of rune r.
func RuneWidth(r rune) int {
	return runewidth.RuneWidth(r)
}

// VisibleWidth returns the display column width of s in the terminal:
//   - Strips ANSI/CSI/OSC sequences before measuring
//   - Expands tabs to 3 columns (matching terminal default)
//   - Accounts for CJK fullwidth characters and wide emojis
func VisibleWidth(s string) int {
	if s == "" {
		return 0
	}
	// Fast path for plain printable ASCII
	if isPrintableASCII(s) {
		return len(s)
	}

	width := 0
	i := 0
	for i < len(s) {
		if s[i] == 0x1B {
			if n := ExtractAnsi(s, i); n > 0 {
				i += n
				continue
			}
		}
		if s[i] == '\t' {
			width += 3
			i++
			continue
		}
		if s[i] < 0x80 {
			if s[i] >= 0x20 && s[i] <= 0x7E {
				width++
			}
			i++
			continue
		}

		r, size := utf8.DecodeRuneInString(s[i:])
		if size > 0 {
			width += runewidth.RuneWidth(r)
			i += size
		} else {
			i++
		}
	}
	return width
}

func isPrintableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c > 0x7E {
			return false
		}
	}
	return true
}
