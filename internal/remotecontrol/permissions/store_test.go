// Package permissions — store_test.go: the permissions domain's
// own tests.
//
// The domain is the fact: the closed-set guards, the wire encoding
// of a grant rule, the store's snapshot / upsert / duplicate-add /
// malformed-key behaviour, the on-disk round trip, the insertion
// order contract and the reference's vocabulary.
//
// The dispatcher's round-trip against this domain lives in
// internal/remotecontrol/permissions_test.go, because the
// dispatcher is the host's.
package permissions

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ---- helper / parse tests --------------------------------

// TestParseToolPattern covers the wire encoding
// helper RemoveGrants depends on:
// "<tool_name>:<pattern>".
func TestParseToolPattern(t *testing.T) {
	cases := []struct {
		in          string
		wantTool    string
		wantPattern string
		wantOK      bool
	}{
		{"bash:npm install", "bash", "npm install", true},
		{"write_file:*.go", "write_file", "*.go", true},
		// Empty.
		{"", "", "", false},
		// No colon.
		{"no-colon-here", "", "", false},
		// Leading colon → empty tool, fail.
		{":pattern-only", "", "", false},
		// Trailing colon → empty pattern, fail.
		{"tool-only:", "", "", false},
		// Whitespace tolerance (trimmed before split).
		{"  bash  :  npm install  ", "bash", "npm install", true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			gotTool, gotPattern, gotOK := ParseToolPattern(tc.in)
			if gotTool != tc.wantTool {
				t.Errorf("tool = %q, want %q", gotTool, tc.wantTool)
			}
			if gotPattern != tc.wantPattern {
				t.Errorf("pattern = %q, want %q", gotPattern, tc.wantPattern)
			}
			if gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tc.wantOK)
			}
		})
	}
}

// TestIsValidPreset / TestIsValidReviewMode pin
// the closed-set validator the dispatcher uses to
// guard the store call.
func TestIsValidPreset(t *testing.T) {
	for _, p := range []Preset{PresetDefault, PresetStrict, PresetPermissive} {
		if !IsValidPreset(p) {
			t.Errorf("IsValidPreset(%q) = false, want true", p)
		}
	}
	if IsValidPreset("not-a-real-preset") {
		t.Errorf("IsValidPreset(\"not-a-real-preset\") = true, want false")
	}
	if IsValidPreset("") {
		t.Errorf("IsValidPreset(\"\") = true, want false")
	}
}

func TestIsValidReviewMode(t *testing.T) {
	for _, m := range []ReviewMode{ReviewModeAlways, ReviewModeNever, ReviewModeAuto} {
		if !IsValidReviewMode(m) {
			t.Errorf("IsValidReviewMode(%q) = false, want true", m)
		}
	}
	if IsValidReviewMode("maybe") {
		t.Errorf("IsValidReviewMode(\"maybe\") = true, want false")
	}
	if IsValidReviewMode("") {
		t.Errorf("IsValidReviewMode(\"\") = true, want false")
	}
}

// ---- MemStore in-process tests -------------------

// TestMemPermissionStore_Defaults pins the
// fresh-store baseline: preset=default, review=always,
// grants=[].
func TestMemPermissionStore_Defaults(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != DefaultPreset {
		t.Errorf("Preset = %q, want %q", state.Preset, DefaultPreset)
	}
	if state.Review != DefaultReviewMode {
		t.Errorf("Review = %q, want %q", state.Review, DefaultReviewMode)
	}
	if len(state.Grants) != 0 {
		t.Errorf("len(Grants) = %d, want 0", len(state.Grants))
	}
	if state.Version != StateVersion {
		t.Errorf("Version = %d, want %d", state.Version, StateVersion)
	}
}

