// subagentquestions_test.go — unit tests for the cycle
// subagent-question-picker data model.
//
// The Manager owns per-subagent pending question slices; every
// public method on it is exercised here. The test cases cover the
// 5 invariants the changelog line demands:
//
//  1. parent + subagents share one Manager with disjoint keys
//     (parent = "", subagent = "subagent-...");
//  2. a pending question survives Drain/List/AllPending so the
//     picker's history tab can render it;
//  3. answering transitions State to answered and unblocks any
//     Wait() caller;
//  4. headless mode auto-skips on Enqueue (the -p path);
//  5. multiple pending questions route to the correct subagent
//     and answerer.
package subagentquestions

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// TestEnqueuePendingIsInteractive confirms the TUI's path: a
// freshly enqueued question lives in StatePending until Answer is
// called.
func TestEnqueuePendingIsInteractive(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue(ParentKey, "Which directory?", nil)
	if q == nil {
		t.Fatal("Enqueue returned nil")
	}
	if q.State != StatePending {
		t.Errorf("State = %q, want %q", q.State, StatePending)
	}
	if m.PendingCount(ParentKey) != 1 {
		t.Errorf("PendingCount = %d, want 1", m.PendingCount(ParentKey))
	}
}

// TestEnqueueHeadlessAutoSkips confirms the print path's
// behaviour: Enqueue in a headless Manager transitions the
// question to StateSkipped and Answer reports SkipAnswer.
func TestEnqueueHeadlessAutoSkips(t *testing.T) {
	m := NewManager(true)
	if !m.Headless() {
		t.Fatal("Headless() = false, want true")
	}
	q := m.Enqueue(ParentKey, "Anything?", nil)
	if q.State != StateSkipped {
		t.Errorf("State = %q, want %q", q.State, StateSkipped)
	}
	if q.Answer != SkipAnswer {
		t.Errorf("Answer = %q, want %q", q.Answer, SkipAnswer)
	}
	if m.PendingCount(ParentKey) != 0 {
		t.Errorf("PendingCount = %d, want 0 (headless auto-skip)", m.PendingCount(ParentKey))
	}
}

// TestAnswerUnblocksWait exercises the question/answer
// choreography the subagent's loop drives: Enqueue from the tool
// runner, Wait from the goroutine that issued the ask_question
// call, Answer from the picker.
func TestAnswerUnblocksWait(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue("subagent-1", "Pick a colour", []string{"red", "blue"})

	done := make(chan struct{})
	var got string
	go func() {
		got, _ = q.Wait()
		close(done)
	}()

	// Give the goroutine a tick to enter Wait. Without the
	// tick, Answer might land before Wait starts (the channel
	// close still unblocks Wait, but only on the next read).
	time.Sleep(10 * time.Millisecond)

	if err := m.Answer("subagent-1", q.ID, "red"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait did not unblock after Answer")
	}
	if got != "red" {
		t.Errorf("Wait returned %q, want %q", got, "red")
	}
	if q.State != StateAnswered {
		t.Errorf("State = %q, want %q", q.State, StateAnswered)
	}
	if q.Answer != "red" {
		t.Errorf("Answer = %q, want %q", q.Answer, "red")
	}
}

// TestAnswerScopedToSubagent confirms a wrong-subagent Answer
// call is rejected so the picker cannot address the wrong
// session's question.
func TestAnswerScopedToSubagent(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue("subagent-1", "Which?", nil)
	if err := m.Answer("subagent-2", q.ID, "wrong scope"); err == nil {
		t.Fatal("Answer on wrong subagent should error")
	}
	if q.State != StatePending {
		t.Errorf("State = %q, want pending (wrong-scope Answer must not transition)", q.State)
	}
}

