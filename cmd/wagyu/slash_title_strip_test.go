package main

// slash_title_strip_test.go — cycle "session-title-strip-changes":
// AGY 1.2.13 stopped /rename from passing raw leading/trailing whitespace and
// embedded newlines into the conversation title. These tests pin the new
// behaviour for both the unit-level helper and the /rename + /title paths.
//
// Five behaviours the helper must satisfy (per the AGY 1.2.13 changelog and
// the gap description):
//
//  1. leading whitespace is trimmed;
//  2. trailing whitespace is trimmed (including \n);
//  3. a single embedded newline is collapsed to one space;
//  4. runs of multiple newlines / mixed whitespace are collapsed to one space;
//  5. a name that is only whitespace (incl. newlines) is rejected.
//
// Two integration tests cover the same rules through the slash-command router
// and the conversationTitle() display path, so a regression in either layer
// is caught even if one of them regresses while the other does not.

import (
	"reflect"
	"strings"
	"testing"
)

// Test 1 of 5: leading whitespace is stripped.
//
// " My Session" (one leading space) must become "My Session" and produce the
// same success line as the un-spaced input. Before this cycle, trimSpace
// already handled the leading-space case for the renamer, but the rename
// flow predates this test, and the new helper does the same job plus
// collapses internal newlines — so we want a positive case on the helper
// directly.
func TestNormalizeTitle_StripsLeadingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{" My Session", "My Session"},
		{"   My Session", "My Session"},
		{"\tMy Session", "My Session"},   // leading tab
		{" \t My Session", "My Session"}, // mixed leading whitespace
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Test 2 of 5: trailing whitespace is stripped, including \n.
//
// "My Session\n" must become "My Session". This is the exact symptom from the
// AGY 1.2.13 bug report ("session names like ' My Session\n' appeared in
// lists and breadcrumbs with odd surrounding whitespace"). A trailing \r\n
// is also covered, since the cycle spec is "all surrounding whitespace" and
// many clipboard managers emit CRLF.
func TestNormalizeTitle_StripsTrailingWhitespace(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My Session ", "My Session"},
		{"My Session   ", "My Session"},
		{"My Session\n", "My Session"},   // single trailing newline (the bug)
		{"My Session\r\n", "My Session"}, // trailing CRLF
		{"My Session\t", "My Session"},   // trailing tab
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Test 3 of 5: a single embedded newline collapses to a single space.
//
// "My\nSession" must become "My Session" — not "My\nSession", and not
// "My Session" with two spaces. This is the rule that keeps a single-line
// picker row from breaking layout because of a stray \n.
func TestNormalizeTitle_CollapsesInternalNewline(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My\nSession", "My Session"},
		{"My\rSession", "My Session"},
		{"\nMy Session\n", "My Session"}, // also exercises both ends
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Test 4 of 5: multiple embedded newlines and mixed internal whitespace
// collapse to exactly one space, never two.
//
// A pasted multi-line clipboard line is the canonical symptom — a title
// like "My\n\n\nSession\n\n" used to render with three blank lines between
// the words. The fix must collapse every run to one space, and never to
// zero spaces (which would join the words).
func TestNormalizeTitle_CollapsesMultipleNewlines(t *testing.T) {
	for _, tc := range []struct {
		in, want string
	}{
		{"My\n\nSession", "My Session"},   // double newline in the middle
		{"My\n\n\nSession", "My Session"}, // triple newline
		{"My\nSession\nTitle", "My Session Title"},
		{"My\n  Session", "My Session"}, // mixed \n + space
		{"My \t\n Session", "My Session"},
	} {
		if got := normalizeTitle(tc.in); got != tc.want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Test 5 of 5: a name that is only whitespace (incl. newlines) becomes the
// empty string, so the caller's "please provide a name" branch fires.
//
// Pre-cycle, only the literal empty string took that branch; "\n   \n" was
// committed to conversationName as a "rename" of eight whitespace chars. The
// fix routes all-whitespace inputs to the same error as the empty case.
func TestNormalizeTitle_EmptyAfterNormalization(t *testing.T) {
	for _, in := range []string{
		"",
		" ",
		"   ",
		"\n",
		"\n\n",
		"\n   \n",
		"\t",
		" \t \n \r\n ",
	} {
		if got := normalizeTitle(in); got != "" {
			t.Errorf("normalizeTitle(%q) = %q, want \"\"", in, got)
		}
	}
}

// TestRenameFeedback_StripsAndCollapses drives the renameFeedback helper
// directly with a representative set of inputs covering all four rules above
// (leading, trailing, internal-collapse, all-whitespace). It also confirms
// the "Conversation renamed to: %s" line is built from the cleaned name, not
// the raw input — without that, a name like "  Foo  " would echo back as
// "Conversation renamed to:   Foo  " in the TUI.
func TestRenameFeedback_StripsAndCollapses(t *testing.T) {
	cases := []struct {
		name    string
		arg     string
		current string
		want    string
		wantMsg string
	}{
		{"leading spaces", " My Session", "old", "My Session", "Conversation renamed to: My Session"},
		{"trailing newline", "My Session\n", "old", "My Session", "Conversation renamed to: My Session"},
		{"internal newline", "My\nSession", "old", "My Session", "Conversation renamed to: My Session"},
		{"many internal newlines", "My\n\n\nSession\n\n", "old", "My Session", "Conversation renamed to: My Session"},
		{"both ends and middle", "  My\n\tSession  \n", "old", "My Session", "Conversation renamed to: My Session"},
		{"only newline is rejected", "\n\n", "old", "old", "Error: Please provide a new name for the conversation."},
		{"plain name still works", "Refactor auth", "old", "Refactor auth", "Conversation renamed to: Refactor auth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := renameFeedback(tc.arg, tc.current)
			if got != tc.want || msg != tc.wantMsg {
				t.Errorf("renameFeedback(%q, %q) = (%q, %q); want (%q, %q)",
					tc.arg, tc.current, got, msg, tc.want, tc.wantMsg)
			}
		})
	}
}

// TestSlashRename_NormalizesConversationName is the integration test: it
// runs /rename through the slash-command router and asserts that
// conversationName ends up as the cleaned value, not the raw input. This is
// what the picker reads when it lists the session, so a regression here is
// the user-visible regression.
func TestSlashRename_NormalizesConversationName(t *testing.T) {
	for _, tc := range []struct {
		name, arg, want string
	}{
		{"leading space", " My Session", "My Session"},
		{"trailing newline", "My Session\n", "My Session"},
		{"internal newline", "My\nSession", "My Session"},
		{"many internal newlines", "Refactor\n\n\nauth", "Refactor auth"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newSlashModel(t)
			m.handleSlashCommand("/rename " + tc.arg)
			if m.conversationName != tc.want {
				t.Errorf("conversationName = %q, want %q", m.conversationName, tc.want)
			}
			if got := m.conversationTitle(); got != tc.want {
				t.Errorf("conversationTitle() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestConversationTitle_DisplayNormalisesUncleanStoredName covers the
// defence-in-depth path. conversationName is set directly here — bypassing
// /rename, the same way a future /resume hydration or test helper might —
// to a value with leading whitespace and a trailing newline. The title that
// conversationTitle() returns must still be clean, because that is the
// value that becomes the terminal WindowTitle and the conversation_title
// JSON field the status-line script reads.
func TestConversationTitle_DisplayNormalisesUncleanStoredName(t *testing.T) {
	m := newSlashModel(t)
	m.conversationName = " My Display Title\n"
	if got := m.conversationTitle(); got != "My Display Title" {
		t.Errorf("conversationTitle() = %q, want %q", got, "My Display Title")
	}
	// All-whitespace stored name falls through to the first user message,
	// which here is empty, so we end up at the program-name fallback. The
	// exact basename is platform-dependent (wagyu in CI, wagyu locally) so
	// we just assert it is non-empty and is *not* the unclean stored value.
	m.conversationName = "\n   \n"
	if got := m.conversationTitle(); got == "" || strings.TrimSpace(got) == "" {
		t.Errorf("conversationTitle() = %q, want non-empty fallback", got)
	}
	if got := m.conversationTitle(); reflect.DeepEqual(got, "\n   \n") {
		t.Errorf("conversationTitle() leaked raw whitespace: %q", got)
	}
}

// TestNormalizeTitle_NoOpOnCleanInput is a negative regression: a clean
// title must come back unchanged. Without this, a future "be more
// aggressive" rewrite of normalizeTitle could start inserting or stripping
// characters in already-clean inputs.
func TestNormalizeTitle_NoOpOnCleanInput(t *testing.T) {
	for _, in := range []string{
		"My Session",
		"a",
		" leading only",  // single leading space, must survive as "leading only"
		"trailing only ", // single trailing space, must survive as "trailing only"
		"multi word title",
	} {
		want := in
		if in == " leading only" {
			want = "leading only"
		}
		if in == "trailing only " {
			want = "trailing only"
		}
		if got := normalizeTitle(in); got != want {
			t.Errorf("normalizeTitle(%q) = %q, want %q", in, got, want)
		}
	}
}
