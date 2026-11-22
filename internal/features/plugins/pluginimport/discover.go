// discover.go — the two discovery passes.
//
// 【一手】 agy 1.4.11: the Claude Code importer reads
// ~/.claude/plugins/installed_plugins.json and the top-level plugin
// directories under ~/.claude/plugins/; the Gemini CLI importer reads
// ~/.gemini/extensions/<name>/gemini-extension.json.
//
// The reference's own discovery errors are reproduced verbatim, including
// the Go type names they carry (`claude.pluginInstalls`,
// `claude.pluginInstall`) — the clone's package is named differently, so
// the strings are emitted as compatibility shims rather than produced by
// the clone's own decoder.
package pluginimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// sourceManifest is the manifest of a plugin: the Claude Code
// `.claude-plugin/plugin.json` or the Gemini CLI `gemini-extension.json`.
//
// The reference carries name, version, description and the author's name
// over into the plugin.json it writes; every other field is dropped.
type sourceManifest struct {
	Name        string
	Version     string
	Description string
	Author      string
}

// candidate is one discovered plugin: the directory it lives in and the
// manifest read from it.
type candidate struct {
	name     string
	dir      string
	manifest sourceManifest
}

// readSourceManifest reads a plugin manifest. A missing or unparseable
// file yields ok=false.
func readSourceManifest(path string) (sourceManifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return sourceManifest{}, false
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return sourceManifest{}, false
	}
	return sourceManifest{
		Name:        rawString(raw, "name"),
		Version:     rawString(raw, "version"),
		Description: rawString(raw, "description"),
		Author:      rawAuthor(raw),
	}, true
}

// rawString reads one string member of a decoded object.
func rawString(raw map[string]json.RawMessage, key string) string {
	value, ok := raw[key]
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil {
		return ""
	}
	return s
}

// rawAuthor reads the author member. Claude Code writes an object
// ({"name","url"}); the Gemini CLI writes a plain string. The reference
// carries the author's *name* over into the plugin.json it writes.
func rawAuthor(raw map[string]json.RawMessage) string {
	value, ok := raw["author"]
	if !ok {
		return ""
	}
	var obj struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(value, &obj); err == nil && obj.Name != "" {
		return obj.Name
	}
	var s string
	if err := json.Unmarshal(value, &s); err == nil {
		return s
	}
	return ""
}

// discover returns the plugins one source offers.
func discover(src Source, roots Roots) ([]candidate, error) {
	if src == SourceClaude {
		return discoverClaude(roots)
	}
	return discoverGemini(roots), nil
}

// discoverGemini lists ~/.gemini/extensions/<name>/gemini-extension.json.
func discoverGemini(roots Roots) []candidate {
	entries, err := os.ReadDir(roots.ExtensionsDir)
	if err != nil {
		return nil
	}
	var out []candidate
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(roots.ExtensionsDir, e.Name())
		m, ok := readSourceManifest(filepath.Join(dir, "gemini-extension.json"))
		if !ok {
			continue
		}
		out = append(out, candidate{name: m.Name, dir: dir, manifest: m})
	}
	return out
}

// discoverClaude reads ~/.claude/plugins/installed_plugins.json and the
// top-level plugin directories.
//
// The reference's order is: the marketplaces of installed_plugins.json in
// key order, each marketplace's installs in file order, then the top-level
// directories in name order.
func discoverClaude(roots Roots) ([]candidate, error) {
	installs, err := readInstalledPlugins(filepath.Join(roots.ClaudePlugins, "installed_plugins.json"))
	if err != nil {
		return nil, fmt.Errorf("error discovering extensions: %w", err)
	}
	var out []candidate
	seen := map[string]bool{}
	for _, market := range sortedKeys(installs) {
		for _, inst := range installs[market] {
			if inst.Scope == "project" || inst.Scope == "local" {
				continue
			}
			if inst.InstallPath == "" {
				continue
			}
			c, ok := claudeCandidate(inst.InstallPath)
			if !ok || seen[c.name] {
				continue
			}
			seen[c.name] = true
			out = append(out, c)
		}
	}
	for _, dir := range topLevelPluginDirs(roots.ClaudePlugins) {
		c, ok := claudeCandidate(dir)
		if !ok || seen[c.name] {
			continue
		}
		seen[c.name] = true
		out = append(out, c)
	}
	return out, nil
}