// TestMultipleSubagentsPendingIsolated exercises the routing
// guarantee the changelog line demands: a parent question and
// two subagent questions all live in the same Manager without
// leaking into each other's lists.
func TestMultipleSubagentsPendingIsolated(t *testing.T) {
	m := NewManager(false)
	parentQ := m.Enqueue(ParentKey, "Parent Q", nil)
	bot1Q := m.Enqueue("subagent-bot-1", "Bot1 Q", nil)
	bot2Q := m.Enqueue("subagent-bot-2", "Bot2 Q", nil)

	if got := len(m.List(ParentKey)); got != 1 || m.List(ParentKey)[0].ID != parentQ.ID {
		t.Errorf("parent list = %+v, want [%s]", m.List(ParentKey), parentQ.ID)
	}
	if got := len(m.Pending("subagent-bot-1")); got != 1 || m.Pending("subagent-bot-1")[0].ID != bot1Q.ID {
		t.Errorf("bot1 pending = %+v, want [%s]", m.Pending("subagent-bot-1"), bot1Q.ID)
	}
	if got := len(m.Pending("subagent-bot-2")); got != 1 || m.Pending("subagent-bot-2")[0].ID != bot2Q.ID {
		t.Errorf("bot2 pending = %+v, want [%s]", m.Pending("subagent-bot-2"), bot2Q.ID)
	}

	// Answer bot1; bot2's question must remain pending and
	// unaffected.
	if err := m.Answer("subagent-bot-1", bot1Q.ID, "bot1 answer"); err != nil {
		t.Fatalf("Answer bot1: %v", err)
	}
	if bot2Q.State != StatePending {
		t.Errorf("bot2 State = %q, want pending (bot1 Answer must not bleed across)", bot2Q.State)
	}

	// TotalPending counts every pending question across every
	// subagent: 1 parent + 1 bot2 = 2.
	if got := m.TotalPending(); got != 2 {
		t.Errorf("TotalPending = %d, want 2 (parent + bot2)", got)
	}

	// AllPending maps subagents with at least one pending
	// question to their list.
	all := m.AllPending()
	if _, ok := all["subagent-bot-1"]; ok {
		t.Errorf("AllPending should not include bot1 (it was answered)")
	}
	if _, ok := all[ParentKey]; !ok {
		t.Errorf("AllPending missing parent key")
	}
	if _, ok := all["subagent-bot-2"]; !ok {
		t.Errorf("AllPending missing bot2 key")
	}
}

