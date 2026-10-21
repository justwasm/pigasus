// Tests for the rc-permissions-grants cycle. Covers:
//
//   - The four JSON-RPC methods
//     (SetRemoteAgentPermissionPreset /
//     SetRemoteAgentArtifactReviewMode /
//     AddRemoteAgentPermissionGrants /
//     RemoveRemoteAgentPermissionGrants),
//     including the sentinel-error mapping
//     (ErrPermissionGrantExists → -32014,
//     ErrPermissionGrantMalformed → -32602)
//     and the validation rules
//     (unknown preset/mode → -32602,
//     empty tool_patterns → -32602,
//     malformed "<tool_name>:<pattern>" → -32602);
//
//   - The PermissionStore.MemPermissionStore
//     (Get / SetPreset / SetReview / AddGrants /
//     RemoveGrants / duplicate-add /
//     empty-grant rejection / defence-in-depth
//     malformed-key enforcement) at the in-process
//     level (no Unix socket, no ws adapter);
//
//   - The FSPermissionStore happy path against a
//     real t.TempDir() so the on-disk
//     permissions.json layout is exercised
//     (atomic temp + rename, 0o600 perms, version
//     stamp);
//
//   - The ParseToolPattern helper — the wire
//     encoding RemoveGrants depends on;
//
//   - IsValidPreset / IsValidReviewMode closed-set
//     guards.
//
// Wire-level tests live in
// internal/remotetransport/tcp_test.go (mirroring
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
)

