package slashparse

import "strings"

// Composed-command scanning.
//
// The reference parses a prompt for slash commands anywhere in the text, not
// only in a leading run, and it expands each expandable command into its own
// @-mention block rather than concatenating the bodies. Two rules, both
// recovered from ~50 live agy 1.5.18 captures (`-p` through the wirealign
// capture proxy):
//
//  1. Leading run. Consecutive whitespace-delimited tokens from the start of
//     the text that name an *expandable* command each contribute one mention,
//     in order. The first token that is not an expandable command — whether
//     it is a non-expandable command (/plan, /boost, /teamwork) or ordinary
//     text — ends the run.
//
//  2. Trailing append. In the text *after* the leading run, the reference
//     finds the LAST token that names an expandable command and appends that
//     command's name, with no separator, to the end of the user text, then
//     expands it too. Expandable commands in the middle of the remaining text
//     produce nothing.
//
// Rule 2 is a quirk, not a design: `x /goal y` reaches the model as
// `x /goal y/goal`. We reproduce it byte for byte (the project's parity
// stance is to copy the reference's behaviour, quirks included, and isolate
// it in one place), but the function says so here so nobody mistakes it for
// intentional grammar.
//
// The bodies themselves are not this package's business: the caller supplies
// the expandable predicate and the command→body map, because the bodies live
// in the app tier.

// composedToken is one whitespace-delimited token of the prompt.
type composedToken struct {
	start int
	end   int
	name  string // command name without the leading slash; "" when not a command
	slash bool   // true when the token begins with '/'
}

// isSep reports whether b separates tokens. The set is space, tab, newline
// and carriage return — the intersection of the interactive rule (Rule A:
// space, tab, newline) and the print-mode rule (Rule B: every Unicode
// whitespace rune). Every capture that exercised a separator used one of
// these four.
func isSep(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r'
}

// scanComposedTokens splits text on the separator set and records, for each
// token, whether it begins with '/'.
func scanComposedTokens(text string) []composedToken {
	var out []composedToken
	i := 0
	for i < len(text) {
		for i < len(text) && isSep(text[i]) {
			i++
		}
		if i >= len(text) {
			break
		}
		start := i
		for i < len(text) && !isSep(text[i]) {
			i++
		}
		tok := composedToken{start: start, end: i}
		if text[start] == '/' && i-start > 1 {
			tok.slash = true
			tok.name = text[start+1 : i]
		}
		out = append(out, tok)
	}
	return out
}

// ExpandComposed applies the reference's composed-command rule to text.
//
// It returns the text the model should see — text itself, plus the trailing
// append of rule 2 when applicable — and the expandable command names to
// expand, in the order the reference expands them (leading run first, then
// the trailing one).
//
// expandable reports whether a bare command name (no leading slash) expands
// into a mention. Names the predicate rejects never appear in the result and
// never terminate the leading run differently from ordinary text.
func ExpandComposed(text string, expandable func(name string) bool) (rewritten string, names []string) {
	trimmed := strings.TrimSpace(text)
	toks := scanComposedTokens(trimmed)

	// Rule 1: the leading run. The reference re-serialises it — the commands
	// it recognised come back joined by single spaces, whatever the original
	// separators were (`/goal\tx` reaches the wire as `/goal x`) — while the
	// tail after the run keeps its own whitespace (`/goal  x  /learn` reaches
	// the wire as `/goal x  /learn/learn`). Both shapes are captured.
	consumed := 0
	lastEnd := 0
	for _, t := range toks {
		if !t.slash || !expandable(t.name) {
			break
		}
		names = append(names, t.name)
		consumed++
		lastEnd = t.end
	}
	if consumed > 0 {
		run := make([]string, 0, consumed)
		for _, n := range names {
			run = append(run, "/"+n)
		}
		rebuilt := strings.Join(run, " ")
		if rest := strings.TrimSpace(trimmed[lastEnd:]); rest != "" {
			rebuilt += " " + rest
		}
		trimmed = rebuilt
	}

	// Rule 2: the last expandable command after the leading run, appended to
	// the text with no separator. This is the reference's quirk, reproduced
	// deliberately — see the file comment.
	tail := scanComposedTokens(trimmed)
	for i := len(tail) - 1; i >= consumed; i-- {
		t := tail[i]
		if t.slash && expandable(t.name) {
			names = append(names, t.name)
			return trimmed + "/" + t.name, names
		}
	}

	if consumed == 0 {
		return text, names
	}
	return trimmed, names
}
