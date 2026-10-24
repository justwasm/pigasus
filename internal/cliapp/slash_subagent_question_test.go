package cliapp

// slash_cycle_subagent_question_test.go — cycle subagent-question-picker.
//
// Verifies the interactive TUI surface for AGY 1.3.6 changelog line 6:
//   - "<agent> has a question" alert renders in the status row
//   - Ctrl+K opens a picker that lists the active scope's pending
//     questions and routes answers back to the right subagent
//   - the headless `-p` runtime auto-skips with "(skipped)"
//   - multiple subagents route independently; one subagent's question
//     never bleeds into another's picker
//   - the `a` keybinding still falls through to the textarea when
//     there is no pending question (regression guard for the two
//     tests that share the key)
//
// Most assertions target the helper layer
// (runMode.subAgentQuestion* methods, subagentquestions.Manager +
// tools.AskQuestion) because driving a real bubbletea program from a
// test would require a TTY and the picker text depends on terminal
// width. The Alert-in-viewBody surface is asserted by walking
// runMode.View() and stripping ANSI.

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagentrouter"
	"github.com/justwasm/pigasus/internal/tools"
)

// newRunModeForSubagentQuestion constructs a runMode suitable for
// driving subAgentQuestions methods directly. The TUI's catalog is
// not initialised here; tests that want the catalog wiring should
// call runLiveTUI on a fully-built cfg, the same way the slash_btw
// tests use newSlashModel for the underlying mode.
func newRunModeForSubagentQuestion(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.subAgentQuestions == nil {
		t.Fatalf("runMode.subAgentQuestions must be initialised by NewRunMode")
	}
	// Default scope is the parent ("") — tests that exercise a
	// subagent scope rewrite this before enqueuing.
	m.subAgentQuestionsActive = subagentquestions.ParentKey
	m.subAgentQuestionPickerOpen = false
	m.subAgentQuestionPickerCursor = 0
	m.subAgentQuestionAlert = ""
	m.subAgentQuestionAnswerBuffer = ""
	m.subAgentQuestionAnswerTarget = ""
	return m
}

// viewBodyPlain returns runMode.View() with ANSI escapes stripped so
// the test can assert on plain text fragments regardless of which
// colour codes the renderer picked.
func viewBodyPlain(m *runMode) string {
	return ansi.Strip(m.View().Content)
}

// TestSubAgentQuestion_AlertSurfacesInViewBody pins the changelog
// wording "<agent> has a question" in the rendered status row when
// at least one question is pending for the parent scope.
func TestSubAgentQuestion_AlertSurfacesInViewBody(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)

	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"What port should the dev server bind?", []string{"3000", "8080"})
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}
	body := viewBodyPlain(m)
	if !strings.Contains(body, "you has a question") {
		t.Errorf("expected viewBody to render the parent alert 'you has a question'; got:\n%s",
			body)
	}
	if !strings.Contains(body, "What port should the dev server bind?") {
		t.Errorf("expected viewBody to include the question prompt; got:\n%s",
			body)
	}
}

// TestSubAgentQuestion_AlertNamesSubagentByID pins the per-subagent
// routing: the alert names the subagent by its subagent_id, not by
// the literal empty-string "you" key.
func TestSubAgentQuestion_AlertNamesSubagentByID(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	m.subAgentQuestionsActive = "subagent-1761234567-3"

	q := m.subAgentQuestions.Enqueue("subagent-1761234567-3",
		"Should I commit the WIP?", []string{"yes", "no"})
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}

	body := viewBodyPlain(m)
	if !strings.Contains(body, "subagent-1761234567-3 has a question") {
		t.Errorf("expected viewBody to name the subagent verbatim; got:\n%s",
			body)
	}
	if strings.Contains(body, "you has a question") {
		t.Errorf("viewBody should not show the parent alert when only a "+
			"subagent has a pending question; got:\n%s", body)
	}
}

// TestSubAgentQuestion_PickerOpensViaCtrlK verifies that the Ctrl+K
// keybinding routes through openSubAgentQuestionPicker and toggles
// the picker into the open state when a question is pending.
func TestSubAgentQuestion_PickerOpensViaCtrlK(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)

	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green", "blue"})
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("Ctrl+K did not open the picker with a pending question")
	}

	body := viewBodyPlain(m)
	if !strings.Contains(body, "Pending questions for you") {
		t.Errorf("picker overlay missing 'Pending questions for you'; got:\n%s",
			body)
	}
	if !strings.Contains(body, "Pick a colour") {
		t.Errorf("picker overlay missing the pending question; got:\n%s",
			body)
	}
}

