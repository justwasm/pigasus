package subagentquestionpanel

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
)

// newPanel builds a picker over a fresh Manager and returns it with the
// Manager, so a test can enqueue and assert on the store.
func newPanel(t *testing.T) (*Panel, *subagentquestions.Manager) {
	t.Helper()
	mgr := subagentquestions.NewManager(false)
	var lines []string
	p := New(Seams{
		Manager:    mgr,
		Respond:    func(line string) { lines = append(lines, line) },
		InputValue: func() string { return "" },
		ResetInput: func() {},
	})
	return p, mgr
}

// arrowKey returns a tea.Key for the named arrow. The Code is the
// bubbletea sensor value (a rune); Text is empty because arrow keys have
// no printable rune.
func arrowKey(code rune) tea.Key {
	return tea.Key{Code: code}
}

// plainRows joins the picker's rows with ANSI escapes stripped, so a
// test can assert on plain text fragments regardless of which colour
// codes the renderer picked.
func plainRows(rows []string) string {
	return ansi.Strip(strings.Join(rows, "\n"))
}

// TestToggle_NoPendingQuestionsEchoesTheScope pins the notice the
// picker writes when there is nothing to answer.
func TestToggle_NoPendingQuestionsEchoesTheScope(t *testing.T) {
	p, _ := newPanel(t)
	var lines []string
	p.seams.Respond = func(line string) { lines = append(lines, line) }

	p.Toggle()
	if p.Active() {
		t.Fatal("picker opened with no pending question")
	}
	if len(lines) != 1 {
		t.Fatalf("responded %d lines, want 1: %q", len(lines), lines)
	}
	if got := ansi.Strip(lines[0]); got != "  ⎿  No pending questions for you" {
		t.Errorf("notice = %q, want %q", got, "  ⎿  No pending questions for you")
	}
}

// TestToggle_ClosesAnOpenPicker pins the Ctrl+K toggle: the second
// Ctrl+K closes the picker again.
func TestToggle_ClosesAnOpenPicker(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", []string{"go"})

	p.Toggle()
	if !p.Active() {
		t.Fatal("picker did not open")
	}
	p.Toggle()
	if p.Active() {
		t.Fatal("picker did not close")
	}
}

// TestToggle_ResetsTheCursors pins the reopen behaviour: the cursors
// start at the first question and its first option every time the
// picker opens.
func TestToggle_ResetsTheCursors(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", []string{"go", "node"})

	p.Toggle()
	p.HandleKey(arrowKey(tea.KeyRight))
	p.HandleKey(arrowKey(tea.KeyDown))
	if p.optionCursor != 0 || p.cursor != 0 {
		t.Fatalf("preconditions: cursor = %d, option cursor = %d, want 0/0",
			p.cursor, p.optionCursor)
	}
	p.HandleKey(arrowKey(tea.KeyRight))
	if p.optionCursor != 1 {
		t.Fatalf("option cursor = %d, want 1", p.optionCursor)
	}
	p.Toggle()
	p.Toggle()
	if p.cursor != 0 || p.optionCursor != 0 {
		t.Errorf("after reopen: cursor = %d, option cursor = %d, want 0/0",
			p.cursor, p.optionCursor)
	}
}

// TestHandleKey_EscClosesThePicker pins the cancel path: Esc closes
// the picker without staging an answer.
func TestHandleKey_EscClosesThePicker(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", nil)

	p.Toggle()
	if !p.HandleKey(tea.Key{Code: tea.KeyEsc}) {
		t.Fatal("Esc was not handled")
	}
	if p.Active() {
		t.Error("Esc did not close the picker")
	}
	if p.answerTarget != "" {
		t.Errorf("answer target = %q, want empty (Esc must not stage)",
			p.answerTarget)
	}
}

// TestHandleKey_ClosedPickerHandlesNothing pins the gate: a closed
// picker answers "not handled", so the host's global handlers still
// see the key.
func TestHandleKey_ClosedPickerHandlesNothing(t *testing.T) {
	p, _ := newPanel(t)
	if p.HandleKey(tea.Key{Code: tea.KeyEnter}) {
		t.Error("a closed picker must not handle keys")
	}
}

// TestStage_EnterStagesTheQuestion pins the commit key: Enter stages
// the highlighted question and closes the picker.
func TestStage_EnterStagesTheQuestion(t *testing.T) {
	p, mgr := newPanel(t)
	q := mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", nil)

	p.Toggle()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Active() {
		t.Error("Enter did not close the picker")
	}
	if p.answerTarget != q.ID {
		t.Errorf("answer target = %q, want %q", p.answerTarget, q.ID)
	}
	if !p.HasPendingQuestion() {
		t.Error("a staged answer must count as a pending question")
	}
}

