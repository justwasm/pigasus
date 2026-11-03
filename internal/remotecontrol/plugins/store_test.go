// Package plugins — store_test.go: the plugins domain's own
// tests.
//
// The store is the domain's fact: the fixture set, the id
// normalisation, the disconnect path (including the missing-id
// sentinel), the sort order and the on-disk layout.
//
// The dispatcher's round-trip against the store lives in
// internal/remotecontrol/plugins_test.go, because the
// dispatcher is the host's.
package plugins

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
)

// jsonMarshal is a thin wrapper around encoding/json so the
// test file keeps a small import block.
func jsonMarshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

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

// TestDefaultFixtures pins the canonical
// fixture set the store seeds when no
// plugins.json exists. Future cycles that grow
// the fixture surface (e.g. add slack-bridge)
// bump DefaultFixtures' length here too.
//
// DefaultFixtures is intentionally
// unsorted (insertion-order map; a future
// cycle that wants stable iteration can sort
// the slice — the store's Snapshot/Disconnect
// already sort on the way out).
func TestDefaultFixtures(t *testing.T) {
	if len(DefaultFixtures) != 3 {
		t.Fatalf("len(DefaultFixtures) = %d, want 3", len(DefaultFixtures))
	}
	wantIDs := map[string]bool{
		"claude-code-bridge":    false,
		"mcp-bridge":            false,
		"github-copilot-bridge": false,
	}
	for i, p := range DefaultFixtures {
		if _, ok := wantIDs[p.PluginID]; !ok {
			t.Errorf("DefaultFixtures[%d].PluginID = %q, want one of the canonical 3", i, p.PluginID)
		}
		wantIDs[p.PluginID] = true
	}
	for id, got := range wantIDs {
		if !got {
			t.Errorf("DefaultFixtures is missing %q", id)
		}
	}
	// Every fixture carries at least one capability
	// (dashboards render empty capabilities as
	// "no capabilities advertised", which is
	// ambiguous).
	for i, p := range DefaultFixtures {
		if len(p.Capabilities) == 0 {
			t.Errorf("DefaultFixtures[%d] (%q) has no capabilities", i, p.PluginID)
		}
	}
}

// TestStore_DefaultState_WithFixtures pins
// the fresh-store baseline: a fresh Store
// seeds three well-known bridges (claude-code-
// bridge, github-copilot-bridge, mcp-bridge),
// sorted by plugin_id ascending.
//
// NewMemStore's default-fabrication path
// also drives the dispatcher's NewDispatcher nil-
// guard, so this test is the in-process mirror of
// the brief's "fresh store seeds three fixtures"
// line.
//
// Sort order is char-by-char ASCII on PluginID:
// 'c' < 'g' < 'm', so the canonical sorted
// sequence is `claude-code-bridge`,
// `github-copilot-bridge`, `mcp-bridge`.
func TestStore_DefaultState_WithFixtures(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
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

// TestMemStore_DefaultsFixturesOrigins pins
// the Origin field the DefaultFixtures
// carry. Today's value is "config" for all three
// so a dashboard can distinguish fixture-seeded
// plugins from operator-supplied ones.
func TestMemStore_DefaultsFixturesOrigins(t *testing.T) {
	ps := NewMemStore(nil)
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

// TestStore_Disconnect pins the
// happy-path disconnect: a fresh
// MemStore seeded with the three
// fixtures; disconnect one; the post-state
// has two entries and the right one is gone.
//
// The shape mirrors the FSStore's
// post-mutation state (FSStore's
// atomic-load + Disconnect hands back the
// in-memory post-state), so this test pins
// the in-process semantics the dispatcher
// relies on.
func TestStore_Disconnect(t *testing.T) {
	ps := NewMemStore(nil)
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

// TestStore_Disconnect_Missing pins the
// unknown-id → ErrNotFound path. The
// dispatcher maps the sentinel to JSON-RPC
// -32015 "Plugin not found".
func TestStore_Disconnect_Missing(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.Disconnect(context.Background(), "no-such-plugin")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disconnect(unknown) err = %v, want ErrNotFound", err)
	}
}

// TestMemStore_DisconnectEmpty pins the
// store's defence-in-depth empty-id →
// ErrNotFound guard. The dispatcher's
// primary validation is "empty plugin_id →
// -32602" (a dashboard bug), but the store
// also enforces strictness so a future
// non-dispatcher caller can't slip through.
func TestMemStore_DisconnectEmpty(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.Disconnect(context.Background(), "")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disconnect(\"\") err = %v, want ErrNotFound", err)
	}
	_, err = ps.Disconnect(context.Background(), "   ")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disconnect(\"   \") err = %v, want ErrNotFound", err)
	}
}

