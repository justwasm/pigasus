// Package fastpush — store_test.go: the fastpush store's own
// tests.
//
// The store is the domain's fact: the fresh-store baseline, the
// upsert, the cascade lookup, the on-disk round trip and the
// sort the store applies before handing the slice to the
// dispatcher.
//
// The dispatcher's round-trip against this store lives in
// internal/remotecontrol/fastpush_dispatcher_test.go, because
// the dispatcher is the host's.
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring the
// project's split: in-process envelopes vs. wire round-
// trips).
package fastpush

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
// ChannelName == "fastpush/cascade"; the
// rc-fastpush-channel cycle may introduce a per-
// cascade channel suffix (e.g.
// "fastpush/cascade/<cas_...>") but the dispatcher
// helper reads the constant verbatim.
func TestFastpushChannelName(t *testing.T) {
	if ChannelName != "fastpush/cascade" {
		t.Errorf("ChannelName = %q, want fastpush/cascade", ChannelName)
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
	if PushDeliveryDeferred != "deferred" {
		t.Errorf("PushDeliveryDeferred = %q, want deferred", PushDeliveryDeferred)
	}
}

// TestFastpushStateVersion pins the on-disk format
// version the current store writes. v1 is the brief-
// pinned value; a future cycle that introduces a
// breaking change (renamed keys, removed fields)
// bumps this constant AND updates the load() branch
// that rejects version > StateVersion.
func TestFastpushStateVersion(t *testing.T) {
	if StateVersion != 1 {
		t.Errorf("StateVersion = %d, want 1", StateVersion)
	}
}

// ---- MemStore in-process tests -------------------

// TestFastpushStore_DefaultState_Empty pins the
// fresh-store baseline: a fresh MemStore
// has no settings yet (the brief's "fresh host has no
// fastpush rows" rule). The store's Snapshot returns
// version=1 with settings=[] so the wire always sees
// "[]" rather than "null".
//
// NewMemStore's default-fabrication path
// also drives the dispatcher's NewDispatcher nil-
// guard, so this test is the in-process mirror of
// the brief's "fresh store seeds empty state" line.
func TestFastpushStore_DefaultState_Empty(t *testing.T) {
	ps := NewMemStore(nil, nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
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
	ps := NewMemStore(nil, check)
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
	if setting.Channel != ChannelName {
		t.Errorf("setting.Channel = %q, want %q", setting.Channel, ChannelName)
	}
	if setting.PushDelivery != PushDeliveryDeferred {
		t.Errorf("setting.PushDelivery = %q, want %q", setting.PushDelivery, PushDeliveryDeferred)
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
	ps := NewMemStore(nil, check)

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
// the unknown-cascade → ErrCascadeNotFound
// path. The dispatcher maps the sentinel to JSON-RPC
// -32004 "Cascade not found" via mapFastpushError.
//
// The checkCascade closure returns false (the
// cascade isn't registered); the store refuses
// without touching the on-disk state.
func TestFastpushStore_SetEnabled_CascadeNotFound(t *testing.T) {
	check := func(id string) bool { return false }
	ps := NewMemStore(nil, check)
	_, err := ps.SetEnabled(context.Background(), "cas_unknown", true)
	if !errors.Is(err, ErrCascadeNotFound) {
		t.Fatalf("SetEnabled(unknown) err = %v, want ErrCascadeNotFound", err)
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
	ps := NewMemStore(nil, nil)
	_, err := ps.SetEnabled(context.Background(), "", true)
	if !errors.Is(err, ErrCascadeNotFound) {
		t.Fatalf("SetEnabled(\"\") err = %v, want ErrCascadeNotFound", err)
	}
	_, err = ps.SetEnabled(context.Background(), "   ", true)
	if !errors.Is(err, ErrCascadeNotFound) {
		t.Fatalf("SetEnabled(\"   \") err = %v, want ErrCascadeNotFound", err)
	}
}

// TestFastpushStore_Get_Unknown pins the Get path
// for an unknown cascade_id: returns
// ErrCascadeNotFound. The dispatcher's
// handler doesn't currently call Get (the brief
// doesn't ship a list method), but the seam is here
// for future cycles (rc-fastpush-channel) that may
// add a per-cascade lookup.
func TestFastpushStore_Get_Unknown(t *testing.T) {
	check := func(id string) bool { return false }
	ps := NewMemStore(nil, check)
	_, err := ps.Get(context.Background(), "cas_unknown")
	if !errors.Is(err, ErrCascadeNotFound) {
		t.Fatalf("Get(unknown) err = %v, want ErrCascadeNotFound", err)
	}
}

// TestFastpushStore_Get_MissingReturnsFalse pins the
// Get path for a cascade that has no stored row but
// passes the existence check: returns a zero-valued
// Setting (no error) so the caller can
// distinguish "no row" from "unknown cascade".
//
// "MissingReturnsFalse" is the brief-pinned name —
// the post-mutation Enabled field is the
// `zero-value bool` (false), so callers that need
// "has it been touched?" can compare UpdatedAt.IsZero().
func TestFastpushStore_Get_MissingReturnsFalse(t *testing.T) {
	check := func(id string) bool { return true }
	ps := NewMemStore(nil, check)
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
	ps := NewMemStore(nil, check)
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
	if setting.PushDelivery != PushDeliveryDeferred {
		t.Errorf("setting.PushDelivery = %q, want %q", setting.PushDelivery, PushDeliveryDeferred)
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
	ps := NewMemStore(nil, check)
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
	settings := []Setting{
		{CascadeID: "cas_zzz", Enabled: false, UpdatedAt: now},
		{CascadeID: "cas_aaa", Enabled: true, UpdatedAt: now},
	}
	check := func(string) bool { return true }
	ps := NewMemStoreWith(settings, nil, check)
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

// ---- FSStore happy-path tests --------------------

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
	store := NewFSStoreWithCheck(path, nil, check)

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
	reopened := NewFSStoreWithCheck(path, nil, check)
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
	store := NewFSStoreWithCheck(path, nil, check)
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
	if rec.Version != StateVersion {
		t.Errorf("rec.Version = %d, want %d", rec.Version, StateVersion)
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
	store := NewFSStore(path, nil)
	_, err := store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on malformed file: err = nil, want parse error")
	}
}

// TestFSFastpushStore_NewerVersion pins the
// "on-disk version > StateVersion" branch:
// the store refuses to touch the file (returns a
// "newer binary required" envelope).
func TestFSFastpushStore_NewerVersion(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "fastpush.json")
	body := `{"version": 999, "settings": [], "updated_at": "2026-01-02T03:04:05Z"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	store := NewFSStore(path, nil)
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
	settings := []Setting{
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
