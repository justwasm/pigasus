package main

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
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/subagentquestions"
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
