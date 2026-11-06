package agyprompt

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
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
// It mirrors the skills feature's Discover (internal/features/tools/skills):
// a directory without a readable SKILL.md frontmatter is skipped, and a
// frontmatter without a name falls back to the directory name.
//
// The roots are the ones whose skills appear in the prompt: the builtin
// bundle (allowlisted by BuiltinSkillDirs), the shared ~/.gemini/skills
// library, <workspace>/.agents/skills, and each loadable plugin's skills/
// directory. AgySkillPaths computes the prompt roots.
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

// AgySkillPaths returns the skill directories whose entries appear in the
// system prompt, given the agy config home (`~/.gemini/antigravity-cli`, or
// $ANTIGRAVITY_HOME) and the workspace.
func AgySkillPaths(agentHome, workspace string) []string {
	if agentHome == "" {
		return nil
	}
	gemini := filepath.Dir(agentHome) // ~/.gemini
	candidates := []string{
		filepath.Join(agentHome, "builtin", "skills"),
		filepath.Join(gemini, "skills"),
		filepath.Join(agentHome, "skills"),
	}
	if workspace != "" {
		candidates = append(candidates,
			filepath.Join(workspace, ".agents", "skills"),
			filepath.Join(workspace, ".agent", "skills"),
		)
	}
	candidates = append(candidates, pluginSkillDirs(
		filepath.Join(gemini, "config", "plugins"),
		filepath.Join(gemini, "config", "config.json"))...)
	if workspace != "" {
		candidates = append(candidates, pluginSkillDirs(
			filepath.Join(workspace, ".agents", "plugins"), "")...)
	}
	var out []string
	for _, dir := range candidates {
		if isDir(dir) {
			out = append(out, dir)
		}
	}
	return out
}

func pluginSkillDirs(pluginsDir, configPath string) []string {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil
	}
	enabled := plugins.Enabled(configPath)
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		raw, err := os.ReadFile(filepath.Join(dir, "plugin.json"))
		if err != nil {
			continue
		}
		var manifest any
		if err := json.Unmarshal(raw, &manifest); err != nil {
			continue
		}
		if on, ok := enabled[e.Name()]; ok && !on {
			continue
		}
		out = append(out, plugins.SkillDirs(dir)...)
	}
	return out
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// DefaultAgentHome is the reference's config directory:
// $ANTIGRAVITY_HOME, else $HOME/.gemini/antigravity-cli.
func DefaultAgentHome() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	if h, err := os.UserHomeDir(); err == nil {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}