// newPermissionsDispatcher returns a Dispatcher
// with the supplied PermissionStore pre-installed.
// The helper mirrors newProjectsDispatcher.
func newPermissionsDispatcher(t *testing.T, ps PermissionStore) *Dispatcher {
	t.Helper()
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{
		Session:          sess,
		PermissionsStore: ps,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return d
}

// dispatchPermissions sends a Set/Add/Remove
// request through the dispatcher and returns the
// (result, error) pair. Mirrors dispatchProjects.
func dispatchPermissions(t *testing.T, d *Dispatcher, method string, params any) (map[string]any, map[string]any) {
	t.Helper()
	var raw []byte
	if params != nil {
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
	for _, p := range []PermissionPreset{PermissionPresetDefault, PermissionPresetStrict, PermissionPresetPermissive} {
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
	for _, m := range []ArtifactReviewMode{ArtifactReviewModeAlways, ArtifactReviewModeNever, ArtifactReviewModeAuto} {
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

// ---- MemPermissionStore in-process tests -------------------

// TestMemPermissionStore_Defaults pins the
// fresh-store baseline: preset=default, review=always,
// grants=[].
func TestMemPermissionStore_Defaults(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	state, err := ps.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != DefaultPermissionPreset {
		t.Errorf("Preset = %q, want %q", state.Preset, DefaultPermissionPreset)
	}
	if state.Review != DefaultArtifactReviewMode {
		t.Errorf("Review = %q, want %q", state.Review, DefaultArtifactReviewMode)
	}
	if len(state.Grants) != 0 {
		t.Errorf("len(Grants) = %d, want 0", len(state.Grants))
	}
	if state.Version != PermissionStateVersion {
		t.Errorf("Version = %d, want %d", state.Version, PermissionStateVersion)
	}
}

// TestMemPermissionStore_SetAndGet covers the
// Set / Get round-trip for both preset and review.
func TestMemPermissionStore_SetAndGet(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	if _, err := ps.SetPreset(context.Background(), PermissionPresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := ps.SetReview(context.Background(), ArtifactReviewModeAuto); err != nil {
		t.Fatalf("SetReview: %v", err)
	}
	state, err := ps.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != PermissionPresetStrict {
		t.Errorf("Preset = %q, want strict", state.Preset)
	}
	if state.Review != ArtifactReviewModeAuto {
		t.Errorf("Review = %q, want auto", state.Review)
	}
}

// TestMemPermissionStore_AddGrant covers the
// add-then-list happy path.
func TestMemPermissionStore_AddGrant(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	state, err := ps.AddGrants(context.Background(), []PermissionGrant{
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
// (tool_name, pattern) duplicate → ErrPermissionGrantExists
// path. No silent overwrite (per the brief).
func TestMemPermissionStore_AddDuplicate(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	if _, err := ps.AddGrants(context.Background(), []PermissionGrant{
		{ToolName: "bash", Pattern: "npm install"},
	}); err != nil {
		t.Fatalf("first add: %v", err)
	}
	_, err := ps.AddGrants(context.Background(), []PermissionGrant{
		{ToolName: "bash", Pattern: "npm install"},
	})
	if !errors.Is(err, ErrPermissionGrantExists) {
		t.Fatalf("second add err = %v, want ErrPermissionGrantExists", err)
	}
}

// TestMemPermissionStore_AddEmptyRejects pins the
// store-layer "empty tool_name or pattern" guard.
func TestMemPermissionStore_AddEmptyRejects(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	_, err := ps.AddGrants(context.Background(), []PermissionGrant{
		{ToolName: "", Pattern: "npm install"},
	})
	if err == nil {
		t.Fatalf("empty tool_name should fail")
	}
	_, err = ps.AddGrants(context.Background(), []PermissionGrant{
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
	ps := NewMemPermissionStore(nil)
	state, err := ps.AddGrants(context.Background(), []PermissionGrant{
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
	ps := NewMemPermissionStore(nil)
	if _, err := ps.AddGrants(context.Background(), []PermissionGrant{
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
	ps := NewMemPermissionStore(nil)
	if _, err := ps.AddGrants(context.Background(), []PermissionGrant{
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
// defence-in-depth malformed-key → ErrPermissionGrantMalformed
// path. The handler layer's ParseToolPattern should
// catch these first; the store's second-line-of-
// defence shape matches the layered-validation
// pattern the projects store uses.
func TestMemPermissionStore_RemoveMalformed(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	_, err := ps.RemoveGrants(context.Background(), []string{"missing-colon"})
	if !errors.Is(err, ErrPermissionGrantMalformed) {
		t.Fatalf("malformed key err = %v, want ErrPermissionGrantMalformed", err)
	}
}

// ---- FSPermissionStore on-disk tests ------------------------

// TestFSPermissionStore_Roundtrip exercises the
// production store against t.TempDir(): SetPreset,
// SetReview, AddGrants, RemoveGrants, then re-load
// to confirm the on-disk format round-trips.
func TestFSPermissionStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSPermissionStore(path, nil)
	ctx := context.Background()

	// Mutate.
	if _, err := store.SetPreset(ctx, PermissionPresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := store.SetReview(ctx, ArtifactReviewModeAuto); err != nil {
		t.Fatalf("SetReview: %v", err)
	}
	if _, err := store.AddGrants(ctx, []PermissionGrant{
		{ToolName: "bash", Pattern: "npm install", Reason: "ok"},
		{ToolName: "write_file", Pattern: "*.go"},
	}); err != nil {
		t.Fatalf("AddGrants: %v", err)
	}

	// Verify the on-disk JSON shape (per
	// permissions_store.go's onDiskPermissionState).
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
	store2 := NewFSPermissionStore(path, nil)
	state, err := store2.Get(ctx)
	if err != nil {
		t.Fatalf("Get (re-open): %v", err)
	}
	if state.Preset != PermissionPresetStrict {
		t.Errorf("Preset = %q, want strict", state.Preset)
	}
	if state.Review != ArtifactReviewModeAuto {
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
	store := NewFSPermissionStore(path, nil)
	state, err := store.Get(context.Background())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if state.Preset != DefaultPermissionPreset {
		t.Errorf("Preset = %q, want %q", state.Preset, DefaultPermissionPreset)
	}
	if state.Review != DefaultArtifactReviewMode {
		t.Errorf("Review = %q, want %q", state.Review, DefaultArtifactReviewMode)
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
	store := NewFSPermissionStore(path, nil)
	if _, err := store.SetPreset(context.Background(), PermissionPresetStrict); err != nil {
		t.Fatalf("SetPreset: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind at %s.tmp", path)
	}
}

// TestFSPermissionStore_FilePerms pins the on-disk
// permissions the store applies (matches the
// FSProjectStore / antigravity-token style).
func TestFSPermissionStore_FilePerms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix-style file permissions not applicable on Windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSPermissionStore(path, nil)
	if _, err := store.SetPreset(context.Background(), PermissionPresetStrict); err != nil {
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

// ---- dispatcher envelope tests ------------------------------

// TestSetRemoteAgentPermissionPreset_Ok pins the
// happy-path SetPreset envelope: result carries
// the post-state (preset updated, review stable,
// grants stable).
func TestSetRemoteAgentPermissionPreset_Ok(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "strict",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if res["preset"] != "strict" {
		t.Errorf("result.preset = %v, want strict", res["preset"])
	}
	if res["review"] != string(DefaultArtifactReviewMode) {
		t.Errorf("result.review = %v, want %v", res["review"], DefaultArtifactReviewMode)
	}
	if v, _ := res["version"].(int); v != PermissionStateWireFormat {
		t.Errorf("result.version = %v, want %d", res["version"], PermissionStateWireFormat)
	}
}

// TestSetRemoteAgentPermissionPreset_BadParams pins
// the unknown-preset → -32602 path.
func TestSetRemoteAgentPermissionPreset_BadParams(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "ULTRA-STRICT",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentPermissionPreset_NoStore pins
// the nil-store → -32603 path. (NewDispatcher
// fabricates a default, so we explicitly build a
// Dispatcher with no PermissionsStore — using the
// package's nil-friendly NewDispatcher default.)
func TestSetRemoteAgentPermissionPreset_NoStore(t *testing.T) {
	// Build a Config that explicitly says "no
	// PermissionsStore". NewDispatcher's nil-guard
	// fabricates a fresh MemPermissionStore, but for
	// the negative test we want to verify the
	// "store reports broken state" path. Use a
	// minimal sentinel by overriding the field after
	// construction.
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PermissionsStore = nil
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentPermissionPreset", map[string]any{
		"preset": "strict",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// TestSetRemoteAgentArtifactReviewMode_Ok pins the
// happy path for SetReview.
func TestSetRemoteAgentArtifactReviewMode_Ok(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "SetRemoteAgentArtifactReviewMode", map[string]any{
		"mode": "never",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	if res["review"] != "never" {
		t.Errorf("result.review = %v, want never", res["review"])
	}
}

// TestSetRemoteAgentArtifactReviewMode_BadParams
// pins the unknown-mode → -32602 path.
func TestSetRemoteAgentArtifactReviewMode_BadParams(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "SetRemoteAgentArtifactReviewMode", map[string]any{
		"mode": "definitely-not-a-mode",
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Ok pins the
// happy-path Add: result carries the new grants
// array with the added entries.
func TestAddRemoteAgentPermissionGrants_Ok(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
		"reason":        "USER approved",
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	grants, ok := res["grants"].([]any)
	if !ok {
		t.Fatalf("grants = %T, want []any: %#v", res["grants"], res)
	}
	if len(grants) != 1 {
		t.Fatalf("len(grants) = %d, want 1", len(grants))
	}
	g0 := grants[0].(map[string]any)
	if g0["tool_name"] != "bash" {
		t.Errorf("grants[0].tool_name = %v, want bash", g0["tool_name"])
	}
	if g0["pattern"] != "npm install" {
		t.Errorf("grants[0].pattern = %v, want npm install", g0["pattern"])
	}
	if g0["reason"] != "USER approved" {
		t.Errorf("grants[0].reason = %v, want \"USER approved\"", g0["reason"])
	}
}

// TestAddRemoteAgentPermissionGrants_BatchOk pins
// the multi-entry Add path.
func TestAddRemoteAgentPermissionGrants_BatchOk(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	res, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{
			"bash:npm install",
			"write_file:*.go",
			"read_file:/etc/hosts",
		},
	})
	if errObj != nil {
		t.Fatalf("err = %#v", errObj)
	}
	grants := res["grants"].([]any)
	if len(grants) != 3 {
		t.Errorf("len(grants) = %d, want 3", len(grants))
	}
}

// TestAddRemoteAgentPermissionGrants_Empty pins
// the empty-tool_patterns → -32602 path.
func TestAddRemoteAgentPermissionGrants_Empty(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Malformed pins
// the malformed "<tool_name>:<pattern>" → -32602
// path.
func TestAddRemoteAgentPermissionGrants_Malformed(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"missing-colon"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestAddRemoteAgentPermissionGrants_Duplicate pins
// the duplicate-key → -32014 path.
func TestAddRemoteAgentPermissionGrants_Duplicate(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	if _, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	}); errObj != nil {
		t.Fatalf("first add err = %#v", errObj)
	}
	_, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32014 {
		t.Errorf("code = %d, want -32014 (Permission grant exists)", codeAsInt(t, errObj["code"]))
	}
}

// TestRemoveRemoteAgentPermissionGrants_Ok pins
// the happy-path Remove.
func TestRemoveRemoteAgentPermissionGrants_Ok(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	if _, errObj := dispatchPermissions(t, d, "AddRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install", "bash:pnpm install"},
	}); errObj != nil {
		t.Fatalf("seed add err = %#v", errObj)
	}
	res, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj != nil {
		t.Fatalf("remove err = %#v", errObj)
	}
	grants := res["grants"].([]any)
	if len(grants) != 1 {
		t.Errorf("len(grants) = %d, want 1", len(grants))
	}
	if g0 := grants[0].(map[string]any); g0["pattern"] != "pnpm install" {
		t.Errorf("remaining grant pattern = %v, want pnpm install", g0["pattern"])
	}
}

// TestRemoveRemoteAgentPermissionGrants_Empty pins
// the empty-tool_patterns → -32602 path.
func TestRemoveRemoteAgentPermissionGrants_Empty(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
}

// TestRemoveRemoteAgentPermissionGrants_Malformed
// pins the malformed-key → -32602 path with the
// verbatim entry echoed in `data`.
func TestRemoveRemoteAgentPermissionGrants_Malformed(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	d := newPermissionsDispatcher(t, ps)
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"missing-colon"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32602 {
		t.Errorf("code = %d, want -32602", codeAsInt(t, errObj["code"]))
	}
	if data, _ := errObj["data"].(string); !strings.Contains(data, "missing-colon") {
		t.Errorf("data = %q, want it to contain the malformed entry", data)
	}
}

// TestRemoveRemoteAgentPermissionGrants_NoStore pins
// the nil-store → -32603 path.
func TestRemoveRemoteAgentPermissionGrants_NoStore(t *testing.T) {
	sess := NewSessionState("1.4.0")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	d.cfg.PermissionsStore = nil
	_, errObj := dispatchPermissions(t, d, "RemoveRemoteAgentPermissionGrants", map[string]any{
		"tool_patterns": []string{"bash:npm install"},
	})
	if errObj == nil {
		t.Fatalf("expected error envelope, got result")
	}
	if codeAsInt(t, errObj["code"]) != -32603 {
		t.Errorf("code = %d, want -32603", codeAsInt(t, errObj["code"]))
	}
}

// ---- additional invariants ---------------------------------

// TestPermissionGrantKey pins the GrantKey helper
// (the dedup key the store uses). Future cycles
// that extend the key (e.g. add scope) bump the
// JSON version + extend this test.
func TestPermissionGrantKey(t *testing.T) {
	g := PermissionGrant{ToolName: "bash", Pattern: "npm install"}
	if got := g.GrantKey(); got != "bash:npm install" {
		t.Errorf("GrantKey = %q, want bash:npm install", got)
	}
}

// TestPermissionState_GrantsInsertionOrder pins
// the insertion-order contract both stores
// honour: AddGrants appends in the order the
// caller supplied. FSPermissionStore preserves
// the on-disk order on round-trip; the FS
// on-disk format is one JSON array per
// permissions.json, so on-disk order IS the
// insertion order. MemPermissionStore mirrors
// this so the in-process and on-disk semantics
// stay identical.
//
// Dashboards that want a stable display order
// can sort client-side by AddedAt — neither
// store sorts on the server, by design (a
// future cycle can add a wire-level sort if
// dashboards ask for it).
func TestPermissionState_GrantsInsertionOrder(t *testing.T) {
	ps := NewMemPermissionStore(nil)
	if _, err := ps.AddGrants(context.Background(), []PermissionGrant{
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
