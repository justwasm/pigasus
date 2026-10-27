package builtinskills

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// wantDirs is the reference's builtin bundle: the two external skills and the
// seven shared ones (【一手】, the reference's own install on this machine
// lists exactly these nine).
var wantDirs = []string{
	"agy-customizations",
	"antigravity_guide",
	"automation",
	"generative_ui",
	"migrate-workflows",
	"permissioned-github",
	"plugin",
	"run-workflow",
	"ui-plugin-navigation",
}

func TestDirsMatchTheReferenceBundle(t *testing.T) {
	got := Dirs()
	if strings.Join(got, ",") != strings.Join(wantDirs, ",") {
		t.Fatalf("bundle = %v, want %v", got, wantDirs)
	}
}

func TestInstallUnpacksTheBundle(t *testing.T) {
	root := t.TempDir()
	written, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if written == 0 {
		t.Fatal("Install wrote nothing")
	}

	skills := filepath.Join(root, "builtin", "skills")
	entries, err := os.ReadDir(skills)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	if strings.Join(names, ",") != strings.Join(wantDirs, ",") {
		t.Fatalf("installed %v, want %v", names, wantDirs)
	}

	// Every skill has the SKILL.md the reference's discovery reads.
	for _, name := range wantDirs {
		p := filepath.Join(skills, name, "SKILL.md")
		body, err := os.ReadFile(p)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if !strings.HasPrefix(string(body), "---") {
			t.Errorf("%s/SKILL.md has no frontmatter", name)
		}
	}
}

func TestInstallIsIdempotentAndKeepsExistingFiles(t *testing.T) {
	root := t.TempDir()
	if _, err := Install(root); err != nil {
		t.Fatal(err)
	}

	// A file the reference (or the user) already owns must survive.
	owned := filepath.Join(root, "builtin", "skills", "automation", "SKILL.md")
	if err := os.WriteFile(owned, []byte("---\nname: local\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	written, err := Install(root)
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 {
		t.Errorf("the second Install wrote %d files, want 0", written)
	}
	body, err := os.ReadFile(owned)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "---\nname: local\n---\n" {
		t.Errorf("the existing file was overwritten: %q", string(body))
	}
}

func TestInstallWithoutHomeIsANoOp(t *testing.T) {
	written, err := Install("")
	if err != nil || written != 0 {
		t.Fatalf("Install(\"\") = %d, %v; want 0, nil", written, err)
	}
}
