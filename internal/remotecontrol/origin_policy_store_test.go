// Tests for the rc-auth-tokens cycle's
// OriginPolicyStore seam. Covers:
//
//   - FSOriginPolicyStore — the production
//     path: load-missing-file, parse-error
//     surfacing, version-greater rejection,
//     atomic-rename round-trip, sort-on-Snapshot,
//     Subscribe fan-out, drop-on-backpressure,
//     unsubscribe closes the chan.
//
//   - MemOriginPolicyStore — the in-process
//     test seam: Snapshot defaults, SetPatterns
//     validation + sorting + rejected echo,
//     Subscribe fan-out, mirror of the FS store's
//     shape so the dispatcher's handler is
//     agnostic of the backend.
//
//   - mapOriginPolicyError — sentinel envelope
//     mapping (per-pattern validator → -32602 is
//     emitted directly by the handler, so this
//     helper only sees the persistence-failure
//     arm and the catch-all → -32603).
//
// Wire-level tests for the
// SetRemoteControlOriginPatterns RPC live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/originpolicy"
)

// newOriginPolicyDispatcher returns a Dispatcher with the
// supplied OriginPolicyStore pre-installed. Mirrors
// newPluginsDispatcher / newFastpushDispatcher.
//
// When os nil, NewDispatcher fabricates a fresh
// empty MemOriginPolicyStore (the nil-guard pattern
// rc-auth-tokens adds). Tests that want to assert
// "no store installed" pass nil and exercise the
// -32603 path; tests that want a specific store
// pass it directly.
func newOriginPolicyDispatcher(t *testing.T, os OriginPolicyStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:           sess,
		OriginPolicyStore: os,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchOriginPolicy posts one JSON-RPC
// request to the dispatcher and returns the
// (result, error) pair. Mirrors
// dispatchPlugins / dispatchFastpush.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so
// we mirror the convention `[]byte("null")`
// rather than a missing-key shape.
func dispatchOriginPolicy(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
		var err error
		raw, err = jsonMarshal(v)
		if err != nil {
			t.Fatalf("marshal params: %v", err)
		}
	}
	req := jsonRPCFor(1, method, raw)
	resp := d.Dispatch(context.Background(), req)
	if errObj, ok := resp["error"].(map[string]any); ok {
		return nil, errObj
	}
	res, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("%s: no result, got %#v", method, resp)
	}
	return res, nil
}

// ---- constant pin -----------------------------------------

// TestNotifOriginPolicyChangedMethod pins the
// canonical notification method the
// SetRemoteControlOriginPatterns handler emits
// on every successful mutation. The future
// rc-all-sinks-fanout cycle will register a
// per-channel dispatcher with this method.
func TestNotifOriginPolicyChangedMethod(t *testing.T) {
	if NotifOriginPolicyChangedMethod != "origin_policy/changed" {
		t.Errorf("NotifOriginPolicyChangedMethod = %q, want origin_policy/changed", NotifOriginPolicyChangedMethod)
	}
}

// TestOriginPolicyStateVersion pins the
// on-disk format version the current store
// writes. v1 is the brief-pinned value.
func TestOriginPolicyStateVersion(t *testing.T) {
	if OriginPolicyStateVersion != 1 {
		t.Errorf("OriginPolicyStateVersion = %d, want 1", OriginPolicyStateVersion)
	}
}

// ---- MemOriginPolicyStore in-process tests -----------------

// TestMemOriginPolicyStore_DefaultState pins the
// fresh-store baseline: a fresh
// MemOriginPolicyStore has version=1, patterns=[],
// and a non-zero UpdatedAt. The dispatcher's
// NewDispatcher nil-fabrication relies on this
// "empty is the starting state" surface so an
// in-process dispatcher built without an
// explicit store still has a coherent answer to
// SetRemoteControlOriginPatterns.
func TestMemOriginPolicyStore_DefaultState(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	state, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != OriginPolicyStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, OriginPolicyStateVersion)
	}
	if len(state.Patterns) != 0 {
		t.Errorf("len(Patterns) = %d, want 0 (fresh)", len(state.Patterns))
	}
	if state.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt is zero, want non-zero")
	}
}

