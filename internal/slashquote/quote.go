// Package slashquote implements the /quote slash command logic.
//
// AGY 1.2.13 changelog:
//
//	"Fixed /quote (quote a previous turn) inserting a trailing newline
//	 before the user submits, so quoting 'line1' actually inserted
//	 'line1\n' and looked like an extra blank line at the top of the
//	 prompt."
package slashquote

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// InsertText prepares a previous turn's content for verbatim
// insertion: any trailing '\n' bytes are stripped so the prompt renders
// as the user typed it without phantom trailing newlines.
func InsertText(content string) string {
	return strings.TrimRight(content, "\n")
}

// NThUserTurn returns the content of the n-th previous user turn in
// sess (1-based; n=1 is the most recent). Returns "" when n<1 or when
// the requested turn does not exist.
func NThUserTurn(sess []backend.Message, n int) string {
	if n < 1 || len(sess) == 0 {
		return ""
	}
	seen := 0
	for i := len(sess) - 1; i >= 0; i-- {
		if sess[i].Role == "user" {
			seen++
			if seen == n {
				return sess[i].Content
			}
		}
	}
	return ""
}

// CountUserTurns returns the total number of user-role turns in sess.
func CountUserTurns(sess []backend.Message) int {
	count := 0
	for _, m := range sess {
		if m.Role == "user" {
			count++
		}
	}
	return count
}

// ResolveQuote resolves the /quote argument against the session.
// Returns the resolved text and an empty error string on success;
// on error returns ("", errorMessage).
func ResolveQuote(arg string, sess []backend.Message) (string, string) {
	arg = strings.TrimSpace(arg)
	n := 1
	if arg != "" {
		v, err := strconv.Atoi(arg)
		if err != nil || v < 1 {
			return "", "Usage: /quote [n]"
		}
		n = v
	}
	raw := NThUserTurn(sess, n)
	if raw == "" {
		total := CountUserTurns(sess)
		if total == 0 {
			return "", "No previous turn to quote."
		}
		return "", "Quote index out of range (1.." + strconv.Itoa(total) + ")."
	}
	return InsertText(raw), ""
}
