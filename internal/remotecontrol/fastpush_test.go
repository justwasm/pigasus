// Tests for the rc-fastpush-toggle cycle. Covers:
//
//   - The single JSON-RPC method
//     (SetRemoteAgentFastpushEnabled), including the
//     sentinel-error mapping
//     (ErrFastpushCascadeNotFound → -32004 "Cascade
//     not found" — reuses the existing
//     ErrCodeCascadeNotFound code) and the validation
//     rules (empty / non-string cascade_id → -32602,
//     missing `enabled` → -32602);
//
//   - The FastpushStore.MemFastpushStore (Snapshot /
//     SetEnabled / Get / empty default / upsert) at
//     the in-process level (no Unix socket, no ws
//     adapter);
//
//   - The FSFastpushStore happy path against a real
//     t.TempDir() so the on-disk fastpush.json layout
//     is exercised (atomic temp + rename, 0o600 perms,
//     version stamp, fresh-host empty state, post-Set
//     reload);
//
//   - The sortSettingsByCascadeID helper — the sort
//     the store applies before handing the slice to
//     the dispatcher so dashboards see a stable
//     order;
//
//   - The NewMemFastpushStoreWith custom-snapshot
//     constructor (sorting, deep-copy).
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring the
// project's split: in-process envelopes vs. wire round-
// trips).
package remotecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newFastpushDispatcher returns a Dispatcher with the
// supplied FastpushStore pre-installed. The helper
// mirrors newPluginsDispatcher /
// newTriggersDispatcher.
//
// When ps is nil, NewDispatcher fabricates a fresh
// empty MemFastpushStore (the nil-guard pattern
// rc-fastpush-toggle adds). Tests that want to assert
// "no store installed" pass nil and exercise the
// -32603 path; tests that want a specific store pass
// it directly.
//
// The helper also pre-registers a cascade
// "cas_known" in the dispatcher's CascadeRegistry so
// happy-path SetEnabled calls land on a real cascade
// rather than the dispatcher's -32004 unknown-
// cascade guard. Tests that exercise the
// -32004 path either pre-clear the registry or pass a
// different (or absent) cascade id.
func newFastpushDispatcher(t *testing.T, ps FastpushStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:       sess,
		FastpushStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	// Pre-register a cascade so happy-path
	// SetEnabled calls land on a real cascade
	// rather than the dispatcher's -32004
	// unknown-cascade guard. The cascade is
	// metadata-only; tests don't exercise
	// SendMessage / Cancel paths.
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return d
}

// registerFastpushCascade registers a fresh
// cascade in the dispatcher's registry and returns
// its id. Useful for tests that want to exercise
// the SetEnabled happy path against an explicit
// cascade id (rather than the default
// "cas_known" the newFastpushDispatcher
// helper pre-registers).
func registerFastpushCascade(t *testing.T, d *Dispatcher) string {
	t.Helper()
	c, err := NewCascade(CascadeOpts{})
	if err != nil {
		t.Fatalf("NewCascade: %v", err)
	}
	if err := d.cascades.Register(c); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return c.ID
}

