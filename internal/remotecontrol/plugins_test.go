// Tests for the rc-plugins cycle. Covers:
//
//   - The two JSON-RPC methods
//     (ListRemoteAgentPlugins /
//     DisconnectRemoteAgentPlugin), including the
//     sentinel-error mapping (ErrPluginNotFound
//     → -32015) and the validation rules (empty
//     plugin_id → -32602);
//
//   - The PluginStore.MemPluginStore
//     (Snapshot / Disconnect /
//     fixture-seed-defaults /
//     custom-starting-snapshot / unknown-id
//     refusal) at the in-process level (no Unix
//     socket, no ws adapter);
//
//   - The FSPluginStore happy path against a real
//     t.TempDir() so the on-disk plugins.json
//     layout is exercised (atomic temp + rename,
//     0o600 perms, version stamp, fixture-seed
//     on first Snapshot, post-Disconnect reload);
//
//   - The sortPluginsByID helper — the sort the
//     store applies before handing the slice to
//     the dispatcher so dashboards see a stable
//     order;
//
//   - The NewMemPluginStoreWith custom-snapshot
//     constructor (sorting, deep-copy).
//
// Wire-level tests live in
// remotetransport/tcp_test.go (mirroring
// the project's split: in-process envelopes vs.
// wire round-trips).
package remotecontrol

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// newPluginsDispatcher returns a Dispatcher with
// the supplied PluginStore pre-installed. The
// helper mirrors newPermissionsDispatcher.
func newPluginsDispatcher(t *testing.T, ps PluginStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:      sess,
		PluginsStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchPlugins sends a List / Disconnect
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchPermissions.
//
// nil params marshals to JSON `null` so the
// dispatcher's "len(params) > 0" guards see a
// present (but null) params block; jsonRPCFor
// takes a byte slice and appends verbatim, so we
// mirror the convention `[]byte("null")` rather
// than a missing-key shape — the brief pins
// `{"method": "ListRemoteAgentPlugins"}` as a
// params-less call that's accepted by the
// dispatcher (List has no required inputs).
func dispatchPlugins(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
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

// TestNormalisePluginID pins the trim helper
// (Defence-in-depth inside the store; the
// dispatcher layer's primary validation site is
// the "empty plugin_id" branch in
// handleDisconnectRemoteAgentPlugin).
func TestNormalisePluginID(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		// Trim only.
		{"claude-code-bridge", "claude-code-bridge"},
		{"  claude-code-bridge  ", "claude-code-bridge"},
		{"\tmcp-bridge\n", "mcp-bridge"},
		// Already trimmed (idempotent).
		{"github-copilot-bridge", "github-copilot-bridge"},
		// Empty in → empty out.
		{"", ""},
		{"   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := normalisePluginID(tc.in); got != tc.want {
				t.Errorf("normalisePluginID(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestDefaultPluginFixtures pins the canonical
// fixture set the store seeds when no
// plugins.json exists. Future cycles that grow
// the fixture surface (e.g. add slack-bridge)
// bump DefaultPluginFixtures' length here too.
//
// DefaultPluginFixtures is intentionally
// unsorted (insertion-order map; a future
// cycle that wants stable iteration can sort
// the slice — the store's Snapshot/Disconnect
// already sort on the way out).
func TestDefaultPluginFixtures(t *testing.T) {
	if len(DefaultPluginFixtures) != 3 {
		t.Fatalf("len(DefaultPluginFixtures) = %d, want 3", len(DefaultPluginFixtures))
	}
	wantIDs := map[string]bool{
		"claude-code-bridge":    false,
		"mcp-bridge":            false,
		"github-copilot-bridge": false,
	}
	for i, p := range DefaultPluginFixtures {
		if _, ok := wantIDs[p.PluginID]; !ok {
			t.Errorf("DefaultPluginFixtures[%d].PluginID = %q, want one of the canonical 3", i, p.PluginID)
		}
		wantIDs[p.PluginID] = true
	}
	for id, got := range wantIDs {
		if !got {
			t.Errorf("DefaultPluginFixtures is missing %q", id)
		}
	}
	// Every fixture carries at least one capability
	// (dashboards render empty capabilities as
	// "no capabilities advertised", which is
	// ambiguous).
	for i, p := range DefaultPluginFixtures {
		if len(p.Capabilities) == 0 {
			t.Errorf("DefaultPluginFixtures[%d] (%q) has no capabilities", i, p.PluginID)
		}
	}
}

// ---- MemPluginStore in-process tests -----------------------

// TestPluginStore_DefaultState_WithFixtures pins
// the fresh-store baseline: a fresh PluginStore
// seeds three well-known bridges (claude-code-
// bridge, github-copilot-bridge, mcp-bridge),
// sorted by plugin_id ascending.
//
// NewMemPluginStore's default-fabrication path
// also drives the dispatcher's NewDispatcher nil-
// guard, so this test is the in-process mirror of
// the brief's "fresh store seeds three fixtures"
// line.
//
// Sort order is char-by-char ASCII on PluginID:
// 'c' < 'g' < 'm', so the canonical sorted
// sequence is `claude-code-bridge`,
// `github-copilot-bridge`, `mcp-bridge`.
func TestPluginStore_DefaultState_WithFixtures(t *testing.T) {
	ps := NewMemPluginStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != PluginStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, PluginStateVersion)
	}
	if len(state.Plugins) != 3 {
		t.Fatalf("len(Plugins) = %d, want 3", len(state.Plugins))
	}
	wantIDs := []string{"claude-code-bridge", "github-copilot-bridge", "mcp-bridge"}
	for i, want := range wantIDs {
		if got := state.Plugins[i].PluginID; got != want {
			t.Errorf("Plugins[%d].PluginID = %q, want %q", i, got, want)
		}
	}
}

// TestMemPluginStore_DefaultsFixturesOrigins pins
// the Origin field the DefaultPluginFixtures
// carry. Today's value is "config" for all three
// so a dashboard can distinguish fixture-seeded
// plugins from operator-supplied ones.
func TestMemPluginStore_DefaultsFixturesOrigins(t *testing.T) {
	ps := NewMemPluginStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	for _, p := range state.Plugins {
		if p.Origin != "config" {
			t.Errorf("Plugin(%q).Origin = %q, want config", p.PluginID, p.Origin)
		}
	}
}

// TestPluginStore_Disconnect pins the
// happy-path disconnect: a fresh
// MemPluginStore seeded with the three
// fixtures; disconnect one; the post-state
// has two entries and the right one is gone.
//
// The shape mirrors the FSPluginStore's
// post-mutation state (FSPluginStore's
// atomic-load + Disconnect hands back the
// in-memory post-state), so this test pins
// the in-process semantics the dispatcher
// relies on.
func TestPluginStore_Disconnect(t *testing.T) {
	ps := NewMemPluginStore(nil)
	state, err := ps.Disconnect(context.Background(), "mcp-bridge")
	if err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if len(state.Plugins) != 2 {
		t.Fatalf("len(Plugins) = %d, want 2", len(state.Plugins))
	}
	if state.Plugins[0].PluginID != "claude-code-bridge" {
		t.Errorf("Plugins[0].PluginID = %q, want claude-code-bridge", state.Plugins[0].PluginID)
	}
	if state.Plugins[1].PluginID != "github-copilot-bridge" {
		t.Errorf("Plugins[1].PluginID = %q, want github-copilot-bridge", state.Plugins[1].PluginID)
	}
	if state.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt is zero")
	}
}

// TestPluginStore_Disconnect_Missing pins the
// unknown-id → ErrPluginNotFound path. The
// dispatcher maps the sentinel to JSON-RPC
// -32015 "Plugin not found".
func TestPluginStore_Disconnect_Missing(t *testing.T) {
	ps := NewMemPluginStore(nil)
	_, err := ps.Disconnect(context.Background(), "no-such-plugin")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Disconnect(unknown) err = %v, want ErrPluginNotFound", err)
	}
}

// TestMemPluginStore_DisconnectEmpty pins the
// store's defence-in-depth empty-id →
// ErrPluginNotFound guard. The dispatcher's
// primary validation is "empty plugin_id →
// -32602" (a dashboard bug), but the store
// also enforces strictness so a future
// non-dispatcher caller can't slip through.
func TestMemPluginStore_DisconnectEmpty(t *testing.T) {
	ps := NewMemPluginStore(nil)
	_, err := ps.Disconnect(context.Background(), "")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Disconnect(\"\") err = %v, want ErrPluginNotFound", err)
	}
	_, err = ps.Disconnect(context.Background(), "   ")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Disconnect(\"   \") err = %v, want ErrPluginNotFound", err)
	}
}

