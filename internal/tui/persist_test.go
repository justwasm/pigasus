package tui

// persist_test.go — the background conversation writer.
//
// The writer is what makes Enter return immediately: the Update goroutine
// prepares a plan and hands it over, and the write happens elsewhere. Two
// properties carry the correctness, and both are tested here:
//
//   - Close drains the queue, so quitting persists the last state.
//   - Coalescing cannot lose steps. Dropping a queued plan must widen the
//     surviving plan's diff back to where the dropped one started, because
//     each plan's Prev is the previous plan's Next.

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
)

// persistFixture points the store at a temp dir and returns a plan builder.
func persistFixture(t *testing.T) (string, func(base, next []convstore.Step) supervisor.SavePlan) {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const id = "aaaaaaaa-bbbb-4000-8000-000000000901"
	return id, func(base, next []convstore.Step) supervisor.SavePlan {
		return supervisor.SavePlan{
			ConversationID: id,
			CWD:            "/tmp/persist-ws",
			Prev:           base,
			Next:           next,
		}
	}
}

// persistSteps builds a step list of n user inputs.
func persistSteps(n int) []convstore.Step {
	steps := make([]convstore.Step, 0, n)
	for i := 0; i < n; i++ {
		steps = append(steps, convstore.Step{
			Idx:    int64(i),
			Type:   convstore.StepUserInput,
			Status: 3,
			Text:   "step " + string(rune('a'+i)),
		})
	}
	return steps
}

// loadPersistedSteps reads the conversation back.
func loadPersistedSteps(t *testing.T, id string) []convstore.Step {
	t.Helper()
	c, err := convstore.LoadConversation(filepath.Join(convstore.StoreDir(), "conversations", id+".db"))
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	return c.Steps
}

// TestSessionWriterCloseDrainsTheQueue pins the shutdown guarantee: a save
// queued behind a slow write still lands before Close returns.
func TestSessionWriterCloseDrainsTheQueue(t *testing.T) {
	id, plan := persistFixture(t)
	steps := persistSteps(3)

	w := newSessionWriter()
	w.enqueue(&persistJob{plan: plan(nil, steps), hasPlan: true})
	w.Close()

	got := loadPersistedSteps(t, id)
	if len(got) != len(steps) {
		t.Fatalf("stored %d steps after Close, want %d", len(got), len(steps))
	}
}

// TestSessionWriterCoalescingKeepsTheDroppedSteps is the invariant behind
// coalescing.
//
// Two plans are queued back to back: the first adds steps 3-4, the second
// adds 5-6. Whichever one the writer picks up first, both sets of steps have
// to reach the store — the second plan's Prev only covers what the first
// plan's Next was, so dropping the first without widening would diff past
// steps 3-4 and never write them.
func TestSessionWriterCoalescingKeepsTheDroppedSteps(t *testing.T) {
	id, plan := persistFixture(t)
	stored := persistSteps(2)
	if err := convstore.SaveConversation(id, "/tmp/persist-ws", stored); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	first := persistSteps(4)  // 0..3: adds steps 2-3
	second := persistSteps(6) // 0..5: adds steps 4-5

	w := newSessionWriter()
	// Queue both while the writer is busy applying nothing: enqueue twice
	// before it can drain, which is exactly the burst case.
	w.enqueue(&persistJob{plan: plan(stored, first), hasPlan: true})
	w.enqueue(&persistJob{plan: plan(first, second), hasPlan: true})
	w.Close()

	got := loadPersistedSteps(t, id)
	if len(got) != len(second) {
		t.Fatalf("stored %d steps, want %d: coalescing lost a plan's steps", len(got), len(second))
	}
	for i, want := range second {
		if got[i].Text != want.Text {
			t.Errorf("step %d = %q, want %q", i, got[i].Text, want.Text)
		}
	}
}

