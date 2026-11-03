// print_slash_skills_test.go — the print path's `/skills reload` verb.
//
// Cycle 383, slice A, finding A-27: the print path ignored the argument
// of `/skills` and always printed the same listing. The reference's own
// string table carries the literal "Reload skills from disk" next to
// "/skills reload" 【strings】.

package printmode

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// printSkillsHome points the skill discovery at a temporary home with one
// skill in the shared ~/.gemini/skills library.
func printSkillsHome(t *testing.T) {
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
}

// TestPrintSlashSkillsReload pins the verb in the print path: the
// reference's own line, then the reloaded listing.
func TestPrintSlashSkillsReload(t *testing.T) {
	printSkillsHome(t)
	fs := newStandardFlagSetForTest(t, nil)
	res := dispatchPrintSlash("/skills reload", fs)
	if !res.Handled {
		t.Fatal("/skills reload was not handled")
	}
	if !strings.HasPrefix(res.Output, "Reload skills from disk\n") {
		t.Errorf("/skills reload did not answer the reference's line; output:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "demo") {
		t.Errorf("/skills reload did not list the reloaded skills; output:\n%s", res.Output)
	}
}

// TestPrintSlashSkillsBare pins that the bare command still prints the
// listing, without the reload's line.
func TestPrintSlashSkillsBare(t *testing.T) {
	printSkillsHome(t)
	fs := newStandardFlagSetForTest(t, nil)
	res := dispatchPrintSlash("/skills", fs)
	if !res.Handled {
		t.Fatal("/skills was not handled")
	}
	if strings.Contains(res.Output, "Reload skills from disk") {
		t.Errorf("the bare command answered the reload's line; output:\n%s", res.Output)
	}
	if !strings.Contains(res.Output, "demo") {
		t.Errorf("the bare command did not list the skills; output:\n%s", res.Output)
	}
}
