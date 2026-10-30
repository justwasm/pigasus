package core

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestManager_Stop_RemovesFromActiveList covers the active-agents
// regression: a stopped subagent must NOT still be visible in
// Manager.List() after Stop returns. The AGY 1.3.3 changelog line
// 52 names this symptom verbatim.
func TestManager_Stop_RemovesFromActiveList(t *testing.T) {
	m := NewManager()
	sa := NewSubagent("echo", "hello")
	m.Add(sa)

	if got := len(m.List()); got != 1 {
		t.Fatalf("expected 1 agent before stop, got %d", got)
	}
	if _, ok := m.Get(sa.ID); !ok {
		t.Fatalf("expected subagent visible at Get(%q) before stop", sa.ID)
	}

	killed, removed := m.Stop(sa.ID)
	if !killed {
		t.Errorf("expected killed=true, got false")
	}
	if !removed {
		t.Errorf("expected removed=true, got false")
	}

	if got := len(m.List()); got != 0 {
		t.Errorf("expected 0 agents after stop, got %d (the AGY 1.3.3 stop-list bug)", got)
	}
	if _, ok := m.Get(sa.ID); ok {
		t.Errorf("expected Get(%q) to miss after stop, got hit (the AGY 1.3.3 stop-list bug)", sa.ID)
	}
}

// TestManager_Stop_UnknownID confirms Stop on a never-registered
// id is a clean no-op: no kill, no remove, no hook fired. The
// picker re-issues stop on the same id when the user double-taps
// Enter, so a missing entry must not crash.
func TestManager_Stop_UnknownID(t *testing.T) {
	m := NewManager()
	var fired int32
	m.SetOnStop(func(string) { atomic.AddInt32(&fired, 1) })

	killed, removed := m.Stop("sub-does-not-exist")
	if killed || removed {
		t.Errorf("expected (false,false) for unknown id, got (%v,%v)", killed, removed)
	}
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("expected OnStop NOT fired for unknown id, fired=%d", got)
	}

	// Same for the empty-string id.
	if killed, removed = m.Stop(""); killed || removed {
		t.Errorf("expected (false,false) for empty id, got (%v,%v)", killed, removed)
	}
}

// TestManager_Stop_FiresOnStopHook verifies the OnStop hook is
// invoked exactly once with the stopped subagent's id. The hook
// is the seam the gap names:
//
//	m.subagentManager.OnStop(id) → clearApprovalsBySubagent(id)
//
// Without this hook, the cascade approval queue would still hold
// the cleared subagent's approvals — the bug is in the
// hook NO LONGER firing, so this test is the regression net for
// both halves.
func TestManager_Stop_FiresOnStopHook(t *testing.T) {
	m := NewManager()
	sa := NewSubagent("echo", "hello")
	m.Add(sa)

	var (
		mu     sync.Mutex
		gotIDs []string
	)
	m.SetOnStop(func(id string) {
		mu.Lock()
		gotIDs = append(gotIDs, id)
		mu.Unlock()
	})

	m.Stop(sa.ID)

	mu.Lock()
	defer mu.Unlock()
	if len(gotIDs) != 1 {
		t.Fatalf("expected OnStop fired once, got %d (ids=%v)", len(gotIDs), gotIDs)
	}
	if gotIDs[0] != sa.ID {
		t.Errorf("expected OnStop fired with %q, got %q", sa.ID, gotIDs[0])
	}
}

// TestManager_Stop_NilHook covers the safe-by-default path: no
// OnStop wired → Stop still removes + kills, just without the
// downstream cleanup. Run-time callers should always wire a hook
// in production; the nil case is purely for tests and offline
// tooling.
func TestManager_Stop_NilHook(t *testing.T) {
	m := NewManager()
	sa := NewSubagent("echo", "hello")
	m.Add(sa)

	// No SetOnStop call.
	if killed, removed := m.Stop(sa.ID); !killed || !removed {
		t.Errorf("expected (true,true) with nil hook, got (%v,%v)", killed, removed)
	}
	if got := len(m.List()); got != 0 {
		t.Errorf("expected subagent removed with nil hook, got %d remaining", got)
	}
}

// TestManager_Stop_SetOnStopClearsHook verifies SetOnStop(nil)
// clears a previously-installed hook. Tests that share a Manager
// across cases rely on this to avoid a previous test's hook
// bleeding into the next.
func TestManager_Stop_SetOnStopClearsHook(t *testing.T) {
	m := NewManager()
	sa := NewSubagent("echo", "hello")
	m.Add(sa)

	var fired int32
	m.SetOnStop(func(string) { atomic.AddInt32(&fired, 1) })
	m.SetOnStop(nil)

	m.Stop(sa.ID)
	if got := atomic.LoadInt32(&fired); got != 0 {
		t.Errorf("expected OnStop NOT fired after SetOnStop(nil), fired=%d", got)
	}
}

