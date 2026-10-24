package skillsui_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/skillsui"
)

func writeSkillInDir(t *testing.T, root, subdir, body string) string {
	t.Helper()
	dir := filepath.Join(root, subdir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "SKILL.md")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDedup_SameSkillViaPluginAndProject(t *testing.T) {
	body := "---\nname: chrome-devtools\ndescription: CDP body\n---\nCDP instructions.\n"
	root := t.TempDir()
	pluginPath := writeSkillInDir(t, root, "chrome-devtools-plugin/skills/chrome-devtools", body)
	sharedPath := writeSkillInDir(t, root, ".claude/skills/chrome-devtools", body)

	in := []skillsui.SkillInfo{
		{Name: "chrome-devtools-plugin:chrome-devtools", Description: "CDP body",
			Path: pluginPath, Plugin: "chrome-devtools-plugin", ModelInvocable: true},
		{Name: "chrome-devtools", Description: "CDP body",
			Path: sharedPath, ModelInvocable: true},
	}

	out := skillsui.DedupeSkillInfosByHash(in)
	if len(out) != 1 {
		t.Fatalf("dedupe collapsed %d → %d entries, want 1", len(in), len(out))
	}
	if out[0].Name != in[0].Name {
		t.Errorf("expected %q, got %q", in[0].Name, out[0].Name)
	}
}

func TestDedup_DifferentContentBothKept(t *testing.T) {
	root := t.TempDir()
	p1 := writeSkillInDir(t, root, "a/skills/test", "content 1")
	p2 := writeSkillInDir(t, root, "b/skills/test", "content 2")

	in := []skillsui.SkillInfo{
		{Name: "test-1", Path: p1},
		{Name: "test-2", Path: p2},
	}

	out := skillsui.DedupeSkillInfosByHash(in)
	if len(out) != 2 {
		t.Fatalf("expected 2 distinct skills kept, got %d", len(out))
	}
}
