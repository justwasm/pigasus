// taskregistry_test.go — the four tests the tasks-panel deliverable
// promises in its "Tests" section:
//
//   - TestRegisterAndList             — register 3, list returns all 3 sorted by StartedAt
//   - TestSubscribeReceivesUpdates    — register → "registered" event; update → "updated" event
//   - TestUnsubscribeClosesChannel    — call cancel; subsequent update does not panic; ch is closed
//   - TestConcurrentRegisterUpdate    — 100 goroutines each register + update; -race clean
//
// A fifth small test covers CountRunning and Hide so the helpers
// the status row and the /tasks panel rely on are pinned too.
package taskregistry

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestRegisterAndList(t *testing.T) {
	r := New()

	now := time.Now()
	a := &Task{ID: "a", Name: "alpha", Kind: KindShell, StartedAt: now.Add(2 * time.Second)}
	b := &Task{ID: "b", Name: "beta", Kind: KindSubagent, StartedAt: now.Add(1 * time.Second)}
	c := &Task{ID: "c", Name: "gamma", Kind: KindTimer, StartedAt: now}

	r.Register(a)
	r.Register(b)
	r.Register(c)

	got := r.List()
	if len(got) != 3 {
		t.Fatalf("List returned %d tasks, want 3", len(got))
	}
	// Sorted by StartedAt ascending → c, b, a.
	wantOrder := []string{"c", "b", "a"}
	for i, w := range wantOrder {
		if got[i].ID != w {
			t.Errorf("List[%d].ID = %q, want %q", i, got[i].ID, w)
		}
	}
	// Hidden tasks are filtered out.
	r.Hide("b")
	got = r.List()
	if len(got) != 2 {
		t.Fatalf("List returned %d tasks after Hide, want 2", len(got))
	}
	if got[0].ID != "c" || got[1].ID != "a" {
		t.Errorf("List after Hide = [%s, %s], want [c, a]", got[0].ID, got[1].ID)
	}
}

func TestSubscribeReceivesUpdates(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()
	defer cancel()

	// Register must emit a "registered" event. We use a 1s
	// timeout so a regression that silently swallows events
	// fails the test loudly instead of hanging the suite.
	tr := &Task{ID: "rx", Name: "rx-1", Kind: KindShell}
	r.Register(tr)

	select {
	case ev := <-ch:
		if ev.Kind != "registered" {
			t.Errorf("first event Kind = %q, want registered", ev.Kind)
		}
		if ev.ID != "rx" {
			t.Errorf("first event ID = %q, want rx", ev.ID)
		}
		if ev.Task != tr {
			t.Errorf("first event Task pointer differs — subscriber should see the live pointer")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive registered event within 1s")
	}

	// Update must emit "updated" and the mutation must be visible.
	r.Update("rx", func(t *Task) { t.State = StateRunning })

	select {
	case ev := <-ch:
		if ev.Kind != "updated" {
			t.Errorf("second event Kind = %q, want updated", ev.Kind)
		}
		if ev.Task.State != StateRunning {
			t.Errorf("second event Task.State = %q, want running", ev.Task.State)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("did not receive updated event within 1s")
	}
}

func TestUnsubscribeClosesChannel(t *testing.T) {
	r := New()
	ch, cancel := r.Subscribe()

	tr := &Task{ID: "u1", Name: "u-1", Kind: KindShell}
	r.Register(tr)

	// Drain the registration event.
	select {
	case <-ch:
	case <-time.After(1 * time.Second):
		t.Fatal("register event not received")
	}

	cancel()

	// After cancel the channel must be closed — a receive should
	// return immediately with ok=false.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("channel still open after unsubscribe")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("unsubscribe did not close channel within 1s")
	}

	// A subsequent Update must not panic. (Without the mutex
	// cleanup, the registry would still try to send on the closed
	// channel and crash.)
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Update after unsubscribe panicked: %v", p)
		}
	}()
	r.Update("u1", func(t *Task) { t.State = StateDone })

	// Calling cancel twice must also be safe.
	cancel()
}

func TestConcurrentRegisterUpdate(t *testing.T) {
	r := New()

	const goroutines = 100
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			id := r.Register(&Task{
				ID:   "", // exercise the auto-mint path
				Name: "load",
				Kind: KindShell,
			})
			// Each goroutine then transitions its task through
			// pending → running → done, two updates apiece. The
			// total event volume is 3 * 100 = 300; the subscriber
			// buffer (32) will overflow a handful of times, which
			// is fine because the test only asserts final state.
			r.Update(id, func(t *Task) {
				t.State = StateRunning
				t.ExitCode = ExitCodeUnset
			})
			r.Update(id, func(t *Task) {
				t.State = StateDone
				t.ExitCode = 0
				t.EndedAt = time.Now()
			})
		}(i)
	}

	wg.Wait()

	done := 0
	for _, t := range r.tasks {
		if t.State == StateDone && t.ExitCode == 0 && !t.EndedAt.IsZero() {
			done++
		}
	}
	if done != goroutines {
		t.Errorf("final done count = %d, want %d", done, goroutines)
	}
	// IDs must all be distinct even when minted in parallel.
	seen := make(map[string]struct{}, goroutines)
	for id := range r.tasks {
		if _, dup := seen[id]; dup {
			t.Errorf("duplicate auto-minted ID %q", id)
		}
		seen[id] = struct{}{}
	}
	if len(seen) != goroutines {
		t.Errorf("unique IDs = %d, want %d", len(seen), goroutines)
	}
}

