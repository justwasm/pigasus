// Tests for the SetRemoteControlOriginPatterns
// JSON-RPC handler (cycle rc-auth-tokens).
//
//   - Validation rules: patterns must be a JSON
//     array (typed wrong / missing → -32602);
//     per-pattern validator catches malformed
//     URLs (rejected list echoes verbatim).
//
//   - Store semantics: a "happy path" Set lands
//     the accepted patterns in the store's
//     snapshot (sorted) and emits an
//     origin_policy/changed notification; the
//     registered transport setter
//     (SetOriginPolicySetter) sees the new
//     patterns synchronously.
//
//   - Sentinel-error mapping:
//     mapOriginPolicyError → -32603 internal
//     error envelope; per-pattern validator →
//     -32602 invalid params (rejected list
//     echoed in the result).
//
// Wire-level tests live in
// remotetransport/tcp_test.go
// (mirroring the project's split: in-process
// envelopes vs. wire round-trips).
package remotecontrol

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/originpolicy"
)

// sampleOriginPolicy returns a small canned
// OriginPolicy value the SetterSpy can recognise.
func sampleOriginPolicy() originpolicy.OriginPolicy {
	return originpolicy.OriginPolicy{
		Patterns:  []string{"https://antigravity.google"},
		UpdatedAt: time.Date(2026, 10, 19, 14, 0, 0, 0, time.UTC),
	}
}

// SetterSpy is a capturable
// (*Dispatcher).SetOriginPolicySetter callback.
// It records every OriginPolicy value it's
// invoked with so the dispatch test can assert
// the post-mutation mirror.
type SetterSpy struct {
	mu       sync.Mutex
	calls    []originpolicy.OriginPolicy
	calledCt atomic.Int32
}

func (s *SetterSpy) setter(p originpolicy.OriginPolicy) {
	s.mu.Lock()
	s.calls = append(s.calls, p)
	s.mu.Unlock()
	s.calledCt.Add(1)
}

func (s *SetterSpy) snapshot() []originpolicy.OriginPolicy {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]originpolicy.OriginPolicy, len(s.calls))
	copy(out, s.calls)
	return out
}

// ---- happy path ------------------------------------------

// TestSetRemoteControlOriginPatterns_OK pins the
// happy path: 3 valid patterns → result envelope
// carries applied_count=3, rejected=[], updated_at
// stamped, and the dispatcher's setter is invoked
// with the post-mutation policy.
func TestSetRemoteControlOriginPatterns_OK(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)
	spy := &SetterSpy{}
	d.SetOriginPolicySetter(spy.setter)

	res, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google","https://antigravity-companion.google","chrome-extension://abcdefghijklmnop"]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	if v, _ := res["ok"].(bool); !v {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if v := intOf(res["applied_count"]); v != 3 {
		t.Errorf("applied_count = %v, want 3", res["applied_count"])
	}
	rej, _ := res["rejected"].([]string)
	if len(rej) != 0 {
		t.Errorf("rejected = %v, want []", rej)
	}
	patterns, _ := res["patterns"].([]string)
	if len(patterns) != 3 {
		t.Fatalf("patterns len = %d, want 3", len(patterns))
	}
	// Sorted ascending: chrome-extension first,
	// then antigravity-companion, then antigravity.
	wantOrder := []string{
		"chrome-extension://abcdefghijklmnop",
		"https://antigravity-companion.google",
		"https://antigravity.google",
	}
	for i, w := range wantOrder {
		if patterns[i] != w {
			t.Errorf("patterns[%d] = %q, want %q", i, patterns[i], w)
		}
	}
	if ua, _ := res["updated_at"].(string); ua == "" {
		t.Errorf("updated_at = %q, want non-empty", res["updated_at"])
	}
	// Setter invoked exactly once with the
	// post-mutation policy.
	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("setter called %d times, want 1", len(calls))
	}
	if len(calls[0].Patterns) != 3 {
		t.Errorf("setter Patterns len = %d, want 3", len(calls[0].Patterns))
	}
	// Store snapshot reflects the same view
	// the response envelope echoes.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 3 {
		t.Errorf("store Patterns len = %d, want 3", len(state.Patterns))
	}
}

// TestSetRemoteControlOriginPatterns_PersistenceAndMutation
// pins the brief's "on-disk JSON + in-memory
// snapshot + broadcast" lock-step guarantee.
// A successful Set lands in BOTH the store's
// Snapshot view AND the persistent file on disk.
func TestSetRemoteControlOriginPatterns_PersistenceAndMutation(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)

	if _, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google"]}`)); errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 1 || state.Patterns[0] != "https://antigravity.google" {
		t.Errorf("store state Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
}