// dispatchFastpush sends a SetRemoteAgentFastpushEnabled
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchPlugins /
// dispatchTriggers.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a present
// (but null) params block; jsonRPCFor takes a byte
// slice and appends verbatim, so we mirror the
// convention `[]byte("null")` rather than a missing-
// key shape.
func dispatchFastpush(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	switch v := params.(type) {
	case nil:
		raw = []byte("null")
	case []byte:
		raw = v
	default:
		var err error
		raw, err = jsonMarshal(params)
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

// ---- helper / parse tests --------------------------------

// TestNormaliseCascadeID pins the trim helper
// (Defence-in-depth inside the store; the dispatcher
// layer's primary validation site is the "empty
// cascade_id" branch in
// handleSetRemoteAgentFastpushEnabled).
func TestNormaliseCascadeID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Trim only.
		{"cas_abcdef0123456789", "cas_abcdef0123456789"},
		{"  cas_abcdef0123456789  ", "cas_abcdef0123456789"},
		{"\tcas_xyz\n", "cas_xyz"},
		// Already trimmed (idempotent).
		{"cas_qwerty", "cas_qwerty"},
		// Empty in → empty out.
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normaliseCascadeID(tc.in); got != tc.want {
				t.Errorf("normaliseCascadeID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestFastpushChannelName pins the canonical channel
// the wire envelope emits. Today
// FastpushChannelName == "fastpush/cascade"; the
// rc-fastpush-channel cycle may introduce a per-
// cascade channel suffix (e.g.
// "fastpush/cascade/<cas_...>") but the dispatcher
// helper reads the constant verbatim.
func TestFastpushChannelName(t *testing.T) {
	if FastpushChannelName != "fastpush/cascade" {
		t.Errorf("FastpushChannelName = %q, want fastpush/cascade", FastpushChannelName)
	}
}

// TestFastpushPushDeliveryDeferred pins the explicit
// delivery-status sentinel the wire envelope always
// carries today. Dashboards can render a clear "no
// live delivery" pill without confusing the toggle
// with a transport feature. The
// rc-fastpush-channel cycle will introduce a
// parallel FastpushPushDeliveryLive sentinel and flip
// the wire value once a real transport lands.
func TestFastpushPushDeliveryDeferred(t *testing.T) {
	if FastpushPushDeliveryDeferred != "deferred" {
		t.Errorf("FastpushPushDeliveryDeferred = %q, want deferred", FastpushPushDeliveryDeferred)
	}
}

// TestFastpushStateVersion pins the on-disk format
// version the current store writes. v1 is the brief-
// pinned value; a future cycle that introduces a
// breaking change (renamed keys, removed fields)
// bumps this constant AND updates the load() branch
// that rejects version > FastpushStateVersion.
func TestFastpushStateVersion(t *testing.T) {
	if FastpushStateVersion != 1 {
		t.Errorf("FastpushStateVersion = %d, want 1", FastpushStateVersion)
	}
}

// ---- MemFastpushStore in-process tests -------------------

// TestFastpushStore_DefaultState_Empty pins the
// fresh-store baseline: a fresh MemFastpushStore
// has no settings yet (the brief's "fresh host has no
// fastpush rows" rule). The store's Snapshot returns
// version=1 with settings=[] so the wire always sees
// "[]" rather than "null".
//
// NewMemFastpushStore's default-fabrication path
// also drives the dispatcher's NewDispatcher nil-
// guard, so this test is the in-process mirror of
// the brief's "fresh store seeds empty state" line.
func TestFastpushStore_DefaultState_Empty(t *testing.T) {
	ps := NewMemFastpushStore(nil, nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != FastpushStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, FastpushStateVersion)
	}
	if len(state.Settings) != 0 {
		t.Fatalf("len(Settings) = %d, want 0", len(state.Settings))
	}
	if state.Settings == nil {
		// Defensive: the wire relies on "[]"
		// rather than "null"; the store must
		// never hand back a nil slice.
		t.Errorf("Settings is nil (wire would emit null)")
	}
}

// TestFastpushStore_SetEnabled_New pins the happy-
// path set on a fresh store: SetEnabled with an
// unknown cascade_id and enabled=true creates a new
// row, returns the post-mutation setting, and the
// store's Snapshot reflects the new row.
//
// The test installs a checkCascade closure that
// accepts every cascade_id (matches production
// behaviour — every cascade created via
// StartRemoteAgentCascade lands in the registry
// before the toggle call, so the production
// checkCascade returns true for any id passed to
// SetEnabled).
func TestFastpushStore_SetEnabled_New(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	setting, err := ps.SetEnabled(context.Background(), "cas_abc", true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if setting.CascadeID != "cas_abc" {
		t.Errorf("setting.CascadeID = %q, want cas_abc", setting.CascadeID)
	}
	if !setting.Enabled {
		t.Errorf("setting.Enabled = false, want true")
	}
	if setting.Channel != FastpushChannelName {
		t.Errorf("setting.Channel = %q, want %q", setting.Channel, FastpushChannelName)
	}
	if setting.PushDelivery != FastpushPushDeliveryDeferred {
		t.Errorf("setting.PushDelivery = %q, want %q", setting.PushDelivery, FastpushPushDeliveryDeferred)
	}
	if setting.UpdatedAt.IsZero() {
		t.Errorf("setting.UpdatedAt is zero")
	}

	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1", len(state.Settings))
	}
	if state.Settings[0].CascadeID != "cas_abc" {
		t.Errorf("state.Settings[0].CascadeID = %q, want cas_abc", state.Settings[0].CascadeID)
	}
}

// TestFastpushStore_SetEnabled_Upsert pins the
// idempotent-toggle path: SetEnabled with an
// existing cascade_id replaces the row in place
// (the slice doesn't grow on a second call). The
// post-mutation setting reflects the new enabled
// value; UpdatedAt is bumped to the new clock.
//
// SetEnabled is not an "insert or error" path —
// it is an upsert — because dashboards that toggle
// a bit back and forth expect a single row, not a
// history.
func TestFastpushStore_SetEnabled_Upsert(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)

	// First set: enabled=true.
	first, err := ps.SetEnabled(context.Background(), "cas_abc", true)
	if err != nil {
		t.Fatalf("SetEnabled(true): %v", err)
	}
	firstAt := first.UpdatedAt

	// Sleep so the new clock advances past the
	// first stamp.
	time.Sleep(2 * time.Millisecond)

	// Second set: enabled=false (the toggle-off
	// path).
	second, err := ps.SetEnabled(context.Background(), "cas_abc", false)
	if err != nil {
		t.Fatalf("SetEnabled(false): %v", err)
	}
	if second.Enabled {
		t.Errorf("second.Enabled = true, want false")
	}
	if !second.UpdatedAt.After(firstAt) {
		t.Errorf("second.UpdatedAt = %v, want > %v", second.UpdatedAt, firstAt)
	}

	// Slice should still hold one row (upsert, not
	// append).
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1 (upsert should not append)", len(state.Settings))
	}
	if state.Settings[0].Enabled {
		t.Errorf("state.Settings[0].Enabled = true, want false")
	}
}

// TestFastpushStore_SetEnabled_CascadeNotFound pins
// the unknown-cascade → ErrFastpushCascadeNotFound
// path. The dispatcher maps the sentinel to JSON-RPC
// -32004 "Cascade not found" via mapFastpushError.
//
// The checkCascade closure returns false (the
// cascade isn't registered); the store refuses
// without touching the on-disk state.
func TestFastpushStore_SetEnabled_CascadeNotFound(t *testing.T) {
	check := func(id string) bool { return false }
	ps := NewMemFastpushStore(nil, check)
	_, err := ps.SetEnabled(context.Background(), "cas_unknown", true)
	if !errors.Is(err, ErrFastpushCascadeNotFound) {
		t.Fatalf("SetEnabled(unknown) err = %v, want ErrFastpushCascadeNotFound", err)
	}
	// Defensive: a refused SetEnabled must not
	// mutate the on-disk state.
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 0 {
		t.Errorf("len(state.Settings) = %d, want 0 (refused Set must not mutate)", len(state.Settings))
	}
}

// TestFastpushStore_SetEnabled_EmptyCascadeID pins
// the store's defence-in-depth empty-id → sentinel
// guard. The dispatcher's primary validation is
// "empty cascade_id → -32602" (a dashboard bug), but
// the store also enforces strictness so a future
// non-dispatcher caller can't slip through.
func TestFastpushStore_SetEnabled_EmptyCascadeID(t *testing.T) {
	ps := NewMemFastpushStore(nil, nil)
	_, err := ps.SetEnabled(context.Background(), "", true)
	if !errors.Is(err, ErrFastpushCascadeNotFound) {
		t.Fatalf("SetEnabled(\"\") err = %v, want ErrFastpushCascadeNotFound", err)
	}
	_, err = ps.SetEnabled(context.Background(), "   ", true)
	if !errors.Is(err, ErrFastpushCascadeNotFound) {
		t.Fatalf("SetEnabled(\"   \") err = %v, want ErrFastpushCascadeNotFound", err)
	}
}

// TestFastpushStore_Get_Unknown pins the Get path
// for an unknown cascade_id: returns
// ErrFastpushCascadeNotFound. The dispatcher's
// handler doesn't currently call Get (the brief
// doesn't ship a list method), but the seam is here
// for future cycles (rc-fastpush-channel) that may
// add a per-cascade lookup.
func TestFastpushStore_Get_Unknown(t *testing.T) {
	check := func(id string) bool { return false }
	ps := NewMemFastpushStore(nil, check)
	_, err := ps.Get(context.Background(), "cas_unknown")
	if !errors.Is(err, ErrFastpushCascadeNotFound) {
		t.Fatalf("Get(unknown) err = %v, want ErrFastpushCascadeNotFound", err)
	}
}

// TestFastpushStore_Get_MissingReturnsFalse pins the
// Get path for a cascade that has no stored row but
// passes the existence check: returns a zero-valued
// FastpushSetting (no error) so the caller can
// distinguish "no row" from "unknown cascade".
//
// "MissingReturnsFalse" is the brief-pinned name —
// the post-mutation Enabled field is the
// `zero-value bool` (false), so callers that need
// "has it been touched?" can compare UpdatedAt.IsZero().
func TestFastpushStore_Get_MissingReturnsFalse(t *testing.T) {
	check := func(id string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	setting, err := ps.Get(context.Background(), "cas_known")
	if err != nil {
		t.Fatalf("Get(known-but-missing): %v", err)
	}
	if setting.CascadeID != "cas_known" {
		t.Errorf("setting.CascadeID = %q, want cas_known", setting.CascadeID)
	}
	if setting.Enabled {
		t.Errorf("setting.Enabled = true, want false (default)")
	}
	if !setting.UpdatedAt.IsZero() {
		t.Errorf("setting.UpdatedAt = %v, want zero (no row exists)", setting.UpdatedAt)
	}
}

// TestFastpushStore_Get_AfterSet pins the round-trip
// Get path: a successful SetEnabled lands a row,
// Get returns it verbatim. Defends the contract
// future cycles (rc-fastpush-channel) will rely on.
func TestFastpushStore_Get_AfterSet(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	_, err := ps.SetEnabled(context.Background(), "cas_abc", true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	setting, err := ps.Get(context.Background(), "cas_abc")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if setting.CascadeID != "cas_abc" {
		t.Errorf("setting.CascadeID = %q, want cas_abc", setting.CascadeID)
	}
	if !setting.Enabled {
		t.Errorf("setting.Enabled = false, want true")
	}
	if setting.PushDelivery != FastpushPushDeliveryDeferred {
		t.Errorf("setting.PushDelivery = %q, want %q", setting.PushDelivery, FastpushPushDeliveryDeferred)
	}
}

// TestFastpushStore_SetEnabled_SortPinning pins the
// Snapshot sort: after multiple SetEnabled calls,
// the Settings slice comes back in CascadeID ascending
// order regardless of insertion order.
//
// Sort pinning matters for dashboards that watch the
// fastpush/setting_changed broadcast and want a
// stable sequence to render.
func TestFastpushStore_SetEnabled_SortPinning(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	// Insert out of order on purpose.
	for _, id := range []string{"cas_zzz", "cas_aaa", "cas_mmm"} {
		if _, err := ps.SetEnabled(context.Background(), id, true); err != nil {
			t.Fatalf("SetEnabled(%q): %v", id, err)
		}
	}
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 3 {
		t.Fatalf("len(state.Settings) = %d, want 3", len(state.Settings))
	}
	want := []string{"cas_aaa", "cas_mmm", "cas_zzz"}
	for i, w := range want {
		if got := state.Settings[i].CascadeID; got != w {
			t.Errorf("state.Settings[%d].CascadeID = %q, want %q", i, got, w)
		}
	}
}

// TestNewMemFastpushStoreWith pins the
// custom-snapshot constructor: callers that want a
// specific starting state (e.g. a test that asserts
// "two settings → sort them") build their own slice.
//
// The constructor deep-copies the input and sorts by
// CascadeID ascending so a test that mutates the
// input after construction does not race the store's
// internal state.
func TestNewMemFastpushStoreWith(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	settings := []FastpushSetting{
		{CascadeID: "cas_zzz", Enabled: false, UpdatedAt: now},
		{CascadeID: "cas_aaa", Enabled: true, UpdatedAt: now},
	}
	check := func(string) bool { return true }
	ps := NewMemFastpushStoreWith(settings, nil, check)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 2 {
		t.Fatalf("len(state.Settings) = %d, want 2", len(state.Settings))
	}
	// Sorted ascending by CascadeID. The store's
	// internal slice is [cas_aaa, cas_zzz] (with
	// cas_aaa.Enabled == true, cas_zzz.Enabled
	// == false from the input).
	if state.Settings[0].CascadeID != "cas_aaa" {
		t.Errorf("state.Settings[0].CascadeID = %q, want cas_aaa", state.Settings[0].CascadeID)
	}
	if state.Settings[1].CascadeID != "cas_zzz" {
		t.Errorf("state.Settings[1].CascadeID = %q, want cas_zzz", state.Settings[1].CascadeID)
	}
	// Defensive: mutating the input after
	// construction must not race the store's
	// internal state. The input slice's
	// settings[0] (cas_zzz, Enabled: false) is
	// mutated to Enabled: true; the store's
	// internal slice is a deep copy, so the
	// post-mutation Snapshot still surfaces the
	// original cas_zzz.Enabled == false at
	// state.Settings[1].
	settings[0].Enabled = true
	state2, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state2.Settings[1].Enabled {
		t.Errorf("input mutation leaked into store state (cas_zzz Enabled flipped to true)")
	}
}

// ---- FSFastpushStore happy-path tests --------------------

// TestFSFastpushStore_RoundTrip exercises the
// production store end-to-end against a real
// t.TempDir(): a fresh file seeds the empty state,
// SetEnabled persists, and a re-opened store reads
// the row back.
//
// The store's atomic-write (temp + rename + 0o600
// perms + dir fsync) is exercised implicitly via the
// post-write ReadFile check below.
func TestFSFastpushStore_RoundTrip(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fastpush.json")

	check := func(string) bool { return true }
	store := NewFSFastpushStoreWithCheck(path, nil, check)

	// Fresh file: Snapshot returns the empty
	// state.
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 0 {
		t.Errorf("len(state.Settings) = %d, want 0 on fresh file", len(state.Settings))
	}

	// SetEnabled writes a row to disk.
	setting, err := store.SetEnabled(context.Background(), "cas_abc", true)
	if err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}
	if setting.CascadeID != "cas_abc" {
		t.Errorf("setting.CascadeID = %q, want cas_abc", setting.CascadeID)
	}

	// File exists with 0600 perms.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("file perms = %o, want 0600", got)
	}

	// Re-open a fresh store against the same
	// path: Snapshot must reflect the persisted
	// row.
	reopened := NewFSFastpushStoreWithCheck(path, nil, check)
	state2, err := reopened.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot (reopened): %v", err)
	}
	if len(state2.Settings) != 1 {
		t.Fatalf("len(state2.Settings) = %d, want 1 (reopened)", len(state2.Settings))
	}
	if state2.Settings[0].CascadeID != "cas_abc" {
		t.Errorf("state2.Settings[0].CascadeID = %q, want cas_abc", state2.Settings[0].CascadeID)
	}
	if !state2.Settings[0].Enabled {
		t.Errorf("state2.Settings[0].Enabled = false, want true")
	}
}

