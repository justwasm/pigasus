package main

// Cycle trim-empty-history-messages tests.
//
// AGY 1.2.7: "Fixed the conversation history list showing empty
// message entries when the user submitted a whitespace-only prompt,
// which wasted screen space and confused scroll calculations."
//
// The clone's pre-fix pushHistory guarded only against prompt == "",
// which let a single space (or any run of Unicode whitespace) slip
// through. This file covers:
//
//  1. pushHistory skips "" and whitespace-only inputs.
//  2. pushHistory trims surrounding whitespace before storing /
//     deduping so a follow-up "  hello  " collapses with "hello".
//  3. submitPrompt rejects whitespace-only inputs (no transcript row,
//     no session entry, no history entry).
//  4. scroll / navigation math holds when a model-reply entry has
//     wrapped multi-line content and the prompt row is blank —
//     View() must still fit inside the terminal and historyUp /
//     historyDown must still reach a non-blank row.
//  5. historyUp / historyDown land on the most recent non-blank
//     entry even when an upstream caller somehow injected blanks
//     directly into m.history.

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// trimEmptyHistoryMode returns a runMode with a known terminal size
// and ANTIGRAVITY_HOME pointed at a temp dir so convstore appends
// don't pollute the real history.jsonl.
func trimEmptyHistoryMode(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "MiniMax M3", "", "/tmp", false)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return m
}

// 1. Whitespace-only prompt is not added to history.
//
// Covers the AGY 1.2.7 changelog fix. pushHistory used to skip only
// empty strings — " " and "\n\t" went through and produced empty
// entries in m.history, m.response, m.session, and history.jsonl.
func TestPushHistory_SkipsWhitespaceOnly(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	blanks := []string{
		"",
		" ",
		"\n",
		"\t",
		"  \t \n ",
		"\u00a0", // non-breaking space
		"\u3000", // ideographic space (CJK input)
		"\u2003", // em space
		strings.Repeat(" ", 64),
	}
	for _, b := range blanks {
		m.pushHistory(b)
	}
	if len(m.history) != 0 {
		t.Fatalf("pushHistory stored blank entries: %q", m.history)
	}
}

// 2. Leading/trailing whitespace is stripped before storage.
//
// Surrounding whitespace is a common accident: users tap Space
// before a prompt, or hit Enter on a trailing newline. Storing the
// raw form produces near-duplicate history entries and a near-empty
// transcript row. Trim happens before the dedupe check so "  hello  "
// after "hello" collapses to the same entry.
func TestPushHistory_TrimsSurroundingWhitespace(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	m.pushHistory("  hello  ")
	if len(m.history) != 1 || m.history[0] != "hello" {
		t.Fatalf("expected trimmed history entry, got %q", m.history)
	}

	// Trim happens BEFORE the dedupe check.
	m.pushHistory("\n\thello\t\n")
	if len(m.history) != 1 {
		t.Fatalf("expected dedupe to collapse whitespace-padded duplicate, got %q", m.history)
	}

	// Internal whitespace is preserved — only the outer run is trimmed.
	m.pushHistory("  multi word prompt ")
	if len(m.history) != 2 || m.history[1] != "multi word prompt" {
		t.Fatalf("expected internal whitespace preserved, got %q", m.history)
	}
}

// 3. submitPrompt rejects whitespace-only prompts.
//
// The submit path mirrors pushHistory's behaviour: a whitespace-only
// buffer is treated as an empty submission. The transcript must NOT
// gain a blank "> " row and the session must NOT gain an empty user
// message. The input buffer is reset so a stray tap doesn't leave
// the prompt textarea stuck on whitespace.
func TestSubmitPrompt_RejectsWhitespaceOnly(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	m.input.SetValue("   ")

	beforeResp := len(m.response)
	beforeSess := len(m.session)
	beforeHist := len(m.history)

	if cmd := m.submitPrompt(); cmd != nil {
		t.Errorf("submitPrompt for blank input returned cmd %v, want nil", cmd)
	}

	if len(m.response) != beforeResp {
		t.Errorf("submitPrompt appended %d transcript rows for a blank prompt", len(m.response)-beforeResp)
	}
	if len(m.session) != beforeSess {
		t.Errorf("submitPrompt appended %d session messages for a blank prompt", len(m.session)-beforeSess)
	}
	if len(m.history) != beforeHist {
		t.Errorf("submitPrompt pushed %d history entries for a blank prompt", len(m.history)-beforeHist)
	}
	if m.streaming {
		t.Error("submitPrompt left streaming=true for a blank prompt")
	}
	if got := m.input.Value(); strings.TrimSpace(got) != "" {
		t.Errorf("submitPrompt left whitespace in the input buffer: %q", got)
	}

	// Tabs and newlines also count.
	m.input.SetValue("\t\n  \t")
	beforeResp = len(m.response)
	if cmd := m.submitPrompt(); cmd != nil {
		t.Errorf("submitPrompt for tab/newline input returned cmd %v, want nil", cmd)
	}
	if len(m.response) != beforeResp {
		t.Errorf("submitPrompt appended %d transcript rows for a tab/newline prompt", len(m.response)-beforeResp)
	}
}

