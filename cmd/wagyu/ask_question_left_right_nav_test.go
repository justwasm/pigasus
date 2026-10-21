package main

// ask_question_left_right_nav_test.go — feature/ask-question-left-right-nav.
//
// Verifies the cycle feature/ask-question-left-right-nav behaviour:
//   - Right arrow advances the option cursor through the highlighted
//     question's Options list.
//   - Left arrow regresses the option cursor.
//   - Both cursors clamp at the option-list bounds (Left at 0 stays
//     put; Right at len-1 stays put).
//   - Left/Right alone never commit: neither the picker state nor
//     the manager's pending count moves.
//   - Up/Down reset the option cursor so a fresh question begins on
//     its first option.
//   - Opening the picker (Ctrl+K / `a`) resets the option cursor
//     even when the cursor was non-zero from a prior question.
//
// The harness reuses the cycle subagent-question-picker's
// newRunModeForSubagentQuestion helper so this file picks up the same
// test environment (TempDir HOME, fakeClient backend, Manager wired
// into the runMode) the picker tests use.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/subagentquestions"
)

// arrowKey returns a tea.KeyPressMsg for the named arrow. The Code
// is the bubbletea sensor value (a rune); Text is empty because
// arrow keys have no printable rune.
func arrowKey(code rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code})
}

// TestAskQuestion_LeftRightNav_RightAdvancesOptionCursor verifies
// the forward step: pressing Right from option 0 advances to
// option 1, then to option 2. The picker remains open and the
// pending count stays unchanged.
func TestAskQuestion_LeftRightNav_RightAdvancesOptionCursor(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("picker did not open")
	}

	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 1 {
		t.Errorf("option cursor after 1× Right = %d, want 1", got)
	}
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Right must not close the picker")
	}

	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 2 {
		t.Errorf("option cursor after 2× Right = %d, want 2", got)
	}

	// No commit: picker still open, manager still pending.
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Right must not commit; picker closed")
	}
	if got := m.subAgentQuestions.PendingCount(subagentquestions.ParentKey); got != 1 {
		t.Errorf("PendingCount = %d after Right, want 1 (no commit)", got)
	}
	if m.subAgentQuestionAnswerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (no commit)", m.subAgentQuestionAnswerTarget)
	}
}

// TestAskQuestion_LeftRightNav_LeftRegressesOptionCursor verifies
// the backward step: starting on option 2, pressing Left moves
// the cursor back to 1, then to 0. The picker remains open the
// whole way.
func TestAskQuestion_LeftRightNav_LeftRegressesOptionCursor(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})

	m.Update(ctrlKey('k'))
	// Skip ahead to option 2.
	m.Update(arrowKey(tea.KeyRight))
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 2 {
		t.Fatalf("preconditions: option cursor = %d, want 2", got)
	}

	m.Update(arrowKey(tea.KeyLeft))
	if got := m.subAgentQuestionPickerOptionCursor; got != 1 {
		t.Errorf("option cursor after 1× Left = %d, want 1", got)
	}
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Left must not close the picker")
	}

	m.Update(arrowKey(tea.KeyLeft))
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor after 2× Left = %d, want 0", got)
	}
}

// TestAskQuestion_LeftRightNav_LeftClampsAtFirst pins the
// left-bound behaviour: pressing Left on option 0 leaves the
// cursor at 0 (no wrap to the end, no negative index). The
// picker must remain open.
func TestAskQuestion_LeftRightNav_LeftClampsAtFirst(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	m.Update(ctrlKey('k'))
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	m.Update(arrowKey(tea.KeyLeft))
	m.Update(arrowKey(tea.KeyLeft))
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor after 2× Left on first = %d, want 0 (clamped)",
			got)
	}
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Left at first must not close the picker")
	}
}

// TestAskQuestion_LeftRightNav_RightClampsAtLast pins the
// right-bound behaviour: pressing Right on the last option keeps
// the cursor at len-1 (no wrap to 0, no overflow). The picker
// must remain open and the manager must still show the question
// pending.
func TestAskQuestion_LeftRightNav_RightClampsAtLast(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	m.Update(ctrlKey('k'))
	// Move to last option.
	m.Update(arrowKey(tea.KeyRight))
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 2 {
		t.Fatalf("preconditions: option cursor = %d, want 2", got)
	}

	m.Update(arrowKey(tea.KeyRight))
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 2 {
		t.Errorf("option cursor after 2× Right on last = %d, want 2 (clamped)",
			got)
	}
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Right at last must not close the picker")
	}
	if got := m.subAgentQuestions.PendingCount(subagentquestions.ParentKey); got != 1 {
		t.Errorf("PendingCount = %d after Right-at-last, want 1 (no commit)",
			got)
	}
}