// TestSubAgentQuestion_PickerClosesViaCtrlKAndEscape covers the
// toggle path: Ctrl+K opens, then closes; Esc only closes.
func TestSubAgentQuestion_PickerClosesViaCtrlKAndEscape(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey, "Q?", nil)

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("Ctrl+K should open picker")
	}
	// Second Ctrl+K closes.
	m.Update(ctrlKey('k'))
	if m.subAgentQuestionPickerOpen {
		t.Fatal("Ctrl+K should toggle picker closed")
	}

	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("Ctrl+K should re-open picker")
	}
	// Esc closes.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.subAgentQuestionPickerOpen {
		t.Fatal("Esc should close the picker")
	}
}

// TestSubAgentQuestion_PickerRoutedAnswerUnblocksWait confirms the
// round-trip: the picker's answer path Answer()s the right question
// and a concurrent Wait returns that answer (or "(skipped)" when
// the answer is empty).
func TestSubAgentQuestion_PickerRoutedAnswerUnblocksWait(t *testing.T) {
	mgr := subagentquestions.NewManager(false)
	q := mgr.Enqueue(subagentquestions.ParentKey, "Pick a tool", []string{"go", "node"})

	// Simulate a subagent goroutine blocked on Wait while the picker
	// answers the question on the parent goroutine.
	type waitResult struct {
		answer string
		ok     bool
	}
	resCh := make(chan waitResult, 1)
	go func() {
		ans, ok := q.Wait()
		resCh <- waitResult{ans, ok}
	}()

	if err := mgr.Answer(subagentquestions.ParentKey, q.ID, "go"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	res := <-resCh
	if !res.ok {
		t.Fatalf("Wait returned ok=false")
	}
	if res.answer != "go" {
		t.Errorf("Wait answer = %q, want %q", res.answer, "go")
	}
	if got := mgr.PendingCount(subagentquestions.ParentKey); got != 0 {
		t.Errorf("PendingCount after Answer = %d, want 0", got)
	}
}

// TestSubAgentQuestion_SubmitRoutesViaInputStageing drives the
// runMode end-to-end: enqueue a question, open the picker, select
// the question, type the answer into the input, and Enter. The
// submitPrompt() intercept calls submitSubAgentQuestionAnswer() and
// the question's Wait must return the typed answer.
func TestSubAgentQuestion_SubmitRoutesViaInputStageing(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Name the new file", nil)

	// Open picker, cursor=0 already, press Enter to stage.
	m.Update(ctrlKey('k'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatal("picker did not open")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Text: "\n"}))
	if m.subAgentQuestionAnswerTarget == "" {
		t.Fatal("picker did not stage a question target")
	}

	// Type the answer into the existing input textarea.
	for _, r := range "example.txt" {
		m.Update(runeKey(r))
	}
	if got := m.input.Value(); got != "example.txt" {
		t.Fatalf("input = %q, want %q", got, "example.txt")
	}

	// Wait goroutine: the answer should arrive when submitPrompt
	// runs submitSubAgentQuestionAnswer.
	type waitResult struct {
		answer string
		ok     bool
	}
	resCh := make(chan waitResult, 1)
	go func() {
		ans, ok := q.Wait()
		resCh <- waitResult{ans, ok}
	}()

	m.streaming = false
	_, _ = m.Update(enterKey())

	res := <-resCh
	if !res.ok {
		t.Fatalf("Wait returned ok=false")
	}
	if res.answer != "example.txt" {
		t.Errorf("Wait answer = %q, want %q", res.answer, "example.txt")
	}
	if got := m.subAgentQuestions.PendingCount(subagentquestions.ParentKey); got != 0 {
		t.Errorf("PendingCount after submit = %d, want 0", got)
	}
	if got := m.input.Value(); got != "" {
		t.Errorf("input not cleared after submit: %q", got)
	}
}

// TestSubAgentQuestion_EmptyAnswerSkips pins the "empty answer is a
// skip" behaviour: when the picker stages a question and the user
// submits without typing, the model sees "(skipped)" and the
// pending count drops to zero.
func TestSubAgentQuestion_EmptyAnswerSkips(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	q := m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a colour", []string{"red", "green"})

	m.Update(ctrlKey('k'))
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Text: "\n"}))
	if m.subAgentQuestionAnswerTarget == "" {
		t.Fatal("picker did not stage a target")
	}

	resCh := make(chan string, 1)
	go func() {
		ans, _ := q.Wait()
		resCh <- ans
	}()

	m.streaming = false
	_, _ = m.Update(enterKey())

	ans := <-resCh
	if ans != subagentquestions.SkipAnswer {
		t.Errorf("empty-submit answer = %q, want %q", ans,
			subagentquestions.SkipAnswer)
	}
	if got := m.subAgentQuestions.PendingCount(subagentquestions.ParentKey); got != 0 {
		t.Errorf("PendingCount after empty-submit = %d, want 0", got)
	}
}