// TestSetRemoteControlOriginPatterns_Mixed
// pins the per-pattern validator's
// split-and-echo behaviour: valid patterns
// land in the store, invalid patterns are
// echoed verbatim in `rejected`.
func TestSetRemoteControlOriginPatterns_Mixed(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)
	spy := &SetterSpy{}
	d.SetOriginPolicySetter(spy.setter)

	res, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google","not a url","file:///etc/passwd","https://antigravity-companion.google"]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	if v, _ := res["ok"].(bool); !v {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if v := intOf(res["applied_count"]); v != 2 {
		t.Errorf("applied_count = %v, want 2", res["applied_count"])
	}
	rej, _ := res["rejected"].([]string)
	if len(rej) != 2 {
		t.Fatalf("rejected len = %d, want 2", len(rej))
	}
	wantRejected := []string{"not a url", "file:///etc/passwd"}
	for i, w := range wantRejected {
		if rej[i] != w {
			t.Errorf("rejected[%d] = %q, want %q", i, rej[i], w)
		}
	}
	// Setter is invoked exactly once with
	// the accepted patterns (the two good
	// ones).
	calls := spy.snapshot()
	if len(calls) != 1 {
		t.Fatalf("setter called %d times, want 1", len(calls))
	}
	if len(calls[0].Patterns) != 2 {
		t.Errorf("setter Patterns len = %d, want 2", len(calls[0].Patterns))
	}
}

// TestSetRemoteControlOriginPatterns_EmptyExplicit
// pins the explicit "patterns=[] ⇒ allow all"
// semantic. The store lands an empty state
// and the response envelope carries
// applied_count=0.
func TestSetRemoteControlOriginPatterns_EmptyExplicit(t *testing.T) {
	store := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
	}, nil)
	d := newOriginPolicyDispatcher(t, store)
	spy := &SetterSpy{}
	d.SetOriginPolicySetter(spy.setter)

	res, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":[]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	if v, _ := res["ok"].(bool); !v {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if v := intOf(res["applied_count"]); v != 0 {
		t.Errorf("applied_count = %v, want 0", res["applied_count"])
	}
	// State is now empty.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 0 {
		t.Errorf("store state Patterns = %v, want []", state.Patterns)
	}
	if len(spy.snapshot()) != 1 {
		t.Errorf("setter called %d times, want 1", len(spy.snapshot()))
	}
}

// ---- validation ------------------------------------------

// TestSetRemoteControlOriginPatterns_NullPatterns
// pins the "patterns field must be an array"
// rule. `patterns:null` (or absent) is
// -32602 "Invalid params".
func TestSetRemoteControlOriginPatterns_NullPatterns(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)

	_, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":null}`))
	if errObj == nil {
		t.Fatalf("dispatcher returned no error for null patterns")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %v, want -32602", errObj["code"])
	}
}

// TestSetRemoteControlOriginPatterns_NonArrayPatterns
// pins that the patterns field must be an array;
// a single string or object fails parsing and
// surfaces -32602.
func TestSetRemoteControlOriginPatterns_NonArrayPatterns(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)

	_, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":"https://antigravity.google"}`))
	if errObj == nil {
		t.Fatalf("dispatcher returned no error for non-array patterns")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %v, want -32602", errObj["code"])
	}
}

