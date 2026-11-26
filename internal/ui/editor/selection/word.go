// word.go — the composer's word boundaries for double-click selection.
//
// A double-click selects the word under the pointer; a double-click-drag
// extends that selection a whole word at a time. Both need the same
// answer: "where does the word at this byte offset begin and end?".
//
// The classification is the composer's own, and it is deliberately the
// same three-way split its word motions already use (internal/ui/editor/
// composer/word_nav.go): runs of whitespace, runs of word characters
// (letters, digits, underscore — CJK included, since those are letters)
// and runs of everything else, so a double-click on "://" selects the
// punctuation run rather than a single character.
//
// The scan is host-free and byte-exact: it never splits a UTF-8 rune,
// so a double-click in the middle of "世界" selects both runes of the
// one word.
package selection

import (
	"unicode"
	"unicode/utf8"
)

// wordClass is the kind of run of runes a word boundary is scanned over.
type wordClass uint8

const (
	classSpace wordClass = iota
	classWord
	classOther
)

// classifyWordRune is the three-way split the word scan walks.
//
// It mirrors composer.classifyRune: whitespace is its own run, letters,
// digits and the underscore are word runes, and everything else —
// punctuation, symbols, CJK punctuation, emoji — is a single "other"
// run. The two packages cannot share the function (composer sits above
// this module), so the rule is restated here with the same cases.
func classifyWordRune(r rune) wordClass {
	switch {
	case unicode.IsSpace(r):
		return classSpace
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return classWord
	default:
		return classOther
	}
}

// WordBounds returns the byte range [start, end) of the word at offset
// in value.
//
// The offset is clamped into the value. An offset at a rune boundary is
// the rune that starts there; an offset sitting on the end of the value
// (or on a newline) reads the rune before it, so a double-click just
// past the last character still selects the word it follows — the same
// forgiving reading a terminal gives a click on the right edge.
//
// An empty value, or an offset that lands on a newline, yields the
// empty range [offset, offset): there is no word there, and the caller
// keeps the plain selection the click already made.
func WordBounds(value string, offset int) (start, end int) {
	if value == "" {
		return 0, 0
	}
	if offset < 0 {
		offset = 0
	}
	if offset > len(value) {
		offset = len(value)
	}
	// The rune the click names: the one starting at offset, or the one
	// before it when offset is at the end or on a line break. An offset
	// that lands mid-rune snaps back to that rune's start, so a click is
	// never read as half a character.
	at := offset
	if at == len(value) || value[at] == '\n' || !utf8.RuneStart(value[at]) {
		if at == 0 {
			return 0, 0
		}
		at--
		for at > 0 && !utf8.RuneStart(value[at]) {
			at--
		}
	}
	r, _ := utf8.DecodeRuneInString(value[at:])
	if r == '\n' {
		return offset, offset
	}
	class := classifyWordRune(r)

	start = at
	for start > 0 {
		prev, size := utf8.DecodeLastRuneInString(value[:start])
		if prev == '\n' || classifyWordRune(prev) != class {
			break
		}
		start -= size
	}
	end = at
	for end < len(value) {
		next, size := utf8.DecodeRuneInString(value[end:])
		if next == '\n' || classifyWordRune(next) != class {
			break
		}
		end += size
	}
	// A whitespace run is not a word: a double-click in the gap between
	// two words leaves the plain caret position alone.
	if class == classSpace {
		return offset, offset
	}
	return start, end
}