// TestSubAgentQuestion_MultipleSubagentsRouteIndependently verifies
// that one subagent's pending question does not surface on a
// sibling subagent's picker — and that answering one does not
// resolve the other.
func TestSubAgentQuestion_MultipleSubagentsRouteIndependently(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	qA := m.subAgentQuestions.Enqueue("subagent-A", "A's question", nil)
	qB := m.subAgentQuestions.Enqueue("subagent-B", "B's question", nil)

	// Active scope = A; the picker lists A only.
	m.subAgentQuestionsActive = "subagent-A"
	if got := len(m.subAgentQuestions.Pending("subagent-A")); got != 1 {
		t.Errorf("Pending(subagent-A) = %d, want 1", got)
	}
	if got := len(m.subAgentQuestions.Pending("subagent-B")); got != 1 {
		t.Errorf("Pending(subagent-B) = %d, want 1", got)
	}

	body := viewBodyPlain(m)
	if !strings.Contains(body, "subagent-A has a question") {
		t.Errorf("expected A's alert; got:\n%s", body)
	}
	if !strings.Contains(body, "subagent-B has a question") {
		t.Errorf("expected B's alert; got:\n%s", body)
	}

	// Answer A; B stays pending.
	if err := m.subAgentQuestions.Answer("subagent-A", qA.ID, "A's reply"); err != nil {
		t.Fatalf("Answer A: %v", err)
	}
	if got := m.subAgentQuestions.PendingCount("subagent-A"); got != 0 {
		t.Errorf("A's PendingCount = %d after Answer, want 0", got)
	}
	if got := m.subAgentQuestions.PendingCount("subagent-B"); got != 1 {
		t.Errorf("B's PendingCount = %d after A's answer, want 1", got)
	}

	// Cross-scope Answer must fail (B's question from A's scope).
	if err := m.subAgentQuestions.Answer("subagent-A", qB.ID, "wrong-scope"); err == nil {
		t.Errorf("cross-scope Answer unexpectedly succeeded; expected error")
	}

	// Drain for "subagent-A" must not touch "subagent-B".
	if drained := m.subAgentQuestions.Drain("subagent-A"); len(drained) != 0 {
		t.Errorf("Drain(subagent-A) returned %d, want 0", len(drained))
	}
	if got := m.subAgentQuestions.PendingCount("subagent-B"); got != 1 {
		t.Errorf("B's PendingCount = %d after Drain(A), want 1", got)
	}
}

// TestSubAgentQuestion_AFallsThroughToTextareaWhenNoPending pins
// the regression guard: pressing `a` while the input is empty and
// no question is pending must type the rune into the textarea. The
// pending-question guard on the `a` keybinding is what kept
// Test_ShiftEnterNewline and TestVimDisabledLeavesInputAlone green.
func TestSubAgentQuestion_AFallsThroughToTextareaWhenNoPending(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	if got := m.subAgentQuestions.TotalPending(); got != 0 {
		t.Fatalf("preconditions: TotalPending = %d, want 0", got)
	}

	m.Update(runeKey('a'))
	if got := m.input.Value(); got != "a" {
		t.Fatalf("input = %q, want %q (regression: 'a' must fall through to textarea)",
			got, "a")
	}
	if m.subAgentQuestionPickerOpen {
		t.Fatalf("picker opened despite no pending questions")
	}
}

// TestSubAgentQuestion_AOpensPickerWhenPending is the positive side
// of the same guard: with one pending question, pressing `a` while
// the input is empty opens the picker.
func TestSubAgentQuestion_AOpensPickerWhenPending(t *testing.T) {
	m := newRunModeForSubagentQuestion(t)
	_ = m.subAgentQuestions.Enqueue(subagentquestions.ParentKey,
		"Pick a tool", []string{"go", "node"})

	m.Update(runeKey('a'))
	if !m.subAgentQuestionPickerOpen {
		t.Fatalf("'a' did not open picker with pending question")
	}
}