// TestMemOriginPolicyStore_DefaultStateNow pins
// the optional now-function: a MemOriginPolicyStore
// stamped with a fixed clock reports that clock's
// value as the UpdatedAt, so a test that wants
// monotonic time can drive the store without
// time.Sleep sequences.
func TestMemOriginPolicyStore_DefaultStateNow(t *testing.T) {
	fixed := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	m := NewMemOriginPolicyStore(func() time.Time { return fixed })
	state, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if !state.UpdatedAt.Equal(fixed) {
		t.Errorf("UpdatedAt = %v, want %v", state.UpdatedAt, fixed)
	}
}

// TestMemOriginPolicyStore_NewMemWith pins the
// NewMemOriginPolicyStoreWith constructor: the
// store pre-populates with the supplied patterns
// (sorted, deep-copied) so a test that wants to
// exercise the snapshot-after-load path doesn't
// have to round-trip through SetPatterns.
func TestMemOriginPolicyStore_NewMemWith(t *testing.T) {
	m := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity-companion.google",
		"https://antigravity.google",
		"chrome-extension://abcdefghijklmnop",
	}, nil)
	state, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []string{
		"chrome-extension://abcdefghijklmnop",
		"https://antigravity-companion.google",
		"https://antigravity.google",
	}
	if len(state.Patterns) != len(want) {
		t.Fatalf("len(Patterns) = %d, want %d", len(state.Patterns), len(want))
	}
	for i := range want {
		if state.Patterns[i] != want[i] {
			t.Errorf("Patterns[%d] = %q, want %q", i, state.Patterns[i], want[i])
		}
	}
}

// TestMemOriginPolicyStore_SetPatterns_Valid pins
// the happy path: SetPatterns with three valid
// entries accepts all three (sorted), bumps
// UpdatedAt, and echoes applied_count via the
// returned state. rejected is empty.
func TestMemOriginPolicyStore_SetPatterns_Valid(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	state, rejected, _, err := m.SetPatterns(context.Background(), []string{
		"https://antigravity-companion.google",
		"https://antigravity.google",
		"chrome-extension://abcdefghijklmnop",
	})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(rejected) != 0 {
		t.Errorf("rejected = %v, want empty", rejected)
	}
	if len(state.Patterns) != 3 {
		t.Fatalf("len(Patterns) = %d, want 3", len(state.Patterns))
	}
	// Sorted ascending.
	want := []string{
		"chrome-extension://abcdefghijklmnop",
		"https://antigravity-companion.google",
		"https://antigravity.google",
	}
	for i := range want {
		if state.Patterns[i] != want[i] {
			t.Errorf("Patterns[%d] = %q, want %q", i, state.Patterns[i], want[i])
		}
	}
}

// TestMemOriginPolicyStore_SetPatterns_Mixed
// pins the per-pattern validation behaviour:
// valid entries land in state.Patterns (sorted),
// invalid entries land in rejected verbatim so
// the dispatcher's response envelope echoes the
// verbatim failure.
func TestMemOriginPolicyStore_SetPatterns_Mixed(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	state, rejected, _, err := m.SetPatterns(context.Background(), []string{
		"https://antigravity.google",           // ok
		"not a url",                            // bad
		"file:///etc/passwd",                   // bad (file:// scheme)
		"",                                     // bad (empty)
		"https://antigravity-companion.google", // ok
	})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(state.Patterns) != 2 {
		t.Fatalf("len(Patterns) = %d, want 2; got %v", len(state.Patterns), state.Patterns)
	}
	wantRejected := []string{"not a url", "file:///etc/passwd", ""}
	if len(rejected) != len(wantRejected) {
		t.Fatalf("rejected len = %d, want %d", len(rejected), len(wantRejected))
	}
	for i := range wantRejected {
		if rejected[i] != wantRejected[i] {
			t.Errorf("rejected[%d] = %q, want %q", i, rejected[i], wantRejected[i])
		}
	}
}

