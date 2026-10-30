// Package builtinskills carries the skill bundle the Antigravity CLI ships
// inside its binary and unpacks into the config home.
//
// The reference embeds two asset trees under assets/ in its //go:embed file
// system (【一手】, recovered from the agy 1.4.7 binary):
//
//	assets/external/skills/agy-customizations   (SKILL.md + docs/)
//	assets/external/skills/antigravity_guide    (SKILL.md + references/)
//	assets/shared/skills/{automation,generative_ui,migrate-workflows,
//	                      permissioned-github,plugin,run-workflow,
//	                      ui-plugin-navigation}   (SKILL.md each)
//
// and unpacks all nine into <AppDataDir>/builtin/skills/ — the tree the
// clone's skill discovery reads and the reference's <skills> section
// advertises. The installed layout is flat: the external/shared split is an
// asset-path detail and does not survive the unpacking.
//
// The files under skills/ are the reference's, recovered verbatim from the
// binary; see skills/PROVENANCE.md.
//
// Which of the nine appear in the prompt is a separate question. The
// reference advertises only agy-customizations and antigravity_guide in the
// CLI's <skills> section — the seven shared skills sit behind server-side
// feature flags — and agyprompt.BuiltinSkillDirs is that allowlist.
//
// # Installing
//
// Install never overwrites a file that is already there. The reference and
// the clone share one config home (~/.gemini/antigravity-cli), and the
// reference unpacks its own bundle there; the clone filling in what is
// missing keeps a machine that has both working, and a machine that has only
// the clone still gets the skills. The reference re-unpacks on a version
// change (it keeps a builtin/.checksum file); the clone does not, so a
// reference upgrade is never clobbered by the clone.
package skills

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed skills
var skillFS embed.FS

// Dirs returns the names of the bundled skill directories, sorted.
func Dirs() []string {
	entries, err := fs.ReadDir(skillFS, "skills")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Install unpacks the bundle into <appDataDir>/builtin/skills and returns
// the number of files it wrote. Files that already exist are left alone.
//
// The destination is the reference's own tree, so the reference's install
// wins over the clone's — see the package comment.
func Install(appDataDir string) (int, error) {
	if appDataDir == "" {
		return 0, nil
	}
	root := filepath.Join(appDataDir, "builtin", "skills")
	written := 0
	err := fs.WalkDir(skillFS, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "skills" {
			return nil // the bundle root is not a skill directory
		}
		rel := strings.TrimPrefix(p, "skills/")
		target := filepath.Join(root, filepath.FromSlash(rel))
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		body, err := skillFS.ReadFile(p)
		if err != nil {
			return err
		}
		if _, err := os.Stat(target); err == nil {
			return nil // the reference's copy wins
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(target, body, 0o644); err != nil {
			return err
		}
		written++
		return nil
	})
	return written, err
}