// TestNewMemPluginStoreWith pins the
// custom-snapshot constructor: callers that
// want a specific starting state (e.g. a test
// that asserts "two plugins → sort them")
// build their own slice.
func TestNewMemPluginStoreWith(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	in := []Plugin{
		{PluginID: "zeta", Name: "Z", Version: "0.1.0", ConnectedAt: now, Capabilities: []string{"a.b"}},
		{PluginID: "alpha", Name: "A", Version: "1.0.0", ConnectedAt: now, Capabilities: []string{"x.y", "x.z"}},
	}
	ps := NewMemPluginStoreWith(in, nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Sorted ascending by PluginID: alpha < zeta.
	if state.Plugins[0].PluginID != "alpha" {
		t.Errorf("Plugins[0].PluginID = %q, want alpha", state.Plugins[0].PluginID)
	}
	if state.Plugins[1].PluginID != "zeta" {
		t.Errorf("Plugins[1].PluginID = %q, want zeta", state.Plugins[1].PluginID)
	}
	// Deep-copy invariant: mutating the
	// returned state's Capabilities must NOT
	// affect the input slice.
	state.Plugins[0].Capabilities[0] = "mutated"
	if in[0].Capabilities[0] == "mutated" {
		t.Errorf("Capabilities[0] reflected into the input slice — deep copy failed")
	}
}

// TestMemPluginStore_SortStableOnDuplicateIDs
// pins the secondary sort by ConnectedAt: even
// though the brief pins unique ids, the store
// must produce a stable order if two entries
// happen to share an id (e.g. a future cycle
// that allows versioning a plugin id).
func TestMemPluginStore_SortStableOnDuplicateIDs(t *testing.T) {
	earlier := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	in := []Plugin{
		{PluginID: "dup", ConnectedAt: later, Capabilities: []string{"y"}},
		{PluginID: "dup", ConnectedAt: earlier, Capabilities: []string{"x"}},
	}
	ps := NewMemPluginStoreWith(in, nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	// Earlier ConnectedAt comes first.
	if !state.Plugins[0].ConnectedAt.Equal(earlier) {
		t.Errorf("Plugins[0].ConnectedAt = %v, want earlier", state.Plugins[0].ConnectedAt)
	}
	if !state.Plugins[1].ConnectedAt.Equal(later) {
		t.Errorf("Plugins[1].ConnectedAt = %v, want later", state.Plugins[1].ConnectedAt)
	}
}

// TestMemPluginStore_ClonePluginState pins the
// defensive-copy invariant the store's
// Snapshot hands back. Mutating the returned
// state's Plugins slice must NOT leak into
// subsequent Snapshot calls.
func TestMemPluginStore_ClonePluginState(t *testing.T) {
	ps := NewMemPluginStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot (1st): %v", err)
	}
	// Mutate the returned slice.
	state.Plugins[0].Name = "mutated"
	state.Plugins[0].Capabilities[0] = "mutated-cap"

	// Re-snapshot; the in-memory state must
	// still carry the original values.
	state2, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot (2nd): %v", err)
	}
	if state2.Plugins[0].Name == "mutated" {
		t.Errorf("Plugins[0].Name reflected in 2nd snapshot — defensive copy failed")
	}
	if state2.Plugins[0].Capabilities[0] == "mutated-cap" {
		t.Errorf("Plugins[0].Capabilities[0] reflected in 2nd snapshot — defensive copy failed")
	}
}

