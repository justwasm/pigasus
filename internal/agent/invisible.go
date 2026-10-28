// Package agent — invisible.go: the two interlinear-annotation
// characters that must never reach the terminal.
//
// U+FFF9 (INTERLINEAR ANNOTATION ANCHOR) and U+FFFA (INTERLINEAR
// ANNOTATION SEPARATOR) are the Unicode "interlinear annotation" pair.
// They have no glyph of their own, so a reader never sees them — but
// they are not whitespace either, and a terminal or a multiplexer
// that draws a box for an unrenderable code point leaves a stray
// frame on the screen.
//
// 【一手】 agy 1.4.9, the vendor's own release notes: "Fixed invisible
// U+FFF9 and U+FFFA characters reaching the terminal around wrapped
// code in agent replies, which made some terminals and multiplexers
// draw boxes or leave stale characters on screen."
//
// The characters arrive in the model's text (they ride along with a
// wrapped code block), so they are dropped where the reply text is
// sanitised — the same place the `<think>` blocks are dropped, and
// therefore on both the interactive and the print path.
package agent

import "strings"

// invisibleAnchor and invisibleSeparator are the two code points the
// reference strips, and their UTF-8 encodings.
//
//	U+FFF9 → EF BF B9
//	U+FFFA → EF BF BA
const (
	invisibleAnchor    = '\ufff9'
	invisibleSeparator = '\ufffa'

	invisibleAnchorSeq    = "\xef\xbf\xb9"
	invisibleSeparatorSeq = "\xef\xbf\xba"
)

// stripInvisible removes every U+FFF9 and U+FFFA from s.
//
// It also returns the trailing bytes it held back because they could be
// the *start* of one of the two sequences: the reply text arrives in
// chunks, and a chunk can end in the middle of a three-byte sequence.
// The held-back bytes must be prepended to the next chunk, otherwise
// the halves would reach the terminal as two broken runes.
//
// The two sequences are the only ones the reference drops; every other
// byte — including a lone EF or BF that is not part of one of them —
// is left alone.
func stripInvisible(s string) (string, string) {
	hold := trailingPartialInvisible(s)
	held := ""
	if hold > 0 {
		held = s[len(s)-hold:]
		s = s[:len(s)-hold]
	}
	if !strings.Contains(s, invisibleAnchorSeq) && !strings.Contains(s, invisibleSeparatorSeq) {
		return s, held
	}
	var out strings.Builder
	out.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] == 0xef && i+2 < len(s) && s[i+1] == 0xbf &&
			(s[i+2] == 0xb9 || s[i+2] == 0xba) {
			i += 3
			continue
		}
		out.WriteByte(s[i])
		i++
	}
	return out.String(), held
}

// trailingPartialInvisible returns the length of the trailing byte
// sequence that could be the start of one of the two invisible
// characters: 2 for a trailing "\xef\xbf", 1 for a trailing "\xef", and
// 0 when the tail cannot start one.
//
// A trailing "\xbf" alone is not a prefix of either sequence (both
// start with "\xef"), so it is never held back.
func trailingPartialInvisible(s string) int {
	if len(s) >= 2 && s[len(s)-2] == 0xef && s[len(s)-1] == 0xbf {
		return 2
	}
	if len(s) >= 1 && s[len(s)-1] == 0xef {
		return 1
	}
	return 0
}
