// placeholder.go — the pasted-text placeholder.
//
// A pasted block of text is reduced to a placeholder in the prompt:
// "[Pasted text #3]", "[Pasted text #3 +12 lines]" or
// "[Pasted text #3 120 chars]". The placeholder is one unit: a
// backspace or a delete that lands inside it removes the whole
// placeholder, so a pasted block is undone in a single key.
//
// The scan is a pure text concern — it has no home of its own in the
// host, and it does not need one.

package composer

import "regexp"

// pastePlaceholderPattern matches the placeholder a pasted block of
// text is reduced to.
var pastePlaceholderPattern = regexp.MustCompile(`\[Pasted text #\d+(?:\s+\+\d+\s+lines|\s+\d+\s+chars)?\]`)

// placeholderAt returns the byte range of the pasted-text placeholder
// the cursor is in, and whether there is one.
//
// A backspace removes the placeholder when the cursor sits anywhere
// after its first byte — inside it, or just past its end, which is
// where the cursor sits after the user typed the last character of
// the placeholder. A delete removes it when the cursor sits before
// its last byte.
//
// The first placeholder the cursor is in is the one that goes.
func placeholderAt(value string, offset int, backspace bool) (start, end int, ok bool) {
	for _, loc := range pastePlaceholderPattern.FindAllStringIndex(value, -1) {
		start, end = loc[0], loc[1]
		if backspace {
			if offset > start && offset <= end {
				return start, end, true
			}
			continue
		}
		if offset >= start && offset < end {
			return start, end, true
		}
	}
	return 0, 0, false
}
