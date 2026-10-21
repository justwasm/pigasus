// observer_test.go — cycle retry-30s-cap-stays Observer tests. The
// Budget's invariants are pinned in retrybudget_test.go; here we
// verify the contract with the caller: a sequence of
// agent.AutoRetryStartEvent values lands in the budget and overflow
// calls Aborter.AbortRetry exactly once.
//
// Test name prefixes are constrained to the cycle's regex
// (TestRetry|TestBackoff|TestCap|Test30) so the targeted-run check the
// deliverable specifies resolves them.
package retrybudget

import (
	"sync"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
)

// stubAborter records every AbortRetry call. It implements Aborter
// without depending on PiG's *coding.Session, so the observer test
// stays a pure unit test (no session, no transport, no MCP).
type stubAborter struct {
	mu    sync.Mutex
	calls int
}

func (s *stubAborter) AbortRetry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
}

func (s *stubAborter) CallCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// retryStart is a small helper that builds an AutoRetryStartEvent with
// the fields the observer actually consumes (DelayMs). The other
// fields are kept at zero so a regression that mis-indexes them
// surfaces in the test, not as a downstream nil-deref.
func retryStart(delay time.Duration) agent.AutoRetryStartEvent {
	return agent.AutoRetryStartEvent{DelayMs: int(delay / time.Millisecond)}
}

// TestBackoffObserverWithinCap pins the happy path: a sequence of
// exponentially increasing backoffs that all fit inside the cap must
// NOT call Aborter. This is the case AGY 1.2.13 leaves alone — the
// changelog pins the cap, not the routine.
func TestBackoffObserverWithinCap(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	for _, d := range []time.Duration{2 * time.Second, 4 * time.Second, 8 * time.Second} {
		obs(retryStart(d))
	}
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on a fitting sequence; want 0", got)
	}
	if got := b.Used(); got != 14*time.Second {
		t.Fatalf("Used=%v after 2+4+8s sequence, want 14s", got)
	}
}

// TestCapOverflowAborts pins the surface-error-after-cap case: a
// backoff that would overflow the cap must trigger exactly one
// AbortRetry call, and the failed TryConsume must leave Used unchanged
// (so the abort is observable to the rest of the system as a clean
// "we hit the cap, here is the underlying error" rather than a
// half-consumed delta).
func TestCapOverflowAborts(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	// 25s consumed; an 8s wait would push total to 33s, overflowing.
	if !b.TryConsume(25 * time.Second) {
		t.Fatal("setup: 25s should fit on a fresh 30s budget")
	}
	obs(retryStart(8 * time.Second))

	if got := aborter.CallCount(); got != 1 {
		t.Fatalf("Aborter called %d times after a single overflow; want 1", got)
	}
	if got := b.Used(); got != 25*time.Second {
		t.Fatalf("Used=%v after overflow, want 25s (rejection leaves state unchanged)", got)
	}

	// A subsequent overflow event must also call Aborter — the
	// observer does not latch a "do not call again" flag. PiG's
	// prepareRetry stops scheduling retries after AbortRetry cancels
	// the in-flight wait, so this second call is a defence-in-depth
	// assertion: the observer's contract is "every overflow fires an
	// AbortRetry", regardless of upstream behaviour.
	obs(retryStart(8 * time.Second))
	if got := aborter.CallCount(); got != 2 {
		t.Fatalf("Aborter called %d times after two overflows; want 2", got)
	}
}