// claudeCandidate reads a Claude Code plugin directory: a directory with a
// `.claude-plugin/plugin.json` manifest.
func claudeCandidate(dir string) (candidate, bool) {
	m, ok := readSourceManifest(filepath.Join(dir, ".claude-plugin", "plugin.json"))
	if !ok {
		return candidate{}, false
	}
	return candidate{name: m.Name, dir: dir, manifest: m}, true
}

// topLevelPluginDirs lists the direct children of the Claude plugins
// directory that carry a `.claude-plugin/plugin.json`.
func topLevelPluginDirs(pluginsDir string) []string {
	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(pluginsDir, e.Name())
		if _, err := os.Stat(filepath.Join(dir, ".claude-plugin", "plugin.json")); err != nil {
			continue
		}
		out = append(out, dir)
	}
	sort.Strings(out)
	return out
}

// pluginInstall is one record of installed_plugins.json.
//
// 【一手】 agy 1.4.11: the record's `installPath` is the only field the
// importer reads for the location (`path` is not accepted), `scope`
// decides whether the record is skipped ("project" and "local" are
// skipped) and `name` is only a fallback — the manifest's name wins.
type pluginInstall struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Scope       string `json:"scope"`
	InstallPath string `json:"installPath"`
}

// installedPlugins is the parsed shape of installed_plugins.json:
//
//	{"plugins": {"<marketplace>": [ <install>, … ]}}
//
// The reference's decoder accepts an array or an object per marketplace
// (a custom UnmarshalJSON on `pluginInstalls`), but only the array form
// yields plugins — the object form imports nothing.
func readInstalledPlugins(path string) (map[string][]pluginInstall, error) {
	out := map[string][]pluginInstall{}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	raw, ok := top["plugins"]
	if !ok {
		return out, nil
	}
	if kind := jsonKind(raw); kind != "object" {
		return nil, fmt.Errorf("parsing %s: %s", path, unmarshalErr(kind,
			"Go struct field .plugins of type map[string]claude.pluginInstalls"))
	}
	var markets map[string]json.RawMessage
	if err := json.Unmarshal(raw, &markets); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	for _, market := range sortedRawKeys(markets) {
		value := markets[market]
		if kind := jsonKind(value); kind != "array" {
			if kind == "object" {
				// The object form parses but yields no plugins.
				continue
			}
			return nil, fmt.Errorf("parsing %s: %s", path, unmarshalErr(kind,
				"Go value of type []claude.pluginInstall"))
		}
		var installs []pluginInstall
		if err := json.Unmarshal(value, &installs); err != nil {
			return nil, fmt.Errorf("parsing %s: %s", path, installUnmarshalErr(err))
		}
		out[market] = installs
	}
	return out, nil
}

// installUnmarshalErr rewrites a decode error into the reference's own
// wording. The clone's package is named differently, so the type names the
// reference carries are shimmed.
func installUnmarshalErr(err error) string {
	return strings.ReplaceAll(err.Error(), "pluginimport.pluginInstall", "claude.pluginInstall")
}

// unmarshalErr renders the reference's "json: cannot unmarshal …" error.
func unmarshalErr(kind, target string) string {
	return fmt.Sprintf("json: cannot unmarshal %s into %s", kind, target)
}

// jsonKind names the JSON kind of a raw value the way Go's decoder does.
func jsonKind(raw json.RawMessage) string {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "value"
	}
	switch trimmed[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 't', 'f':
		return "bool"
	case 'n':
		return "null"
	default:
		return "number"
	}
}

// sortedKeys returns the map's keys in order.
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sortedRawKeys returns the raw map's keys in order.
func sortedRawKeys(m map[string]json.RawMessage) []string {
	return sortedKeys(m)
}