// TestMemPermissionStore_SetAndGet covers the
// Set / Get round-trip for both preset and review.
func TestMemPermissionStore_SetAndGet(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.SetPreset(context.Background(), PresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := ps.SetReview(context.Background(), ReviewModeAuto); err != nil {
		t.Fatalf("SetReview: %v", err)
	}
	state, err := ps.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != PresetStrict {
		t.Errorf("Preset = %q, want strict", state.Preset)
	}
	if state.Review != ReviewModeAuto {
		t.Errorf("Review = %q, want auto", state.Review)
	}
}

// TestMemPermissionStore_AddGrant covers the
// add-then-list happy path.
func TestMemPermissionStore_AddGrant(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install", Reason: "USER-approved via npm wrapper"},
	})
	if err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(state.Grants))
	}
	if state.Grants[0].ToolName != "bash" {
		t.Errorf("Grants[0].ToolName = %q, want bash", state.Grants[0].ToolName)
	}
	if state.Grants[0].Pattern != "npm install" {
		t.Errorf("Grants[0].Pattern = %q, want npm install", state.Grants[0].Pattern)
	}
	if state.Grants[0].Reason != "USER-approved via npm wrapper" {
		t.Errorf("Grants[0].Reason = %q", state.Grants[0].Reason)
	}
	if state.Grants[0].AddedAt.IsZero() {
		t.Errorf("Grants[0].AddedAt is zero")
	}
}

// TestMemPermissionStore_AddDuplicate pins the
// (tool_name, pattern) duplicate → ErrGrantExists
// path. No silent overwrite (per the brief).
func TestMemPermissionStore_AddDuplicate(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install"},
	}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install"},
	})
	if !errors.Is(err, ErrGrantExists) {
		t.Fatalf("second add err = %v, want ErrGrantExists", err)
	}
}

// TestMemPermissionStore_AddEmptyRejects pins the
// store-layer "empty tool_name or pattern" guard.
func TestMemPermissionStore_AddEmptyRejects(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "", Pattern: "npm install"},
	})
	if err == nil {
		t.Fatalf("empty tool_name should fail")
	}
	_, err = ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "   "},
	})
	if err == nil {
		t.Fatalf("empty (whitespace) pattern should fail")
	}
}

// TestMemPermissionStore_AddBatch covers the
// batch-add path: three grants in a single call,
// store preserves order, all land.
func TestMemPermissionStore_AddBatch(t *testing.T) {
	ps := NewMemStore(nil)
	state, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install"},
		{ToolName: "write_file", Pattern: "*.go"},
		{ToolName: "read_file", Pattern: "/etc/**"},
	})
	if err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	if len(state.Grants) != 3 {
		t.Fatalf("len(Grants) = %d, want 3", len(state.Grants))
	}
}