// TestManager_Stop_OtherSubagentUntouched is the AGY 1.3.3
// "other subagents' approvals untouched" invariant. Stopping
// sub-A must not remove sub-B's pending approvals from the hook
// downstream; the cascade queue keys on SubagentID, and the hook
// passes ONLY the stopped id to ClearApprovalsBySubagent.
//
// This test wires a Manager with a hook that simulates the
// cascade: a small map[id][]string recording cleared ids. Two
// subagents are registered; only sub-A is stopped; sub-B must
// still be present.
func TestManager_Stop_OtherSubagentUntouched(t *testing.T) {
	m := NewManager()
	sa := NewSubagent("echo", "hello")
	sa.ID = "sub-A" // override so we can identify in the test
	sb := NewSubagent("echo", "world")
	sb.ID = "sub-B"
	m.Add(sa)
	m.Add(sb)

	var (
		mu     sync.Mutex
		clears = map[string][]string{}
	)
	m.SetOnStop(func(id string) {
		mu.Lock()
		// Mirror the cascade semantics: only clear entries
		// for this stopped id, not "everything that wasn't
		// touched". A buggy hook that passed "" or "all"
		// would still clear sub-B's approvals here.
		clears[id] = append(clears[id], id)
		mu.Unlock()
	})

	m.Stop(sa.ID)

	mu.Lock()
	defer mu.Unlock()

	if got := len(clears["sub-A"]); got != 1 {
		t.Errorf("expected 1 clear call for sub-A, got %d", got)
	}
	if got, ok := clears["sub-B"]; ok {
		t.Errorf("expected NO clear call for sub-B (other subagent), got %v", got)
	}
	if _, ok := m.Get("sub-B"); !ok {
		t.Errorf("expected sub-B still present, got missing")
	}
}

// TestManager_Stop_ConcurrentRace exercises the documented race:
// a goroutine adds subagents while another goroutine stops them.
// The test passes when `go test -race` reports zero data races
// AND the post-condition holds (every stopped subagent is gone).
//
// Why it lives here: the AGY 1.3.3 changelog says "stopping it
// now clears its pending approvals right away" — "right away"
// is the load-bearing word. A race-prone Stop that lists a
// half-removed state violates the picker invariant.
func TestManager_Stop_ConcurrentRace(t *testing.T) {
	m := NewManager()
	const N = 200

	var (
		mu      sync.Mutex
		stopped = map[string]bool{}
	)
	m.SetOnStop(func(id string) {
		// Touch a shared map to give `-race` something to
		// scream about IF the hook fires under a stale
		// Manager.mu.
		mu.Lock()
		stopped[id] = true
		mu.Unlock()
	})

	// Add half, then have a stopper race against more adds.
	for i := 0; i < N/2; i++ {
		s := NewSubagent("echo", "x")
		m.Add(s)
	}

	var wg sync.WaitGroup
	wg.Add(2)

	// Stopper: stops everything currently in the map, then
	// repeats until the map is empty. The map mutation inside
	// Stop is safe under mu; this goroutine just keeps
	// iterating while the adder races.
	go func() {
		defer wg.Done()
		for i := 0; i < 5; i++ {
			for _, sa := range m.List() {
				m.Stop(sa.ID)
			}
			runtime_Gosched()
		}
	}()

	// Adder: adds the rest, concurrently.
	go func() {
		defer wg.Done()
		for i := 0; i < N/2; i++ {
			s := NewSubagent("echo", "y")
			m.Add(s)
			runtime_Gosched()
		}
	}()

	wg.Wait()

	// Final drain: stop everything still in the list.
	for _, sa := range m.List() {
		m.Stop(sa.ID)
	}
	if got := len(m.List()); got != 0 {
		t.Errorf("expected empty Manager after stop drain, got %d still active", got)
	}

	mu.Lock()
	defer mu.Unlock()
	if got := len(stopped); got == 0 {
		t.Errorf("expected OnStop fired at least once, fired=%d", got)
	}
}

// runtime_Gosched is runtime.Gosched inlined to avoid the import
// (the test stays in the stdlib test deps).
func runtime_Gosched() { time.Sleep(time.Microsecond) }