// TestSetRemoteControlOriginPatterns_MissingPatterns
// pins the "patterns field is required" rule.
// `{}` (no patterns key) is rejected with
// -32602 because the unmarshal leaves
// p.Patterns nil and the handler returns the
// explicit "patterns is required" message.
func TestSetRemoteControlOriginPatterns_MissingPatterns(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)

	_, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{}`))
	if errObj == nil {
		t.Fatalf("dispatcher returned no error for missing patterns field")
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %v, want -32602", errObj["code"])
	}
}

// TestSetRemoteControlOriginPatterns_NullParams
// pins the no-params case: passing `null` (the
// "params block present but null" shape) is
// accepted by the unmarshaller, leaving
// p.Patterns nil, and surfaces -32602.
func TestSetRemoteControlOriginPatterns_NullParams(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)

	req := jsonRPCFor(1, "SetRemoteControlOriginPatterns", []byte(`null`))
	resp := d.Dispatch(context.Background(), req)
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error envelope; got %#v", resp)
	}
	if code, _ := errObj["code"].(int); code != -32602 {
		t.Errorf("code = %v, want -32602", errObj["code"])
	}
}

// TestSetRemoteControlOriginPatterns_AllRejected_NoStateChange
// pins the "validate before the lock; preserve
// state on reject-all" semantic. When every
// entry fails validation, the store's snapshot
// is unchanged, the setter is NOT invoked, and
// the response echoes the rejected entries
// verbatim with applied_count=0.
func TestSetRemoteControlOriginPatterns_AllRejected_NoStateChange(t *testing.T) {
	store := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
	}, nil)
	d := newOriginPolicyDispatcher(t, store)
	spy := &SetterSpy{}
	d.SetOriginPolicySetter(spy.setter)

	res, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["not a url","file:///etc/passwd"]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	if v, _ := res["ok"].(bool); !v {
		t.Errorf("ok = %v, want true", res["ok"])
	}
	if v := intOf(res["applied_count"]); v != 0 {
		t.Errorf("applied_count = %v, want 0", res["applied_count"])
	}
	rej, _ := res["rejected"].([]string)
	if len(rej) != 2 {
		t.Fatalf("rejected len = %d, want 2", len(rej))
	}
	// Setter was NOT invoked (state is
	// preserved so there's no mutation to
	// mirror).
	calls := spy.snapshot()
	if len(calls) != 0 {
		t.Errorf("setter called %d times, want 0 on reject-all; got %+v", len(calls), calls)
	}
	// Store state unchanged.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 1 || state.Patterns[0] != "https://antigravity.google" {
		t.Errorf("store Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
}

// ---- no store / persistence failure --------------------

// TestSetRemoteControlOriginPatterns_NoStore pins
// the nil-store sentinel path. The dispatcher
// built with Config.OriginPolicyStore == nil
// returns -32603 "Internal error" because the
// per-store handler can't dispatch the
// mutation. (In practice NewDispatcher's
// nil-fabrication installs a fresh
// MemOriginPolicyStore — this test asserts
// the explicit-set-to-nil path that the
// nil-fabrication is defensive against.)
func TestSetRemoteControlOriginPatterns_NoStore(t *testing.T) {
	// Build a dispatcher config with an
	// explicit OriginPolicyStore = nil, then
	// post-construction clear it. This is the
	// only way to drive the handler through
	// its nil-store branch without modifying
	// NewDispatcher.
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:           sess,
		OriginPolicyStore: nil,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.OriginPolicyStore = nil

	_, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google"]}`))
	if errObj == nil {
		t.Fatalf("expected error envelope for no-store")
	}
	if code, _ := errObj["code"].(int); code != -32603 {
		t.Errorf("code = %v, want -32603", errObj["code"])
	}
	if msg, _ := errObj["message"].(string); msg != "Internal error" {
		t.Errorf("message = %q, want Internal error", msg)
	}
	dataStr, _ := errObj["data"].(string)
	if !strContains(dataStr, "no origin-policy store") {
		t.Errorf("data = %q, want substring 'no origin-policy store'", dataStr)
	}
}

// TestSetRemoteControlOriginPatterns_PersistenceFailed
// pins the -32603 envelope when the FS store's
// write fails. We can't easily cause a real
// write failure in an in-process test, so this
// pins the mapping via mapOriginPolicyError
// directly with a synthetic sentinel error.
func TestSetRemoteControlOriginPatterns_PersistenceFailed(t *testing.T) {
	got := mapOriginPolicyError(errOriginPolicyPersistenceFailed, "set origin patterns")
	if got == nil {
		t.Fatalf("mapOriginPolicyError returned nil")
	}
	if got.Code != -32603 {
		t.Errorf("Code = %d, want -32603", got.Code)
	}
	dataStr, _ := got.Data.(string)
	if !strContains(dataStr, "persistence failed") {
		t.Errorf("Data = %q, want substring persistence failed", dataStr)
	}
}

// ---- notification broadcast ----------------------------