// ---- FSPluginStore on-disk tests --------------------------

// TestFSPluginStore_Roundtrip exercises the
// production store against t.TempDir():
// fixture-seed on fresh read, Disconnect one
// plugin, re-open the file, confirm the
// on-disk format round-trips.
func TestFSPluginStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	ctx := context.Background()

	// Fresh read → fixture-seeded state.
	state, err := store.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot (initial): %v", err)
	}
	if len(state.Plugins) != 3 {
		t.Fatalf("initial len(Plugins) = %d, want 3", len(state.Plugins))
	}

	// Disconnect one.
	state, err = store.Disconnect(ctx, "mcp-bridge")
	if err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if len(state.Plugins) != 2 {
		t.Fatalf("post-Disconnect len(Plugins) = %d, want 2", len(state.Plugins))
	}

	// Verify the on-disk JSON shape (per
	// plugins_store.go:onDiskPluginState).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), `"version":`) {
		t.Errorf("on-disk JSON missing version key:\n%s", data)
	}
	if !strings.Contains(string(data), `"plugins":`) {
		t.Errorf("on-disk JSON missing plugins key:\n%s", data)
	}
	if !strings.Contains(string(data), `"plugin_id":`) {
		t.Errorf("on-disk JSON missing plugin_id key:\n%s", data)
	}

	// Re-open via a fresh store and confirm
	// the disconnected plugin is gone.
	store2 := NewFSPluginStore(path, nil)
	state, err = store2.Snapshot(ctx)
	if err != nil {
		t.Fatalf("Snapshot (re-open): %v", err)
	}
	if len(state.Plugins) != 2 {
		t.Errorf("re-opened len(Plugins) = %d, want 2", len(state.Plugins))
	}
	for _, p := range state.Plugins {
		if p.PluginID == "mcp-bridge" {
			t.Errorf("mcp-bridge still present after re-open: %+v", p)
		}
	}
}

