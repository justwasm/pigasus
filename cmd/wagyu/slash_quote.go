// slash_quote.go — feature/trim-quote-trailing-newline-pretty:
//
// The AGY 1.2.13 changelog records:
//
//	"Fixed /quote (quote a previous turn) inserting a trailing newline
//	 before the user submits, so quoting 'line1' actually inserted
//	 'line1\\n' and looked like an extra blank line at the top of the
//	 prompt."
//
// The bug was a stray "\n" appended to the inserted text. The reference
// binary's /quote is intended to be verbatim: the prior user turn's
// content goes into the prompt exactly as the user typed it. Internal
// newlines are preserved (multi-line prompts stay multi-line); only the
// trailing newline(s) that the renderer tacks on at submit time are
// removed.
//
// Cycle TQ (this commit) implements /quote on the clone:
//
//	/quote            → insert the most recent user turn
//	/quote <n>        → insert the n-th previous user turn (1-based;
//	                    1 = most recent, 2 = second most recent, ...)
//
// The helper split mirrors the other "feedback → input" slash commands
// (openFeedback, editPromptCmd): resolveQuote picks the text, and the
// caller drops it into m.input verbatim. The cycle-AGY-1.2.13 fix lives
// in quoteInsertText — strings.TrimRight(content, "\n") strips the
// trailing newline that previously showed up as an extra blank line at
// the top of the prompt.

package main

import (
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
)

// quoteInsertText prepares a previous turn's content for verbatim
// insertion: any trailing '\n' bytes are stripped so the prompt renders
// as the user typed it (not with a phantom trailing newline that would
// push the cursor onto a new line and visually create a blank line at
// the top of the prompt). Internal newlines are preserved — multi-line
// user messages keep their line breaks.
//
// Reference: AGY 1.2.13 changelog entry "Fixed /quote (quote a previous
// turn) inserting a trailing newline before the user submits".
func quoteInsertText(content string) string {
	return strings.TrimRight(content, "\n")
}

// nThUserTurn returns the content of the n-th previous user turn in
// sess (1-based; n=1 is the most recent). Returns "" when n<1 or when
// the requested turn does not exist. The slice is scanned tail-to-head
// so we stop at the first user-role match — the n-th scan iteration.
//
// Only user-role messages count; assistant responses and tool-step
// pseudo-messages are skipped. The trailing-newline trim is the
// caller's responsibility (see quoteInsertText) so this helper stays a
// pure projection over the session.
func nThUserTurn(sess []backend.Message, n int) string {
	if n < 1 {
		return ""
	}
	seen := 0
	for i := len(sess) - 1; i >= 0; i-- {
		if sess[i].Role != "user" {
			continue
		}
		seen++
		if seen == n {
			return sess[i].Content
		}
	}
	return ""
}

// countUserTurns returns the number of user-role messages in sess.
// Used by resolveQuote to distinguish "no turns at all" from
// "n out of range" so the error message tells the user which case
// they hit.
func countUserTurns(sess []backend.Message) int {
	n := 0
	for _, m := range sess {
		if m.Role == "user" {
			n++
		}
	}
	return n
}

// resolveQuote picks the prior user turn text for /quote [n] and
// returns (text, "") on success or ("", errorMessage) on failure.
// The errorMessage is what /quote echoes through slashRespond.
//
//	/quote        → n=1, most recent user turn
//	/quote <n>    → n-th previous user turn, 1-based
//	/quote 0      → "Usage: /quote [n]"
//	/quote abc    → "Usage: /quote [n]"
//	/quote 99     → "Quote index out of range (1..N)." when N≥1
//	(no turns)    → "No previous turn to quote."
//
// resolveQuote is a pure helper — it does not touch m.input or the
// picker. The caller (handleSlashCommand's "quote" branch) decides
// whether to insert into the input or to echo the error.
func resolveQuote(args string, sess []backend.Message) (string, string) {
	arg := trimSpace(args)
	n := 1
	if arg != "" {
		v, err := strconv.Atoi(arg)
		if err != nil || v < 1 {
			return "", "Usage: /quote [n]"
		}
		n = v
	}
	raw := nThUserTurn(sess, n)
	if raw == "" {
		// No n-th user turn. Distinguish empty-session from
		// out-of-range so the user knows which knob to turn.
		total := countUserTurns(sess)
		if total == 0 {
			return "", "No previous turn to quote."
		}
		return "", "Quote index out of range (1.." + itoa(total) + ")."
	}
	return quoteInsertText(raw), ""
}

// applyQuoteInsert drops content into m.input and refreshes the picker
// overlay so it tracks the new first line. handleSlashCommand has
// already reset m.input/picker/history before the switch fires, so
// this method only needs to set the new value.
//
// The cursor lands at the end of the inserted text — SetValue on the
// textarea moves the cursor there automatically (per cycle-71
// history.go's note: "m.input.SetValue() rewrites the textarea's
// content, which automatically resets the cursor to the end of the
// inserted text").
//
// The picker overlay reads the first line of the buffer; we pass
// firstLine(content) so a multi-line quote still keeps the picker in
// sync (m.input.Value() is the canonical source, but the picker is
// driven independently in this clone).
func (m *runMode) applyQuoteInsert(content string) tea.Cmd {
	m.input.SetValue(content)
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Update([]rune(firstLine(content)))
	return nil
}

// handleQuote is the slash-command body for /quote [n]. It resolves the
// requested prior turn and either inserts the text into m.input or
// echoes a recoverable error through slashRespond. The wrapped return
// is (handled, tea.Cmd) — /quote never schedules a follow-up tea.Cmd,
// so the cmd is always nil.
func (m *runMode) handleQuote(args string) (bool, tea.Cmd) {
	content, errMsg := resolveQuote(args, m.session)
	if errMsg != "" {
		m.slashRespond("/quote", errMsg)
		return true, nil
	}
	cmd := m.applyQuoteInsert(content)
	return true, cmd
}
