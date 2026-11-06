// observer_test.go — cycle retry-30s-cap-stays Observer tests.
package retrybudget

import (
	"sync"
	"testing"
	"time"
)

// stubAborter records every AbortRetry call. It implements Aborter
// without depending on any external runtime.
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

func retryStart(delay time.Duration) AutoRetryStartEvent {
	return AutoRetryStartEvent{DelayMs: int(delay / time.Millisecond)}
}

type dummyCompactionStartEvent struct{ Reason string }
type dummyCompactionEndEvent struct{ Reason string }
type dummyMessageUpdateEvent struct{}

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

	obs(retryStart(8 * time.Second))
	if got := aborter.CallCount(); got != 2 {
		t.Fatalf("Aborter called %d times after two overflows; want 2", got)
	}
}

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

	b.Reset()
	obs(retryStart(30 * time.Second))
	if got := aborter.CallCount(); got != 1 {
		t.Fatalf("turn 2: Aborter called %d times after Reset+30s; want 1 (unchanged)", got)
	}
	if got := b.Used(); got != 30*time.Second {
		t.Fatalf("turn 2: Used=%v, want 30s", got)
	}
}

func TestRetryObserverSingleUnaffected(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(retryStart(2 * time.Second))
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

func TestRetryObserverIgnoresNonRetry(t *testing.T) {
	b := New(time.Second) // tiny cap so any false-trigger overflows
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(dummyCompactionStartEvent{Reason: "threshold"})
	obs(dummyCompactionEndEvent{Reason: "threshold"})
	obs(dummyMessageUpdateEvent{})
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on non-retry events; want 0", got)
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("Used=%v after non-retry events; want 0", got)
	}
}

func TestCapObserverNilBudgetNoOp(t *testing.T) {
	aborter := &stubAborter{}
	obs := Observer(nil, aborter)

	obs(retryStart(time.Hour))
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("nil-budget observer called Aborter %d times; want 0", got)
	}
}

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

func TestRetryObserverEndIgnored(t *testing.T) {
	b := New(30 * time.Second)
	aborter := &stubAborter{}
	obs := Observer(b, aborter)

	obs(AutoRetryEndEvent{Success: false, Attempt: 3, FinalError: "exhausted"})
	obs(AutoRetryEndEvent{Success: true, Attempt: 1})
	if got := aborter.CallCount(); got != 0 {
		t.Fatalf("Aborter called %d times on retry-end events; want 0", got)
	}
	if got := b.Used(); got != 0 {
		t.Fatalf("Used=%v after retry-end events; want 0 (event is not consumed)", got)
	}
}