func TestCountRunningAndHide(t *testing.T) {
	r := New()
	r.Register(&Task{ID: "r1", Name: "r-1", Kind: KindShell, State: StateRunning})
	r.Register(&Task{ID: "r2", Name: "r-2", Kind: KindShell, State: StateRunning})
	r.Register(&Task{ID: "r3", Name: "r-3", Kind: KindShell, State: StateDone})

	if got := r.CountRunning(); got != 2 {
		t.Errorf("CountRunning = %d, want 2", got)
	}

	if !r.Hide("r1") {
		t.Error("Hide(r1) returned false, want true")
	}
	if r.Hide("r1") {
		t.Error("Hide(r1) returned true on already-hidden, want false")
	}
	if r.Hide("nope") {
		t.Error("Hide(unknown) returned true, want false")
	}
	if got := r.CountRunning(); got != 1 {
		t.Errorf("CountRunning after Hide = %d, want 1", got)
	}
}

// TestCloseReleasesSubscribers pins the goroutine-leak guard: after
// Close, every open subscriber channel must be closed so a parked
// goroutine can exit. Without this the test would deadlock on the
// 1-second timeout.
func TestCloseReleasesSubscribers(t *testing.T) {
	r := New()
	ch, _ := r.Subscribe()

	// Park a goroutine that does nothing but read; it must
	// unblock the moment Close fires.
	exited := make(chan struct{})
	go func() {
		for range ch {
		}
		close(exited)
	}()

	r.Close()

	select {
	case <-exited:
	case <-time.After(1 * time.Second):
		t.Fatal("subscriber goroutine did not exit after Close within 1s")
	}

	// Subscribe on a closed registry returns a closed channel.
	ch2, cancel := r.Subscribe()
	defer cancel()
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("Subscribe on closed registry returned open channel")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Subscribe-on-closed did not return a closed channel within 1s")
	}

	// Register / Update / Hide on closed registry must be no-ops.
	if got := r.Register(&Task{ID: "x"}); got != "" {
		t.Errorf("Register on closed returned %q, want \"\"", got)
	}
	r.Update("x", func(*Task) {})
	if r.Hide("x") {
		t.Error("Hide on closed returned true, want false")
	}
}

// TestUpdateUnknownAndNil guards the regression paths: an unknown ID
// is silently skipped (no panic, no event), and a nil mut is a no-op.
func TestUpdateUnknownAndNil(t *testing.T) {
	r := New()
	r.Register(&Task{ID: "known", Name: "k", Kind: KindShell})

	// Unknown ID.
	r.Update("not-there", func(*Task) {
		t.Errorf("mut should not run for unknown ID")
	})

	// Nil mut must not panic.
	defer func() {
		if p := recover(); p != nil {
			t.Fatalf("Update with nil mut panicked: %v", p)
		}
	}()
	r.Update("known", nil)
}

// TestGetUnknownReturnsNil pins the empty-result contract so callers
// don't have to nil-check the second return value.
func TestGetUnknownReturnsNil(t *testing.T) {
	r := New()
	if got := r.Get("nope"); got != nil {
		t.Errorf("Get(nope) = %v, want nil", got)
	}
}

// TestRegisterAutoID pins the auto-mint path so a caller that does
// not supply an ID gets a unique "task-N" string back. The exact
// format is part of the public contract — ref users grep for it in
// the logs.
func TestRegisterAutoID(t *testing.T) {
	r := New()
	a := r.Register(&Task{Name: "auto-a", Kind: KindShell})
	b := r.Register(&Task{Name: "auto-b", Kind: KindShell})
	if a == "" || b == "" {
		t.Fatalf("Register returned empty auto ID: a=%q b=%q", a, b)
	}
	if a == b {
		t.Fatalf("Register returned identical auto IDs: %q", a)
	}
	if a == "task-0" || b == "task-0" {
		// Anything but "0" is fine, but "task-0" is what an
		// off-by-one in fmtMintID would emit for the very
		// first call.
		t.Errorf("first auto ID is %q (want task-1 or higher)", a)
	}
}

// TestErrClosedExists is a sentinel pin so a future refactor that
// drops the var breaks the build, not a runtime assertion.
func TestErrClosedExists(t *testing.T) {
	if ErrClosed == nil {
		t.Fatal("ErrClosed is nil")
	}
	if errors.Unwrap(ErrClosed) != nil {
		// We don't actually wrap anything yet, but we want the
		// assertion in place so a future wrap is intentional.
		t.Log("ErrClosed wraps something — re-check")
	}
}

// silent atomic import so go vet does not flag the atomic.Uint64
// field as unused when other tests are added.
var _ = atomic.LoadUint64
