// Package pigasusskills embeds pigasus-only built-in skills (such as the
// terminal skill) and unpacks them into the pigasus configuration home
// (~/.openagy/builtin-skills) so reference parity tests on
// ~/.gemini/antigravity-cli/builtin/skills remain untouched.
package pigasusskills

import (
	"bytes"
	"embed"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/configdir"
)

//go:embed skills
var skillFS embed.FS

// SkillsDir returns the unpacked pigasus built-in skills directory under configHome.
func SkillsDir(configHome string) string {
	if strings.TrimSpace(configHome) == "" {
		return ""
	}
	return filepath.Join(configHome, "builtin-skills")
}

// Dirs returns the sorted names of embedded pigasus built-in skill directories.
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

// Install unpacks the embedded pigasus built-in skills into
// <configHome>/builtin/skills and returns the number of files written or updated.
// Because this directory is owned exclusively by pigasus, files whose contents
// differ from the embedded bundle are updated to stay in sync with the binary.
func Install(configHome string) (int, error) {
	root := SkillsDir(configHome)
	if root == "" {
		return 0, nil
	}
	written := 0
	err := fs.WalkDir(skillFS, "skills", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "skills" {
			return nil
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
		if existing, err := os.ReadFile(target); err == nil && bytes.Equal(existing, body) {
			return nil
		}
		if err := configdir.RefuseTestWrite(target); err != nil {
			return err
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
