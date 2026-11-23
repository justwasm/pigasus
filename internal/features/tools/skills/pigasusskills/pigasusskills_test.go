package pigasusskills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
)

func TestInstallAndDiscoverTerminalSkill(t *testing.T) {
	dirs := Dirs()
	if len(dirs) != 1 || dirs[0] != "terminal" {
		t.Fatalf("Dirs() = %v, want [terminal]", dirs)
	}

	home := t.TempDir()
	written, err := Install(home)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if written != 1 {
		t.Fatalf("written = %d, want 1", written)
	}

	// Second install is a no-op when content matches.
	written2, err := Install(home)
	if err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if written2 != 0 {
		t.Fatalf("second written = %d, want 0", written2)
	}

	// Stale content is updated to match the embedded binary bundle.
	skillPath := filepath.Join(SkillsDir(home), "terminal", "SKILL.md")
	if err := os.WriteFile(skillPath, []byte("stale"), 0o644); err != nil {
		t.Fatal(err)
	}
	written3, err := Install(home)
	if err != nil {
		t.Fatalf("third Install: %v", err)
	}
	if written3 != 1 {
		t.Fatalf("third written = %d, want 1", written3)
	}

	discovered := agyprompt.DiscoverSkills([]string{SkillsDir(home)})
	if len(discovered) != 1 || discovered[0].Name != "terminal" {
		t.Fatalf("DiscoverSkills = %+v, want terminal", discovered)
	}
	if !strings.Contains(discovered[0].Description, "terminal") {
		t.Fatalf("unexpected description: %q", discovered[0].Description)
	}
}
