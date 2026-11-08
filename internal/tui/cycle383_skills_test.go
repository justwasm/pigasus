// cycle383_skills_test.go — finding A-27: `/skills reload` re-reads the
// skills from disk.
//
// Cycle 383, slice A: official-docs/cli/reference.md:47 documents the
// verb — "Run `/skills reload` to reload skills from disk" — and the
// binary's string table carries the literal "Reload skills from disk"
// next to "/skills reload" 【strings】.
//
// The bare `/skills` is unchanged: it opens the /skills panel, and the
// handler's own listing keeps the bytes it had.

package tui

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/rows"
)

// skillsHome points the skill discovery at a temporary home with one
// skill in the shared ~/.gemini/skills library, and returns the home.
//
// The discovery reads the home at call time, so the environment is enough
// to isolate it.
func skillsHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, ".gemini", "antigravity-cli")
	skillDir := filepath.Join(root, ".gemini", "skills", "demo")
	for _, dir := range []string{home, skillDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	body := "---\nname: demo\ndescription: A demo skill.\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", home)
	return home
}

// TestSlashSkillsReloadAnswersTheReferenceLine pins A-27 at the host's
// seam: `/skills reload` answers the reference's own line and lists the
// skills it re-read.
func TestSlashSkillsReloadAnswersTheReferenceLine(t *testing.T) {
	skillsHome(t)
	m := newSlashModel(t)
	handled, _ := m.dispatchSlashLine("/skills reload")
	if !handled {
		t.Fatal("/skills reload was not handled")
	}
	got := termcolor.StripSGR(strings.Join(m.conversation.response, "\n"))
	if !strings.HasPrefix(got, "Reload skills from disk\n") {
		t.Errorf("/skills reload did not answer the reference's line; transcript:\n%s", got)
	}
	if !strings.Contains(got, "demo") {
		t.Errorf("/skills reload did not list the reloaded skills; transcript:\n%s", got)
	}
}

// TestSlashSkillsBareStillOpensThePanel pins that the bare command is
// unchanged: `/skills` opens the /skills panel and writes nothing to the
// transcript.
func TestSlashSkillsBareStillOpensThePanel(t *testing.T) {
	skillsHome(t)
	m := newSlashModel(t)
	handled, _ := m.dispatchSlashLine("/skills")
	if !handled {
		t.Fatal("/skills was not handled")
	}
	if m.overlays.panel.kind != panelregistry.Skills {
		t.Errorf("the open panel = %v, want panelregistry.Skills", m.overlays.panel.kind)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("the bare command wrote %d transcript rows, want none", len(m.conversation.response))
	}
}

// TestSlashSkillsListingIsUnchanged pins that the handler's own listing
// keeps the bytes it had: the rows are the listing, rendered by the ui
// tier's row layout.
func TestSlashSkillsListingIsUnchanged(t *testing.T) {
	skillsHome(t)
	m := newSlashModel(t)
	handled, _ := m.slashSkills("")
	if !handled {
		t.Fatal("/skills was not handled")
	}
	want := rows.TabAlignedRows(skills.Render())
	got := afterEcho(m)
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the listing = %q, want %q", got, want)
	}
}

// TestSlashSkillsReloadKeepsTheLoadedSkills pins the best-effort rule at
// the host's seam: a reload over a root that cannot be read does not lose
// the skills that were loaded from it, and it does not panic.
func TestSlashSkillsReloadKeepsTheLoadedSkills(t *testing.T) {
	home := skillsHome(t)
	m := newSlashModel(t)
	if handled, _ := m.dispatchSlashLine("/skills reload"); !handled {
		t.Fatal("the first /skills reload was not handled")
	}
	// The shared library goes away between the two reloads.
	if err := os.RemoveAll(filepath.Join(filepath.Dir(home), "skills")); err != nil {
		t.Fatal(err)
	}
	m2 := newSlashModel(t)
	if handled, _ := m2.dispatchSlashLine("/skills reload"); !handled {
		t.Fatal("the second /skills reload was not handled")
	}
	got := termcolor.StripSGR(strings.Join(m2.conversation.response, "\n"))
	if !strings.Contains(got, "demo") {
		t.Errorf("the reload lost the loaded skill; transcript:\n%s", got)
	}
}