// TestEnqueueWidensTheDiffWhenItDropsAPlan pins the widening itself, without
// a race: the writer is built but not started, so both plans have to be
// queued before anything is written.
//
// This is the deterministic form of the invariant above — the end-to-end test
// can pass by luck when the writer drains the first plan before the second is
// queued, this one cannot.
func TestEnqueueWidensTheDiffWhenItDropsAPlan(t *testing.T) {
	stored := persistSteps(2)
	first := persistSteps(4)
	second := persistSteps(6)

	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)

	// The first plan's base is what the store holds.
	w.enqueue(&persistJob{plan: supervisor.SavePlan{
		ConversationID: "x", Prev: stored, Next: first,
	}, hasPlan: true})
	// The second plan's base is the first plan's Next; dropping the first
	// must widen it back to what the store actually holds.
	w.enqueue(&persistJob{plan: supervisor.SavePlan{
		ConversationID: "x", Prev: first, Next: second,
	}, hasPlan: true})

	if w.pending == nil {
		t.Fatal("the second plan was not queued")
	}
	if got := len(w.pending.plan.Prev); got != len(stored) {
		t.Fatalf("pending plan's diff base has %d steps, want %d: the dropped plan's steps would never be written",
			got, len(stored))
	}
}

// TestEnqueueDoesNotMergeTwoConversations is the tenant-switch half of
// coalescing.
//
// /resume and /fork save the conversation they are leaving and then adopt
// another. Both jobs are queued; their plans describe different conversations
// and their step lists have nothing to do with each other, so merging them
// would write one conversation's steps into the other's file.
func TestEnqueueDoesNotMergeTwoConversations(t *testing.T) {
	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)

	stepsA := persistSteps(3)
	stepsB := persistSteps(5)
	w.enqueue(&persistJob{plan: supervisor.SavePlan{
		ConversationID: "conv-a", Prev: nil, Next: stepsA,
	}, hasPlan: true, convID: "conv-a"})
	w.enqueue(&persistJob{plan: supervisor.SavePlan{
		ConversationID: "conv-b", Prev: nil, Next: stepsB,
	}, hasPlan: true, convID: "conv-b"})

	// Both are queued: the first stays, the second waits behind it.
	if w.pending == nil || w.pending.plan.ConversationID != "conv-a" {
		t.Fatalf("the head of the queue is %v, want conv-a", w.pending)
	}
	if w.pending.next == nil || w.pending.next.plan.ConversationID != "conv-b" {
		t.Fatal("the second conversation's save was dropped by coalescing")
	}
	if len(w.pending.next.plan.Next) != len(stepsB) {
		t.Error("the second conversation's steps were replaced by the first's")
	}
}

// TestEnqueueDoesNotMergeAPlanWithAFallback keeps the two save kinds apart:
// only one of them carries the session's full state in a plan, so folding them
// together would lose whichever one was dropped.
func TestEnqueueDoesNotMergeAPlanWithAFallback(t *testing.T) {
	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)

	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: "c", Next: persistSteps(3)},
		hasPlan: true, convID: "c",
	})
	w.enqueue(&persistJob{convID: "c", hasSteps: true, steps: persistSteps(5)})

	if w.pending == nil || !w.pending.hasPlan {
		t.Fatal("the queued plan was replaced by a fallback job")
	}
	if w.pending.next == nil || !w.pending.next.hasSteps {
		t.Fatal("the fallback job was dropped")
	}
}

// TestEnqueueSupersedesOnlyTheLastQueuedSave pins where coalescing is allowed
// to act: on the queue's last save, never on one whose neighbours are still
// waiting.
//
// A switch queues conv-a then conv-b. The next save is conv-b's again, so it
// may replace the queued conv-b — but conv-a is an earlier conversation whose
// steps nothing else carries, and it must survive.
func TestEnqueueSupersedesOnlyTheLastQueuedSave(t *testing.T) {
	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)

	stepsA := persistSteps(3)
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: "conv-a", Next: stepsA},
		hasPlan: true, convID: "conv-a",
	})
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: "conv-b", Next: persistSteps(4)},
		hasPlan: true, convID: "conv-b",
	})
	stepsBFinal := persistSteps(6)
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: "conv-b", Prev: persistSteps(4), Next: stepsBFinal},
		hasPlan: true, convID: "conv-b",
	})

	if w.pending == nil || w.pending.convID != "conv-a" {
		t.Fatalf("the head of the queue is %+v, want conv-a: an earlier conversation was superseded", w.pending)
	}
	if len(w.pending.plan.Next) != len(stepsA) {
		t.Error("conv-a's steps were replaced")
	}
	last := w.pending.next
	if last == nil || last.convID != "conv-b" {
		t.Fatalf("the queue's last save is %+v, want conv-b", last)
	}
	if last.next != nil {
		t.Error("the superseded conv-b save is still queued")
	}
	if len(last.plan.Next) != len(stepsBFinal) {
		t.Errorf("the queued conv-b save has %d steps, want the newest %d",
			len(last.plan.Next), len(stepsBFinal))
	}
}