// TestCapObserverPerTurnReset pins the per-turn semantics at the
// Observer level: Reset between turns must restore the budget so
// the next AutoRetryStartEvent of the new turn fits. This is the
// property the cycle's spec calls out ("reset per turn, surface
// errors after cap").
func TestCapObserverPerTurnReset(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	// Turn 1: burn the budget to overflow on the third retry.
	obs(retryStart(15 * time.Second))
	obs(retryStart(15 * time.Second))
	obs(retryStart(15 * time.Second)) // overflows: 30+15 > 30
	if got := aborter.CallCount(); got != 1 {
		t.Fatalf("turn 1: Aborter called %d times after 3x 15s overflows; want 1", got)
	}

	// New turn: Reset zeroes the budget. The next 30s attempt must
	// fit — and crucially must NOT call Aborter (the past turn's
	// exhausted state does not leak).
	b.Reset()
	obs(retryStart(30 * time.Second))
	if got := aborter.CallCount(); got != 1 {
		t.Fatalf("turn 2: Aborter called %d times after Reset+30s; want 1 (unchanged)", got)
	}
	if got := b.Used(); got != 30*time.Second {
		t.Fatalf("turn 2: Used=%v, want 30s", got)
	}
}

// TestRetryObserverSingleUnaffected pins the most common case in
// production: a single retry that fits inside the cap. The observer
// must consume its delay and leave the rest untouched, with zero
// Aborter calls. This is what AGY 1.2.13 users see 99% of the time.
func TestRetryObserverSingleUnaffected(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(retryStart(2 * time.Second)) // PiG default BaseDelayMs
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on a single fitting retry; want 0", got)
	}
	if got := b.Used(); got != 2*time.Second {
		t.Fatalf("Used=%v, want 2s", got)
	}
	if got := b.Remaining(); got != 28*time.Second {
		t.Fatalf("Remaining=%v, want 28s", got)
	}
}

// TestRetryObserverIgnoresNonRetry confirms the observer is a
// type-filtered EventFunc: it must not call Aborter on
// CompactionStartEvent, MessageUpdateEvent, or any other event type
// the session may emit. The drain goroutine forwards every event the
// session produces; without the type switch, a compaction event with
// a numeric field could spuriously trip the abort.
func TestRetryObserverIgnoresNonRetry(t *testing.T) {
	b := New(time.Second) // tiny cap so any false-trigger overflows
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(agent.CompactionStartEvent{Reason: "threshold"})
	obs(agent.CompactionEndEvent{Reason: "threshold"})
	obs(agent.MessageUpdateEvent{}) // zero-value AssistantMessageEvent
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on non-retry events; want 0", got)
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("Used=%v after non-retry events; want 0", got)
	}
}

// TestCapObserverNilBudgetNoOp confirms the runtime's "cap disabled"
// wiring (retryBudget == nil when Options.RetryBudgetCap < 0). The
// observer must not panic, must not call Aborter, and must not
// consume. Pinning this case is what lets the runtime disable the
// cap in tests without rebuilding the observer.
func TestCapObserverNilBudgetNoOp(t *testing.T) {
	aborter := &stubAborter{}
	obs := Observer(nil, aborter)

	// Even a 1-hour retry must not be routed anywhere — the observer
	// is a no-op without a budget to consult.
	obs(retryStart(time.Hour))
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("nil-budget observer called Aborter %d times; want 0", got)
	}
}

// TestRetryObserverNilAborterPanics confirms the loud-failure
// contract: wiring a nil Aborter alongside a non-nil Budget panics
// the first time AutoRetryStartEvent fires. A silent no-op here
// would defeat the cycle's purpose — the cap exists to abort, not to
// log.
func TestRetryObserverNilAborterPanics(t *testing.T) {
	b := New(30 * time.Second)
	obs := Observer(b, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("nil Aborter with non-nil Budget should panic on AutoRetryStartEvent")
		}
	}()
	obs(retryStart(time.Second))
}

// TestRetryObserverEndIgnored confirms the observer does not act on
// AutoRetryEndEvent. PiG emits one of these after every retry
// sequence (success or exhausted). If the observer treated it as a
// retry start, the "DelayMs=0" event would slip a no-op consume into
// the budget and skew the cumulative reading.
func TestRetryObserverEndIgnored(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(agent.AutoRetryEndEvent{Success: false, Attempt: 3, FinalError: "exhausted"})
	obs(agent.AutoRetryEndEvent{Success: true, Attempt: 1})
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on retry-end events; want 0", got)
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("Used=%v after retry-end events; want 0 (event is not consumed)", got)
	}
}