// TestSubAgentQuestion_AskQuestionToolRoutesThroughManager drives
// the tools.AskQuestion tool directly with a wired Manager and
// confirms the Wait round-trip the catalog->registryRunner path
// will use in production.
func TestSubAgentQuestion_AskQuestionToolRoutesThroughManager(t *testing.T) {
	mgr := subagentquestions.NewManager(false)
	tool := &tools.AskQuestion{
		SubAgentQuestions: mgr,
		SubAgentID:        subagentquestions.ParentKey,
	}

	args, err := json.Marshal(map[string]any{
		"questions": []map[string]any{{
			"question": "Choose a port",
			"options":  []string{"3000", "8080"},
		}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	// Run the tool in a goroutine because it blocks on Wait; answer
	// from the main goroutine to mimic the picker.
	type runResult struct {
		out any
		err error
	}
	resCh := make(chan runResult, 1)
	// enqueued is closed by the goroutine once the tool's Run
	// has registered the question on the Manager, so the main
	// goroutine can call Pending without racing the Enqueue.
	enqueued := make(chan struct{})
	go func() {
		// Tap the Manager with a brief wait loop that
		// notices the Enqueue by polling. We could
		// also expose an "OnEnqueue" hook, but polling
		// is simpler and good enough for a unit test.
		defer close(enqueued)
		out, err := tool.Run(context.Background(), args)
		resCh <- runResult{out, err}
	}()

	// Wait until the goroutine has had a chance to register the
	// question. The tool's first action is Enqueue, so once
	// mgr.Pending returns >= 1 the goroutine is blocked in Wait.
	deadline := time.After(5 * time.Second)
	for {
		if got := mgr.PendingCount(subagentquestions.ParentKey); got >= 1 {
			break
		}
		select {
		case <-deadline:
			t.Fatalf("Pending count never reached 1; goroutine stuck before Enqueue")
		case <-time.After(5 * time.Millisecond):
		}
	}

	pending := mgr.Pending(subagentquestions.ParentKey)
	if len(pending) != 1 {
		t.Fatalf("Pending count = %d, want 1", len(pending))
	}
	if err := mgr.Answer(subagentquestions.ParentKey, pending[0].ID, "3000"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	res := <-resCh
	if res.err != nil {
		t.Fatalf("AskQuestion.Run error: %v", res.err)
	}
	got, ok := res.out.(string)
	if !ok {
		t.Fatalf("AskQuestion returned %T, want string", res.out)
	}
	if got != "3000" {
		t.Errorf("AskQuestion returned %q, want %q", got, "3000")
	}
}

// TestSubAgentQuestion_AskQuestionNilManagerKeepsLegacyPath pins
// the backward-compat behaviour: a nil SubAgentQuestions keeps the
// cycle-214 noUserText reminder the tool returned before this
// cycle.
func TestSubAgentQuestion_AskQuestionNilManagerKeepsLegacyPath(t *testing.T) {
	tool := &tools.AskQuestion{} // nil SubAgentQuestions

	args, err := json.Marshal(map[string]any{
		"questions": []map[string]any{{"question": "Q?"}},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	got, ok := out.(string)
	if !ok {
		t.Fatalf("Run returned %T, want string", out)
	}
	if got != "The user cannot be asked in this mode, so no answer was provided." {
		t.Errorf("Run = %q, want noUserText", got)
	}
}

// TestSubAgentQuestion_HeadlessManagerAutoSkips pins the print `-p`
// path: a headless Manager's Enqueue returns a question already in
// StateSkipped whose Answer is the SkipAnswer literal. Wait
// returns immediately without blocking.
func TestSubAgentQuestion_HeadlessManagerAutoSkips(t *testing.T) {
	mgr := subagentquestions.NewManager(true)
	q := mgr.Enqueue(subagentquestions.ParentKey, "Q?", nil)
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}
	if q.State != subagentquestions.StateSkipped {
		t.Errorf("headless Enqueue state = %q, want %q", q.State,
			subagentquestions.StateSkipped)
	}
	if q.Answer != subagentquestions.SkipAnswer {
		t.Errorf("headless Enqueue answer = %q, want %q", q.Answer,
			subagentquestions.SkipAnswer)
	}

	// Wait must return immediately (non-blocking) with SkipAnswer.
	type waitResult struct {
		answer string
		ok     bool
	}
	resCh := make(chan waitResult, 1)
	go func() {
		ans, ok := q.Wait()
		resCh <- waitResult{ans, ok}
	}()
	res := <-resCh
	if !res.ok {
		t.Fatal("Wait ok = false on headless skip")
	}
	if res.answer != subagentquestions.SkipAnswer {
		t.Errorf("Wait answer = %q, want %q", res.answer,
			subagentquestions.SkipAnswer)
	}

	if got := mgr.TotalPending(); got != 0 {
		t.Errorf("TotalPending = %d on headless, want 0", got)
	}
}

// TestSubAgentQuestion_HeadlessAskQuestionReturnsSkipAnswer is the
// print-path integration: the tool + a headless Manager returns
// SkipAnswer without any goroutine having to call Answer first.
// This is the behaviour the AGY changelog line asks for.
func TestSubAgentQuestion_HeadlessAskQuestionReturnsSkipAnswer(t *testing.T) {
	mgr := subagentquestions.NewManager(true)
	tool := &tools.AskQuestion{
		SubAgentQuestions: mgr,
		SubAgentID:        subagentquestions.ParentKey,
	}
	args, _ := json.Marshal(map[string]any{
		"questions": []map[string]any{{"question": "Pick a tool"}},
	})
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, ok := out.(string)
	if !ok {
		t.Fatalf("Run returned %T, want string", out)
	}
	if got != subagentquestions.SkipAnswer {
		t.Errorf("headless Run = %q, want %q", got, subagentquestions.SkipAnswer)
	}
}

// ---------------------------------------------------------------- left/right nav (from ask_question_left_right_nav_test.go)

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

// ---------------------------------------------------------------- parser

func TestParseAtSubagentPrompt_HappyPath(t *testing.T) {
	tests := []struct {
		raw     string
		name    string
		message string
		ok      bool
	}{
		{"@explore-bot find the auth code", "explore-bot", "find the auth code", true},
		{"@explore-bot ", "explore-bot", "", true}, // parse match; caller decides "no body"
		{"@Explore-Bot hi", "Explore-Bot", "hi", true},
		{"@a x", "a", "x", true},
		{"@foo.bar.baz hello world", "foo.bar.baz", "hello world", true},
		{"@_under_score hi", "_under_score", "hi", true},
		{"@name with\ttabs", "name", "with\ttabs", true}, // tabs are terminators too
		{"@name line\nbreak", "name", "line\nbreak", true},
	}
	for _, tc := range tests {
		name, msg, ok := parseAtSubagentPrompt(tc.raw)
		if ok != tc.ok || name != tc.name || msg != tc.message {
			t.Errorf("parseAtSubagentPrompt(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.raw, name, msg, ok, tc.name, tc.message, tc.ok)
		}
	}
}

func TestParseAtSubagentPrompt_RejectsNonMatch(t *testing.T) {
	tests := []struct {
		raw string
		why string
	}{
		{"", "empty buffer"},
		{"@", "bare @, no name"},
		{"@explore-bot", "still typing — no whitespace yet"},
		{"hello world", "no leading @"},
		{"/help", "slash command"},
		{"@/", "name slot contains slash"},
		{"@file/foo.txt", "name slot contains slash (regression)"},
		{"@explore-bot@next", "name slot contains @"},
		{"@name!", "name slot contains punctuation"},
		// Long name (65 runes) must fail (> atNameMaxLen=64).
		{strings.Repeat("a", 65), "name over 64 chars"},
		// name with control char
		{"@explo\x01re-bot hello", "name slot contains control"},
		// Leading space inside name slot — whitespace immediately
		// after @ terminates with empty name, parser rejects.
		{"@ hi", "empty name"},
	}
	for _, tc := range tests {
		name, msg, ok := parseAtSubagentPrompt(tc.raw)
		if ok {
			t.Errorf("%s: parseAtSubagentPrompt(%q) unexpectedly matched: (%q,%q)",
				tc.why, tc.raw, name, msg)
		}
	}
}

func TestParseAtSubagentPrompt_AcceptsLongNameAtBoundary(t *testing.T) {
	// 64 chars is the boundary; we accept it.
	raw := "@" + strings.Repeat("a", 64) + " hello"
	name, _, ok := parseAtSubagentPrompt(raw)
	if !ok || len(name) != 64 {
		t.Errorf("boundary 64-chars name: got ok=%v name_len=%d, want true,64", ok, len(name))
	}
}

// ---------------------------------------------------------------- runMode routing

// atFakeSender records Send calls so tests can assert which (if any)
// message the dispatch path delivered. Implements subagentrouter.Sender.
type atFakeSender struct {
	called atomic.Int32
	last   atomic.Value // string
	err    error
	cb     func(string)
}

func (f *atFakeSender) Send(_ context.Context, msg string) error {
	f.called.Add(1)
	f.last.Store(msg)
	if f.cb != nil {
		f.cb(msg)
	}
	return f.err
}
func (f *atFakeSender) Calls() int32 { return f.called.Load() }
func (f *atFakeSender) LastMsg() string {
	v, _ := f.last.Load().(string)
	return v
}

func newAtRouterWith(t *testing.T, name string, sender subagentrouter.Sender) *runMode {
	t.Helper()
	// DefaultRouter is package-scoped; tests would otherwise see
	// each other's registrations and the second Register would
	// silently fail with ErrAlreadyRegistered. Replace it with a
	// fresh Manager at the start of every test so each test owns
	// its own registry. AtCurRouter captures the previous one so
	// the global pointer is restored after the test suite finishes
	// (cosmetic — the test binary ends, but discipline matches the
	// rest of the package).
	prev := DefaultRouter
	DefaultRouter = subagentrouter.NewManager()
	t.Cleanup(func() { DefaultRouter = prev })
	m := newSlashModel(t)
	m.atRouter = DefaultRouter
	if _, err := m.atRouter.Register(name, sender); err != nil {
		t.Fatalf("Register(%q): %v", name, err)
	}
	return m
}

// submitAt is the test entry point — wrap buffer in m.input the
// same way TestTUISessionPersistence does, then submit.
func submitAt(t *testing.T, m *runMode, buf string) {
	t.Helper()
	m.input.SetValue(buf)
	cmd := m.submitPrompt()
	if cmd != nil {
		_ = cmd // stream prompts not exercised; at-routing is sync
	}
}

func TestHandlePromptAtPrefix_RoutesToRegistered(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	if handled, _ := m.handlePromptAtPrefix("@explore-bot find the auth code"); !handled {
		t.Fatal("handlePromptAtPrefix should have handled a routed buffer")
	}
	if got := sender.Calls(); got != 1 {
		t.Errorf("Sender.calls = %d, want 1", got)
	}
	if got := sender.LastMsg(); got != "find the auth code" {
		t.Errorf("Sender.lastMsg = %q, want %q", got, "find the auth code")
	}
	// Echo: at least one transcript row should mention explore-bot
	// + the message body.
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "explore-bot") {
		t.Errorf("transcript missing explore-bot mention: %v", m.response)
	}
	if !strings.Contains(joined, "find the auth code") {
		t.Errorf("transcript missing message body: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_LookupCaseInsensitive(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	// User types a different-cased name; the lookup must still resolve.
	if handled, _ := m.handlePromptAtPrefix("@Explore-Bot hi"); !handled {
		t.Fatal("expected routed")
	}
	if sender.Calls() != 1 {
		t.Errorf("Sender.calls = %d, want 1", sender.Calls())
	}
}

func TestHandlePromptAtPrefix_UnknownNameSurfacesError(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("@ghost hello world"); !handled {
		t.Fatal("handlePromptAtPrefix should still handle (and surface an error)")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "No subagent named") {
		t.Errorf("transcript missing unknown-name error; got: %v", m.response)
	}
	if !strings.Contains(joined, "ghost") {
		t.Errorf("transcript should mention ghost; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_EmptyMessageSurfacesError(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	// "@explore-bot " — name matches, message is empty.
	if handled, _ := m.handlePromptAtPrefix("@explore-bot "); !handled {
		t.Fatal("handlePromptAtPrefix should still handle (and surface an error)")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "no message body") {
		t.Errorf("transcript missing empty-body hint; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_DispatchErrorIsEchoed(t *testing.T) {
	sender := &atFakeSender{err: errors.New("pipe closed")}
	m := newAtRouterWith(t, "explore-bot", sender)
	if handled, _ := m.handlePromptAtPrefix("@explore-bot hi"); !handled {
		t.Fatal("expected handled")
	}
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "pipe closed") {
		t.Errorf("transcript should surface the dispatch failure; got: %v", m.response)
	}
}

func TestHandlePromptAtPrefix_NonAtBufferFallsThrough(t *testing.T) {
	// "@file/foo.txt" is the regression-guard case: the parser
	// disqualifies it (slash in name slot), so the routing layer
	// returns false and lets submitPrompt take the chat path.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "file", sender) // even if name existed, this buffer wouldn't bind to it
	if handled, _ := m.handlePromptAtPrefix("@file/foo.txt hello"); handled {
		t.Fatal("a buffer with / in the name slot must NOT route")
	}
	if sender.Calls() != 0 {
		t.Errorf("Sender.calls = %d, want 0 (no dispatch)", sender.Calls())
	}
}

func TestHandlePromptAtPrefix_BareAtFallsThrough(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("@"); handled {
		t.Fatal("a bare @ must not be interpreted as a routed prompt")
	}
	if handled, _ := m.handlePromptAtPrefix("@explore-bot"); handled {
		t.Fatal("a @<name> with no whitespace must not be interpreted as a routed prompt")
	}
}

func TestHandlePromptAtPrefix_PlainChatFallsThrough(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	if handled, _ := m.handlePromptAtPrefix("hello world"); handled {
		t.Fatal("plain chat must not be intercepted")
	}
	if handled, _ := m.handlePromptAtPrefix("/help"); handled {
		t.Fatal("slash command must not be intercepted by at-routing")
	}
}

// ---------------------------------------------------------------- end-to-end through submitPrompt

func TestSubmitPrompt_AtSubagentRouting_HappyPath(t *testing.T) {
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	submitAt(t, m, "@explore-bot find the auth code")
	if sender.Calls() != 1 {
		t.Errorf("Sender.calls = %d, want 1", sender.Calls())
	}
	if got := sender.LastMsg(); got != "find the auth code" {
		t.Errorf("Sender.lastMsg = %q, want %q", got, "find the auth code")
	}
}

func TestSubmitPrompt_AtSubagentRouting_UnknownNameNoBackend(t *testing.T) {
	m := newSlashModel(t)
	m.atRouter = subagentrouter.NewManager()
	submitAt(t, m, "@ghost what is up")
	// The error row must mention "ghost" so the user knows which name was rejected.
	joined := stripColor(strings.Join(m.response, "\n"))
	if !strings.Contains(joined, "ghost") {
		t.Errorf("transcript should mention the unknown name; got: %v", m.response)
	}
}

func TestSubmitPrompt_AtSubagentRouting_DoesNotTouchBackend(t *testing.T) {
	// Critical regression guard: a `@<name> <msg>` prompt must
	// NOT be appended to m.session (the chat session) and must
	// NOT call the backend. The cycle-231 layer is pure routing.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	before := len(m.session)
	submitAt(t, m, "@explore-bot do something")
	if got := len(m.session); got != before {
		t.Errorf("submitPrompt appended %d entries to m.session; want no change", got-before)
	}
}

func TestSubmitPrompt_NonRoutedBuffersUnchanged(t *testing.T) {
	// Sanity: a chat buffer still appends a user message, AND the
	// @-router did not capture it. This is the cycle-225-style
	// guard: every legacy path stays intact.
	sender := &atFakeSender{}
	m := newAtRouterWith(t, "explore-bot", sender)
	submitAt(t, m, "hello world")
	if sender.Calls() != 0 {
		t.Errorf("chat buffer accidentally routed to subagent")
	}
	if len(m.session) == 0 {
		t.Fatalf("chat submit did not register a session message")
	}
}

func withAtFileFixtures(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := []string{
		"main.go",
		"main_test.go",
		"other.go",
		"cmd/pigasus/main.go",
		"cmd/pigasus/main_test.go",
		"internal/atfiles/atfiles.go",
	}
	for _, f := range files {
		p := filepath.Join(dir, f)
		_ = os.MkdirAll(filepath.Dir(p), 0o755)
		_ = os.WriteFile(p, []byte(""), 0o644)
	}
	return dir
}

func newAtFileDropdownTestMode(t *testing.T) *runMode {
	t.Helper()
	if os.Getenv("ANTIGRAVITY_HOME") == "" {
		t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	}
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	return m
}

func TestRunMode_AtFileDropdownView_EmptyBuffer(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	if got := m.atFileView(); got != "" {
		t.Errorf("atFileView() with empty buffer = %q, want \"\"", got)
	}
}

func TestRunMode_AtFileDropdownView_FiresAfterNewline(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	m.cwd = withAtFileFixtures(t)
	m.input.SetValue("first line\n@ma")
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	if !m.atFileDropdown.Visible() {
		t.Fatalf("atFileDropdown should be visible after newline trigger")
	}
	rendered := m.atFileView()
	if rendered == "" {
		t.Fatalf("atFileView() returned empty string for visible dropdown")
	}
	if !strings.Contains(rendered, "main.go") {
		t.Errorf("rendered overlay missing candidate main.go: %q", rendered)
	}
}

func TestRunMode_AtFileDropdown_TabSplicesInput(t *testing.T) {
	m := newAtFileDropdownTestMode(t)
	m.cwd = withAtFileFixtures(t)
	m.input.SetValue("look at @ma")
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	if !m.atFileDropdown.Visible() {
		t.Fatalf("dropdown should be visible")
	}
	if !m.tabSpliceAtFile(true) {
		t.Fatalf("tabSpliceAtFile should have mutated the input")
	}
	got := m.input.Value()
	if !strings.HasPrefix(got, "look at @") {
		t.Errorf("post-splice prefix = %q, want \"look at @\"", got)
	}
	if strings.HasSuffix(got, " ") {
		t.Errorf("post-splice should not append a trailing space; got %q", got)
	}
	if !m.atFileDropdown.Visible() {
		t.Errorf("Tab must keep the picker open for directory/file browsing")
	}
}

// ---------------------------------------------------------------- agent not found CLI tests (from agent_not_found_test.go)

const wagyuBinary = "/tmp/wagyu-anf"

func requireWagyuBinary(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(wagyuBinary); err != nil {
		t.Skipf("binary %s not present; run `go build -o %s ./cmd/pigasus` first: %v",
			wagyuBinary, wagyuBinary, err)
	}
}

func runWagyu(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	cmd := exec.Command(wagyuBinary, args...)
	cmd.Env = append(os.Environ(), "OPENAGY_BACKEND=shim")
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("unexpected run error (non-ExitError): %v", err)
	}
	return stdout.String(), stderr.String(), code
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

func TestAgentFlag_ValidNameContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi", "--agent", "default")
	if code != 0 {
		t.Errorf("--agent default -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("--agent default -p hi: stdout is empty")
	}
	if strings.TrimSpace(out) == "" {
		t.Errorf("--agent default -p hi stdout is empty")
	}
}

func TestAgentFlag_MissingContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi")
	if code != 0 {
		t.Errorf("no --agent -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("no --agent -p hi: stdout is empty")
	}
}

func TestAgentFlag_EmptyValueContinues(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "-p", "hi", "--agent=")
	if code != 0 {
		t.Errorf("--agent= -p hi exit code = %d, want 0", code)
	}
	if out == "" {
		t.Errorf("--agent= -p hi: stdout is empty")
	}
}

func TestAgentFlag_UnknownInPrintMode_ExitsOne(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "nosuchagent"
	out, errb, code := runWagyu(t, "-p", "hi", "--agent", missing)

	if code != 1 {
		t.Errorf("--agent %s -p hi exit code = %d, want 1", missing, code)
	}
	if out != "" {
		t.Errorf("--agent %s -p hi: stdout should be empty, got %q", missing, out)
	}
	wantMsg := `error: unknown agent "` + missing + `" (no custom agents are available)`
	if !strings.Contains(errb, wantMsg) {
		t.Errorf("--agent %s -p hi: stderr missing %q\ngot: %q", missing, wantMsg, errb)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--agent %s -p hi: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
	if strings.Contains(errb, "Available agents:") || strings.Contains(errb, "available: default") {
		t.Errorf("--agent %s -p hi: default must not be listed\ngot: %q", missing, errb)
	}
}

func TestAgentFlag_UnknownInPrintMode_LongForm(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "weirdagent"
	out, errb, code := runWagyu(t, "--print", "hi", "--agent", missing)
	if code != 1 {
		t.Errorf("--print --agent %s exit code = %d, want 1", missing, code)
	}
	if out != "" {
		t.Errorf("--print --agent %s: stdout should be empty, got %q", missing, out)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--print --agent %s: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
}

func TestAgentFlag_UnknownInPrintMode_PromptAlias(t *testing.T) {
	requireWagyuBinary(t)
	const missing = "not-real"
	_, errb, code := runWagyu(t, "--prompt", "hi", "--agent", missing)
	if code != 1 {
		t.Errorf("--prompt --agent %s exit code = %d, want 1", missing, code)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--prompt --agent %s: stderr missing the verbatim name %q\ngot: %q",
			missing, missing, errb)
	}
}

func TestAgentsCLISubcommand_StillWorks(t *testing.T) {
	requireWagyuBinary(t)
	out, _, code := runWagyu(t, "agents")
	if code != 0 {
		t.Errorf("agents exit code = %d, want 0", code)
	}
	if out != "" {
		t.Errorf("agents stdout should be empty, got %q", out)
	}
}

func TestAgentFlag_AvailableList_UsesFrontmatterNames(t *testing.T) {
	requireWagyuBinary(t)
	ws := t.TempDir()
	home := t.TempDir()
	dir := filepath.Join(ws, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, name := range map[string]string{"1.md": "zebra", "2.md": "alpha"} {
		body := "---\nname: " + name + "\ndescription: d\n---\nbody\n"
		if err := os.WriteFile(filepath.Join(dir, file), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cmd := exec.Command(wagyuBinary, "-p", "hi", "--agent", "nosuch")
	cmd.Dir = ws
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if code := exitCode(err); code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if want := `error: unknown agent "nosuch" (available: alpha, zebra)`; !strings.Contains(string(out), want) {
		t.Errorf("output missing %q:\n%s", want, out)
	}
}

func TestAgentFlag_InteractiveWarning(t *testing.T) {
	requireWagyuBinary(t)
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const missing = "nosuchagent"
	out, errb, code := runWagyu(t, "--agent", missing)
	_ = code
	if out != "" {
		t.Logf("--agent %s (no -p): stdout = %q", missing, out)
	}
	if !strings.Contains(errb, "Agent Not Found:") {
		t.Errorf("--agent %s (no -p) stderr missing banner title:\n%q", missing, errb)
	}
	if !strings.Contains(errb, missing) {
		t.Errorf("--agent %s (no -p) stderr missing the verbatim name:\n%q", missing, errb)
	}
	if !strings.Contains(errb, "(no custom agents are available)") {
		t.Errorf("--agent %s (no -p) stderr missing the empty-catalogue clause:\n%q", missing, errb)
	}
	if strings.Contains(errb, "Available agents:") {
		t.Errorf("--agent %s (no -p) stderr still uses the old list:\n%q", missing, errb)
	}
	if !strings.Contains(errb, "Using the default agent instead.") {
		t.Errorf("--agent %s (no -p) stderr missing the fallback tail:\n%q", missing, errb)
	}
}
