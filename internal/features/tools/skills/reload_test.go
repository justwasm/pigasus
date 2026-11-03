// reload_test.go — the `/skills reload` verb: the re-scan from disk and
// the best-effort rule.
//
// Cycle 383, slice A, finding A-27: the clone ignored the argument of
// `/skills` and always printed the same listing. The reference's own
// string table carries "Reload skills from disk" next to "/skills
// reload" 【strings】.

package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// skillsHome points the discovery at a temporary home and returns it.
//
// The discovery reads confhome.Antigravity() at call time, so the
// environment is enough to isolate it. The loader's state is reset as
// well: it is process-wide, and a test must not inherit another test's
// roots.
func skillsHome(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	home := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", home)
	resetLoader(t)
	return home
}

// resetLoader empties the loader's state and restores it after the test.
func resetLoader(t *testing.T) {
	t.Helper()
	skillsLoader.mu.Lock()
	skillsLoader.roots = nil
	skillsLoader.mu.Unlock()
	t.Cleanup(func() {
		skillsLoader.mu.Lock()
		skillsLoader.roots = nil
		skillsLoader.mu.Unlock()
	})
}

// writeSkill writes a SKILL.md into the shared ~/.gemini/skills library.
func writeSkill(t *testing.T, home, dir, name, description string) {
	t.Helper()
	skillDir := filepath.Join(filepath.Dir(home), "skills", dir)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: " + name + "\ndescription: " + description + "\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestReloadReadsTheRootsFromDisk pins the verb's job: the reload reads
// the skill roots again, so a skill written after the previous listing
// shows up.
func TestReloadReadsTheRootsFromDisk(t *testing.T) {
	home := skillsHome(t)
	writeSkill(t, home, "demo", "demo", "A demo skill.")

	list := Reload()
	if len(list) != 1 || list[0].Name != "demo" {
		t.Fatalf("Reload() = %+v, want one skill named demo", list)
	}

	writeSkill(t, home, "second", "second", "Another skill.")
	list = Reload()
	if len(list) != 2 {
		t.Fatalf("Reload() = %+v, want two skills", list)
	}
	if list[0].Name != "demo" || list[1].Name != "second" {
		t.Errorf("Reload() = %q, %q; want demo, second", list[0].Name, list[1].Name)
	}
}

// TestReloadKeepsTheLoadedSkillsWhenARootCannotBeRead pins the
// best-effort rule: a root that cannot be read keeps the skills that were
// loaded from it, so a read error never drops a loaded skill.
func TestReloadKeepsTheLoadedSkillsWhenARootCannotBeRead(t *testing.T) {
	home := skillsHome(t)
	writeSkill(t, home, "demo", "demo", "A demo skill.")
	if got := Reload(); len(got) != 1 {
		t.Fatalf("the first Reload() = %+v, want one skill", got)
	}

	// The shared library goes away between the two reloads.
	if err := os.RemoveAll(filepath.Join(filepath.Dir(home), "skills")); err != nil {
		t.Fatal(err)
	}
	got := Reload()
	if len(got) != 1 || got[0].Name != "demo" {
		t.Fatalf("Reload() after the root went away = %+v, want the loaded skill to survive", got)
	}
}

// TestReloadOnAMissingHomeDoesNotPanic pins that an unreadable tree is
// an empty tree, not a crash.
func TestReloadOnAMissingHomeDoesNotPanic(t *testing.T) {
	resetLoader(t)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(t.TempDir(), "nowhere"))
	got := Reload()
	if len(got) != 0 {
		t.Errorf("Reload() on a missing home = %+v, want no skills", got)
	}
}

// TestIsReload pins the verb's spelling rule: the verb is matched
// case-insensitively and after trimming.
func TestIsReload(t *testing.T) {
	for _, in := range []string{"reload", "RELOAD", "Reload", " reload "} {
		if !IsReload(in) {
			t.Errorf("IsReload(%q) = false, want true", in)
		}
	}
	for _, in := range []string{"", " ", "reloadall", "bogus", "reload skills"} {
		if IsReload(in) {
			t.Errorf("IsReload(%q) = true, want false", in)
		}
	}
}
