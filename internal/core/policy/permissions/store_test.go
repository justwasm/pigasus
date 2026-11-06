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

func TestParseToolPattern(t *testing.T) {
	cases := []struct {
		in          string
		wantTool    string
		wantPattern string
		wantOK      bool
	}{
		{"bash:npm install", "bash", "npm install", true},
		{"write_file:*.go", "write_file", "*.go", true},
		{"", "", "", false},
		{"no-colon-here", "", "", false},
		{":pattern-only", "", "", false},
		{"tool-only:", "", "", false},
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

func TestPreset_ToolPermission(t *testing.T) {
	if got := PresetStrict.ToolPermission(); got != ToolPermissionStrict {
		t.Errorf("PresetStrict.ToolPermission() = %q, want %q", got, ToolPermissionStrict)
	}
	if got := PresetPermissive.ToolPermission(); got != ToolPermissionAlwaysProceed {
		t.Errorf("PresetPermissive.ToolPermission() = %q, want %q", got, ToolPermissionAlwaysProceed)
	}
	if got := PresetDefault.ToolPermission(); got != ToolPermissionRequestReview {
		t.Errorf("PresetDefault.ToolPermission() = %q, want %q", got, ToolPermissionRequestReview)
	}
	if got := PresetFromToolPermission(ToolPermissionStrict); got != PresetStrict {
		t.Errorf("PresetFromToolPermission(ToolPermissionStrict) = %q, want %q", got, PresetStrict)
	}
	if got := PresetFromToolPermission(ToolPermissionAlwaysProceed); got != PresetPermissive {
		t.Errorf("PresetFromToolPermission(ToolPermissionAlwaysProceed) = %q, want %q", got, PresetPermissive)
	}
}

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

func TestMemPermissionStore_RemoveMalformed(t *testing.T) {
	ps := NewMemStore(nil)
	_, err := ps.RemoveGrants(context.Background(), []string{"missing-colon"})
	if !errors.Is(err, ErrGrantMalformed) {
		t.Fatalf("malformed key err = %v, want ErrGrantMalformed", err)
	}
}

func TestFSPermissionStore_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "permissions.json")
	store := NewFSStore(path, nil)
	ctx := context.Background()

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

	// Test ToRules and ToSettings integration
	rules := state.ToRules()
	if len(rules) != 2 {
		t.Errorf("len(rules) = %d, want 2", len(rules))
	}
	settings := state.ToSettings()
	if len(settings.Allow) != 2 {
		t.Errorf("len(settings.Allow) = %d, want 2", len(settings.Allow))
	}
}

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

func TestPermissionGrantKey(t *testing.T) {
	g := Grant{ToolName: "bash", Pattern: "npm install"}
	if got := g.GrantKey(); got != "bash:npm install" {
		t.Errorf("GrantKey = %q, want bash:npm install", got)
	}
	actionGrant := Grant{Action: "command", Pattern: "git status"}
	if got := actionGrant.GrantKey(); got != "command(git status)" {
		t.Errorf("GrantKey = %q, want command(git status)", got)
	}
}
