package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
)

func withSharedHome(t *testing.T) string {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	return DefaultPath()
}

func TestAlwaysAllowAAS_RemoteControlRoutesToShared(t *testing.T) {
	shared := withSharedHome(t)
	project := filepath.Join(t.TempDir(), "default-cli-project.json")
	before := `{"permissionGrants":{"permissionGrants":{"allow":["stale:rule"]}}}`
	if err := os.WriteFile(project, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllowFromSource(SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatalf("AlwaysAllowFromSource: %v", err)
	}

	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 {
		t.Fatalf("Allow = %+v, want one entry", cfg.Allow)
	}
	if cfg.Allow[0].Source != SourceRemoteControl {
		t.Errorf("Source = %q, want %q", cfg.Allow[0].Source, SourceRemoteControl)
	}

	data, err := os.ReadFile(project)
	if err != nil {
		t.Fatalf("project file vanished: %v", err)
	}
	if string(data) != before {
		t.Errorf("project file changed:\nbefore: %s\nafter:  %s", before, data)
	}
}

func TestAlwaysAllowAAS_LocalDoesNotTouchProject(t *testing.T) {
	shared := withSharedHome(t)
	project := filepath.Join(t.TempDir(), "default-cli-project.json")
	before := `{"sentinel": true}`
	if err := os.WriteFile(project, []byte(before), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := AlwaysAllowFromSource(SourceLocal, "view_file"); err != nil {
		t.Fatalf("AlwaysAllowFromSource: %v", err)
	}
	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Source != SourceLocal {
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

func TestAlwaysAllowAAS_BrowserEditorRemoveCoversPath(t *testing.T) {
	shared := withSharedHome(t)
	if err := AlwaysAllowFromSource(SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatal(err)
	}
	if err := AlwaysAllowFromSource(SourceRemoteControl, "view_file"); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAllowFromSource(SourceRemoteControl, "run_command:rm"); err != nil {
		t.Fatalf("RemoveAllowFromSource: %v", err)
	}
	cfg, err := Load(shared)
	if err != nil {
		t.Fatalf("store.Load: %v", err)
	}
	if len(cfg.Allow) != 1 || cfg.Allow[0].Rule.Pattern != "view_file" {
		t.Errorf("Allow = %+v, want the single view_file survivor", cfg.Allow)
	}
}

func TestAlwaysAllowAAS_MultipleApprovalsAccumulate(t *testing.T) {
	shared := withSharedHome(t)
	steps := []struct {
		src  Source
		rule string
	}{
		{SourceLocal, "view_file"},
		{SourceRemoteControl, "run_command:rm"},
		{SourceLocal, "grep_search:src/"},
		{SourceRemoteControl, "list_dir"},
	}
	for _, s := range steps {
		if err := AlwaysAllowFromSource(s.src, s.rule); err != nil {
			t.Fatalf("AlwaysAllowFromSource(%v): %v", s.rule, err)
		}
	}
	cfg, err := Load(shared)
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
		want := ParseRule(s.rule)
		if got.Rule != want {
			t.Errorf("[%d] Rule = %+v, want %+v", i, got.Rule, want)
		}
	}
}

func TestAlwaysAllowAAS_EmptyRuleRejected(t *testing.T) {
	withSharedHome(t)
	if err := AlwaysAllowFromSource(SourceRemoteControl, "  "); err == nil {
		t.Error("empty rule accepted; want error")
	}
}

func TestAlwaysAllowAAS_UsesAntigravityHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	want := filepath.Join(home, "shared.json")
	if got := DefaultPath(); got != want {
		t.Errorf("DefaultPath() = %q, want %q", got, want)
	}
	if got := auth.AntigravityConfigHome(); got != home {
		t.Errorf("AntigravityConfigHome() = %q, want %q", got, home)
	}
}