// TestMemOriginPolicyStore_SetPatterns_Trim pins
// the TrimSpace-before-validate behaviour:
// leading / trailing whitespace is stripped
// before the strict-on-write validator runs.
func TestMemOriginPolicyStore_SetPatterns_Trim(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	state, rejected, _, err := m.SetPatterns(context.Background(), []string{
		"  https://antigravity.google  ",
	})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(rejected) != 0 {
		t.Errorf("rejected = %v, want empty", rejected)
	}
	if len(state.Patterns) != 1 || state.Patterns[0] != "https://antigravity.google" {
		t.Errorf("Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
}

// TestMemOriginPolicyStore_SetPatterns_Empty
// pins the explicit "patterns=[] ⇒ allow all"
// semantic the dispatcher relies on: SetPatterns
// with no entries produces an empty state, and
// the brief pins this is NOT a no-op (a dashboard
// can verify the operator explicitly cleared
// the allowlist).
func TestMemOriginPolicyStore_SetPatterns_Empty(t *testing.T) {
	m := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
		"https://antigravity-companion.google",
	}, nil)
	if len(m.state.Patterns) != 2 {
		t.Fatalf("seeded store: len(Patterns) = %d, want 2", len(m.state.Patterns))
	}
	state, _, _, err := m.SetPatterns(context.Background(), []string{})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(state.Patterns) != 0 {
		t.Errorf("Patterns = %v, want []", state.Patterns)
	}
}

