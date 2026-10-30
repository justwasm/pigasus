// Package plugins — which user plugins the reference loads.
//
// A directory under a plugins root is a plugin only when it carries a
// parseable plugin.json; the reference ignores a folder without one. A
// plugin named in the user config.json with `enabled: false` is disabled;
// a plugin that is absent from the config.json is enabled.
//
// The three facts were re-verified against agy 1.4.5 (【一手】, live request
// bodies through the cycle-274 capture wrapper): a plugin directory with no
// plugin.json contributed no rules, one with a plugin.json but no config.json
// entry contributed its rules, and one with `enabled: false` contributed
// none.
//
// The package holds no plugin *content* — it only answers "which plugin
// directories are loaded", so the custom-agent loader and the prompt builder
// can agree on the set.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// Enabled reports the plugins.<name>.enabled flags of a config.json.
//
// A missing file, a missing entry, or an entry without "enabled" all mean
// enabled. The returned map holds only the plugins the file names.
func Enabled(configPath string) map[string]bool {
	if configPath == "" {
		return nil
	}
	raw, err := os.ReadFile(configPath)
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

// LoadedDirs returns the plugin directories under pluginsDir that the
// reference loads, in directory-name order.
//
// configPath is the user config.json whose plugins.<name>.enabled=false
// disables a plugin; pass "" when no config applies (the workspace plugin
// root has no config of its own).
func LoadedDirs(pluginsDir, configPath string) []string {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil
	}
	enabled := Enabled(configPath)
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
		if isEnabled, ok := enabled[e.Name()]; ok && !isEnabled {
			continue
		}
		out = append(out, dir)
	}
	return out
}

// SkillDirs returns the skill directories a plugin contributes.
//
// The plugin.json "skills" field names them: either an array of paths
// relative to the plugin directory (["skills"]) or a single path string
// ("./skills/") — the form some plugins written for other tools use, which
// the reference used to reject (the whole plugin failed to load) and
// accepts since 1.4.5.
//
// 【一手】 agy 1.4.5 changelog: "Fixed plugins failing to load when
// plugin.json gives skills as a single path string, such as
// "skills": "./skills/", instead of an array, as some plugins written for
// other tools do." (docs/cycles/reference-drift-146/)
//
// A plugin without the field contributes its default skills/ directory. A
// relative path is resolved against the plugin directory; an absolute path
// is taken as is. The returned paths are not checked for existence — the
// caller filters.
func SkillDirs(pluginDir string) []string {
	paths := manifestSkillPaths(pluginDir)
	if len(paths) == 0 {
		paths = []string{"skills"}
	}
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "" {
			continue
		}
		if filepath.IsAbs(p) {
			out = append(out, filepath.Clean(p))
			continue
		}
		out = append(out, filepath.Clean(filepath.Join(pluginDir, p)))
	}
	if len(out) == 0 {
		out = append(out, filepath.Join(pluginDir, "skills"))
	}
	return out
}

// manifestSkillPaths reads the "skills" field of a plugin's plugin.json.
// It accepts an array of strings and a single string; anything else (a
// missing file, a missing field, an object) yields no paths.
func manifestSkillPaths(pluginDir string) []string {
	raw, err := os.ReadFile(filepath.Join(pluginDir, "plugin.json"))
	if err != nil {
		return nil
	}
	var doc struct {
		Skills json.RawMessage `json:"skills"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc.Skills) == 0 {
		return nil
	}
	var list []string
	if err := json.Unmarshal(doc.Skills, &list); err == nil {
		return list
	}
	var one string
	if err := json.Unmarshal(doc.Skills, &one); err == nil {
		return []string{one}
	}
	return nil
}