// TestEnqueueKeepsALongChainOfSwitches pins that the queue grows by one per
// switch and loses none of them: a burst of /resume is exactly a chain, and
// every conversation in it has steps only it carries.
func TestEnqueueKeepsALongChainOfSwitches(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const n = 32

	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)
	ids := make([]string, n)
	for i := 0; i < n; i++ {
		ids[i] = fmt.Sprintf("aaaaaaaa-bbbb-4000-8000-%012d", i)
		w.enqueue(&persistJob{
			plan:    supervisor.SavePlan{ConversationID: ids[i], CWD: "/tmp/w", Next: persistSteps(2)},
			hasPlan: true, convID: ids[i],
		})
	}

	seen := 0
	for j := w.pending; j != nil; j = j.next {
		if j.convID != ids[seen] {
			t.Fatalf("link %d is %q, want %q: the chain reordered or dropped a switch",
				seen, j.convID, ids[seen])
		}
		seen++
	}
	if seen != n {
		t.Fatalf("the queue holds %d saves, want %d", seen, n)
	}

	// And every one of them lands.
	applyPersistJob(w.pending)
	for i, id := range ids {
		if got := loadPersistedSteps(t, id); len(got) != 2 {
			t.Fatalf("conversation %d (%s) has %d steps, want 2", i, id, len(got))
		}
	}
}

// TestCloseAppliesEveryQueuedConversation pins the drain: a queue holding two
// conversations writes both, in order.
func TestCloseAppliesEveryQueuedConversation(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const (
		idA = "aaaaaaaa-bbbb-4000-8000-000000000911"
		idB = "aaaaaaaa-bbbb-4000-8000-000000000912"
	)
	stepsA := persistSteps(2)
	stepsB := persistSteps(3)

	w := &sessionWriter{done: make(chan struct{})}
	w.cond = sync.NewCond(&w.mu)
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: idA, Next: stepsA},
		hasPlan: true, convID: idA,
	})
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: idB, Next: stepsB},
		hasPlan: true, convID: idB,
	})
	// Drain by hand: the writer goroutine was never started.
	applyPersistJob(w.pending)

	if got := loadPersistedSteps(t, idA); len(got) != len(stepsA) {
		t.Errorf("conversation A has %d steps, want %d", len(got), len(stepsA))
	}
	if got := loadPersistedSteps(t, idB); len(got) != len(stepsB) {
		t.Errorf("conversation B has %d steps, want %d: the queued save behind A never ran",
			len(got), len(stepsB))
	}
}

// TestDrainWaitsForTheQueuedSave pins the read-after-write sites (/resume,
// /fork): after drain returns, the store holds the save that was queued.
func TestDrainWaitsForTheQueuedSave(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const id = "aaaaaaaa-bbbb-4000-8000-000000000921"

	w := newSessionWriter()
	defer w.Close()
	steps := persistSteps(4)
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: id, CWD: "/tmp/w", Next: steps},
		hasPlan: true, convID: id,
	})
	w.drain()

	// No further synchronisation: drain is the guarantee.
	got := loadPersistedSteps(t, id)
	if len(got) != len(steps) {
		t.Fatalf("after drain the store has %d steps, want %d", len(got), len(steps))
	}
}

// TestDrainOnAQuietWriterReturns pins the no-op case: drain must not wait for
// a write that is never coming.
func TestDrainOnAQuietWriterReturns(t *testing.T) {
	w := newSessionWriter()
	defer w.Close()
	done := make(chan struct{})
	go func() {
		w.drain()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("drain hung on a writer with nothing queued")
	}
}