// TestMemOriginPolicyStore_SetPatterns_RejectsAll_NoMutation
// pins the "validate before the lock" semantic:
// when every entry is rejected the store does
// NOT touch the on-disk state. (The MemOriginPolicyStore
// has no file but the in-memory state must also
// remain untouched.) A successful Set that rejects
// everything must NOT zero out a previously
// stored policy.
func TestMemOriginPolicyStore_SetPatterns_RejectsAll_NoMutation(t *testing.T) {
	m := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
	}, nil)
	state, rejected, mutated, err := m.SetPatterns(context.Background(), []string{
		"not a url",
		"file:///etc/passwd",
	})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(rejected) != 2 {
		t.Fatalf("rejected = %v, want both entries", rejected)
	}
	if mutated {
		t.Errorf("mutated = true, want false on reject-all")
	}
	if len(state.Patterns) != 1 || state.Patterns[0] != "https://antigravity.google" {
		t.Errorf("Patterns = %v, want [https://antigravity.google] (preserved)", state.Patterns)
	}
	// The reject-all path must NOT broadcast either.
	ch, unsub := m.Subscribe()
	defer unsub()
	state, rejected, mutated, err = m.SetPatterns(context.Background(), []string{"not a url"})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if len(rejected) != 1 || rejected[0] != "not a url" {
		t.Errorf("rejected = %v, want [not a url]", rejected)
	}
	if mutated {
		t.Errorf("mutated = true on second reject-all, want false")
	}
	if len(state.Patterns) != 1 {
		t.Errorf("Patterns = %v, want [https://antigravity.google]", state.Patterns)
	}
	select {
	case pol := <-ch:
		t.Errorf("reject-all must NOT broadcast; got %+v", pol)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestMemOriginPolicyStore_SubscribeFanOut pins
// the brief's "every successful Set broadcasts
// to every subscriber" guarantee. The post-
// mutation OriginPolicy arrives on every chan
// in the subscribers slice.
func TestMemOriginPolicyStore_SubscribeFanOut(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	ch1, unsub1 := m.Subscribe()
	defer unsub1()
	ch2, unsub2 := m.Subscribe()
	defer unsub2()
	if _, _, _, err := m.SetPatterns(context.Background(), []string{
		"https://antigravity.google",
	}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	deadline := time.After(200 * time.Millisecond)
	var p1, p2 originpolicy.OriginPolicy
	for i := 0; i < 2; i++ {
		select {
		case p1 = <-ch1:
		case p2 = <-ch2:
		case <-deadline:
			t.Fatalf("subscriber didn't receive: ch1=%v ch2=%v", p1, p2)
		}
	}
	if len(p1.Patterns) != 1 || p1.Patterns[0] != "https://antigravity.google" {
		t.Errorf("ch1 received %v, want state.Patterns", p1.Patterns)
	}
	if len(p2.Patterns) != 1 || p2.Patterns[0] != "https://antigravity.google" {
		t.Errorf("ch2 received %v, want state.Patterns", p2.Patterns)
	}
}

// TestMemOriginPolicyStore_UnsubscribeStopsDelivery
// pins that the unsubscribe function removes the
// chan from the broadcast slice and closes it. A
// post-unsubscribe Set does NOT enqueue further
// frames, and the chan is readable-only-until-close.
func TestMemOriginPolicyStore_UnsubscribeStopsDelivery(t *testing.T) {
	m := NewMemOriginPolicyStore(nil)
	ch, unsub := m.Subscribe()
	if _, _, _, err := m.SetPatterns(context.Background(), []string{"https://antigravity.google"}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	select {
	case <-ch:
	case <-time.After(50 * time.Millisecond):
		t.Fatalf("subscriber didn't receive first broadcast")
	}
	unsub()
	// Chan is closed after unsubscribe: reading
	// yields zero-value + (closed). A subsequent
	// Set must NOT enqueue.
	if _, _, _, err := m.SetPatterns(context.Background(), []string{"https://antigravity-companion.google"}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	select {
	case <-ch:
		// Either a closed chan firing (zero
		// value), or nothing.
	case <-time.After(50 * time.Millisecond):
	}
	// Two reads total: the post-unsubscribe select
	// above + this one. The chan is closed; the
	// second read returns immediately with the
	// zero value and ok=false.
	if _, ok := <-ch; ok {
		t.Errorf("read on closed chan should report ok=false")
	}
}

// TestMemOriginPolicyStore_DefensiveCopy pins
// that the snapshot returned by Snapshot doesn't
// share the underlying slice with the store's
// internal state — a caller that mutates the
// returned slice doesn't race a Set's mutation.
func TestMemOriginPolicyStore_DefensiveCopy(t *testing.T) {
	m := NewMemOriginPolicyStoreWith([]string{
		"https://antigravity.google",
	}, nil)
	state, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Mutate the returned slice.
	state.Patterns[0] = "https://attacker.example"
	state2, err := m.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state2.Patterns[0] != "https://antigravity.google" {
		t.Errorf("caller mutation leaked: %v", state2.Patterns)
	}
}

// TestMemOriginPolicyStore_NowStampsUpdatedAt
// pins that SetPatterns stamps UpdatedAt via
// the store's now function — both before and
// after a successful mutation the time is the
// supplied now, so a deterministic-clock test
// can assert it.
func TestMemOriginPolicyStore_NowStampsUpdatedAt(t *testing.T) {
	fixed := time.Date(2026, 10, 19, 13, 0, 0, 0, time.UTC)
	m := NewMemOriginPolicyStore(func() time.Time { return fixed })
	state, _, _, err := m.SetPatterns(context.Background(), []string{"https://antigravity.google"})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if !state.UpdatedAt.Equal(fixed) {
		t.Errorf("UpdatedAt = %v, want %v", state.UpdatedAt, fixed)
	}
	// Move the clock.
	fixedLater := fixed.Add(time.Hour)
	m.now = func() time.Time { return fixedLater }
	state2, _, _, err := m.SetPatterns(context.Background(), []string{"https://antigravity-companion.google"})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	if !state2.UpdatedAt.Equal(fixedLater) {
		t.Errorf("UpdatedAt after time advance = %v, want %v", state2.UpdatedAt, fixedLater)
	}
}

// ---- FSOriginPolicyStore end-to-end tests -------------------

// TestFSOriginPolicyStore_LoadMissing pins the
// fresh-host behaviour: a fresh store's Snapshot
// returns the empty state (version=1, patterns=[])
// rather than an error, so the dispatcher always
// has a coherent answer.
func TestFSOriginPolicyStore_LoadMissing(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	s := NewFSOriginPolicyStore(path, nil)
	state, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != OriginPolicyStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, OriginPolicyStateVersion)
	}
	if len(state.Patterns) != 0 {
		t.Errorf("Patterns = %v, want []", state.Patterns)
	}
	if state.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt is zero, want non-zero")
	}
}

// TestFSOriginPolicyStore_RoundTrip pins the
// atomic-write path: a SetPatterns call writes
// the JSON to disk with version=1 + Patterns +
// UpdatedAt; a fresh store opened against the
// same path reads what was written.
func TestFSOriginPolicyStore_RoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	fixed := time.Date(2026, 10, 19, 13, 30, 0, 0, time.UTC)
	s1 := NewFSOriginPolicyStore(path, func() time.Time { return fixed })
	_, _, _, err := s1.SetPatterns(context.Background(), []string{
		"https://antigravity.google",
		"https://antigravity-companion.google",
		"chrome-extension://abcdefghijklmnop",
	})
	if err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	// Reload via a fresh store.
	s2 := NewFSOriginPolicyStore(path, nil)
	state, err := s2.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []string{
		"chrome-extension://abcdefghijklmnop",
		"https://antigravity-companion.google",
		"https://antigravity.google",
	}
	if len(state.Patterns) != len(want) {
		t.Fatalf("reloaded: len(Patterns) = %d, want %d; got %v", len(state.Patterns), len(want), state.Patterns)
	}
	for i := range want {
		if state.Patterns[i] != want[i] {
			t.Errorf("reloaded: Patterns[%d] = %q, want %q", i, state.Patterns[i], want[i])
		}
	}
	if !state.UpdatedAt.Equal(fixed) {
		t.Errorf("reloaded: UpdatedAt = %v, want %v", state.UpdatedAt, fixed)
	}
}

