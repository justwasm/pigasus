package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// TestFindMostRecentSameWorkspace seeds three conversations inside one
// temp workspace and asks FindMostRecent for that workspace. The
// expected candidate is the seed directory itself.
func TestFindMostRecentSameWorkspace(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(filepath.Join(root, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{
		"aaaa-1111-4000-8000-000000000001",
		"bbbb-2222-4000-8000-000000000002",
		"cccc-3333-4000-8000-000000000003",
	} {
		step := []Step{{Idx: 0, Type: StepUserInput, Status: 3, Text: "hello from " + id}}
		if err := WriteConversation(
			filepath.Join(root, "conversations", id+".db"), "traj-"+id, step); err != nil {
			t.Fatal(err)
		}
		seedIndex(t, id, ws)
	}

	got, err := FindMostRecent(ws, 8)
	if err != nil {
		t.Fatalf("FindMostRecent: %v", err)
	}
	if got == nil {
		t.Fatal("expected a workspace candidate, got nil")
	}
	if got.Dir != ws {
		t.Errorf("got.Dir = %q, want %q", got.Dir, ws)
	}
	if got.ConvCount != 3 {
		t.Errorf("got.ConvCount = %d, want 3", got.ConvCount)
	}
}

// TestFindMostRecentParentFallback seeds a conversation in /tmp/a and asks
// from /tmp/a/sub, expecting the parent /tmp/a to be picked.
func TestFindMostRecentParentFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "a")
	sub := filepath.Join(ws, "sub")
	if err := os.MkdirAll(filepath.Join(root, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "dddd-4444-4000-8000-000000000004"
	step := []Step{{Idx: 0, Type: StepUserInput, Status: 3, Text: "parent owned"}}
	if err := WriteConversation(
		filepath.Join(root, "conversations", id+".db"), "traj", step); err != nil {
		t.Fatal(err)
	}
	seedIndex(t, id, ws)

	got, err := FindMostRecent(sub, 4)
	if err != nil {
		t.Fatalf("FindMostRecent: %v", err)
	}
	if got == nil {
		t.Fatal("expected parent candidate, got nil")
	}
	if got.Dir != ws {
		t.Errorf("got.Dir = %q, want parent %q", got.Dir, ws)
	}
}

// TestFindMostRecentChildFallback seeds a conversation under /tmp/a/sub
// and asks from /tmp/a, expecting the child /tmp/a/sub.
func TestFindMostRecentChildFallback(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "a")
	child := filepath.Join(ws, "sub")
	if err := os.MkdirAll(filepath.Join(root, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "eeee-5555-4000-8000-000000000005"
	step := []Step{{Idx: 0, Type: StepUserInput, Status: 3, Text: "child owned"}}
	if err := WriteConversation(
		filepath.Join(root, "conversations", id+".db"), "traj", step); err != nil {
		t.Fatal(err)
	}
	seedIndex(t, id, child)

	got, err := FindMostRecent(ws, 0)
	if err != nil {
		t.Fatalf("FindMostRecent: %v", err)
	}
	if got == nil {
		t.Fatal("expected child candidate, got nil")
	}
	if got.Dir != child {
		t.Errorf("got.Dir = %q, want child %q", got.Dir, child)
	}
}

// TestFindMostRecentNotFound runs in a fresh ANTIGRAVITY_HOME with no
// conversations; FindMostRecent must return (nil, nil) without an error.
func TestFindMostRecentNotFound(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "empty")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := FindMostRecent(ws, 4)
	if err != nil {
		t.Fatalf("FindMostRecent: %v", err)
	}
	if got != nil {
		t.Errorf("expected nil candidate, got %+v", got)
	}
}

// TestMostRecentInWorkspace_LoadsCacheOnce pins the once-per-scan
// invariant: MostRecentInWorkspace must read + parse
// cache/conversation_metadata.json exactly once even when the picker
// has many rows, not once per row. The previous implementation did the
// latter, which made startup O(N×M) and pinned a CPU core at 100 % on a
// long-running install (~2.1 MB cache).
//
// The test swaps loadWorkspaceURIsFromCache with a counting stub so we
// can assert the call count independently of the rest of the picker.
func TestMostRecentInWorkspace_LoadsCacheOnce(t *testing.T) {
	// Seed ANTIGRAVITY_HOME so StoreDir() returns a writable root.
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	// Install the counter stub; restore on exit.
	var calls atomic.Int32
	orig := loadWorkspaceURIsFromCacheFn
	loadWorkspaceURIsFromCacheFn = func() map[string][]string {
		calls.Add(1)
		return nil
	}
	t.Cleanup(func() { loadWorkspaceURIsFromCacheFn = orig })

	// Call into the public API; expect exactly one cache read even when
	// there are zero rows (the loop never enters — the helper still
	// runs once, by design, so we don't pay the file-read cost lazily).
	_ = MostRecentInWorkspace(filepath.Join(root, "ws"))
	if got := calls.Load(); got != 1 {
		t.Errorf("loadWorkspaceURIsFromCache calls = %d, want 1 (once-per-scan)", got)
	}
}

// TestRowMatchesCacheURI_NilURIsMapIsSafe pins the nil-tolerant
// behaviour added alongside the once-per-scan fix: when the cache file
// is missing or unreadable, loadWorkspaceURIsFromCache returns nil,
// and rowMatchesCacheURI must treat nil as "no entries" without
// panicking on the lookup.
func TestRowMatchesCacheURI_NilURIsMapIsSafe(t *testing.T) {
	for name, m := range map[string]map[string][]string{
		"nil":     nil,
		"empty":   {},
		"missing": {"other-conv": {"file:///elsewhere"}},
	} {
		t.Run(name, func(t *testing.T) {
			if rowMatchesCacheURI("any-id", "file:///want", "/want", m) {
				t.Errorf("rowMatchesCacheURI(%q) = true, want false", name)
			}
		})
	}
}

// TestLoadWorkspaceURIsFromCache_ParsesShape pins the JSON shape the
// helper expects from cache/conversation_metadata.json. The structure
// is the cycle-184 picker shape, so a regression here means the
// workspace-match path silently returns false for every row.
func TestLoadWorkspaceURIsFromCache_ParsesShape(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	cacheDir := filepath.Join(root, "cache")
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cachePath := filepath.Join(cacheDir, "conversation_metadata.json")
	payload := map[string]any{
		"conversations": map[string]any{
			"conv-a": map[string]any{
				"summary": map[string]any{
					"WorkspaceURIs": []string{"file:///workspace/a"},
				},
			},
			"conv-b": map[string]any{
				"summary": map[string]any{
					"WorkspaceURIs": []string{"file:///workspace/b", "file:///workspace/b-old"},
				},
			},
			// No WorkspaceURIs at all — must be omitted from the map.
			"conv-c": map[string]any{"summary": map[string]any{}},
		},
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	got := loadWorkspaceURIsFromCache()
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (conv-a, conv-b; conv-c has no URIs)", len(got))
	}
	if want := []string{"file:///workspace/a"}; !equalURIs(got["conv-a"], want) {
		t.Errorf("conv-a = %v, want %v", got["conv-a"], want)
	}
	if want := []string{"file:///workspace/b", "file:///workspace/b-old"}; !equalURIs(got["conv-b"], want) {
		t.Errorf("conv-b = %v, want %v", got["conv-b"], want)
	}
}

func equalURIs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestMostRecentInWorkspace_LoadsIndexOnce pins the once-per-scan
// invariant for conversation_summaries.db: the index must be opened and
// scanned exactly once per call, never once per picker row. The previous
// implementation did the latter (Open + full table scan inside the
// per-row matcher), which cost ~55 s on an install with 1.1 K
// conversations before the TUI could draw its first frame.
func TestMostRecentInWorkspace_LoadsIndexOnce(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	var calls atomic.Int32
	orig := loadWorkspaceURIsFromIndexFn
	loadWorkspaceURIsFromIndexFn = func() map[string][]string {
		calls.Add(1)
		return nil
	}
	t.Cleanup(func() { loadWorkspaceURIsFromIndexFn = orig })

	_ = MostRecentInWorkspace(filepath.Join(root, "ws"))
	if got := calls.Load(); got != 1 {
		t.Errorf("loadWorkspaceURIsFromIndex calls = %d, want 1 (once-per-scan)", got)
	}
}

// TestRowMatchesIndexURI_MapLookup pins the map-based matcher that
// replaced the per-row SQLite scan: a hit by literal URI, a miss for an
// unknown id, and nil-map tolerance (index file absent or unreadable).
func TestRowMatchesIndexURI_MapLookup(t *testing.T) {
	uris := map[string][]string{
		"conv-a": {"file:///tmp/ws"},
	}
	if !rowMatchesIndexURI("conv-a", "file:///tmp/ws", "/tmp/ws", uris) {
		t.Error("expected literal workspace URI match")
	}
	if rowMatchesIndexURI("conv-b", "file:///tmp/ws", "/tmp/ws", uris) {
		t.Error("unknown conversation id must not match")
	}
	if rowMatchesIndexURI("conv-a", "file:///tmp/ws", "/tmp/ws", nil) {
		t.Error("nil index map must not match")
	}
	if rowMatchesIndexURI("conv-a", "file:///tmp/ws", "/tmp/ws", map[string][]string{}) {
		t.Error("empty index map must not match")
	}
}