// TestSubmitAnswer_SendsTheComposerContent pins the answer path: the
// composer's content is sent to the staged question and the composer
// is cleared.
func TestSubmitAnswer_SendsTheComposerContent(t *testing.T) {
	p, mgr := newPanel(t)
	q := mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", nil)

	composer := "go"
	cleared := false
	p.seams.InputValue = func() string { return composer }
	p.seams.ResetInput = func() { cleared = true }

	p.Toggle()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmitAnswer() {
		t.Fatal("SubmitAnswer did not consume the composer")
	}
	if !cleared {
		t.Error("the composer was not cleared")
	}
	if p.answerTarget != "" {
		t.Errorf("answer target = %q, want empty after the answer",
			p.answerTarget)
	}
	if got := q.Answer; got != "go" {
		t.Errorf("answer = %q, want %q", got, "go")
	}
	if got := mgr.PendingCount(subagentquestions.ParentKey); got != 0 {
		t.Errorf("PendingCount = %d, want 0", got)
	}
}

// TestSubmitAnswer_EmptyAnswerSkips pins the "empty answer is a skip"
// behaviour: the model sees "User Skipped".
func TestSubmitAnswer_EmptyAnswerSkips(t *testing.T) {
	p, mgr := newPanel(t)
	q := mgr.Enqueue(subagentquestions.ParentKey, "Pick a colour", nil)

	p.Toggle()
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.SubmitAnswer() {
		t.Fatal("SubmitAnswer did not consume the composer")
	}
	if got := q.Answer; got != subagentquestions.SkipAnswer {
		t.Errorf("answer = %q, want %q", got, subagentquestions.SkipAnswer)
	}
}

// TestSubmitAnswer_NoStagedQuestionDoesNotConsume pins the gate: with
// no staged question the composer belongs to the normal prompt flow.
func TestSubmitAnswer_NoStagedQuestionDoesNotConsume(t *testing.T) {
	p, _ := newPanel(t)
	if p.SubmitAnswer() {
		t.Error("SubmitAnswer consumed the composer with nothing staged")
	}
}

// TestAlertRows_NamesEveryPendingScope pins the status badge: one row
// per scope that has a pending question, the parent first.
func TestAlertRows_NamesEveryPendingScope(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Parent's question", nil)
	mgr.Enqueue("subagent-B", "B's question", nil)
	mgr.Enqueue("subagent-A", "A's question", nil)

	body := plainRows(p.AlertRows())
	for _, want := range []string{
		"you has a question · Parent's question",
		"subagent-A has a question · A's question",
		"subagent-B has a question · B's question",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("alert rows missing %q; got:\n%s", want, body)
		}
	}
	if !strings.Contains(body, "subagent-A has a question") {
		t.Errorf("alert rows missing the subagent scope; got:\n%s", body)
	}
}

// TestAlertRows_NoPendingQuestionsHasNoRows pins the empty case: the
// badge disappears once every question is answered.
func TestAlertRows_NoPendingQuestionsHasNoRows(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Q?", nil)
	if len(p.AlertRows()) != 1 {
		t.Fatalf("alert rows = %d, want 1", len(p.AlertRows()))
	}
	mgr.Answer(subagentquestions.ParentKey,
		mgr.Pending(subagentquestions.ParentKey)[0].ID, "yes")
	if rows := p.AlertRows(); len(rows) != 0 {
		t.Errorf("alert rows = %d after the answer, want 0", len(rows))
	}
}

// TestPickerRows_DrawsTheScopeAndTheHints pins the overlay's shape.
func TestPickerRows_DrawsTheScopeAndTheHints(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", []string{"go"})

	if rows := p.PickerRows(); len(rows) != 0 {
		t.Errorf("picker rows = %d while closed, want 0", len(rows))
	}
	p.Toggle()
	body := plainRows(p.PickerRows())
	for _, want := range []string{
		"Pending questions for you",
		"Pick a tool",
		"Keyboard:",
		"↑/↓ Questions",
		"←/→ Options",
		"enter Select",
		"esc Skip",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("picker rows missing %q; got:\n%s", want, body)
		}
	}
}

