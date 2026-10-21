package agyprompt

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// BuiltinSkillDirs is the subset of the shipped builtin bundle that appears in
// the prompt and in `/skills`. The bundle contains nine directories; the other
// seven are internal and have no marker that distinguishes them, so the set is
// empirical (1.4.2/1.4.3 captures).
var BuiltinSkillDirs = map[string]bool{
	"agy-customizations": true,
	"antigravity_guide":  true,
}

// DiscoverSkills lists the skills under each root, one level deep, then sorts
// the merged list by name. The reference's <skills> section is globally
// alphabetical across roots (captured 1.4.2/1.4.3 prompts); concatenating
// per-root ReadDir order would group builtin, then plugins, and diverge.
// It mirrors cmd/wagyu's printSlashSkillInfos: a directory without a readable
// SKILL.md frontmatter is skipped, and a frontmatter without a name falls
// back to the directory name.
//
// The roots are the ones whose skills appear in the prompt: the builtin
// bundle (allowlisted by BuiltinSkillDirs), <workspace>/.agents/skills, and
// each loadable plugin's skills/ directory. The shared ~/.gemini/skills
// library is listed by `/skills` but is NOT in the prompt (verified against
// 1.4.3); pigrt.AgySkillPaths computes the prompt roots.
func DiscoverSkills(roots []string) []Skill {
	var out []Skill
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		builtin := filepath.Base(root) == "skills" && filepath.Base(filepath.Dir(root)) == "builtin"
		for _, entry := range entries {
			if builtin && !BuiltinSkillDirs[entry.Name()] {
				continue
			}
			dir := filepath.Join(root, entry.Name())
			info, err := os.Stat(dir)
			if err != nil || !info.IsDir() {
				continue
			}
			skillMD := filepath.Join(dir, "SKILL.md")
			name, description, ok := skillFrontmatter(skillMD)
			if !ok {
				continue
			}
			if name == "" {
				name = entry.Name()
			}
			out = append(out, Skill{Name: name, Path: skillMD, Description: description})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// skillFrontmatter reads the YAML block between the two `---` fences and
// returns its name and description. Block scalars (`>`, `|`, with chomping
// modifiers) are folded by the YAML decoder; descriptions are trimmed because
// agy prints them without the block scalar's trailing newline.
func skillFrontmatter(path string) (name, description string, ok bool) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", "", false
	}
	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) == 0 || !bytes.Equal(bytes.TrimSpace(lines[0]), []byte("---")) {
		return "", "", false
	}
	close := -1
	for i := 1; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("---")) {
			close = i
			break
		}
	}
	if close < 0 {
		return "", "", false
	}
	var doc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(bytes.Join(lines[1:close], []byte("\n")), &doc); err != nil {
		return "", "", false
	}
	return doc.Name, strings.TrimSpace(doc.Description), true
}