// TestMemPermissionStore_Remove covers the
// remove-by-key happy path.
func TestMemPermissionStore_Remove(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install"},
		{ToolName: "bash", Pattern: "pnpm install"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := ps.RemoveGrants(context.Background(), []string{"bash:npm install"})
	if err != nil {
		t.Fatalf("RemoveGrants: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Fatalf("len(Grants) = %d, want 1", len(state.Grants))
	}
	if state.Grants[0].Pattern != "pnpm install" {
		t.Errorf("remaining grant pattern = %q, want pnpm install", state.Grants[0].Pattern)
	}
}

// TestMemPermissionStore_RemoveUnknownIsNoOp
// pins the idempotent-remove behaviour: removing
// a key that's not present silently succeeds.
func TestMemPermissionStore_RemoveUnknownIsNoOp(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "bash", Pattern: "npm install"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := ps.RemoveGrants(context.Background(), []string{"bash:never-added"})
	if err != nil {
		t.Fatalf("RemoveGrants: %v", err)
	}
	if len(state.Grants) != 1 {
		t.Errorf("len(Grants) = %d, want 1 (no-op remove)", len(state.Grants))
	}
}

// TestMemPermissionStore_RemoveMalformed pins the
// defence-in-depth malformed-key → ErrGrantMalformed
// path. The handler layer's ParseToolPattern should
// catch these first; the store's second-line-of-
// defence shape matches the layered-validation
// pattern the projects store uses.
func TestMemPermissionStore_RemoveMalformed(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.RemoveGrants(context.Background(), []string{"missing-colon"})
	if !errors.Is(err, ErrGrantMalformed) {
		t.Fatalf("malformed key err = %v, want ErrGrantMalformed", err)
	}
}

// ---- FSStore on-disk tests ------------------------

// TestFSPermissionStore_Roundtrip exercises the
// production store against t.TempDir(): SetPreset,
// SetReview, AddGrants, RemoveGrants, then re-load
// to confirm the on-disk format round-trips.
func TestFSPermissionStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	ctx := context.Background()

	// Mutate.
	if _, err := store.SetPreset(ctx, PresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := store.SetReview(ctx, ReviewModeAuto); err != nil {
		t.Fatalf("SetReview: %v", err)
	}
	if _, err := store.AddGrants(ctx, []Grant{
		{ToolName: "bash", Pattern: "npm install", Reason: "ok"},
		{ToolName: "write_file", Pattern: "*.go"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}

	// Verify the on-disk JSON shape (per
	// store_mem.go's onDiskState).
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !strings.Contains(string(data), `"version":`) {
		t.Errorf("on-disk JSON missing version key:\n%s", data)
	}
	if !strings.Contains(string(data), `"preset":`) {
		t.Errorf("on-disk JSON missing preset key:\n%s", data)
	}
	if !strings.Contains(string(data), `"grants":`) {
		t.Errorf("on-disk JSON missing grants key:\n%s", data)
	}

	// Re-open the file via a fresh store and confirm
	// state round-trips.
	store2 := NewFSStore(path, nil)
	state, err := store2.Get(ctx)
	if err != nil {
		t.Fatalf("Get (re-open): %v", err)
	}
	if state.Preset != PresetStrict {
		t.Errorf("Preset = %q, want strict", state.Preset)
	}
	if state.Review != ReviewModeAuto {
		t.Errorf("Review = %q, want auto", state.Review)
	}
	if len(state.Grants) != 2 {
		t.Errorf("len(Grants) = %d, want 2", len(state.Grants))
	}
}

// TestFSPermissionStore_FreshFile returns the
// default state when no JSON file exists yet.
func TestFSPermissionStore_FreshFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	state, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != DefaultPreset {
		t.Errorf("Preset = %q, want %q", state.Preset, DefaultPreset)
	}
	if state.Review != DefaultReviewMode {
		t.Errorf("Review = %q, want %q", state.Review, DefaultReviewMode)
	}
	if len(state.Grants) != 0 {
		t.Errorf("len(Grants) = %d, want 0", len(state.Grants))
	}
}

// TestFSPermissionStore_AtomicWriteSentinel
// confirms that the temp+rename atomicity leaves
// no .tmp file behind after a successful write.
func TestFSPermissionStore_AtomicWriteSentinel(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	if _, err := store.SetPreset(context.Background(), PresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind at %s.tmp", path)
	}
}

// TestFSPermissionStore_FilePerms pins the on-disk
// permissions the store applies (matches the
// projects.FSStore / antigravity-token style).
func TestFSPermissionStore_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style file permissions not applicable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	if _, err := store.SetPreset(context.Background(), PresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("perm = %o, want 0o600", perm)
	}
}

// ---- additional invariants ---------------------------------

// TestPermissionGrantKey pins the GrantKey helper
// (the dedup key the store uses). Future cycles
// that extend the key (e.g. add scope) bump the
// JSON version + extend this test.
func TestPermissionGrantKey(t *testing.T) {
	g := Grant{ToolName: "bash", Pattern: "npm install"}
	if got := g.GrantKey(); got != "bash:npm install" {
		t.Errorf("GrantKey = %q, want bash:npm install", got)
	}
}

// TestPermissionState_GrantsInsertionOrder pins
// the insertion-order contract both stores
// honour: AddGrants appends in the order the
// caller supplied. FSStore preserves
// the on-disk order on round-trip; the FS
// on-disk format is one JSON array per
// permissions.json, so on-disk order IS the
// insertion order. MemStore mirrors
// this so the in-process and on-disk semantics
// stay identical.
//
// Dashboards that want a stable display order
// can sort client-side by AddedAt — neither
// store sorts on the server, by design (a
// future cycle can add a wire-level sort if
// dashboards ask for it).
func TestPermissionState_GrantsInsertionOrder(t *testing.T) {
	ps := NewMemStore(nil)
	if _, err := ps.AddGrants(context.Background(), []Grant{
		{ToolName: "z-tool", Pattern: "z"},
		{ToolName: "a-tool", Pattern: "a"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}
	state, err := ps.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(state.Grants) != 2 {
		t.Fatalf("len(Grants) = %d, want 2", len(state.Grants))
	}
	// Insertion order preserved: z-tool first, a-tool second.
	if state.Grants[0].ToolName != "z-tool" {
		t.Errorf("Grants[0].ToolName = %q, want z-tool (insertion order)", state.Grants[0].ToolName)
	}
	if state.Grants[1].ToolName != "a-tool" {
		t.Errorf("Grants[1].ToolName = %q, want a-tool (insertion order)", state.Grants[1].ToolName)
	}
}
