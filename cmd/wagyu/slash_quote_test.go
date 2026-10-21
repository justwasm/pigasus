// slash_quote_test.go — feature/trim-quote-trailing-newline-pretty:
//
// Tests for the /quote slash command. AGY 1.2.13 recorded the fix:
// "/quote (quote a previous turn) inserting a trailing newline before
// the user submits, so quoting 'line1' actually inserted 'line1\\n'
// and looked like an extra blank line at the top of the prompt."
//
// The five required tests below pin the verbatim-insertion contract:
//
//	1. TestQuoteInsertsVerbatim            — content is inserted exactly
//	2. TestQuoteNoTrailingNewline          — no '\n' appended
//	3. TestQuotePreservesInternalNewlines  — multi-line prompts keep
//	                                          their line breaks
//	4. TestQuoteEmptyIsNoOp                — no prior turn → recoverable
//	                                          error, input stays clean
//	5. TestQuoteMultiLineInsertedCorrectly — three-line prior turn lands
//	                                          in the input byte-exact
//
// Plus a handful of supporting tests for resolveQuote's pure-projection
// half (the helper used by the slash router):

package main

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// seedSession writes the supplied messages into m.session verbatim.
// Test fixtures use this to put the runMode into a known prior-turn
// state without driving the full Enter-key path.
func seedSession(m *runMode, msgs []backend.Message) {
	m.session = append(m.session, msgs...)
}

// TestQuoteInsertsVerbatim: the prior turn's content goes into m.input
// exactly as it was typed — no leading or trailing whitespace trim,
// no escape sequences, no quote markers.
func TestQuoteInsertsVerbatim(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "line1"},
		{Role: "assistant", Content: "ok"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled by the router")
	}
	if got := m.input.Value(); got != "line1" {
		t.Errorf("input after /quote = %q, want %q", got, "line1")
	}
}

// TestQuoteNoTrailingNewline is the direct pin on the AGY 1.2.13
// changelog entry. A single-line prior turn must NOT produce a
// trailing '\n' in the input — that was the bug, and the fix is the
// strings.TrimRight(content, "\n") in quoteInsertText.
func TestQuoteNoTrailingNewline(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "single line turn"},
		{Role: "assistant", Content: "ack"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	got := m.input.Value()
	if strings.HasSuffix(got, "\n") {
		t.Errorf("input after /quote ends in '\\n' (the AGY 1.2.13 bug): %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("input after /quote ends in '\\n\\n' (would render as a blank line): %q", got)
	}
	// Belt-and-suspenders: count the trailing newlines explicitly.
	trail := len(got) - len(strings.TrimRight(got, "\n"))
	if trail != 0 {
		t.Errorf("input after /quote carries %d trailing '\\n' bytes: %q", trail, got)
	}
}

// TestQuotePreservesInternalNewlines: a multi-line prior turn lands
// in m.input with its internal line breaks intact. The TrimRight fix
// only strips trailing newlines, never internal ones.
func TestQuotePreservesInternalNewlines(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "first\nsecond\nthird"},
		{Role: "assistant", Content: "ok"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	want := "first\nsecond\nthird"
	if got := m.input.Value(); got != want {
		t.Errorf("input after /quote = %q, want %q", got, want)
	}
	// Sanity: confirm no leading newline snuck in either.
	if strings.HasPrefix(m.input.Value(), "\n") {
		t.Errorf("input after /quote starts with '\\n': %q", m.input.Value())
	}
}

// TestQuoteEmptyIsNoOp: when there are no prior user turns in the
// session, /quote must NOT silently insert an empty string into the
// input — that would visually clear whatever the user had typed and
// would look identical to "the command did nothing". The reference's
// recoverable path is a slashRespond line; we mirror that and keep
// m.input untouched.
func TestQuoteEmptyIsNoOp(t *testing.T) {
	m := newSlashModel(t)
	// Intentionally seed nothing — empty session.
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("input mutated by /quote on empty session: was %q, now %q", pre, got)
	}
	// The recoverable feedback lives in m.response after the echo
	// wrapper (`> /quote` + `⎿  <msg>`); confirm the body is the
	// recovered "No previous turn to quote." line.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "No previous turn to quote.") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("empty-session /quote did not echo 'No previous turn to quote.': %v", m.response)
	}
}

