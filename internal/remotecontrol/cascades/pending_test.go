// Package cascades — pending_test.go: the pending-interaction
// registry's own tests.
//
// A pending interaction is the domain's: it is minted by the
// cascade, parked on by the backend, resolved by the dispatcher's
// RPC and expired by the registry's own tick. The registry and
// the minting path are therefore exercised from inside this
// package; the dispatcher's Handle…UserInteraction handler —
// the JSON-RPC envelope the dashboard sees — stays in the parent
// package, next to the dispatcher it drives.
package cascades

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- PendingInteractions registry tests ----------------------------

// TestPendingInteractions_AddGetResolve pins the headline
// ask of the rc-handle-user-interaction cycle's registry:
// Add + Get returns the entry; Resolve drops it and sends
// the InteractionResult onto ResultCh; a second Resolve
// returns ErrInteractionNotFound (one-shot semantics).
func TestPendingInteractions_AddGetResolve(t *testing.T) {
	var p PendingInteractions
	if got := p.Len(); got != 0 {
		t.Errorf("fresh registry Len = %d, want 0", got)
	}
	intr := &PendingInteraction{
		ID:       "int_a",
		Kind:     InteractionKindPermission,
		Title:    "read main.go?",
		Prompt:   "ok?",
		ResultCh: make(chan InteractionResult, 1),
	}
	p.Add(intr)
	if got := p.Len(); got != 1 {
		t.Errorf("after Add: Len = %d, want 1", got)
	}
	got, ok := p.Get("int_a")
	if !ok || got != intr {
		t.Fatalf("Get(int_a) = (%v, %v); want (intr, true)", got, ok)
	}
	// Missing id returns (nil, false).
	if g, ok2 := p.Get("int_missing"); ok2 || g != nil {
		t.Errorf("Get(int_missing) = (%v, %v); want (nil, false)", g, ok2)
	}

	// Resolve drops the entry and delivers the result.
	resp := json.RawMessage(`"yes"`)
	if err := p.Resolve("int_a", InteractionResult{Response: resp, DecidedAt: time.Now()}); err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got := p.Len(); got != 0 {
		t.Errorf("after Resolve: Len = %d, want 0", got)
	}
	select {
	case r := <-intr.ResultCh:
		if string(r.Response) != `"yes"` {
			t.Errorf("ResultCh.Response = %s, want %q", r.Response, `"yes"`)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("ResultCh did not receive within 50ms")
	}

	// A second Resolve on the same id is a no-op (one-shot).
	if err := p.Resolve("int_a", InteractionResult{Response: resp}); !errors.Is(err, ErrInteractionNotFound) {
		t.Errorf("second Resolve returned %v, want ErrInteractionNotFound", err)
	}
}

// TestPendingInteractions_ResolveUnknown pins the
// "Resolve a missing id → ErrInteractionNotFound" path.
// The dispatcher's handler maps this sentinel to JSON-RPC
// -32006.

// TestPendingInteractions_ResolveUnknown pins the
// "Resolve a missing id → ErrInteractionNotFound" path.
// The dispatcher's handler maps this sentinel to JSON-RPC
// -32006.
func TestPendingInteractions_ResolveUnknown(t *testing.T) {
	var p PendingInteractions
	err := p.Resolve("int_nope", InteractionResult{})
	if !errors.Is(err, ErrInteractionNotFound) {
		t.Errorf("Resolve(unknown) = %v, want ErrInteractionNotFound", err)
	}
}

// TestPendingInteractions_NilSafe covers the "no panic on
// nil receiver" rule. Every method on *PendingInteractions
// is a no-op when the receiver is nil so a deferred
// teardown that nulls the cascade's Pending doesn't crash
// later.

// TestPendingInteractions_NilSafe covers the "no panic on
// nil receiver" rule. Every method on *PendingInteractions
// is a no-op when the receiver is nil so a deferred
// teardown that nulls the cascade's Pending doesn't crash
// later.
func TestPendingInteractions_NilSafe(t *testing.T) {
	var p *PendingInteractions
	p.Add(&PendingInteraction{ID: "x"})      // must not panic
	if g, ok := p.Get("x"); ok || g != nil { // must not panic
		t.Errorf("nil Get returned (%v, %v); want (nil, false)", g, ok)
	}
	if err := p.Resolve("x", InteractionResult{}); !errors.Is(err, ErrInteractionNotFound) {
		t.Errorf("nil Resolve returned %v, want ErrInteractionNotFound", err)
	}
	if e := p.Expire(time.Now()); e != nil {
		t.Errorf("nil Expire returned %v, want nil", e)
	}
	if got := p.Len(); got != 0 {
		t.Errorf("nil Len = %d, want 0", got)
	}
}

// TestPendingInteractions_Expire covers the GC path: an
// interaction with ExpiresAt in the past is removed and
// its ResultCh is closed; a future Resolve for that id
// returns ErrInteractionNotFound.

// TestPendingInteractions_Expire covers the GC path: an
// interaction with ExpiresAt in the past is removed and
// its ResultCh is closed; a future Resolve for that id
// returns ErrInteractionNotFound.
func TestPendingInteractions_Expire(t *testing.T) {
	var p PendingInteractions
	now := time.Now().UTC()
	intr := &PendingInteraction{
		ID:        "int_old",
		Kind:      InteractionKindPermission,
		ExpiresAt: now.Add(-time.Minute), // already expired
		ResultCh:  make(chan InteractionResult, 1),
	}
	p.Add(intr)
	fresh := &PendingInteraction{
		ID:        "int_fresh",
		Kind:      InteractionKindQuestion,
		ExpiresAt: now.Add(time.Hour),
		ResultCh:  make(chan InteractionResult, 1),
	}
	p.Add(fresh)
	if got := p.Len(); got != 2 {
		t.Fatalf("after Adds: Len = %d, want 2", got)
	}
	expired := p.Expire(now)
	if len(expired) != 1 || expired[0] != "int_old" {
		t.Errorf("Expire returned %v, want [int_old]", expired)
	}
	if got := p.Len(); got != 1 {
		t.Errorf("after Expire: Len = %d, want 1", got)
	}
	// The expired entry's channel is closed; the receiver
	// sees the zero value with ok==false.
	select {
	case r, ok := <-intr.ResultCh:
		if ok {
			t.Errorf("expired entry's ResultCh received ok=true (val=%+v); want closed-channel zero", r)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("expired entry's ResultCh did not close within 50ms")
	}
	// Resolve on the expired id returns -32006's sentinel.
	if err := p.Resolve("int_old", InteractionResult{}); !errors.Is(err, ErrInteractionNotFound) {
		t.Errorf("Resolve(expired) = %v, want ErrInteractionNotFound", err)
	}
	// The fresh one is still resolvable.
	if err := p.Resolve("int_fresh", InteractionResult{Response: json.RawMessage(`"y"`)}); err != nil {
		t.Errorf("Resolve(fresh) = %v, want nil", err)
	}
}

// TestPendingInteractions_ConcurrentResolves_NoDoubleAck
// pins the "exactly one Resolve wins" rule when N goroutines
// race to answer the same interaction. The brief's
// TestUserInteraction_ConcurrentResolves_NoDoubleAck rule.
//
// We expect: exactly one Resolve call returns nil; the
// others return ErrInteractionNotFound. The winning resolve
// delivers exactly one value onto the (len 1) ResultCh.

// TestPendingInteractions_ConcurrentResolves_NoDoubleAck
// pins the "exactly one Resolve wins" rule when N goroutines
// race to answer the same interaction. The brief's
// TestUserInteraction_ConcurrentResolves_NoDoubleAck rule.
//
// We expect: exactly one Resolve call returns nil; the
// others return ErrInteractionNotFound. The winning resolve
// delivers exactly one value onto the (len 1) ResultCh.
func TestPendingInteractions_ConcurrentResolves_NoDoubleAck(t *testing.T) {
	var p PendingInteractions
	intr := &PendingInteraction{
		ID:       "int_race",
		ResultCh: make(chan InteractionResult, 1),
	}
	p.Add(intr)

	const N = 16
	var (
		wg    sync.WaitGroup
		wins  atomic.Int32
		lose  atomic.Int32
		other atomic.Int32
	)
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := p.Resolve("int_race", InteractionResult{Response: json.RawMessage(`"x"`)})
			switch {
			case err == nil:
				wins.Add(1)
			case errors.Is(err, ErrInteractionNotFound):
				lose.Add(1)
			default:
				other.Add(1)
			}
		}()
	}
	wg.Wait()

	if got := wins.Load(); got != 1 {
		t.Errorf("wins = %d, want 1", got)
	}
	if got := lose.Load(); got != N-1 {
		t.Errorf("loses = %d, want %d", got, N-1)
	}
	if got := other.Load(); got != 0 {
		t.Errorf("other errors = %d, want 0", got)
	}
	// The buffered channel must have received exactly one
	// value (no double-ack): the second resolve has
	// already dropped the entry before its send.
	count := 0
loop:
	for {
		select {
		case <-intr.ResultCh:
			count++
		default:
			break loop
		}
	}
	if count != 1 {
		t.Errorf("ResultCh received %d values, want exactly 1", count)
	}
}