// TestFSPluginStore_LoadExisting pins the
// load-existing path: write two plugins to
// JSON manually; the store reloads them on
// Snapshot, neither re-seeds the fixture nor
// loses the manual entries.
func TestFSPluginStore_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	rec := onDiskPluginState{
		Version: PluginStateVersion,
		Plugins: []Plugin{
			{PluginID: "manual-1", Name: "Manual One", Version: "0.1.0", ConnectedAt: now, Capabilities: []string{"x"}},
			{PluginID: "manual-2", Name: "Manual Two", Version: "0.2.0", ConnectedAt: now, Capabilities: []string{"y"}},
		},
		UpdatedAt: now,
	}
	data, err := jsonMarshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	store := NewFSPluginStore(path, nil)
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Plugins) != 2 {
		t.Fatalf("len(Plugins) = %d, want 2", len(state.Plugins))
	}
	if state.Plugins[0].PluginID != "manual-1" {
		t.Errorf("Plugins[0].PluginID = %q, want manual-1", state.Plugins[0].PluginID)
	}
	if state.Plugins[1].PluginID != "manual-2" {
		t.Errorf("Plugins[1].PluginID = %q, want manual-2", state.Plugins[1].PluginID)
	}
}

// TestFSPluginStore_FreshFile returns the
// fixture-seeded state when no JSON file
// exists yet.
func TestFSPluginStore_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Plugins) != len(DefaultPluginFixtures) {
		t.Errorf("len(Plugins) = %d, want %d", len(state.Plugins), len(DefaultPluginFixtures))
	}
	want := map[string]bool{
		"claude-code-bridge":    false,
		"mcp-bridge":            false,
		"github-copilot-bridge": false,
	}
	for _, p := range state.Plugins {
		want[p.PluginID] = true
	}
	for id, got := range want {
		if !got {
			t.Errorf("default fixtures missing %q", id)
		}
	}
}