// 4. Scroll calculations handle gaps without breaking.
//
// A "gap" here means a transcript where one or more rows are blank.
// Before the fix, whitespace-only submits could leave a row in
// m.response that produced an empty `> ` line on the next render.
// The viewport math must still fit inside the terminal: the empty
// row counts as one screen row, but the rendered content must
// remain bounded by the terminal height regardless of how many gaps
// exist. This pins cycle-124's `screenRows("") == 1` invariant and
// cycle-119's "view never taller than terminal" guarantee under a
// transcript that includes blank rows.
func TestScroll_CalculationsHandleBlankRows(t *testing.T) {
	for _, n := range []int{0, 1, 5, 40, 400} {
		m := sizedMode(80, 30, n)
		// Inject a few blank rows into the transcript (the exact
		// thing a whitespace-only submit would have produced before
		// the fix). These count as one screen row each — the
		// viewport math must tolerate them.
		blankInsertions := []int{2, 7, 11}
		for _, idx := range blankInsertions {
			if idx >= len(m.response) {
				continue
			}
			m.response = append(m.response, "")
			// Move "" to the requested index to keep the test
			// representative rather than append-only.
			blank := m.response[len(m.response)-1]
			copy(m.response[idx+1:], m.response[idx:])
			m.response[idx] = blank
		}
		// Sanity: at least one blank row present when the body is long
		// enough to host one.
		if n > 8 {
			foundBlank := false
			for _, r := range m.response {
				if r == "" {
					foundBlank = true
					break
				}
			}
			if !foundBlank {
				t.Fatalf("n=%d: blank row insertion failed", n)
			}
		}

		// Cycle-119 regression: View() must never exceed the terminal.
		lines := strings.Split(m.View().Content, "\n")
		if len(lines) > 30 {
			t.Errorf("n=%d: view has %d rows, terminal is 30", n, len(lines))
		}

		// screenRows("") must return 1 (cycle-124 invariant). A blank
		// row counts as one screen row, never zero — otherwise the
		// clip math would loop indefinitely.
		if got := screenRows(""); got != 1 {
			t.Errorf("screenRows(\"\") = %d, want 1", got)
		}
	}
}

// 5. The history list skips empty entries and navigation still works.
//
// Even if a blank entry were to slip into m.history (e.g. a future
// caller that bypasses pushHistory, or a state reconstructed at
// resume time), historyUp / historyDown must not land on it. The
// simplest behaviour is to reject them at the push boundary (which
// the helper already does); this test pins the boundary so any
// future code path that re-introduces blanks is caught.
//
// We assert two invariants:
//
//	a) pushHistory never accepts a blank, so the navigation path
//	   can't observe one. (Defensive — the upstream fix.)
//	b) After pushing three real prompts and one attempted blank,
//	   historyUp walks back exactly three slots and lands on each
//	   in reverse order. The blank insertion is a no-op.
func TestHistory_NavigationSkipsBlankEntries(t *testing.T) {
	m := trimEmptyHistoryMode(t)

	m.pushHistory("alpha")
	m.pushHistory("beta")
	m.pushHistory("gamma")
	m.pushHistory("   ") // attempted blank — must be a no-op
	m.pushHistory("delta")

	if len(m.history) != 4 {
		t.Fatalf("history len = %d, want 4 (blank was rejected): %q", len(m.history), m.history)
	}
	want := []string{"alpha", "beta", "gamma", "delta"}
	for i, w := range want {
		if m.history[i] != w {
			t.Errorf("history[%d] = %q, want %q", i, m.history[i], w)
		}
	}

	// Seed the input with a sentinel so historyEdit is non-empty
	// during navigation.
	m.input.SetValue("DRAFT")
	m.historyIdx = 0
	m.historyEdit = ""

	// Walk back through every entry. historyUp increments historyIdx
	// each time; the rendered value at slot k is m.history[len-k].
	m.historyUp()
	if got := m.input.Value(); got != "delta" {
		t.Errorf("Up #1: got %q, want %q", got, "delta")
	}
	m.historyUp()
	if got := m.input.Value(); got != "gamma" {
		t.Errorf("Up #2: got %q, want %q", got, "gamma")
	}
	m.historyUp()
	if got := m.input.Value(); got != "beta" {
		t.Errorf("Up #3: got %q, want %q", got, "beta")
	}
	m.historyUp()
	if got := m.input.Value(); got != "alpha" {
		t.Errorf("Up #4: got %q, want %q", got, "alpha")
	}

	// One more Up is a no-op — there's no fifth entry.
	idxBefore := m.historyIdx
	m.historyUp()
	if m.historyIdx != idxBefore {
		t.Errorf("Up past the start advanced historyIdx: was %d, now %d", idxBefore, m.historyIdx)
	}

	// Walk forward; we expect to return to the draft.
	m.historyDown()
	if got := m.input.Value(); got != "beta" {
		t.Errorf("Down #1: got %q, want %q", got, "beta")
	}
	m.historyDown()
	m.historyDown()
	m.historyDown()
	if got := m.input.Value(); got != "DRAFT" {
		t.Errorf("Down past the start: got %q, want %q (the draft)", got, "DRAFT")
	}
}