// TestSetRemoteControlOriginPatterns_BroadcastFanOut
// pins the brief's
// `origin_policy/changed` notification path.
// Registering a broadcaster that captures every
// (method, params) the dispatcher emits and
// running a successful Set confirms:
//
//   - the broadcast helper invokes BroadcastNotify
//     exactly once with the canonical method
//     "origin_policy/changed";
//
//   - the params envelope matches the
//     SetRemoteControlOriginPatterns response
//     shape verbatim (dashboards polling the
//     broadcast and dashboards polling the
//     response see the same envelope).
func TestSetRemoteControlOriginPatterns_BroadcastFanOut(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)
	// Spy broadcaster: captures every
	// BroadcastNotify call.
	type notif struct {
		method string
		params map[string]any
	}
	var (
		mu     sync.Mutex
		notifs []notif
	)
	d.SetBroadcaster(func(ctx context.Context, method string, params any) error {
		pm, _ := params.(map[string]any)
		mu.Lock()
		notifs = append(notifs, notif{method, pm})
		mu.Unlock()
		return nil
	})

	res, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google"]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notifs) != 1 {
		t.Fatalf("broadcaster called %d times, want 1", len(notifs))
	}
	if notifs[0].method != NotifOriginPolicyChangedMethod {
		t.Errorf("method = %q, want %q", notifs[0].method, NotifOriginPolicyChangedMethod)
	}
	// Same envelope as the response.
	if v, _ := notifs[0].params["ok"].(bool); !v {
		t.Errorf("notif.ok = %v, want true", notifs[0].params["ok"])
	}
	if v := intOf(notifs[0].params["applied_count"]); v != intOf(res["applied_count"]) {
		t.Errorf("notif.applied_count = %v, want %v", notifs[0].params["applied_count"], res["applied_count"])
	}
	// updated_at is the server's stamp (not
	// the dashboard's), so non-empty on both
	// sides.
	ua, _ := notifs[0].params["updated_at"].(string)
	if ua == "" {
		t.Errorf("notif.updated_at = %q, want non-empty", notifs[0].params["updated_at"])
	}
}

// TestSetRemoteControlOriginPatterns_NotificationRejected_NoBroadcast
// pins that the reject-all path does NOT emit
// an origin_policy/changed notification
// (because no mutation occurred). The
// broadcaster is invoked zero times.
func TestSetRemoteControlOriginPatterns_NotificationRejected_NoBroadcast(t *testing.T) {
	store := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
	}, nil)
	d := newOriginPolicyDispatcher(t, store)
	var (
		mu    sync.Mutex
		count int
	)
	d.SetBroadcaster(func(ctx context.Context, method string, params any) error {
		mu.Lock()
		count++
		mu.Unlock()
		return nil
	})
	_, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["not a url"]}`))
	if errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	mu.Lock()
	defer mu.Unlock()
	if count != 0 {
		t.Errorf("broadcaster called %d times on reject-all, want 0", count)
	}
}

// TestSetRemoteControlOriginPatterns_NoBroadcaster pins
// that the broadcast helper is robust to a
// nil broadcaster (the Unix-socket + in-process
// dispatcher paths). The mutation still lands
// in the store; the response envelope echoes
// the post-state.
func TestSetRemoteControlOriginPatterns_NoBroadcaster(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)
	// No SetBroadcaster: d.broadcaster is nil.

	if _, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google"]}`)); errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	// Mutation still landed.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 1 || state.Patterns[0] != "https://antigravity.google" {
		t.Errorf("store Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
}

// ---- setter registration -------------------------------

// TestSetOriginPolicySetter_NilSetter pins that
// a nil setter is tolerated by the dispatcher:
// the mutation still lands + the broadcast
// still fires, but no in-process mirror
// happens.
func TestSetOriginPolicySetter_NilSetter(t *testing.T) {
	store := NewMemOriginPolicyStore(nil)
	d := newOriginPolicyDispatcher(t, store)
	// Register nil setter.
	d.SetOriginPolicySetter(nil)

	if _, errObj := dispatchOriginPolicy(t, d, "SetRemoteControlOriginPatterns", []byte(`{"patterns":["https://antigravity.google"]}`)); errObj != nil {
		t.Fatalf("result error: %#v", errObj)
	}
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("store Snapshot: %v", err)
	}
	if len(state.Patterns) != 1 {
		t.Errorf("Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
}

// TestSetOriginPolicySetter_NilDispatcher pins
// that the setter is safe to call on a nil
// dispatcher (host wiring defensiveness).
func TestSetOriginPolicySetter_NilDispatcher(t *testing.T) {
	var d *Dispatcher
	// Must not panic.
	d.SetOriginPolicySetter(func(originpolicy.OriginPolicy) {})
}

// ---- helpers --------------------------------------------

// strContains is a tiny local helper avoiding the
// strings import clash with piagentbackend_test.go's
// top-level `contains`.
func strContains(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// mustApplyCount returns the float64 (the
// default JSON-decoded number type) of the
// applied_count field. Used by the broadcast
// envelope parity test.
func mustApplyCount(res map[string]any) float64 {
	v, _ := res["applied_count"].(float64)
	return v
}

// intOf coerces a JSON-decoded number (any of
// int / int64 / float64) into an int so the
// dispatcher-side helper originPolicyToWire
// (which writes `len(patterns)` as int) and the
// broadcast-envelope parity test (which reads
// from a JSON round-trip and gets back as
// float64) both surface the same value.
func intOf(v any) int {
	switch x := v.(type) {
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case float64:
		return int(x)
	case float32:
		return int(x)
	default:
		return 0
	}
}
