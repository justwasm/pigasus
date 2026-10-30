package core

import (
	"os"
	"path/filepath"
	"testing"
)

// The plugin.json "skills" field names the skill directories. It is an array
// of paths, or — since the reference's 1.4.5 fix — a single path string.
func TestSkillDirs_ManifestField(t *testing.T) {
	cases := []struct {
		name     string
		manifest string
		want     []string
	}{
		{
			name:     "absent falls back to skills/",
			manifest: `{"name":"demo"}`,
			want:     []string{"skills"},
		},
		{
			name:     "array of paths",
			manifest: `{"name":"demo","skills":["skills","extra-skills"]}`,
			want:     []string{"skills", "extra-skills"},
		},
		{
			name:     "single path string",
			manifest: `{"name":"demo","skills":"./skills/"}`,
			want:     []string{"./skills/"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(tc.manifest), 0o644); err != nil {
				t.Fatal(err)
			}
			got := SkillDirs(dir)
			if len(got) != len(tc.want) {
				t.Fatalf("SkillDirs = %v, want %v", got, tc.want)
			}
			for i, rel := range tc.want {
				want := filepath.Clean(filepath.Join(dir, rel))
				if got[i] != want {
					t.Errorf("SkillDirs[%d] = %q, want %q", i, got[i], want)
				}
			}
		})
	}
}

// A plugin without a plugin.json contributes the default skills/ directory —
// it is not loadable, but the helper must still answer.
func TestSkillDirs_NoManifest(t *testing.T) {
	dir := t.TempDir()
	got := SkillDirs(dir)
	want := filepath.Join(dir, "skills")
	if len(got) != 1 || got[0] != want {
		t.Fatalf("SkillDirs = %v, want [%q]", got, want)
	}
}

// An absolute path in the manifest is taken as is.
func TestSkillDirs_AbsolutePath(t *testing.T) {
	dir := t.TempDir()
	abs := t.TempDir()
	manifest := `{"name":"demo","skills":["` + abs + `"]}`
	if err := os.WriteFile(filepath.Join(dir, "plugin.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	got := SkillDirs(dir)
	if len(got) != 1 || got[0] != abs {
		t.Fatalf("SkillDirs = %v, want [%q]", got, abs)
	}
}