// TestQuoteMultiLineInsertedCorrectly: a three-line prior turn with
// no trailing newline in storage goes into m.input byte-exact. This
// is the more demanding version of TestQuotePreservesInternalNewlines
// (different fixtures, different length, pinned across two content
// rounds).
func TestQuoteMultiLineInsertedCorrectly(t *testing.T) {
	cases := []string{
		"alpha\nbeta\ngamma",
		"x\n\ny",       // blank line in the middle
		"one\ntwo",     // two lines, no trailing newline
		"trailing\n\n", // ends in \n\n — TrimRight strips both
		"trailing\n",   // single trailing \n — TrimRight strips it
	}
	for _, prior := range cases {
		m := newSlashModel(t)
		seedSession(m, []backend.Message{
			{Role: "user", Content: prior},
			{Role: "assistant", Content: "ack"},
		})
		if handled, _ := m.handleSlashCommand("/quote"); !handled {
			t.Fatalf("/quote was not handled (prior=%q)", prior)
		}
		want := strings.TrimRight(prior, "\n")
		if got := m.input.Value(); got != want {
			t.Errorf("input after /quote (prior=%q) = %q, want %q", prior, got, want)
		}
	}
}

// TestQuoteIndexedArg: /quote 2 picks the n-th previous user turn
// (1-based; 1 = most recent). The most-recent rule is what the
// reference binary implements — the user expects "the last turn I
// sent" by default.
func TestQuoteIndexedArg(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "first ever"},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "third and latest"},
		{Role: "assistant", Content: "ack"},
	})

	// /quote (no args) → most recent
	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	if got := m.input.Value(); got != "third and latest" {
		t.Errorf("/quote default = %q, want %q", got, "third and latest")
	}

	// /quote 2 → second most recent
	if handled, _ := m.handleSlashCommand("/quote 2"); !handled {
		t.Fatal("/quote 2 was not handled")
	}
	if got := m.input.Value(); got != "second" {
		t.Errorf("/quote 2 = %q, want %q", got, "second")
	}

	// /quote 3 → third most recent (oldest in this fixture)
	if handled, _ := m.handleSlashCommand("/quote 3"); !handled {
		t.Fatal("/quote 3 was not handled")
	}
	if got := m.input.Value(); got != "first ever" {
		t.Errorf("/quote 3 = %q, want %q", got, "first ever")
	}
}

// TestQuoteOutOfRange: when n exceeds the number of user turns, the
// helper returns a recoverable "out of range" message and m.input is
// not mutated.
func TestQuoteOutOfRange(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "only one"},
		{Role: "assistant", Content: "ack"},
	})
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote 5"); !handled {
		t.Fatal("/quote 5 was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("/quote 5 mutated input: was %q, now %q", pre, got)
	}
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Quote index out of range") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/quote 5 did not echo 'Quote index out of range': %v", m.response)
	}
}

// TestQuoteBadArg: a non-numeric /quote argument is a recoverable
// usage error, not a panic and not a silent empty insertion.
func TestQuoteBadArg(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "stuff"},
		{Role: "assistant", Content: "ack"},
	})
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote abc"); !handled {
		t.Fatal("/quote abc was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("/quote abc mutated input: was %q, now %q", pre, got)
	}
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Usage: /quote [n]") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/quote abc did not echo 'Usage: /quote [n]': %v", m.response)
	}
}

// TestResolveQuotePure: the helper's projection over a session slice
// is a pure function. These tests do not exercise the slash router —
// they pin resolveQuote's contract directly so a future refactor that
// re-routes /quote through openSlashPanelArgs (the cycle-185 panel
// pattern) still has a stable input/output to test against.
func TestResolveQuotePure(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "second\n\n"}, // trailing newlines get trimmed
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "third"},
	}
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{"default", "", "third"},
		{"explicit-1", "1", "third"},
		{"explicit-2", "2", "second"}, // "second\n\n" → "second"
		{"explicit-3", "3", "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := resolveQuote(tc.args, sess)
			if msg != "" {
				t.Errorf("resolveQuote(%q) returned error %q on a happy path", tc.args, msg)
			}
			if got != tc.want {
				t.Errorf("resolveQuote(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestQuoteInsertTextTrimsTrailingNewlines is the unit-level pin on
// the AGY 1.2.13 fix: quoteInsertText must strip trailing '\n' bytes
// while leaving internal '\n' bytes alone. This is the smallest possible
// reproduction of the bug — a regression here is a regression in the
// changelog claim.
func TestQuoteInsertTextTrimsTrailingNewlines(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"line1", "line1"},       // no trailing newline → unchanged
		{"line1\n", "line1"},     // one trailing newline → stripped
		{"line1\n\n", "line1"},   // two trailing newlines → both stripped
		{"a\nb", "a\nb"},         // internal newline preserved
		{"a\nb\n", "a\nb"},       // internal + trailing → trailing stripped
		{"\n", ""},               // only-newline input → empty
		{"", ""},                 // empty input → empty
		{"a\n\nb\n\n", "a\n\nb"}, // trailing pair stripped, internal pair preserved
	}
	for _, tc := range cases {
		got := quoteInsertText(tc.in)
		if got != tc.want {
			t.Errorf("quoteInsertText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