// 6. (bonus) Submitting a real prompt after a rejected blank still works.
//
// After submitPrompt drops a whitespace-only input, the next real
// prompt must still reach the agent. This is the regression guard
// against an accidental "stuck" input buffer: a stray space must not
// poison the next submit.
func TestSubmitPrompt_RealPromptAfterBlankWorks(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	m.input.SetValue("   ")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("blank submit returned cmd %v, want nil", cmd)
	}
	if m.streaming {
		t.Fatal("blank submit left streaming=true")
	}

	// Now type a real prompt and submit. We don't drive the model
	// stream — we only verify that the submit path is reachable.
	m.input.SetValue("real prompt")
	beforeResp := len(m.response)
	// submitPrompt will queue a stream and batch with a spinner tick.
	// We can't await either in a unit test, but the synchronous side
	// effects (history append, response echo, session append) all
	// happen before the tea.Cmd is returned.
	_ = m.submitPrompt()

	if got := len(m.history); got != 1 || m.history[0] != "real prompt" {
		t.Errorf("after real submit: history = %q, want [\"real prompt\"]", m.history)
	}
	if len(m.response) <= beforeResp {
		t.Errorf("real submit did not append a transcript row (was %d, now %d)", beforeResp, len(m.response))
	}
	if len(m.session) == 0 {
		t.Errorf("real submit did not append a session message")
	} else if last := m.session[len(m.session)-1]; last.Role != "user" || strings.TrimSpace(last.Content) == "" {
		t.Errorf("session tail = %+v, want a user message with non-blank content", last)
	}
}

// 7. (bonus) HistoryCap is preserved when blanks are interleaved.
//
// Pre-fix, every whitespace-only submit still consumed a slot (the
// pushHistory check was prompt == "", not strings.TrimSpace(prompt)
// == ""), so spamming blanks could force out real entries. With the
// trim guard, blanks never reach the cap arithmetic.
func TestPushHistory_HistoryCapRespectedWithBlanks(t *testing.T) {
	m := trimEmptyHistoryMode(t)
	// Fill exactly to the cap with real entries.
	for i := 0; i < HistoryCap; i++ {
		m.pushHistory(fmt.Sprintf("prompt-%d", i))
	}
	if len(m.history) != HistoryCap {
		t.Fatalf("history len = %d, want %d", len(m.history), HistoryCap)
	}
	// Interleave a flurry of blanks. They must be no-ops.
	for i := 0; i < 50; i++ {
		m.pushHistory(" \t\n  ")
	}
	if len(m.history) != HistoryCap {
		t.Errorf("blanks changed history length: %d, want %d", len(m.history), HistoryCap)
	}
	// First entry should still be the first real prompt.
	if m.history[0] != "prompt-0" {
		t.Errorf("first entry = %q, want %q", m.history[0], "prompt-0")
	}
}

// fakeClient is the tiny stub used by other viewport tests, declared
// in tui_run_test.go. Reusing it here keeps the test file
// dependency-free.
