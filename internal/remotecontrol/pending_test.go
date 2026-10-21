// Tests for the rc-handle-user-interaction cycle. Covers:
//
//   - the PendingInteractions registry (Add / Get / Resolve / Expire);
//   - the Cascade.Pending / Cascade.AddInteraction lazy-init path;
//   - the dispatcher's HandleRemoteAgentCascadeUserInteraction
//     handler at the in-process level;
//   - the StubBackend.Interactions fixture (round-tripped through
//     the dispatcher's SendRemoteAgentCascadeMessage path);
//
// Wire-level coverage (cascade/user_interaction_required
// notification + HandleRemoteAgentCascadeUserInteraction RPC
// over coder/websocket) lives next to the TCP+WS transport
// in internal/remotetransport/tcp_test.go.
package remotecontrol

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

// TestCascade_AddInteractionLazyInit covers the brief's
// "first call allocates Pending, second call sees the same"
// rule. The cascade's *PendingInteractions starts nil; the
// first AddInteraction call lazy-allocates; subsequent
// calls reuse the same registry.
func TestCascade_AddInteractionLazyInit(t *testing.T) {
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
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

// TestCascade_AddInteractionNilSafe pins the "no panic on
// nil cascade" rule for AddInteraction. Pending() on a
// nil cascade returns nil too.
func TestCascade_AddInteractionNilSafe(t *testing.T) {
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
func TestHandleRemoteAgentCascadeUserInteraction_Ok(t *testing.T) {
	be := &StubBackend{
		Reply:      "done",
		StopReason: StopReasonEndTurn,
		Interactions: []PendingInteractionConfig{
			{
				Kind:   InteractionKindPermission,
				Title:  "Read main.go?",
				Prompt: "ok?",
			},
		},
		// Cap the wait so a buggy Resolve path that fails to
		// unblock the channel doesn't pin the test.
		InteractionWait: 2 * time.Second,
	}
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Build a fresh cascade on the dispatcher's own
	// registry (the test helper's registerCascadeWithBackend
	// uses a separate registry). We need the dispatcher
	// to see the cascade so its handler can resolve it.
	c, err := NewCascade(CascadeOpts{Model: "Gemini 3.8 Flash (Medium)"})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	c.SetBackend(be)
	if err := d.Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	cid := c.ID
	reg := d.Cascades()
	// The dispatcher has no notifier; Notify becomes a no-op.
	// The sink still adds to the cascade's pending registry
	// via its OnUserInteraction method.
	sink := NewConnNotifierSink(d, "", c)

	// Drive the turn in a goroutine. The stub will pause on
	// intr.ResultCh; we resolve it from the main goroutine
	// once we know the entry exists.
	type turnResult struct {
		res MessageResult
		err error
	}
	resultCh := make(chan turnResult, 1)
	go func() {
		prompt := MessagePrompt{Text: "hello"}
		res, err := reg.SendStream(context.Background(), cid, prompt, sink, MessageSendOptions{})
		resultCh <- turnResult{res: res, err: err}
	}()

	// Wait for the cascade's pending registry to see the
	// entry — the stub mints a fresh id and the sink adds
	// it via OnUserInteraction before parking on ResultCh.
	var intrID string
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if got, ok := c.Pending().Get(""); ok && got != nil {
			// dummy lookup to ensure alloc has happened
			_ = got
		}
		// Read the entry id from the map directly so we
		// don't need to know the minted id in advance.
		c.mu.Lock()
		pending := c.pending
		c.mu.Unlock()
		if pending != nil {
			pending.mu.Lock()
			for id := range pending.m {
				intrID = id
			}
			pending.mu.Unlock()
			if intrID != "" {
				break
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if intrID == "" {
		t.Fatalf("no pending interaction registered within 2s")
	}

	// Resolve via the dispatcher's handler.
	req := []byte(`{
		"jsonrpc":"2.0",
		"id":99,
		"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":` + jsonString(intrID) + `,"response":"yes"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"]; ok {
		t.Fatalf("HandleRemoteAgentCascadeUserInteraction returned error: %#v", errObj)
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Handle…UserInteraction: no result, got %#v", resp)
	}
	if ok, _ := res["ok"].(bool); !ok {
		t.Errorf("result.ok = %v, want true", res["ok"])
	}
	if _, ok := res["decided_at"].(string); !ok {
		t.Errorf("result.decided_at = %T, want string", res["decided_at"])
	}
	appliedTo, ok := res["applied_to"].([]string)
	if !ok || len(appliedTo) != 0 {
		t.Errorf("result.applied_to = %v (%T), want []string{}", res["applied_to"], res["applied_to"])
	}

	// The stub's park should unblock; the turn completes
	// successfully.
	select {
	case r := <-resultCh:
		if r.err != nil {
			t.Fatalf("SendStream returned error: %v", r.err)
		}
		if r.res.Reply != "done" {
			t.Errorf("Reply = %q, want done", r.res.Reply)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("SendStream did not return within 2s after Resolve")
	}

	// The pending registry is empty after a successful Resolve.
	if got := c.Pending().Len(); got != 0 {
		t.Errorf("after Resolve: Pending().Len = %d, want 0", got)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade
// pins the -32004 envelope path when cascade_id is well-
// formed but no cascade is registered.
func TestHandleRemoteAgentCascadeUserInteraction_UnknownCascade(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":"cas_doesnotexist","interaction_id":"int_x","response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeCascadeNotFound {
		t.Errorf("error.code = %d, want %d (Cascade not found)", got, ErrCodeCascadeNotFound)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction
// pins the -32006 envelope path: the cascade is registered
// but the interaction_id is unknown (never existed, already
// answered, or expired).
func TestHandleRemoteAgentCascadeUserInteraction_UnknownInteraction(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cid := startBareCascade(t, d)

	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":"int_unknown","response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeInteractionNotFound {
		t.Errorf("error.code = %d, want %d (Interaction not found)", got, ErrCodeInteractionNotFound)
	}
}

// TestHandleRemoteAgentCascadeUserInteraction_BadParams
// pins the -32602 envelope path: missing or empty
// cascade_id / interaction_id / response.
func TestHandleRemoteAgentCascadeUserInteraction_BadParams(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cases := map[string]string{
		"missing cascade_id":     `{"interaction_id":"int_x","response":"y"}`,
		"empty cascade_id":       `{"cascade_id":"","interaction_id":"int_x","response":"y"}`,
		"whitespace cascade_id":  `{"cascade_id":"   ","interaction_id":"int_x","response":"y"}`,
		"missing interaction_id": `{"cascade_id":"cas_x","response":"y"}`,
		"empty interaction_id":   `{"cascade_id":"cas_x","interaction_id":"","response":"y"}`,
		"missing response":       `{"cascade_id":"cas_x","interaction_id":"int_x"}`,
		"null response":          `{"cascade_id":"cas_x","interaction_id":"int_x","response":null}`,
	}
	for name, params := range cases {
		t.Run(name, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction","params":` + params + `}`
			resp := d.Dispatch(context.Background(), []byte(body))
			errObj, ok := resp["error"].(map[string]any)
			if !ok {
				t.Fatalf("no error envelope, got %#v", resp)
			}
			if got := errorCode(errObj); got != -32602 {
				t.Errorf("error.code = %d, want -32602", got)
			}
		})
	}
}

// TestUserInteraction_Expires pins the GC path: an
// interaction with ExpiresAt in the past is removed on the
// next Expire tick; resolving a removed entry returns
// -32006.
//
// The dispatcher doesn't ship an expiry goroutine today;
// the brief: "rc-piagent-bind will wire a tick into the
// host's existing tick loop". The test directly invokes
// (*PendingInteractions).Expire and confirms the next
// Handle…UserInteraction call sees the entry gone.
func TestUserInteraction_Expires(t *testing.T) {
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	cid := startBareCascade(t, d)
	c, _ := d.Cascades().Get(cid)

	now := time.Now().UTC()
	intr, err := c.AddInteraction(InteractionKindPermission, "old", "p", nil, false, now.Add(-time.Second), now)
	if err != nil {
		t.Fatalf("AddInteraction: %v", err)
	}

	// Run the expiry tick.
	expired := c.Pending().Expire(now)
	if len(expired) != 1 || expired[0] != intr.ID {
		t.Errorf("Expire returned %v, want [%s]", expired, intr.ID)
	}

	// Resolving an expired entry returns -32006.
	req := []byte(`{
		"jsonrpc":"2.0","id":1,"method":"HandleRemoteAgentCascadeUserInteraction",
		"params":{"cascade_id":` + jsonString(cid) + `,"interaction_id":` + jsonString(intr.ID) + `,"response":"y"}
	}`)
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("no error envelope, got %#v", resp)
	}
	if got := errorCode(errObj); got != ErrCodeInteractionNotFound {
		t.Errorf("error.code = %d, want %d (Interaction not found)", got, ErrCodeInteractionNotFound)
	}
}

// TestUserInteraction_TimeoutInBackend pins the brief's
// "StubBackend + Answers empty + interaction never
// resolved → ErrCascadeInteractionTimeout" path. The
// stub's InteractionWait fires before any handler call;
// SendStream returns the timeout error.
func TestUserInteraction_TimeoutInBackend(t *testing.T) {
	be := &StubBackend{
		Reply:      "should not be reached",
		StopReason: StopReasonEndTurn,
		Interactions: []PendingInteractionConfig{
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
	sess := NewSessionState("")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	reg, cid := registerCascadeWithBackend(t, be)
	_ = d
	_ = cid

	_, err = reg.SendStream(context.Background(), cid, MessagePrompt{Text: "x"}, nil, MessageSendOptions{})
	if err == nil {
		t.Fatalf("SendStream returned nil error; want ErrCascadeInteractionTimeout")
	}
	if !errors.Is(err, ErrCascadeInteractionTimeout) {
		t.Errorf("err = %v, want errors.Is(., ErrCascadeInteractionTimeout)", err)
	}
	if !errors.Is(err, ErrTurnFailed) {
		t.Errorf("err = %v, want errors.Is(., ErrTurnFailed) (registry wraps it)", err)
	}
}

// --- helpers ----------------------------------------------------------

// startBareCascade registers a fresh cascade on d without
// going through the JSON-RPC envelope. Used by handler-only
// tests that don't need to exercise the wire format.
func startBareCascade(t *testing.T, d *Dispatcher) string {
	t.Helper()
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.Cascades().Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c.ID
}

// jsonString returns a JSON-encoded string literal for a
// bare string (used to embed cascade_id / interaction_id
// values inside a JSON-encoded params object).
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
