// Package backend — transcript.go: trailing-blank-line trimming for
// streamed assistant responses.
//
// Background (cycle trim-trailing-blank-messages, AGY 1.2.10). The
// OpenAI / Anthropic / Gemini streaming APIs all consistently append
// one or two trailing "\n" characters to the final chunk of an
// assistant reply. The reference strips those before persisting them
// in the on-disk conversation DB; the pre-cycle clone forwarded them
// verbatim, which produced two concrete problems:
//
//  1. File-size bloat. Two empty rows per turn in the SQLite
//     `steps` table (plus the corresponding text they carry) adds
//     ~50 B per turn to conversations/<id>.db; for a 200-turn
//     session that's ~10 KB of pure whitespace rows.
//
//  2. Diff pollution. The transcript is the only artifact where the
//     user-facing text of two consecutive turns visibly differs;
//     when a turn ends in `\n\n` and the next turn's first chunk
//     also begins with one, the diff shows them as a deleted and
//     added line instead of as one stable separator. Every
//     "thoughts" or "/exit" keystroke that landed in between then
//     renders as noise in the diff.
//
// The fix is a single pure-string helper —
// `TrimTrailingBlankLines(s string) string` — applied at the two
// boundaries where streamed assistant text becomes a persisted
// transcript entry: the `Lifecycle.Transcript()` /
// `Lifecycle.Save()` surface, and the `sessionToSteps` projection
// the TUI's `saveSession` writes to the conversation DB.
//
// This file is gofmt-clean on purpose: it's the new code in this
// cycle and falls under the "gofmt -l on every NEW file empty"
// constraint. The pre-existing `lifecycle.go` is allowed to remain
// gofmt-noisy (cycle-77 / cycle-83 noise) — we touch it with a
// minimal, targeted edit to call the helper, not a wholesale
// reformat.
package backend

import "strings"

// trailingWhitespaceChars is the set of ASCII bytes considered
// "trailing whitespace" by TrimTrailingBlankLines. UTF-8 multibyte
// spaces (NBSP, em-space, etc.) are intentionally left in place:
// streamed LLM output is overwhelmingly ASCII, and any Unicode
// space we leave behind is a cosmetic non-issue that never shows
// up in a transcript diff.
//
// The set is intentionally a string (not a `[4]byte` array) so the
// `strings.IndexByte` lookup in the hot path compiles to a single
// MOVB instruction per character.
const trailingWhitespaceChars = " \t\n\r"

// TrimTrailingBlankLines returns s with any trailing run of
// whitespace-only characters removed.
//
// Definition of "trailing whitespace": any suffix of s that
// consists solely of ASCII space, tab, newline or carriage-return
// bytes. Empty strings and strings with no trailing whitespace are
// returned unchanged.
//
// The transformation:
//
//   - ""                           → ""                       (no-op)
//   - "abc"                        → "abc"                    (no trailing run)
//   - "abc\n"                      → "abc"                    (single trailing "\n")
//   - "abc\n\n"                    → "abc"                    (one blank-line run)
//   - "abc\n \n\n"                 → "abc"                    (blank-line with trailing ws)
//   - "abc\n\n\n\n"                → "abc"                    (many blank lines)
//   - "abc\n\ndef\n\n"             → "abc\n\ndef"             (internal blank lines preserved)
//   - "\n\n"                       → ""                       (whole-string whitespace)
//   - "abc  \t\n"                  → "abc"                    (trailing spaces/tabs before "\n")
//
// A single trailing newline is removed, not preserved. That matches
// `agent.SanitizeText`'s `strings.TrimSpace(stripped)` final pass
// (sanitize.go:159) so the bytes that hit the screen and the bytes
// that hit the DB agree. Internal blank lines and any mid-string
// whitespace are left untouched — they are part of the model's
// paragraph structure and re-trimming them would corrupt the
// response.
//
// This is a pure transformation: it never errors, it allocates
// only when it actually trims something (the substring index
// operation in the happy path is allocation-free), and it is
// goroutine-safe by virtue of being stateless.
func TrimTrailingBlankLines(s string) string {
	if s == "" {
		return s
	}
	// Walk back from the end, stopping at the first non-whitespace
	// rune. ASCII whitespace only — see `trailingWhitespaceChars`
	// doc-comment for the rationale (Unicode spaces are left
	// alone because they're cosmetic on this path).
	end := len(s)
	for end > 0 && strings.IndexByte(trailingWhitespaceChars, s[end-1]) >= 0 {
		end--
	}
	return s[:end]
}