// TestNewMemStoreWith pins the
// custom-snapshot constructor: callers that
// want a specific starting state (e.g. a test
// that asserts "two plugins → sort them")
// build their own slice.
func TestNewMemStoreWith(t *testing.T) {
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	in := []Plugin{
		{PluginID: "zeta", Name: "Z", Version: "0.1.0", ConnectedAt: now, Capabilities: []string{"a.b"}},
		{PluginID: "alpha", Name: "A", Version: "1.0.0", ConnectedAt: now, Capabilities: []string{"x.y", "x.z"}},
	}
	ps := NewMemStoreWith(in, nil)
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

// TestMemStore_SortStableOnDuplicateIDs
// pins the secondary sort by ConnectedAt: even
// though the brief pins unique ids, the store
// must produce a stable order if two entries
// happen to share an id (e.g. a future cycle
// that allows versioning a plugin id).
func TestMemStore_SortStableOnDuplicateIDs(t *testing.T) {
	earlier := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	later := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	in := []Plugin{
		{PluginID: "dup", ConnectedAt: later, Capabilities: []string{"y"}},
		{PluginID: "dup", ConnectedAt: earlier, Capabilities: []string{"x"}},
	}
	ps := NewMemStoreWith(in, nil)
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

// TestMemStore_ClonePluginState pins the
// defensive-copy invariant the store's
// Snapshot hands back. Mutating the returned
// state's Plugins slice must NOT leak into
// subsequent Snapshot calls.
func TestMemStore_ClonePluginState(t *testing.T) {
	ps := NewMemStore(nil)
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

// TestFSStore_Roundtrip exercises the
// production store against t.TempDir():
// fixture-seed on fresh read, Disconnect one
// plugin, re-open the file, confirm the
// on-disk format round-trips.
func TestFSStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
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
	// plugins_store.go:onDiskState).
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
	store2 := NewFSStore(path, nil)
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

// TestFSStore_LoadExisting pins the
// load-existing path: write two plugins to
// JSON manually; the store reloads them on
// Snapshot, neither re-seeds the fixture nor
// loses the manual entries.
func TestFSStore_LoadExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	now := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	rec := onDiskState{
		Version: StateVersion,
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
	store := NewFSStore(path, nil)
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

// TestFSStore_FreshFile returns the
// fixture-seeded state when no JSON file
// exists yet.
func TestFSStore_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
	state, err := store.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if len(state.Plugins) != len(DefaultFixtures) {
		t.Errorf("len(Plugins) = %d, want %d", len(state.Plugins), len(DefaultFixtures))
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

// TestFSStore_Disconnect pins the
// on-disk Disconnect path: a fresh store,
// disconnect one, the file no longer contains
// the disconnected plugin_id.
func TestFSStore_Disconnect(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
	if _, err := store.Disconnect(context.Background(), "claude-code-bridge"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	// Reload from disk and confirm the
	// disconnected plugin is gone.
	store2 := NewFSStore(path, nil)
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

// TestFSStore_DisconnectMissing pins the
// on-disk Disconnect-not-found path: the
// store returns ErrNotFound and the
// on-disk file is left untouched (no
// half-write, no .tmp left behind).
func TestFSStore_DisconnectMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
	_, err := store.Disconnect(context.Background(), "no-such-plugin")
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Disconnect(unknown) err = %v, want ErrNotFound", err)
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

// TestFSStore_AtomicWriteSentinel
// confirms that the temp+rename atomicity
// leaves no .tmp file behind after a
// successful write.
func TestFSStore_AtomicWriteSentinel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
	if _, err := store.Disconnect(context.Background(), "mcp-bridge"); err != nil {
		t.Fatalf("Disconnect: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind at %s.tmp", path)
	}
}

// TestFSStore_FilePerms pins the on-disk
// permissions the store applies (matches
// FSPermissionStore / antigravity-token style).
func TestFSStore_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style file permissions not applicable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	store := NewFSStore(path, nil)
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

// TestFSStore_MalformedFile pins the
// corrupt-file → error path: the store
// surfaces the parse error to the caller
// instead of silently falling back to
// fixtures.
func TestFSStore_MalformedFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	store := NewFSStore(path, nil)
	_, err := store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on malformed file should fail")
	}
	if !strings.Contains(err.Error(), "rc-plugins: parse") {
		t.Errorf("err = %v, want it to mention \"rc-plugins: parse\"", err)
	}
}

// TestFSStore_VersionCheck pins the
// newer-version → error path. The store
// refuses to load a file marked with a
// version > StateVersion so the
// operator sees the "newer binary required"
// envelope.
func TestFSStore_VersionCheck(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "plugins.json")
	future := StateVersion + 1
	rec := onDiskState{
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
	store := NewFSStore(path, nil)
	_, err = store.Snapshot(context.Background())
	if err == nil {
		t.Fatalf("Snapshot on future-version file should fail")
	}
	if !strings.Contains(err.Error(), "newer than supported") {
		t.Errorf("err = %v, want it to mention \"newer than supported\"", err)
	}
}

// TestState_SortPinning pins the
// sortPluginsByID contract: regardless of the
// insertion order, Snapshot hands back the
// plugins sorted by PluginID ascending. This
// pins the dashboard's "stable iteration" UX.
func TestState_SortPinning(t *testing.T) {
	in := []Plugin{
		{PluginID: "z-third", Capabilities: []string{"a"}},
		{PluginID: "a-first", Capabilities: []string{"b"}},
		{PluginID: "m-second", Capabilities: []string{"c"}},
	}
	ps := NewMemStoreWith(in, nil)
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

// TestState_DefaultRoundTrip pins the
// brief's wire envelope shape on the default
// (no-mutation) state: version = 1, three
// plugins sorted by plugin_id, every plugin
// carries the four string fields + the
// connected_at + the capabilities array.
func TestState_DefaultRoundTrip(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.Snapshot(context.Background())
	if err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
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