// TestDrainDoesNotHoldTheLockWhileTheWriterStops pins the lock discipline of
// drain's shutdown branch.
//
// A drain that arrives while the writer is stopping has to wait for it to
// exit — and the writer cannot exit without the mutex. So that wait must
// happen unlocked. A drain that waits on it while holding the mutex deadlocks
// the writer, and with it the shutdown.
//
// done is left open while drain runs: its shutdown branch waits for the
// writer to exit, and the writer's exit has to take the mutex. So the mutex
// must be free while drain waits.
//
// The exit is delayed a hair so drain has certainly reached its wait first.
// With the wait still holding the mutex, the exit below never gets it, drain
// never returns, and the deadline catches the deadlock.
func TestDrainDoesNotHoldTheLockWhileTheWriterStops(t *testing.T) {
	w := &sessionWriter{done: make(chan struct{}), closed: true, busy: true}
	w.cond = sync.NewCond(&w.mu)

	drained := make(chan struct{})
	go func() {
		w.drain()
		close(drained)
	}()
	time.Sleep(2 * time.Millisecond)

	// The writer's last act: take the mutex to notice it may stop, then
	// close done.
	wentOut := make(chan struct{})
	go func() {
		w.mu.Lock()
		w.busy = false
		w.mu.Unlock()
		close(w.done)
		close(wentOut)
	}()

	select {
	case <-drained:
	case <-time.After(5 * time.Second):
		t.Fatal("drain waited for the writer to exit while holding the mutex: the writer can never take it, so shutdown deadlocks")
	}
	select {
	case <-wentOut:
	case <-time.After(5 * time.Second):
		t.Fatal("the writer never got to exit")
	}
}

// TestCloseAndDrainTogetherStillWrite pins the shutdown race: whichever of the
// two runs first, the save that was queued lands exactly once and neither call
// hangs.
func TestCloseAndDrainTogetherStillWrite(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	const id = "aaaaaaaa-bbbb-4000-8000-000000000931"
	steps := persistSteps(4)

	w := newSessionWriter()
	w.enqueue(&persistJob{
		plan:    supervisor.SavePlan{ConversationID: id, CWD: "/tmp/w", Next: steps},
		hasPlan: true, convID: id,
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); w.drain() }()
	go func() { defer wg.Done(); w.Close() }()

	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
	case <-time.After(10 * time.Second):
		t.Fatal("Close and drain together hung")
	}

	got := loadPersistedSteps(t, id)
	if len(got) != len(steps) {
		t.Fatalf("the store has %d steps, want %d: the shutdown race lost the save", len(got), len(steps))
	}
}

// TestSaveSessionWithoutWriterIsSynchronous pins the fallback the tests and
// the non-interactive hosts rely on: with no writer installed, the store is
// observable as soon as saveSession returns.
func TestSaveSessionWithoutWriterIsSynchronous(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)
	const id = "aaaaaaaa-bbbb-4000-8000-000000000902"

	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", tempHome, true)
	m.session.conversationID = id
	m.session.writer = nil
	m.appendUserTurn("hello there")
	m.saveSession()

	got := loadPersistedSteps(t, id)
	if len(got) == 0 {
		t.Fatal("saveSession without a writer wrote nothing")
	}
	if got[0].Text != "hello there" {
		t.Errorf("step 0 = %q, want the submitted prompt", got[0].Text)
	}
}

// TestSaveSessionWithWriterReturnsBeforeTheWrite pins the whole point of the
// writer: the call returns without waiting for SQLite.
func TestSaveSessionWithWriterReturnsBeforeTheWrite(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)
	const id = "aaaaaaaa-bbbb-4000-8000-000000000903"

	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", tempHome, true)
	m.session.conversationID = id
	m.session.writer = newSessionWriter()
	defer m.session.writer.Close()

	m.appendUserTurn("hello there")
	start := time.Now()
	m.saveSession()
	elapsed := time.Since(start)
	if elapsed > 50*time.Millisecond {
		t.Errorf("saveSession took %v with a writer installed; it must not wait for the write", elapsed)
	}

	// Closing the writer makes the queued save land.
	m.session.writer.Close()
	got := loadPersistedSteps(t, id)
	if len(got) == 0 || got[0].Text != "hello there" {
		t.Fatalf("the queued save did not land: %+v", got)
	}
}