// TestPickerRows_TruncatesALongPrompt pins the prompt's own truncation:
// a long question is cut to 57 characters plus an ellipsis.
func TestPickerRows_TruncatesALongPrompt(t *testing.T) {
	p, mgr := newPanel(t)
	long := strings.Repeat("x", 80)
	mgr.Enqueue(subagentquestions.ParentKey, long, nil)

	p.Toggle()
	body := plainRows(p.PickerRows())
	if strings.Contains(body, long) {
		t.Error("the long prompt was not truncated")
	}
	if !strings.Contains(body, strings.Repeat("x", 57)+"...") {
		t.Errorf("picker rows missing the truncated prompt; got:\n%s", body)
	}
}

// TestPickerRows_SubagentScopeNamesTheSubagent pins the scope label: a
// real subagent renders its id verbatim.
func TestPickerRows_SubagentScopeNamesTheSubagent(t *testing.T) {
	p, mgr := newPanel(t)
	p.SetScope("subagent-1761234567-3")
	mgr.Enqueue("subagent-1761234567-3", "A's question", nil)

	p.Toggle()
	body := plainRows(p.PickerRows())
	if !strings.Contains(body, "Pending questions for subagent-1761234567-3") {
		t.Errorf("picker rows missing the subagent scope; got:\n%s", body)
	}
}

// ---------------------------------------------------------------- left/right nav (from ask_question_left_right_nav_test.go)

// TestAskQuestion_LeftRightNav_RightAdvancesOptionCursor verifies
// the forward step: pressing Right from option 0 advances to
// option 1, then to option 2. The picker remains open and the
// pending count stays unchanged.
func TestAskQuestion_LeftRightNav_RightAdvancesOptionCursor(t *testing.T) {
	p, mgr := newPanel(t)
	q := mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}

	p.Toggle()
	if !p.Active() {
		t.Fatal("picker did not open")
	}

	if got := p.optionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 1 {
		t.Errorf("option cursor after 1× Right = %d, want 1", got)
	}
	if !p.Active() {
		t.Fatalf("Right must not close the picker")
	}

	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 2 {
		t.Errorf("option cursor after 2× Right = %d, want 2", got)
	}

	// No commit: picker still open, manager still pending.
	if !p.Active() {
		t.Fatalf("Right must not commit; picker closed")
	}
	if got := mgr.PendingCount(subagentquestions.ParentKey); got != 1 {
		t.Errorf("PendingCount = %d after Right, want 1 (no commit)", got)
	}
	if p.answerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (no commit)", p.answerTarget)
	}
}

// TestAskQuestion_LeftRightNav_LeftRegressesOptionCursor verifies
// the backward step: starting on option 2, pressing Left moves
// the cursor back to 1, then to 0. The picker remains open the
// whole way.
func TestAskQuestion_LeftRightNav_LeftRegressesOptionCursor(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})

	p.Toggle()
	// Skip ahead to option 2.
	p.HandleKey(arrowKey(tea.KeyRight))
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 2 {
		t.Fatalf("preconditions: option cursor = %d, want 2", got)
	}

	p.HandleKey(arrowKey(tea.KeyLeft))
	if got := p.optionCursor; got != 1 {
		t.Errorf("option cursor after 1× Left = %d, want 1", got)
	}
	if !p.Active() {
		t.Fatalf("Left must not close the picker")
	}

	p.HandleKey(arrowKey(tea.KeyLeft))
	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor after 2× Left = %d, want 0", got)
	}
}

// TestAskQuestion_LeftRightNav_LeftClampsAtFirst pins the
// left-bound behaviour: pressing Left on option 0 leaves the
// cursor at 0 (no wrap to the end, no negative index). The
// picker must remain open.
func TestAskQuestion_LeftRightNav_LeftClampsAtFirst(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	p.Toggle()
	if got := p.optionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	p.HandleKey(arrowKey(tea.KeyLeft))
	p.HandleKey(arrowKey(tea.KeyLeft))
	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor after 2× Left on first = %d, want 0 (clamped)",
			got)
	}
	if !p.Active() {
		t.Fatalf("Left at first must not close the picker")
	}
}

