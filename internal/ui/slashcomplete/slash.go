package slashcomplete

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// Port is the single side effect the completion needs: writing the
// resolved completion text back into the composer input.
//
// The host (internal/tui) satisfies it with a thin adapter over its
// prompt input; tests satisfy it with a closure that records the write.
// A nil Port is tolerated: Handle then computes the result but performs
// no write.
type Port interface {
	// SetCompletion replaces the composer's text with text and is the
	// only mutation this package ever requests.
	SetCompletion(text string)
}

// PortFunc adapts a plain function to the Port interface.
type PortFunc func(text string)

// SetCompletion calls f(text).
func (f PortFunc) SetCompletion(text string) { f(text) }

// Kind classifies the outcome of a completion attempt.
type Kind int

const (
	// NoMatch means the token matches no command in the catalogue.
	NoMatch Kind = iota
	// Empty means the input was empty (no leading slash token); there
	// is nothing to complete.
	Empty
	// Single means exactly one command matched; Text is that command's
	// name.
	Single
	// Multiple means several commands matched; Text is their longest
	// common prefix (which may equal the input when no further
	// characters are shared).
	Multiple
)

// Result is the outcome of Match + Complete.
type Result struct {
	// Kind classifies the outcome.
	Kind Kind
	// Text is the resolved completion text. Empty for Empty and NoMatch.
	Text string
	// Matches is the full set of rows that matched the token.
	Matches []slashcommands.Command
}

// readToken returns the leading slash-prefix token from input: the run
// of non-space characters from the start. It returns "" when the input
// does not begin with '/'.
func readToken(input string) string {
	if input == "" || input[0] != '/' {
		return ""
	}
	end := len(input)
	for i := 0; i < len(input); i++ {
		switch input[i] {
		case ' ', '\t', '\n':
			end = i
			return input[:end]
		}
	}
	return input[:end]
}

// Match returns the catalogue rows whose name matches the leading slash
// token of input. An empty input, or one that does not start with '/',
// yields no rows. Matching is delegated to the scoped catalogue filter
// so availability rules stay owned by slashcommands.
func Match(input string, catalogue []slashcommands.Command) []slashcommands.Command {
	tok := readToken(input)
	if tok == "" {
		return nil
	}
	return filterCatalogue(tok, catalogue)
}

// filterCatalogue applies the prefix filter over an explicit catalogue,
// keeping the slashcommands availability semantics for the built-in
// default catalogue and a plain case-insensitive prefix match for a
// caller-supplied one.
func filterCatalogue(prefix string, catalogue []slashcommands.Command) []slashcommands.Command {
	if catalogue == nil {
		return slashcommands.FilterScoped(prefix, slashcommands.ScopeContext{})
	}
	out := make([]slashcommands.Command, 0, len(catalogue))
	for _, c := range catalogue {
		if prefix == "" || containsFold(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

// containsFold reports whether s contains substr, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// longestCommonPrefix returns the longest string that is a prefix of
// every name. It returns "" for an empty set.
func longestCommonPrefix(names []string) string {
	if len(names) == 0 {
		return ""
	}
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) {
			prefix = prefix[:len(prefix)-1]
			if prefix == "" {
				return ""
			}
		}
	}
	return prefix
}

// Complete resolves the completion for input against catalogue. It runs
// Match and reduces the matched rows to a single Kind + Text:
//
//   - empty input (no leading slash token) -> Empty
//   - no matching rows                     -> NoMatch
//   - exactly one matching row             -> Single (that row's name)
//   - several matching rows                -> Multiple (their longest
//     common prefix)
func Complete(input string, catalogue []slashcommands.Command) Result {
	matches := Match(input, catalogue)
	switch {
	case readToken(input) == "":
		return Result{Kind: Empty}
	case len(matches) == 0:
		return Result{Kind: NoMatch}
	case len(matches) == 1:
		return Result{Kind: Single, Text: matches[0].Name, Matches: matches}
	default:
		names := make([]string, len(matches))
		for i, m := range matches {
			names[i] = m.Name
		}
		return Result{Kind: Multiple, Text: longestCommonPrefix(names), Matches: matches}
	}
}

// Handle runs Complete and, when the result carries text, writes it back
// through port. It returns the Result so callers that only need the
// classification (and no write) can ignore the port.
//
// A nil port is allowed: the result is still computed, the write is
// simply skipped. This keeps the matching logic usable in pure tests.
func Handle(input string, catalogue []slashcommands.Command, port Port) Result {
	res := Complete(input, catalogue)
	if port != nil && res.Text != "" {
		port.SetCompletion(res.Text)
	}
	return res
}
