// Package plural renders a count with its singular or plural word.
//
// The rule is the reference's own: "1 file", never "1 files". It shows
// up in four places — the code-search panel's "Found N results" line,
// the tool-detail panel's "Read N lines", the group summary's category
// counts and the /codesearch runner's status line — and each of them
// wrote its own copy of the two-line rule.
//
// The copy in a UI package cannot be reached from a *feature* package:
// the features tier sits below the UI tier, so a feature that needs the
// rule would have to import a view. The rule is pure text formatting
// with no dependency at all, so it belongs in tier 1 next to the other
// contracts the whole tree shares.
package plural

import "fmt"

// CountLabel renders "<n> <word>", using the singular form when n is 1
// ("1 file", never "1 files"). The word pair is {singular, plural}.
func CountLabel(n int, word [2]string) string {
	if n == 1 {
		return "1 " + word[0]
	}
	return fmt.Sprintf("%d %s", n, word[1])
}
