package hooks

import (
	"os"
	"path/filepath"
)

// GlobalPath is the global hooks document: ~/.gemini/config/hooks.json.
//
// 【一手】 the official documentation names the path twice — for the
// Antigravity App and for the CLI — and the CLI's own paragraph says
// that the hooks in ~/.gemini/antigravity-cli/settings.json are *not*
// loaded. The document is therefore the only global source.
//
// The path is the reference's own tree, not the clone's configuration
// home (which is ~/.openagy), so it is resolved here and not through
// configdir: the two are different trees.
func GlobalPath() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "hooks.json")
}

// WorkspacePath is the workspace hooks document:
// <workspace>/.agents/hooks.json.
//
// 【一手】 the official documentation names the path for the App, the
// CLI and the IDE alike: ".agents/hooks.json" at the project root.
func WorkspacePath(workspace string) string {
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, ".agents", "hooks.json")
}

// Load reads the global, the workspace, and the installed plugins' hook documents.
//
// A missing document is not an error: the clone simply has no hooks. A
// document that cannot be read or parsed is skipped, and the reason is
// reported in the registry's Problems — one broken document does not take
// the other one with it.
//
// The order is the reference's: the global document first, the workspace
// document second, and plugin documents, so a workspace or plugin hook fires
// in expected order.
func Load(workspace string) *Registry {
	reg := &Registry{}
	for _, path := range []string{GlobalPath(), WorkspacePath(workspace)} {
		if path == "" {
			continue
		}
		reg.Sources = append(reg.Sources, path)
		raw, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				reg.Problems = append(reg.Problems, Problem{Path: path, Reason: err.Error()})
			}
			continue
		}
		entries, problems := Parse(raw)
		reg.Entries = append(reg.Entries, entries...)
		for _, reason := range problems {
			reg.Problems = append(reg.Problems, Problem{Path: path, Reason: reason})
		}
	}

	// Also load hooks from enabled/installed plugins in ~/.gemini/config/plugins.
	home, _ := os.UserHomeDir()
	if home != "" {
		pluginsDir := filepath.Join(home, ".gemini", "config", "plugins")
		if entries, err := os.ReadDir(pluginsDir); err == nil {
			for _, e := range entries {
				pluginDir := filepath.Join(pluginsDir, e.Name())
				hookPath := filepath.Join(pluginDir, "hooks.json")
				raw, err := os.ReadFile(hookPath)
				if err != nil {
					continue
				}
				reg.Sources = append(reg.Sources, hookPath)
				parsedEntries, problems := Parse(raw)
				pluginDataDir := filepath.Join(home, ".gemini", "antigravity-cli", "plugin_data", e.Name())
				for i := range parsedEntries {
					parsedEntries[i].PluginRoot = pluginDir
					parsedEntries[i].PluginData = pluginDataDir
				}
				reg.Entries = append(reg.Entries, parsedEntries...)
				for _, reason := range problems {
					reg.Problems = append(reg.Problems, Problem{Path: hookPath, Reason: reason})
				}
			}
		}
	}

	return reg
}

// LoadFile reads one hooks document.
//
// A missing file is not an error: the registry comes back empty.
func LoadFile(path string) *Registry {
	reg := &Registry{}
	if path == "" {
		return reg
	}
	reg.Sources = append(reg.Sources, path)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			reg.Problems = append(reg.Problems, Problem{Path: path, Reason: err.Error()})
		}
		return reg
	}
	entries, problems := Parse(raw)
	reg.Entries = append(reg.Entries, entries...)
	for _, reason := range problems {
		reg.Problems = append(reg.Problems, Problem{Path: path, Reason: reason})
	}
	return reg
}