// TestDrainRemovesPendingOnly confirms Drain returns the pending
// questions but keeps answered ones in List (history tab).
func TestDrainRemovesPendingOnly(t *testing.T) {
	m := NewManager(false)
	q1 := m.Enqueue(ParentKey, "Q1", nil)
	q2 := m.Enqueue(ParentKey, "Q2", nil)
	if err := m.Answer(ParentKey, q1.ID, "answer1"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	drained := m.Drain(ParentKey)
	if len(drained) != 1 {
		t.Fatalf("Drain returned %d, want 1 (only q2 pending)", len(drained))
	}
	if drained[0].ID != q2.ID {
		t.Errorf("Drain[0].ID = %q, want %q", drained[0].ID, q2.ID)
	}

	// List now shows only the answered question — q2 was
	// drained (removed from the pending bucket) and q1 stayed
	// as history.
	all := m.List(ParentKey)
	if len(all) != 1 {
		t.Fatalf("List has %d entries, want 1 (answered q1 only)", len(all))
	}
	if all[0].ID != q1.ID {
		t.Errorf("List[0].ID = %q, want %q", all[0].ID, q1.ID)
	}

	// PendingCount is zero after the Drain; q1 was already
	// answered, q2 just was.
	if got := m.PendingCount(ParentKey); got != 0 {
		t.Errorf("PendingCount after Drain = %d, want 0", got)
	}
}

// TestSkipSetsSkippedAnswer confirms the picker's "skip" action
// transitions the question to StateSkipped with SkipAnswer text.
func TestSkipSetsSkippedAnswer(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue(ParentKey, "Anything?", nil)
	if err := m.Skip(ParentKey, q.ID); err != nil {
		t.Fatalf("Skip: %v", err)
	}
	if q.State != StateSkipped {
		t.Errorf("State = %q, want %q", q.State, StateSkipped)
	}
	if q.Answer != SkipAnswer {
		t.Errorf("Answer = %q, want %q", q.Answer, SkipAnswer)
	}
}

// TestUnknownAnswerIsError covers the defensive path: a stale
// question id from a sibling subagent must not silently
// transition an unrelated question.
func TestUnknownAnswerIsError(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue(ParentKey, "Q", nil)
	if err := m.Answer(ParentKey, "q-bogus", "x"); err == nil {
		t.Fatal("Answer on unknown id should error")
	}
	if q.State != StatePending {
		t.Errorf("State = %q, want pending (unknown Answer must not transition)", q.State)
	}
}

// TestConcurrentEnqueueAnswer is the race-detector tripwire.
// Goroutines call Enqueue + Answer on disjoint IDs in parallel;
// the data race detector (-race) flags unsynchronised reads if
// the mutex is missing or partial.
func TestConcurrentEnqueueAnswer(t *testing.T) {
	m := NewManager(false)
	const n = 50

	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			q := m.Enqueue(ParentKey, "Q", nil)
			if err := m.Answer(ParentKey, q.ID, "x"); err != nil {
				t.Errorf("Answer: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := m.TotalPending(); got != 0 {
		t.Errorf("TotalPending = %d, want 0", got)
	}
	if got := len(m.List(ParentKey)); got != n {
		t.Errorf("List has %d entries, want %d (history)", got, n)
	}
}

// TestEnqueueOptionsCopiedDefensively is the input-isolation
// guarantee: a caller that mutates the options slice after
// Enqueue must not see its mutation reflected on the stored
// question. The picker relies on this when it formats options
// for display.
func TestEnqueueOptionsCopiedDefensively(t *testing.T) {
	m := NewManager(false)
	src := []string{"a", "b"}
	q := m.Enqueue(ParentKey, "Pick", src)
	src[0] = "MUTATED"
	if q.Options[0] != "a" {
		t.Errorf("Options[0] = %q, want %q (defensive copy failed)", q.Options[0], "a")
	}
}

// TestEnqueueIDsAreUnique asserts the id format that
// q-<nanos>-<counter> is preserved across calls in the same
// Manager and never collides. The picker keys selection on ID,
// so a collision would silently answer the wrong question.
func TestEnqueueIDsAreUnique(t *testing.T) {
	m := NewManager(false)
	seen := make(map[string]bool)
	for i := 0; i < 20; i++ {
		q := m.Enqueue(ParentKey, "Q", nil)
		if seen[q.ID] {
			t.Fatalf("duplicate ID %q", q.ID)
		}
		seen[q.ID] = true
		if !strings.HasPrefix(q.ID, "q-") {
			t.Errorf("ID %q does not start with q-", q.ID)
		}
	}
}

// TestAnswerBeforeWaitReturnsImmediately covers the "answer
// arrives before Wait is called" race the subagent goroutine
// can hit when the picker is fast. Wait must still return the
// recorded answer rather than blocking forever on a channel
// that was closed before Wait started.
func TestAnswerBeforeWaitReturnsImmediately(t *testing.T) {
	m := NewManager(false)
	q := m.Enqueue(ParentKey, "Q", nil)
	if err := m.Answer(ParentKey, q.ID, "fast"); err != nil {
		t.Fatalf("Answer: %v", err)
	}

	done := make(chan struct{})
	var got string
	go func() {
		got, _ = q.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Wait blocked despite Answer landing first")
	}
	if got != "fast" {
		t.Errorf("Wait returned %q, want %q", got, "fast")
	}
}

// TestHeadlessManagerAutoSkipsAllEnqueues checks a
// corner-case: even when Enqueue is called many times in a
// row in headless mode, every question is skipped. The -p
// path runs many tool calls and must never queue a real
// pending question.
func TestHeadlessManagerAutoSkipsAllEnqueues(t *testing.T) {
	m := NewManager(true)
	for i := 0; i < 5; i++ {
		q := m.Enqueue(ParentKey, "Q", nil)
		if q.State != StateSkipped {
			t.Errorf("iter %d: State = %q, want %q", i, q.State, StateSkipped)
		}
	}
	if m.TotalPending() != 0 {
		t.Errorf("TotalPending = %d, want 0", m.TotalPending())
	}
}

// TestPendingSubagentsStable enumerates the helper the alert
// uses; it's not sorted (intentional — the alert sorts by
// CreatedAt before rendering).
func TestPendingSubagentsStable(t *testing.T) {
	m := NewManager(false)
	m.Enqueue(ParentKey, "p", nil)
	m.Enqueue("sub-a", "a", nil)
	m.Enqueue("sub-b", "b", nil)
	subs := m.PendingSubagents()
	if len(subs) != 3 {
		t.Fatalf("PendingSubagents = %v, want 3 entries", subs)
	}
	got := make(map[string]bool, len(subs))
	for _, s := range subs {
		got[s] = true
	}
	for _, want := range []string{ParentKey, "sub-a", "sub-b"} {
		if !got[want] {
			t.Errorf("PendingSubagents missing %q", want)
		}
	}
}