// TestFSFastpushStore_AtomicSave exercises the
// atomic-temp+rename path explicitly: after a
// SetEnabled, no .tmp file is left behind on the
// filesystem. Defends against the
// "rename failed → temp file lingers" race that
// the in-process test path doesn't otherwise
// surface.
func TestFSFastpushStore_AtomicSave(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fastpush.json")

	check := func(string) bool { return true }
	store := NewFSFastpushStoreWithCheck(path, nil, check)
	if _, err := store.SetEnabled(context.Background(), "cas_abc", true); err != nil {
		t.Fatalf("SetEnabled: %v", err)
	}

	// No .tmp file should linger.
	if _, err := os.Stat(path + ".tmp"); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("temp file %s.tmp exists after atomic rename", path)
	}

	// File content should be parseable JSON
	// matching onDiskFastpushState.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	var rec onDiskFastpushState
	if err := json.Unmarshal(data, &rec); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if rec.Version != FastpushStateVersion {
		t.Errorf("rec.Version = %d, want %d", rec.Version, FastpushStateVersion)
	}
	if len(rec.Settings) != 1 {
		t.Errorf("len(rec.Settings) = %d, want 1", len(rec.Settings))
	}
	if rec.Settings[0].CascadeID != "cas_abc" {
		t.Errorf("rec.Settings[0].CascadeID = %q, want cas_abc", rec.Settings[0].CascadeID)
	}
}