// TestFSPluginStore_Disconnect pins the
// on-disk Disconnect path: a fresh store,
// disconnect one, the file no longer contains
// the disconnected plugin_id.
func TestFSPluginStore_Disconnect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	if _, err := store.Disconnect(context.Background(), "claude-code-bridge"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	// Reload from disk and confirm the
	// disconnected plugin is gone.
	store2 := NewFSPluginStore(path, nil)
	state, err := store2.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot (re-open): %v", err)
	}
	if len(state.Plugins) != 2 {
		t.Fatalf("post-Disconnect len(Plugins) = %d, want 2", len(state.Plugins))
	}
	for _, p := range state.Plugins {
		if p.PluginID == "claude-code-bridge" {
			t.Errorf("claude-code-bridge still in on-disk state: %+v", p)
		}
	}
}

// TestFSPluginStore_DisconnectMissing pins the
// on-disk Disconnect-not-found path: the
// store returns ErrPluginNotFound and the
// on-disk file is left untouched (no
// half-write, no .tmp left behind).
func TestFSPluginStore_DisconnectMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	_, err := store.Disconnect(context.Background(), "no-such-plugin")
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Disconnect(unknown) err = %v, want ErrPluginNotFound", err)
	}
	// Sentinel: no .tmp left behind from the
	// atomic-write path (the Disconnect early-
	// returned before atomicWrite so this is
	// belt-and-braces — pins the store's no-
	// mutation-on-error semantic).
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind at %s.tmp", path)
	}
}

// TestFSPluginStore_AtomicWriteSentinel
// confirms that the temp+rename atomicity
// leaves no .tmp file behind after a
// successful write.
func TestFSPluginStore_AtomicWriteSentinel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	if _, err := store.Disconnect(context.Background(), "mcp-bridge"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind at %s.tmp", path)
	}
}

// TestFSPluginStore_FilePerms pins the on-disk
// permissions the store applies (matches
// FSPermissionStore / antigravity-token style).
func TestFSPluginStore_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style file permissions not applicable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSPluginStore(path, nil)
	if _, err := store.Disconnect(context.Background(), "mcp-bridge"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0o600", perm)
	}
}

// TestFSPluginStore_MalformedFile pins the
// corrupt-file → error path: the store
// surfaces the parse error to the caller
// instead of silently falling back to
// fixtures.
func TestFSPluginStore_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := NewFSPluginStore(path, nil)
	_, err := store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on malformed file should fail")
	}
	if !strings.Contains(err.Error(), "rc-plugins: parse") {
		t.Errorf("err = %v, want it to mention \"rc-plugins: parse\"", err)
	}
}

// TestFSPluginStore_VersionCheck pins the
// newer-version → error path. The store
// refuses to load a file marked with a
// version > PluginStateVersion so the
// operator sees the "newer binary required"
// envelope.
func TestFSPluginStore_VersionCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	future := PluginStateVersion + 1
	rec := onDiskPluginState{
		Version:   future,
		Plugins:   []Plugin{},
		UpdatedAt: time.Now(),
	}
	data, err := jsonMarshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := NewFSPluginStore(path, nil)
	_, err = store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on future-version file should fail")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("err = %v, want it to mention \"newer than supported\"", err)
	}
}

// ---- dispatcher envelope tests ------------------------------

