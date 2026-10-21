// Tests for the cmd/wagyu seam that the Remote Control browser
// approval flow calls. The package-internal contract is pinned by
// internal/permissions/store/store_test.go; this file covers the
// TUI-facing wrapper in cmd/wagyu/always_allow_aas.go.
//
// The seam is small — it parses the rule string and delegates to the
// store — but the tests guard against a regression where a TUI
// approval mistakenly routes through the project writer, which is
// the exact failure mode the AGY 1.3.4 line 40 fix targets.

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/permissions/store"
)

// withSharedHome points ANTIGRAVITY_HOME at a fresh directory so the
// store writer hits a temp file rather than the user's real
// shared.json. Returns the path the writer will use.
func withSharedHome(t *testing.T) string {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	return store.DefaultPath()
}

// TestAlwaysAllowAAS_RemoteControlRoutesToShared pins the AGY 1.3.4
// line 40 contract from the TUI's perspective: an approval with
// SourceRemoteControl writes to shared.json only, and a sentinel
// project config stays untouched.
func TestAlwaysAllowAAS_RemoteControlRoutesToShared(t *testing.T) {
	shared := withSharedHome(t)
	project := filepath.Join(t.TempDir(), "default-cli-project.json")
	// Sentinel project file. The test fails loudly if AlwaysAllowFromSource
	// ever touches the project writer — it must take the shared-only
	// path for SourceRemoteControl.
	before := `{"permissionGrants":{"permissionGrants":{"allow":["stale:rule"]}}}`
	if err := os.WriteFile(project, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllowFromSource(store.SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatalf("AlwaysAllowFromSource: %v", err)
	}

	cfg, err := store.Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("Allow = %+v, want one entry", cfg.Allow)
	}
	if cfg.Allow[0].Source != store.SourceRemoteControl {
		t.Errorf("Source = %q, want %q", cfg.Allow[0].Source, store.SourceRemoteControl)
	}

	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file vanished: %v", err)
	}
	if string(data) != before {
		t.Errorf("project file changed:\nbefore: %s\nafter:  %s", before, data)
	}
}

// TestAlwaysAllowAAS_LocalDoesNotTouchProject covers the existing
// behaviour: a Local approval routes through the shared store, the
// project config stays untouched. This is what the TUI's own
// /permissions panel sees when a user picks the Shared scope.
func TestAlwaysAllowAAS_LocalDoesNotTouchProject(t *testing.T) {
	shared := withSharedHome(t)
	project := filepath.Join(t.TempDir(), "default-cli-project.json")
	before := `{"sentinel": true}`
	if err := os.WriteFile(project, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllowFromSource(store.SourceLocal, "view_file"); err != nil {
		t.Fatalf("AlwaysAllowFromSource: %v", err)
	}
	cfg, err := store.Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Source != store.SourceLocal {
		t.Fatalf("Allow = %+v, want one Local entry", cfg.Allow)
	}
	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file vanished: %v", err)
	}
	if !strings.Contains(string(data), `"sentinel": true`) {
		t.Errorf("project file rewritten by Local approval: %s", data)
	}
}

// TestAlwaysAllowAAS_BrowserEditorRemoveCoversPath: the browser's
// rules editor calls RemoveAllowFromSource with SourceRemoteControl;
// the rule must be gone from shared.json afterwards. This is the
// "deletion takes effect" half of the AGY 1.3.4 fix.
func TestAlwaysAllowAAS_BrowserEditorRemoveCoversPath(t *testing.T) {
	shared := withSharedHome(t)
	if err := AlwaysAllowFromSource(store.SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatal(err)
	}
	if err := AlwaysAllowFromSource(store.SourceRemoteControl, "view_file"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllowFromSource(store.SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatalf("RemoveAllowFromSource: %v", err)
	}
	cfg, err := store.Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Rule.Pattern != "view_file" {
		t.Errorf("Allow = %+v, want the single view_file survivor", cfg.Allow)
	}
}

// TestAlwaysAllowAAS_MultipleApprovalsAccumulate covers the sequence
// of mixed-source approvals: every rule lands in the shared config
// in call order, and no rule is dropped.
func TestAlwaysAllowAAS_MultipleApprovalsAccumulate(t *testing.T) {
	shared := withSharedHome(t)
	steps := []struct {
		src  store.Source
		rule string
	}{
		{store.SourceLocal, "view_file"},
		{store.SourceRemoteControl, "run_command:rm"},
		{store.SourceLocal, "grep_search:src/"},
		{store.SourceRemoteControl, "list_dir"},
	}
	for _, s := range steps {
		if err := AlwaysAllowFromSource(s.src, s.rule); err != nil {
			t.Fatalf("AlwaysAllowFromSource(%v): %v", s.rule, err)
		}
	}
	cfg, err := store.Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != len(steps) {
		t.Fatalf("Allow len = %d, want %d", len(cfg.Allow), len(steps))
	}
	for i, s := range steps {
		got := cfg.Allow[i]
		if got.Source != s.src {
			t.Errorf("[%d] Source = %q, want %q", i, got.Source, s.src)
		}
		want := store.ParseRule(s.rule)
		if got.Rule != want {
			t.Errorf("[%d] Rule = %+v, want %+v", i, got.Rule, want)
		}
	}
}

// TestAlwaysAllowAAS_EmptyRuleRejected pins the recoverable error:
// an empty rule returns an error rather than silently writing a
// zero-value entry that would later match every request.
func TestAlwaysAllowAAS_EmptyRuleRejected(t *testing.T) {
	withSharedHome(t)
	if err := AlwaysAllowFromSource(store.SourceRemoteControl, "  "); err == nil {
		t.Error("empty rule accepted; want error")
	}
}

// TestAlwaysAllowAAS_UsesAntigravityHome pins the home-resolution
// contract: when ANTIGRAVITY_HOME is set, the writer hits that
// directory rather than $HOME. The other auth.* writers (settings,
// jetbox_state, etc.) follow the same contract; this test guards
// against a regression where the shared writer accidentally uses
// os.UserHomeDir() directly and bypasses $ANTIGRAVITY_HOME.
func TestAlwaysAllowAAS_UsesAntigravityHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	want := filepath.Join(home, "shared.json")
	if got := store.DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
	// And the auth writer agrees — both point at the same tree so
	// the shared config sits next to settings.json.
	if got := auth.AntigravityConfigHome(); got != home {
		t.Errorf("AntigravityConfigHome() = %q, want %q", got, home)
	}
}