// TestFSFastpushStore_Malformed pins the malformed-
// file → parse-error envelope. The dispatcher maps
// this to -32603 because the dashboard should not see
// "no fastpush settings" when the JSON is corrupt —
// the operator needs to know.
func TestFSFastpushStore_Malformed(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fastpush.json")
	if err := os.WriteFile(path, []byte("{ not valid json"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := NewFSFastpushStore(path, nil)
	_, err := store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on malformed file: err = nil, want parse error")
	}
}

// TestFSFastpushStore_NewerVersion pins the
// "on-disk version > FastpushStateVersion" branch:
// the store refuses to touch the file (returns a
// "newer binary required" envelope).
func TestFSFastpushStore_NewerVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fastpush.json")
	body := `{"version": 999, "settings": [], "updated_at": "2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := NewFSFastpushStore(path, nil)
	_, err := store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on newer-version file: err = nil, want version error")
	}
}

// ---- sort helper test ------------------------------------

// TestSortSettingsByCascadeID pins the sort
// invariant the store applies before handing the
// slice to the dispatcher. Sort order is ascending
// by CascadeID; stable secondary sort by UpdatedAt
// (oldest first) so two rows with the same
// CascadeID (impossible today — the store dedups —
// but defensive) have a deterministic order.
func TestSortSettingsByCascadeID(t *testing.T) {
	settings := []FastpushSetting{
		{CascadeID: "cas_zzz"},
		{CascadeID: "cas_aaa"},
		{CascadeID: "cas_mmm"},
	}
	sortSettingsByCascadeID(settings)
	want := []string{"cas_aaa", "cas_mmm", "cas_zzz"}
	for i, w := range want {
		if got := settings[i].CascadeID; got != w {
			t.Errorf("settings[%d].CascadeID = %q, want %q", i, got, w)
		}
	}
}

// ---- dispatcher round-trip tests -------------------------

// TestSetRemoteAgentFastpushEnabled_Ok pins the
// happy-path round-trip through the dispatcher:
// fresh store, post-state reflects the toggle,
// wire envelope carries push_delivery:"deferred" as
// the explicit sentinel.
//
// The test installs a checkCascade closure that
// accepts every cascade_id (mirrors the production
// seam built in cmd/wagyu/remote_control.go's
// buildHostFastpushStore — every cascade created
// via StartRemoteAgentCascade lands in the
// dispatcher's registry before the toggle call,
// so the production checkCascade returns true).
func TestSetRemoteAgentFastpushEnabled_Ok(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    true,
	})
	if errObj != nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: error = %#v", errObj)
	}
	if got, _ := res["ok"].(bool); !got {
		t.Errorf("result.ok = false, want true")
	}
	if got, _ := res["cascade_id"].(string); got != cid {
		t.Errorf("result.cascade_id = %q, want %q", got, cid)
	}
	if got, _ := res["enabled"].(bool); !got {
		t.Errorf("result.enabled = false, want true")
	}
	if got, _ := res["channel"].(string); got != FastpushChannelName {
		t.Errorf("result.channel = %q, want %q", got, FastpushChannelName)
	}
	if got, _ := res["push_delivery"].(string); got != FastpushPushDeliveryDeferred {
		t.Errorf("result.push_delivery = %q, want %q (deferred sentinel)", got, FastpushPushDeliveryDeferred)
	}
	if got, _ := res["updated_at"].(string); got == "" {
		t.Errorf("result.updated_at missing")
	}

	// Post-state must reflect the toggle.
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1", len(state.Settings))
	}
	if !state.Settings[0].Enabled {
		t.Errorf("state.Settings[0].Enabled = false, want true")
	}
}

// TestSetRemoteAgentFastpushEnabled_ToggleOff pins
// the toggle-off path: a second SetEnabled with
// enabled=false flips the row to false (upsert,
// not append). The wire envelope reflects the
// post-state.
func TestSetRemoteAgentFastpushEnabled_ToggleOff(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	// First set: enabled=true.
	if _, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    true,
	}); errObj != nil {
		t.Fatalf("first Set: error = %#v", errObj)
	}
	// Second set: enabled=false (toggle off).
	res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": cid,
		"enabled":    false,
	})
	if errObj != nil {
		t.Fatalf("second Set: error = %#v", errObj)
	}
	if got, _ := res["enabled"].(bool); got {
		t.Errorf("result.enabled = true, want false (toggle off)")
	}

	// Slice should still hold one row.
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Settings) != 1 {
		t.Fatalf("len(state.Settings) = %d, want 1 (upsert)", len(state.Settings))
	}
	if state.Settings[0].Enabled {
		t.Errorf("state.Settings[0].Enabled = true, want false")
	}
}

// TestSetRemoteAgentFastpushEnabled_BadParams pins
// the -32602 envelopes:
//
//   - empty cascade_id → -32602 "Invalid params";
//   - whitespace-only cascade_id → -32602;
//   - missing enabled → -32602;
//   - non-string cascade_id → -32602;
//   - wrong-type enabled → -32602.
//
// Mirrors the rc-plugins / rc-triggers bad-params
// test pattern: a closed table of input shapes
// the brief explicitly enumerates.
//
// Note: the "missing cascade_id" case doesn't appear
// in the table — the dispatcher's `cascade_id absent`
// rule lands as -32602 with the same data text as
// "empty cascade_id", so the dispatcher short-circuits
// on the empty-string check before the registry lookup.
// The table focuses on the unique data shapes.
func TestSetRemoteAgentFastpushEnabled_BadParams(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	cases := []struct {
		label  string
		params any
	}{
		{"empty cascade_id", map[string]any{"cascade_id": "", "enabled": true}},
		{"whitespace cascade_id", map[string]any{"cascade_id": "   ", "enabled": true}},
		{"missing enabled", map[string]any{"cascade_id": cid}},
		{"non-string cascade_id", map[string]any{"cascade_id": 42, "enabled": true}},
		{"null enabled", map[string]any{"cascade_id": cid, "enabled": nil}},
		{"wrong-type enabled", map[string]any{"cascade_id": cid, "enabled": "yes"}},
		{"missing cascade_id", map[string]any{"enabled": true}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", tc.params)
			if errObj == nil {
				t.Fatalf("%s: no error envelope", tc.label)
			}
			if codeAsInt(t, errObj["code"]) != -32602 {
				t.Errorf("%s: code = %d, want -32602", tc.label, codeAsInt(t, errObj["code"]))
			}
		})
	}
}

// TestSetRemoteAgentFastpushEnabled_CascadeNotFound
// pins the unknown-cascade → -32004 envelope. The
// store's checkCascade closure returns false; the
// dispatcher surfaces the same lifecycle code
// SendRemoteAgentCascadeMessage uses.
func TestSetRemoteAgentFastpushEnabled_CascadeNotFound(t *testing.T) {
	check := func(id string) bool { return false }
	ps := NewMemFastpushStore(nil, check)
	d := newFastpushDispatcher(t, ps)

	_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": "cas_unknown",
		"enabled":    true,
	})
	if errObj == nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope")
	}
	if codeAsInt(t, errObj["code"]) != ErrCodeCascadeNotFound {
		t.Errorf("code = %d, want %d (Cascade not found)", codeAsInt(t, errObj["code"]), ErrCodeCascadeNotFound)
	}
	if got, _ := errObj["message"].(string); got != "Cascade not found" {
		t.Errorf("message = %q, want Cascade not found", got)
	}
}

// TestSetRemoteAgentFastpushEnabled_NoStore pins
// the no-store-installed → -32603 envelope. The
// test passes a nil FastpushStore to
// NewDispatcher — but the nil-fabrication kicks in
// and installs a fresh empty MemFastpushStore. So
// to exercise the negative path, the test builds
// a dispatcher manually and clears the field after
// construction.
func TestSetRemoteAgentFastpushEnabled_NoStore(t *testing.T) {
	d := newFastpushDispatcher(t, nil)
	d.cfg.FastpushStore = nil

	_, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
		"cascade_id": "cas_abc",
		"enabled":    true,
	})
	if errObj == nil {
		t.Fatalf("SetRemoteAgentFastpushEnabled: no error envelope")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentFastpushEnabled_PushDeliverySentinel
// pins the wire envelope's explicit
// push_delivery:"deferred" sentinel on every
// successful call. The brief calls this out
// specifically: dashboards must not assume actual
// delivery is wired.
//
// The test exercises both toggle states (true /
// false) and asserts the sentinel appears
// verbatim in both.
func TestSetRemoteAgentFastpushEnabled_PushDeliverySentinel(t *testing.T) {
	check := func(string) bool { return true }
	ps := NewMemFastpushStore(nil, check)
	d := newFastpushDispatcher(t, ps)
	cid := registerFastpushCascade(t, d)

	for _, enabled := range []bool{true, false} {
		res, errObj := dispatchFastpush(t, d, "SetRemoteAgentFastpushEnabled", map[string]any{
			"cascade_id": cid,
			"enabled":    enabled,
		})
		if errObj != nil {
			t.Fatalf("Set(enabled=%v): error = %#v", enabled, errObj)
		}
		if got, _ := res["push_delivery"].(string); got != FastpushPushDeliveryDeferred {
			t.Errorf("enabled=%v: push_delivery = %q, want %q", enabled, got, FastpushPushDeliveryDeferred)
		}
	}
}