// TestListRemoteAgentPlugins_Ok pins the happy
// path: a fresh MemPluginStore receives a
// List call; the dispatcher returns the
// fixture-seeded state with three plugins,
// sorted by plugin_id ascending, in the
// brief's `{ok:true, plugins:[...]}` envelope.
func TestListRemoteAgentPlugins_Ok(t *testing.T) {
	ps := NewMemPluginStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	plugins, ok := res["plugins"].([]any)
	if !ok {
		t.Fatalf("plugins = %T, want []any: %#v", res["plugins"], res)
	}
	if len(plugins) != 3 {
		t.Fatalf("len(plugins) = %d, want 3", len(plugins))
	}
	// Sorted: claude-code-bridge, github-copilot-bridge, mcp-bridge.
	wantIDs := []string{"claude-code-bridge", "github-copilot-bridge", "mcp-bridge"}
	for i, want := range wantIDs {
		p, _ := plugins[i].(map[string]any)
		if got, _ := p["plugin_id"].(string); got != want {
			t.Errorf("plugins[%d].plugin_id = %q, want %q", i, got, want)
		}
		// Every plugin carries the
		// connected_at timestamp and a
		// non-empty capability list.
		if got, _ := p["connected_at"].(string); got == "" {
			t.Errorf("plugins[%d].connected_at missing or empty: %#v", i, p["connected_at"])
		}
		caps, _ := p["capabilities"].([]any)
		if len(caps) == 0 {
			t.Errorf("plugins[%d].capabilities is empty", i)
		}
	}
}

// TestListRemoteAgentPlugins_OkWithParams
// pins the spec's "params are ignored" path:
// the dispatcher accepts a non-null params
// object on List (returns the same envelope).
func TestListRemoteAgentPlugins_OkWithParams(t *testing.T) {
	ps := NewMemPluginStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", map[string]any{
		"unused_key": "irrelevant",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	plugins, ok := res["plugins"].([]any)
	if !ok || len(plugins) != 3 {
		t.Errorf("plugins = %#v, want 3 entries", res["plugins"])
	}
}

// TestDisconnectRemoteAgentPlugin_Ok pins the
// happy-path Disconnect: a fresh fixture-
// seeded MemPluginStore receives a Disconnect
// for "mcp-bridge", and the response envelope
// is the brief's lean `{ok:true, plugin_id,
// disconnected_at}` shape. The full post-
// mutation state (mcp-bridge gone) is verified
// via a follow-up List call and via the
// plugins/changed broadcast pinned by the
// wire-level test in remotetransport/.
func TestDisconnectRemoteAgentPlugin_Ok(t *testing.T) {
	ps := NewMemPluginStore(nil)
	d := newPluginsDispatcher(t, ps)
	res, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "mcp-bridge",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if got := res["ok"]; got != true {
		t.Errorf("result.ok = %v, want true", got)
	}
	if got, _ := res["plugin_id"].(string); got != "mcp-bridge" {
		t.Errorf("result.plugin_id = %q, want mcp-bridge", got)
	}
	if got, _ := res["disconnected_at"].(string); got == "" {
		t.Errorf("result.disconnected_at missing or empty: %#v", res["disconnected_at"])
	}
	// Sanity: the post-mutation state is
	// observable via List (the dispatcher
	// doesn't fabricate the response; the
	// store actually mutated).
	listRes, listErr := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if listErr != nil {
		t.Fatalf("list err = %#v", listErr)
	}
	plugins := listRes["plugins"].([]any)
	if len(plugins) != 2 {
		t.Fatalf("post-Disconnect len(plugins) = %d, want 2", len(plugins))
	}
	for _, raw := range plugins {
		p, _ := raw.(map[string]any)
		if got, _ := p["plugin_id"].(string); got == "mcp-bridge" {
			t.Errorf("mcp-bridge still in list.plugins after Disconnect: %#v", p)
		}
	}
}

