// Package textwrap wraps a line of text into rows of terminal cells.
//
// The wrap is charmbracelet/x/ansi's word wrap — the library the
// reference's own binary embeds: its seeds carry
// charmbracelet/x/ansi's Wordwrap (out/seeds/funcs.txt).
//
// The reference's own rule is the one the helper keeps: a word that is
// wider than the budget is emitted whole. It does not hyphenate and it
// does not cut a word in half ("the reference does not hyphenate" — the
// clone's own test for the primitive says so).
package textwrap

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Words wraps s into rows of at most width terminal cells.
//
// The rows break on word boundaries. A word that does not fit on a row
// of its own is emitted whole.
//
// An empty s gives one empty row. A budget of one cell or less puts
// one word per row: the library does not break at a one-cell budget (a
// word that is one cell wide is not narrower than the limit, so it
// never starts a new row), and the clone's own primitive does — its
// cycle-204 test pins that.
func Words(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	if width <= 1 {
		return strings.Split(s, " ")
	}
	return strings.Split(ansi.Wordwrap(s, width, ""), "\n")
}

// Break wraps s into rows of at most width terminal cells, the way
// Words does, but a token that is wider than the budget is broken at
// the budget's boundary instead of being emitted whole.
//
// This is the table cell's need: a shrunk column whose data is one
// long URL would overflow the column if the URL were left whole
// (cycle 218's finding for the GFM pipe tables).
//
// The rows break on whitespace and the whitespace is normalised to a
// single space — a table cell is a column of text, not a paragraph.
//
// An empty s gives one empty row. A budget of zero or less leaves the
// text alone.
func Break(s string, width int) []string {
	if s == "" {
		return []string{""}
	}
	if width < 1 {
		return []string{s}
	}
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	return strings.Split(ansi.Wrap(strings.Join(words, " "), width, ""), "\n")
}