// TestAskQuestion_LeftRightNav_NeitherArrowCommits is the
// no-commit guarantee: a sequence of Left and Right arrows
// never stages an answer, never closes the picker, and leaves
// the manager's pending list intact. The constraint documented
// in feature/ask-question-left-right-nav ("Left/Right should
// not commit the selection — only confirm-key (Enter)
// commits.") is the assertion this test pins down.
func TestAskQuestion_LeftRightNav_NeitherArrowCommits(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python", "rust"})

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("picker did not open")
	}

	// Hit Right, Left, Right, Right in every order; none of
	// these is the commit key.
	for _, code := range []rune{
		tea.KeyRight,
		tea.KeyLeft,
		tea.KeyRight,
		tea.KeyRight,
	} {
		m.Update(arrowKey(code))
	}

	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Left/Right must not close the picker")
	}
	if m.subAgentQuestionAnswerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (Left/Right must not commit)",
			m.subAgentQuestionAnswerTarget)
	}
	if m.subAgentQuestionAnswerBuffer != "" {
		t.Errorf("AnswerBuffer = %q, want empty (Left/Right must not stage)",
			m.subAgentQuestionAnswerBuffer)
	}
	if got := m.subAgentQuestions.PendingCount(subagentquestions.ParentKey); got != 1 {
		t.Errorf("PendingCount = %d, want 1 (Left/Right must not answer)",
			got)
	}
	if got := q.State; got != subagentquestions.StatePending {
		t.Errorf("q.State = %q, want %q (Left/Right must not transition)",
			got, subagentquestions.StatePending)
	}
}

// TestAskQuestion_LeftRightNav_RenderedOptionsRender is the
// surface-level check: when the highlighted question has
// Options, each option string shows up in the picker's rendered
// output, and the option-cursor marker ('›') sits next to the
// currently-highlighted option. This guards against the cursor
// changing but the renderer forgetting to draw the new state.
func TestAskQuestion_LeftRightNav_RenderedOptionsRender(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	m.Update(ctrlKey('k'))
	body := viewBodyPlain(m)
	for _, opt := range []string{"red", "green", "blue"} {
		if !strings.Contains(body, opt) {
			t.Errorf("picker overlay missing option %q; got:\n%s",
				opt, body)
		}
	}

	// Move to "green" (idx 1) and re-render. The marker ‹"
	// precedes the highlighted option in the row.
	m.Update(arrowKey(tea.KeyRight))
	body = viewBodyPlain(m)
	if !strings.Contains(body, "› green") {
		t.Errorf("picker overlay missing highlighted option row; got:\n%s",
			body)
	}
}

// TestAskQuestion_LeftRightNav_UpDownResetsOptionCursor verifies
// the cross-question reset: pressing Right to advance the option
// cursor, then Down to highlight the next question, returns the
// option cursor to 0 so the new question's options begin from
// scratch.
func TestAskQuestion_LeftRightNav_UpDownResetsOptionCursor(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"First?", []string{"a", "b"})
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Second?", []string{"x", "y"})

	m.Update(ctrlKey('k'))
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 1 {
		t.Fatalf("preconditions: option cursor = %d, want 1", got)
	}

	// Down to the second question — option cursor must reset.
	m.Update(arrowKey(tea.KeyDown))
	if got := m.subAgentQuestionPickerCursor; got != 1 {
		t.Errorf("question cursor after Down = %d, want 1", got)
	}
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor after Down = %d, want 0 (reset on Up/Down)",
			got)
	}

	// Up back to the first question — option cursor stays
	// reset (each Up/Down crosses a row, and any cross-row
	// navigation abandons the previous option preview).
	m.Update(arrowKey(tea.KeyUp))
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor after Up = %d, want 0 (reset on Up/Down)",
			got)
	}

	// Now advance again — sanity check the second question's
	// cursor still works after the reset cycle.
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 1 {
		t.Errorf("option cursor after Right from reset = %d, want 1", got)
	}
}

// TestAskQuestion_LeftRightNav_OpenPickerResetsOptionCursor
// covers the reopen path: pressing Left/Right inside an open
// picker moves the option cursor; closing the picker (Esc) and
// reopening it must reset the cursor to 0 even though the
// question's pending Options list is unchanged.
func TestAskQuestion_LeftRightNav_OpenPickerResetsOptionCursor(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})

	m.Update(ctrlKey('k'))
	m.Update(arrowKey(tea.KeyRight))
	if got := m.subAgentQuestionPickerOptionCursor; got != 1 {
		t.Fatalf("preconditions: option cursor = %d, want 1", got)
	}

	// Close via Esc, re-open via Ctrl+K.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.subAgentQuestionPickerOpen {
		t.Fatal("Esc did not close the picker")
	}
	m.Update(ctrlKey('k'))
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor after reopen = %d, want 0 (reset on open)",
			got)
	}
}

// TestAskQuestion_LeftRightNav_NoOptionsIsInert pins the
// free-form-question path: a pending question with a nil
// Options slice does not let Left/Right move any cursor; the
// keys must be no-ops (not commits, not panics).
func TestAskQuestion_LeftRightNav_NoOptionsIsInert(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"What is the goal?", nil)
	if q == nil || len(q.Options) != 0 {
		t.Fatalf("Enqueue must yield a free-form question")
	}

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("picker did not open")
	}

	// Pre-state guard: option cursor stays 0 because there are
	// no options to index into.
	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	m.Update(arrowKey(tea.KeyRight))
	m.Update(arrowKey(tea.KeyLeft))
	m.Update(arrowKey(tea.KeyRight))

	if got := m.subAgentQuestionPickerOptionCursor; got != 0 {
		t.Errorf("option cursor = %d on a free-form question, want 0 (inert)",
			got)
	}
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("Left/Right on a free-form question must not close the picker")
	}
	if m.subAgentQuestionAnswerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (Left/Right must not commit)",
			m.subAgentQuestionAnswerTarget)
	}
}