// TestAskQuestion_LeftRightNav_RightClampsAtLast pins the
// right-bound behaviour: pressing Right past the last row keeps
// the cursor on the write-in row (no wrap to 0, no overflow). The
// picker must remain open and the manager must still show the
// question pending.
func TestAskQuestion_LeftRightNav_RightClampsAtLast(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	p.Toggle()
	// Move to the last real option.
	p.HandleKey(arrowKey(tea.KeyRight))
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 2 {
		t.Fatalf("preconditions: option cursor = %d, want 2", got)
	}

	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 3 {
		t.Errorf("option cursor after Right on last option = %d, want 3 (write-in row)", got)
	}
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 3 {
		t.Errorf("option cursor after 2× Right on last row = %d, want 3 (clamped)",
			got)
	}
	if !p.Active() {
		t.Fatalf("Right at last must not close the picker")
	}
	if got := mgr.PendingCount(subagentquestions.ParentKey); got != 1 {
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
	p, mgr := newPanel(t)
	q := mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python", "rust"})

	p.Toggle()
	if !p.Active() {
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
		p.HandleKey(arrowKey(code))
	}

	if !p.Active() {
		t.Fatalf("Left/Right must not close the picker")
	}
	if p.answerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (Left/Right must not commit)",
			p.answerTarget)
	}
	if got := mgr.PendingCount(subagentquestions.ParentKey); got != 1 {
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
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})

	p.Toggle()
	body := plainRows(p.PickerRows())
	for _, opt := range []string{"red", "green", "blue"} {
		if !strings.Contains(body, opt) {
			t.Errorf("picker overlay missing option %q; got:\n%s",
				opt, body)
		}
	}

	// Move to "green" (idx 1) and re-render. The marker '›'
	// precedes the highlighted option in the row.
	p.HandleKey(arrowKey(tea.KeyRight))
	body = plainRows(p.PickerRows())
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
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"First?", []string{"a", "b"})
	mgr.Enqueue(subagentquestions.ParentKey,
		"Second?", []string{"x", "y"})

	p.Toggle()
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 1 {
		t.Fatalf("preconditions: option cursor = %d, want 1", got)
	}

	// Down to the second question — option cursor must reset.
	p.HandleKey(arrowKey(tea.KeyDown))
	if got := p.cursor; got != 1 {
		t.Errorf("question cursor after Down = %d, want 1", got)
	}
	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor after Down = %d, want 0 (reset on Up/Down)",
			got)
	}

	// Up back to the first question — option cursor stays
	// reset (each Up/Down crosses a row, and any cross-row
	// navigation abandons the previous option preview).
	p.HandleKey(arrowKey(tea.KeyUp))
	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor after Up = %d, want 0 (reset on Up/Down)",
			got)
	}

	// Now advance again — sanity check the second question's
	// cursor still works after the reset cycle.
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 1 {
		t.Errorf("option cursor after Right from reset = %d, want 1", got)
	}
}

// TestAskQuestion_LeftRightNav_OpenPickerResetsOptionCursor
// covers the reopen path: pressing Left/Right inside an open
// picker moves the option cursor; closing the picker (Esc) and
// reopening it must reset the cursor to 0 even though the
// question's pending Options list is unchanged.
func TestAskQuestion_LeftRightNav_OpenPickerResetsOptionCursor(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node", "python"})

	p.Toggle()
	p.HandleKey(arrowKey(tea.KeyRight))
	if got := p.optionCursor; got != 1 {
		t.Fatalf("preconditions: option cursor = %d, want 1", got)
	}

	// Close via Esc, re-open via Ctrl+K.
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Active() {
		t.Fatal("Esc did not close the picker")
	}
	p.Toggle()
	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor after reopen = %d, want 0 (reset on open)",
			got)
	}
}

// TestAskQuestion_LeftRightNav_NoOptionsIsInert pins the
// free-form-question path: a pending question with a nil
// Options slice does not let Left/Right move any cursor; the
// picker stays open and no answer is staged.
func TestAskQuestion_LeftRightNav_NoOptionsIsInert(t *testing.T) {
	p, mgr := newPanel(t)
	mgr.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", nil)

	p.Toggle()
	if !p.Active() {
		t.Fatal("picker did not open")
	}

	// Pre-state guard: option cursor stays 0 because there are
	// no options to index into.
	if got := p.optionCursor; got != 0 {
		t.Fatalf("preconditions: option cursor = %d, want 0", got)
	}

	p.HandleKey(arrowKey(tea.KeyRight))
	p.HandleKey(arrowKey(tea.KeyLeft))
	p.HandleKey(arrowKey(tea.KeyRight))

	if got := p.optionCursor; got != 0 {
		t.Errorf("option cursor = %d on a free-form question, want 0 (inert)",
			got)
	}
	if !p.Active() {
		t.Fatalf("Left/Right on a free-form question must not close the picker")
	}
	if p.answerTarget != "" {
		t.Errorf("AnswerTarget = %q, want empty (Left/Right must not commit)",
			p.answerTarget)
	}
}