// TestFSOriginPolicyStore_AtomicWriteFilePerms
// pins that the JSON file on disk carries 0o600
// perms (matching every other
// $ANTIGRAVITY_HOME/secrets-style file).
func TestFSOriginPolicyStore_AtomicWriteFilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are Unix-only")
	}
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	s := NewFSOriginPolicyStore(path, nil)
	if _, _, _, err := s.SetPatterns(context.Background(), []string{"https://antigravity.google"}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file perms = %o, want 0600", perm)
	}
}

// TestFSOriginPolicyStore_ParseError pins the
// "malformed file bubbles up" semantic: a corrupt
// origin_policy.json surfaces a parse error from
// Snapshot rather than silently returning the
// empty state (the operator needs to know).
func TestFSOriginPolicyStore_ParseError(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	if err := os.WriteFile(path, []byte("not-json-at-all"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSOriginPolicyStore(path, nil)
	_, err := s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on corrupt JSON should error; got nil")
	}
	if !strings.Contains(err.Error(), "origin_policy.json") &&
		!strings.Contains(err.Error(), "origin_policy") {
		t.Errorf("error message should reference the file: %v", err)
	}
}

// TestFSOriginPolicyStore_VersionGreaterRejection
// pins that the store refuses to touch a file
// with a version stamp higher than the current
// supported version (the "newer binary required"
// guard the brief pins).
func TestFSOriginPolicyStore_VersionGreaterRejection(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	rec := onDiskOriginPolicyState{
		Version:   OriginPolicyStateVersion + 1,
		Patterns:  []string{"https://antigravity.google"},
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSOriginPolicyStore(path, nil)
	_, err = s.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on newer version should error; got nil")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("error message should mention newer-than-supported: %v", err)
	}
}

// TestFSOriginPolicyStore_VersionZeroCoerced pins
// the "missing version stamp ⇔ v1" coercion the
// load path applies — a file with version=0 is
// silently bumped to v1 so on-disk upgrades from
// a pre-cycle layout don't require a manual
// migration.
func TestFSOriginPolicyStore_VersionZeroCoerced(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	// Hand-crafted JSON without the version stamp.
	rec := struct {
		Patterns  []string  `json:"patterns"`
		UpdatedAt time.Time `json:"updated_at"`
	}{
		Patterns:  []string{"https://antigravity.google"},
		UpdatedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	s := NewFSOriginPolicyStore(path, nil)
	state, err := s.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != OriginPolicyStateVersion {
		t.Errorf("Version = %d, want %d (coerced)", state.Version, OriginPolicyStateVersion)
	}
}

// TestFSOriginPolicyStore_SubscribeFanOut pins
// the cross-Set fan-out: a successful SetPatterns
// wakes up every subscriber chan's reader. The
// FS store uses the same cap=16 drop-on-
// backpressure semantic as the Mem store.
func TestFSOriginPolicyStore_SubscribeFanOut(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "origin_policy.json")
	s := NewFSOriginPolicyStore(path, nil)
	ch, unsub := s.Subscribe()
	defer unsub()
	if _, _, _, err := s.SetPatterns(context.Background(), []string{"https://antigravity.google"}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	select {
	case policy := <-ch:
		if len(policy.Patterns) != 1 || policy.Patterns[0] != "https://antigravity.google" {
			t.Errorf("subscriber received %v, want [https://antigravity.google]", policy.Patterns)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("subscriber didn't receive")
	}
}

// TestFSOriginPolicyStore_BroadcastFailureNoFile
// pins that a SetPatterns whose on-disk write
// fails does NOT broadcast. The brief's
// "on-disk JSON + in-memory snapshot + broadcast"
// lock-step is preserved even on the failure
// path.
func TestFSOriginPolicyStore_BroadcastFailureNoFile(t *testing.T) {
	// Use a path that can't be written: the
	// parent directory is itself a file (the
	// MkdirAll is a no-op for "", but Rename
	// to a path whose parent is a file
	// fails). Use t.TempDir() with a bogus
	// intermediate.
	root := t.TempDir()
	blocker := filepath.Join(root, "blocker")
	if err := os.WriteFile(blocker, []byte("not a dir"), 0o600); err != nil {
		t.Fatalf("WriteFile blocker: %v", err)
	}
	// The store's Path is <blocker>/origin_policy.json;
	// ensureParentDir tries to MkdirAll(blocker),
	// which fails because blocker is a file.
	path := filepath.Join(blocker, "origin_policy.json")
	s := NewFSOriginPolicyStore(path, nil)
	ch, unsub := s.Subscribe()
	defer unsub()
	_, _, _, err := s.SetPatterns(context.Background(), []string{"https://antigravity.google"})
	if err == nil {
		t.Fatalf("SetPatterns should fail with blocker file; got nil")
	}
	select {
	case pol := <-ch:
		t.Errorf("broadcast must NOT happen on persistence failure; got %+v", pol)
	case <-time.After(50 * time.Millisecond):
	}
}

// TestFSOriginPolicyStore_EnsureParentDir pins
// that the parent's MkdirAll happens on first
// write — a Path that points to a directory
// that doesn't exist yet is created with 0o700
// perms (matching the FSProjectStore /
// FSPermissionStore / FSResourceStore / etc
// style).
func TestFSOriginPolicyStore_EnsureParentDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file permissions are Unix-only")
	}
	root := t.TempDir()
	dir := filepath.Join(root, "subdir", "deeper")
	path := filepath.Join(dir, "origin_policy.json")
	s := NewFSOriginPolicyStore(path, nil)
	if _, _, _, err := s.SetPatterns(context.Background(), []string{"https://antigravity.google"}); err != nil {
		t.Fatalf("SetPatterns: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("expected directory at %s", dir)
	}
}

// ---- mapOriginPolicyError tests ----------------------------

// TestMapOriginPolicyError_PersistenceFailed pins
// that the on-disk persistence failure
// (errOriginPolicyPersistenceFailed) maps to
// -32603 "Internal error". Today the store
// surfaces raw I/O errors rather than the
// sentinel so this helper's other-branch catch-all
// is what's hot; future cycles that want a
// distinguished "persistence unavailable" envelope
// can wire the sentinel through here.
func TestMapOriginPolicyError_PersistenceFailed(t *testing.T) {
	got := mapOriginPolicyError(errOriginPolicyPersistenceFailed, "set origin patterns")
	if got == nil {
		t.Fatalf("mapOriginPolicyError returned nil")
	}
	if got.Code != -32603 {
		t.Errorf("Code = %d, want -32603", got.Code)
	}
	if got.Message != "Internal error" {
		t.Errorf("Message = %q, want Internal error", got.Message)
	}
	dataStr, _ := got.Data.(string)
	if !strings.Contains(dataStr, "persistence failed") {
		t.Errorf("Data = %q, want substring persistence failed", dataStr)
	}
}

// TestMapOriginPolicyError_OtherError pins the
// catch-all: any non-nil error that isn't the
// persistence sentinel also maps to -32603 "Internal
// error" with the operation label + error
// message in Data.
func TestMapOriginPolicyError_OtherError(t *testing.T) {
	got := mapOriginPolicyError(errors.New("boom"), "set origin patterns")
	if got == nil {
		t.Fatalf("mapOriginPolicyError returned nil")
	}
	if got.Code != -32603 {
		t.Errorf("Code = %d, want -32603", got.Code)
	}
	if got.Message != "Internal error" {
		t.Errorf("Message = %q, want Internal error", got.Message)
	}
	dataStr, _ := got.Data.(string)
	if !strings.Contains(dataStr, "boom") {
		t.Errorf("Data = %q, want substring boom", dataStr)
	}
	if !strings.Contains(dataStr, "set origin patterns") {
		t.Errorf("Data = %q, want substring set origin patterns", dataStr)
	}
}

// TestMapOriginPolicyError_NilNoError pins that
// nil errors return nil — the happy path doesn't
// surface an error envelope.
func TestMapOriginPolicyError_NilNoError(t *testing.T) {
	if got := mapOriginPolicyError(nil, "set origin patterns"); got != nil {
		t.Errorf("mapOriginPolicyError(nil) = %+v, want nil", got)
	}
}