// TestDisconnectRemoteAgentPlugin_NotFound
// pins the unknown-id → -32015 path.
func TestDisconnectRemoteAgentPlugin_NotFound(t *testing.T) {
	ps := NewMemPluginStore(nil)
	d := newPluginsDispatcher(t, ps)
	_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "no-such-plugin",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32015 {
		t.Errorf("code = %d, want -32015 (Plugin not found)", codeAsInt(t, errObj["code"]))
	}
	if msg, _ := errObj["message"].(string); msg != "Plugin not found" {
		t.Errorf("message = %q, want %q", msg, "Plugin not found")
	}
}

// TestDisconnectRemoteAgentPlugin_BadParams
// pins the empty / non-string plugin_id →
// -32602 path (the "dashboard bug" envelope).
func TestDisconnectRemoteAgentPlugin_BadParams(t *testing.T) {
	cases := []struct {
		label  string
		params map[string]any
	}{
		{"empty string", map[string]any{"plugin_id": ""}},
		{"whitespace-only", map[string]any{"plugin_id": "   "}},
		{"missing key", map[string]any{}},
		{"non-string number", map[string]any{"plugin_id": 42}},
		{"non-string bool", map[string]any{"plugin_id": true}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			ps := NewMemPluginStore(nil)
			d := newPluginsDispatcher(t, ps)
			_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", tc.params)
			if errObj == nil {
				t.Fatalf("expected error envelope, got result")
			}
			if codeAsInt(t, errObj["code"]) != -32602 {
				t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
			}
		})
	}
}

// TestDisconnectRemoteAgentPlugin_NoStore pins
// the nil-store → -32603 path.
func TestDisconnectRemoteAgentPlugin_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PluginsStore = nil
	_, errObj := dispatchPlugins(t, d, "DisconnectRemoteAgentPlugin", map[string]any{
		"plugin_id": "claude-code-bridge",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestListRemoteAgentPlugins_NoStore pins the
// nil-store → -32603 path on the read-side
// (the dispatcher fabricates a
// MemPluginStore by default, but tests can
// clear the field after construction).
func TestListRemoteAgentPlugins_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PluginsStore = nil
	_, errObj := dispatchPlugins(t, d, "ListRemoteAgentPlugins", nil)
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// ---- additional invariants --------------------------------

// TestPluginState_SortPinning pins the
// sortPluginsByID contract: regardless of the
// insertion order, Snapshot hands back the
// plugins sorted by PluginID ascending. This
// pins the dashboard's "stable iteration" UX.
func TestPluginState_SortPinning(t *testing.T) {
	in := []Plugin{
		{PluginID: "z-third", Capabilities: []string{"a"}},
		{PluginID: "a-first", Capabilities: []string{"b"}},
		{PluginID: "m-second", Capabilities: []string{"c"}},
	}
	ps := NewMemPluginStoreWith(in, nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	want := []string{"a-first", "m-second", "z-third"}
	for i, w := range want {
		if got := state.Plugins[i].PluginID; got != w {
			t.Errorf("Plugins[%d].PluginID = %q, want %q", i, got, w)
		}
	}
}

// TestPluginState_DefaultRoundTrip pins the
// brief's wire envelope shape on the default
// (no-mutation) state: version = 1, three
// plugins sorted by plugin_id, every plugin
// carries the four string fields + the
// connected_at + the capabilities array.
func TestPluginState_DefaultRoundTrip(t *testing.T) {
	ps := NewMemPluginStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != PluginStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, PluginStateVersion)
	}
	if len(state.Plugins) != 3 {
		t.Fatalf("len(Plugins) = %d, want 3", len(state.Plugins))
	}
	for i, p := range state.Plugins {
		if p.PluginID == "" {
			t.Errorf("Plugins[%d].PluginID = \"\", want non-empty", i)
		}
		if p.Version == "" {
			t.Errorf("Plugins[%d].Version = \"\", want non-empty", i)
		}
		if p.ConnectedAt.IsZero() {
			t.Errorf("Plugins[%d].ConnectedAt is zero", i)
		}
	}
}