// --- Cascade.AddInteraction lazy-init tests ------------------------

// TestAddInteraction_LazyInit covers the brief's
// "first call allocates Pending, second call sees the same"
// rule. The cascade's *PendingInteractions starts nil; the
// first AddInteraction call lazy-allocates; subsequent
// calls reuse the same registry.

// --- Cascade.AddInteraction lazy-init tests ------------------------

// TestAddInteraction_LazyInit covers the brief's
// "first call allocates Pending, second call sees the same"
// rule. The cascade's *PendingInteractions starts nil; the
// first AddInteraction call lazy-allocates; subsequent
// calls reuse the same registry.
func TestAddInteraction_LazyInit(t *testing.T) {
	c, err := New(Opts{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// Freshly built: Pending() returns nil until first call.
	c.mu.Lock()
	preNil := c.pending == nil
	c.mu.Unlock()
	if !preNil {
		t.Errorf("freshly-built cascade has pending != nil; want nil for lazy-init")
	}

	intr1, err := c.AddInteraction(InteractionKindPermission, "read main.go?", "ok?", nil, false, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("first AddInteraction: %v", err)
	}
	first := c.Pending()
	if first == nil {
		t.Fatalf("after first AddInteraction: Pending() = nil; want non-nil")
	}
	if first.Len() != 1 {
		t.Errorf("after first AddInteraction: registry.Len = %d, want 1", first.Len())
	}

	intr2, err := c.AddInteraction(InteractionKindQuestion, "second?", "?", nil, false, time.Time{}, time.Time{})
	if err != nil {
		t.Fatalf("second AddInteraction: %v", err)
	}
	second := c.Pending()
	if second != first {
		t.Errorf("after second AddInteraction: Pending() returned a different registry; want the same")
	}
	if first.Len() != 2 {
		t.Errorf("after second AddInteraction: registry.Len = %d, want 2", first.Len())
	}
	if intr1.ID == intr2.ID {
		t.Errorf("two AddInteractions minted the same id %q", intr1.ID)
	}
	if !strings.HasPrefix(intr1.ID, "int_") {
		t.Errorf("intr1.ID = %q, want int_ prefix", intr1.ID)
	}
	if intr1.ResultCh == nil || intr2.ResultCh == nil {
		t.Errorf("ResultCh not initialised; intr1=%v intr2=%v", intr1.ResultCh, intr2.ResultCh)
	}
}

// TestAddInteraction_NilSafe pins the "no panic on
// nil cascade" rule for AddInteraction. Pending() on a
// nil cascade returns nil too.

// TestAddInteraction_NilSafe pins the "no panic on
// nil cascade" rule for AddInteraction. Pending() on a
// nil cascade returns nil too.
func TestAddInteraction_NilSafe(t *testing.T) {
	var c *Cascade
	if got := c.Pending(); got != nil {
		t.Errorf("nil cascade Pending() = %v, want nil", got)
	}
	if _, err := c.AddInteraction(InteractionKindPermission, "t", "p", nil, false, time.Time{}, time.Time{}); err == nil {
		t.Errorf("nil cascade AddInteraction returned nil error; want non-nil")
	}
}

// --- Dispatcher.HandleRemoteAgentCascadeUserInteraction tests -----

// TestHandleRemoteAgentCascadeUserInteraction_Ok pins the
// happy path: StubBackend emits an interaction, the sink
// adds it to the cascade's pending registry, and a
// concurrent client goroutine resolves the interaction
// via the RPC handler. The stub's park unblocks and the
// turn completes.

// TestInteractionTimeoutInBackend pins the brief's
// "StubBackend + Answers empty + interaction never
// resolved → ErrInteractionTimeout" path. The
// stub's InteractionWait fires before any handler call;
// SendStream returns the timeout error.
func TestInteractionTimeoutInBackend(t *testing.T) {
	be := &StubBackend{
		Reply:      "should not be reached",
		StopReason: StopReasonEndTurn,
		Interactions: []InteractionConfig{
			{
				Kind:   InteractionKindPermission,
				Title:  "ignored",
				Prompt: "ignored",
			},
		},
		// Keep the test fast — 100ms is well above the
		// ~10ms handshake of test infrastructure and
		// well below the 2s the happy-path test uses.
		InteractionWait: 50 * time.Millisecond,
	}
	reg, cid := registerWithBackend(t, be)

	_, err := reg.SendStream(context.Background(), cid, Prompt{Text: "x"}, nil, SendOptions{})
	if err == nil {
		t.Fatalf("SendStream returned nil error; want ErrInteractionTimeout")
	}
	if !errors.Is(err, ErrInteractionTimeout) {
		t.Errorf("err = %v, want errors.Is(., ErrInteractionTimeout)", err)
	}
	if !errors.Is(err, ErrTurnFailed) {
		t.Errorf("err = %v, want errors.Is(., ErrTurnFailed) (registry wraps it)", err)
	}
}

// --- helpers ----------------------------------------------------------

// startBareCascade registers a fresh cascade on d without
// going through the JSON-RPC envelope. Used by handler-only
// tests that don't need to exercise the wire format.
