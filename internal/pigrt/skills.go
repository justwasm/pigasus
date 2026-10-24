// skills.go — the skill roots that appear in the model's system prompt.
//
// The reference's <skills> section is built from the builtin bundle, the
// shared ~/.gemini/skills library, the workspace's .agents/skills, and every
// installed plugin's skills/ directory (workspace .agents/plugins and user
// ~/.gemini/config/plugins), then sorted globally by name. The shared
// ~/.gemini/skills library is listed by `/skills` as well — a live 1.4.6
// request carries all nine of this machine's shared skills in the prompt
// (【一手】, docs/cycles/reference-drift-146/).
//
// PiG's DefaultResourceLoader discovers skills from its own convention (a
// `skills/` directory under the agent dir and the project), so the runtime
// passes these roots as AdditionalSkillPaths instead. This package cannot
// import cmd/wagyu (package main), so the roots are restated here; keep this
// list in sync with internal/agyprompt.DiscoverSkills callers.
package pigrt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/plugins"
)

// AgySkillPaths returns the skill directories whose entries appear in the
// system prompt, given the agy config home (`~/.gemini/antigravity-cli`, or
// $ANTIGRAVITY_HOME — auth.AntigravityHome) and the workspace: the builtin
// bundle, <workspace>/.agents/skills, then each loaded plugin's skills/
// directory. The caller sorts the merged list by name.
//
// A plugin contributes only when it has a parseable plugin.json and, for the
// user scope, is not disabled by config.json. Only existing directories are
// returned: PiG's loader treats a missing path as an error condition worth
// logging, and most hosts have no plugins.
func AgySkillPaths(agentHome, workspace string) []string {
	if agentHome == "" {
		return nil
	}
	gemini := filepath.Dir(agentHome) // ~/.gemini
	candidates := []string{
		filepath.Join(agentHome, "builtin", "skills"),
		// The shared ~/.gemini/skills library IS in the reference's prompt.
		// The earlier "it is /skills-only" conclusion came from 1.4.2/1.4.3
		// captures taken on a machine whose shared library was empty; a live
		// 1.4.6 request lists all nine of this machine's shared skills
		// (agents-sdk, cloudflare, …) with their ~/.gemini/skills paths
		// (【一手】, docs/cycles/reference-drift-146/).
		filepath.Join(gemini, "skills"),
	}
	if workspace != "" {
		candidates = append(candidates, filepath.Join(workspace, ".agents", "skills"))
	}
	candidates = append(candidates, pluginSkillDirs(
		filepath.Join(gemini, "config", "plugins"),
		filepath.Join(gemini, "config", "config.json"))...)
	if workspace != "" {
		// Workspace plugins are not toggled by .agents/config.json (probed
		// 1.4.3), so no enablement file is passed.
		candidates = append(candidates, pluginSkillDirs(
			filepath.Join(workspace, ".agents", "plugins"), "")...)
	}
	out := candidates[:0:0]
	for _, dir := range candidates {
		if isDir(dir) {
			out = append(out, dir)
		}
	}
	return out
}

// pluginSkillDirs lists the skill directories of every loadable plugin.
// configPath, when non-empty, is a config.json whose plugins.<name>.enabled
// can disable one.
//
// The directories come from the plugin's plugin.json "skills" field when it
// has one — an array of paths or a single path string — and fall back to
// <plugin>/skills otherwise (plugins.SkillDirs).
func pluginSkillDirs(pluginsDir, configPath string) []string {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil
	}
	enabled := readPluginEnabled(configPath)
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

// readPluginEnabled mirrors internal/agent's config.json reading: a missing
// file, missing entry, or absent `enabled` all mean enabled.
func readPluginEnabled(path string) map[string]bool {
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw = []byte(strings.TrimPrefix(string(raw), "\ufeff"))
	var cfg struct {
		Plugins map[string]struct {
			Enabled *bool `json:"enabled"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return nil
	}
	out := make(map[string]bool, len(cfg.Plugins))
	for name, p := range cfg.Plugins {
		out[name] = p.Enabled == nil || *p.Enabled
	}
	return out
}

// isDir follows symlinks, exactly as the enumerator's isDirFollowingLinks
// does: this machine's ~/.gemini/skills/terminal-browser is a symlink.
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
