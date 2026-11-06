// word_nav.go — smart word and atomic-segment navigation.
//
// Ports smart word boundary navigation and atomic segment jumping from Pi/PiG.
// It recognizes:
//  1. Whitespace runs (spaces, tabs, newlines).
//  2. Word-like alphanumeric & letter sequences (including CJK runs).
//  3. Punctuation and symbol runs (e.g. "...", "://", "/", ":", etc.).
//  4. Indivisible atomic segments (e.g. "[Pasted text +15 lines]").

package composer

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// PunctuationChars defines ASCII punctuation and symbols that separate word tokens.
const PunctuationChars = "(){}[]<>.,;:'\"!?+-=*/\\|&%^$#@~`"

// atomicPlaceholderPattern matches indivisible atomic segments like paste placeholders.
var atomicPlaceholderPattern = regexp.MustCompile(`\[(?:Pasted text|paste)(?:\s+#\d+)?(?:\s+\+\d+\s+lines|\s+\d+\s+chars)?\]`)

type charKind int

const (
	kindWhitespace charKind = iota
	kindPunctuation
	kindWord
	kindOther
)

func classifyRune(r rune) charKind {
	switch {
	case unicode.IsSpace(r):
		return kindWhitespace
	case strings.ContainsRune(PunctuationChars, r) || unicode.IsPunct(r) || unicode.IsSymbol(r):
		return kindPunctuation
	case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_':
		return kindWord
	default:
		return kindOther
	}
}

// atomicRangeBackward returns [start, end, true] if offset sits inside or at the end
// of an atomic segment: offset in (start, end].
func atomicRangeBackward(text string, offset int) (int, int, bool) {
	for _, loc := range atomicPlaceholderPattern.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if offset > start && offset <= end {
			return start, end, true
		}
	}
	return 0, 0, false
}

// atomicRangeForward returns [start, end, true] if offset sits inside or at the start
// of an atomic segment: offset in [start, end).
func atomicRangeForward(text string, offset int) (int, int, bool) {
	for _, loc := range atomicPlaceholderPattern.FindAllStringIndex(text, -1) {
		start, end := loc[0], loc[1]
		if offset >= start && offset < end {
			return start, end, true
		}
	}
	return 0, 0, false
}

// FindWordBackward returns the byte offset when moving backward one word or atomic segment
// from cursorOffset in text.
func FindWordBackward(text string, cursorOffset int) int {
	if cursorOffset <= 0 {
		return 0
	}
	if cursorOffset > len(text) {
		cursorOffset = len(text)
	}

	// If currently inside or at the end of an atomic placeholder, jump to its start.
	if start, _, ok := atomicRangeBackward(text, cursorOffset); ok {
		return start
	}

	// 1. Skip trailing whitespace backward.
	pos := cursorOffset
	for pos > 0 {
		r, size := utf8.DecodeLastRuneInString(text[:pos])
		if !unicode.IsSpace(r) {
			break
		}
		pos -= size
	}
	if pos <= 0 {
		return 0
	}

	// If after skipping whitespace we land inside or at the end of an atomic placeholder, jump to its start.
	if start, _, ok := atomicRangeBackward(text, pos); ok {
		return start
	}

	// 2. Classify the non-whitespace character at pos.
	lastRune, size := utf8.DecodeLastRuneInString(text[:pos])
	targetKind := classifyRune(lastRune)
	pos -= size

	// 3. Consume consecutive characters of the same kind.
	for pos > 0 {
		// Check if we hit an atomic placeholder boundary.
		if _, end, ok := atomicRangeBackward(text, pos); ok && pos == end {
			break
		}
		r, rsize := utf8.DecodeLastRuneInString(text[:pos])
		if classifyRune(r) != targetKind {
			break
		}
		pos -= rsize
	}

	return pos
}

// FindWordForward returns the byte offset when moving forward one word or atomic segment
// from cursorOffset in text.
func FindWordForward(text string, cursorOffset int) int {
	n := len(text)
	if cursorOffset >= n {
		return n
	}
	if cursorOffset < 0 {
		cursorOffset = 0
	}

	// If currently inside or at the start of an atomic placeholder, jump to its end.
	if _, end, ok := atomicRangeForward(text, cursorOffset); ok {
		return end
	}

	// 1. Skip leading whitespace forward.
	pos := cursorOffset
	for pos < n {
		r, size := utf8.DecodeRuneInString(text[pos:])
		if !unicode.IsSpace(r) {
			break
		}
		pos += size
	}
	if pos >= n {
		return n
	}

	// If after skipping whitespace we land inside or at the start of an atomic placeholder, jump to its end.
	if _, end, ok := atomicRangeForward(text, pos); ok {
		return end
	}

	// 2. Classify the non-whitespace character at pos.
	firstRune, size := utf8.DecodeRuneInString(text[pos:])
	targetKind := classifyRune(firstRune)
	pos += size

	// 3. Consume consecutive characters of the same kind.
	for pos < n {
		// Check if we hit an atomic placeholder boundary.
		if start, _, ok := atomicRangeForward(text, pos); ok && pos == start {
			break
		}
		r, rsize := utf8.DecodeRuneInString(text[pos:])
		if classifyRune(r) != targetKind {
			break
		}
		pos += rsize
	}

	return pos
}

// HandleWordLeft moves the cursor backward by one word or atomic segment.
func HandleWordLeft(s Seams) bool {
	val := s.Editor.Value()
	if len(val) == 0 {
		return false
	}
	cur := s.Editor.CursorOffset()
	target := FindWordBackward(val, cur)
	if target == cur {
		return false
	}
	s.MoveCursor(target)
	s.afterEdit()
	return true
}

// HandleWordRight moves the cursor forward by one word or atomic segment.
func HandleWordRight(s Seams) bool {
	val := s.Editor.Value()
	if len(val) == 0 {
		return false
	}
	cur := s.Editor.CursorOffset()
	target := FindWordForward(val, cur)
	if target == cur {
		return false
	}
	s.MoveCursor(target)
	s.afterEdit()
	return true
}
