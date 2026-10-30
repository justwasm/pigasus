// Package plugins — catalog.go: the plugin and marketplace documents
// the remote-control Web UI's plugin surfaces read.
//
// # Why a second reader
//
// data.go reads the plugin tree for the /plugin panel: a name, a
// one-line description, a version. The Web UI's plugin surfaces need
// more than that — the logo, the author, the repository, the
// suggested prompts, the marketplace a plugin came from — because the
// language server's own Plugin message carries all of it.
//
// The documents here are therefore the *full* plugin.json and
// marketplace.json, read with the same paths and the same symlink
// rules (pluginscan.ReadEntries) data.go uses.
//
// # The documents
//
// A plugin's plugin.json is the reference's own shape:
//
//	{
//	  "name": "modern-web-guidance-plugin",
//	  "displayName": "Modern Web Guidance",
//	  "version": "0.0.195",
//	  "description": "Keep your coding agent up to date…",
//	  "logo": "assets/logo.svg",
//	  "suggestedPrompts": ["…"],
//	  "author": {"name": "Google Chrome", "url": "https://…"},
//	  "homepage": "https://…",
//	  "repository": "https://…",
//	  "license": "Apache-2.0",
//	  "keywords": ["web", "html"]
//	}
//
// 【一手】 2026-10-30: read off this machine's
// ~/.gemini/config/plugins/*/plugin.json.
//
// The author is an object in the reference's own plugins. A plugin
// imported from Claude Code carries the author's *name* as a plain
// string instead (the reference's own import contract), so Author
// reads both.
package core

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/justwasm/pigasus/internal/pluginscan"
)

// Author is a plugin's author.
//
// The reference's plugins carry an object — {"name": "Google Chrome",
// "url": "https://developer.chrome.com"} — and the plugins imported
// from Claude Code carry the author's name as a plain string. Author
// reads both.
type Author struct {
	Name  string `json:"name,omitempty"`
	URL   string `json:"url,omitempty"`
	Email string `json:"email,omitempty"`
}

// UnmarshalJSON reads either shape of the author: an object, or the
// author's name as a string.
func (a *Author) UnmarshalJSON(data []byte) error {
	trimmed := strings.TrimSpace(string(data))
	if trimmed == "" || trimmed == "null" {
		return nil
	}
	if strings.HasPrefix(trimmed, "\"") {
		var name string
		if err := json.Unmarshal(data, &name); err != nil {
			return err
		}
		a.Name = name
		return nil
	}
	type plain Author
	var p plain
	if err := json.Unmarshal(data, &p); err != nil {
		return err
	}
	*a = Author(p)
	return nil
}

// Source is where a plugin came from.
//
// The reference's marketplace entries carry
// {"type": "archive", "url": "https://…/plugin.zip?v=…", "sha256": "…"}.
// The type maps onto the language server's own CustomizationSource —
// "archive" is its ArchiveSource, and google3 is its other arm.
type Source struct {
	Type   string `json:"type,omitempty"`
	URL    string `json:"url,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
}

// InstalledPlugin is one installed plugin.
//
// The path is the plugin's own directory; the manifest is its
// plugin.json; the skills are the directories under the plugin's skill
// directories.
type InstalledPlugin struct {
	// Name is the plugin's directory name — the id the Web UI
	// addresses it by.
	Name string
	// Path is the plugin's directory.
	Path string
	// Manifest is the plugin.json, as far as it parsed.
	Manifest Manifest
	// HasManifest says whether the plugin.json was readable. A
	// directory without one is not a plugin — the reference ignores
	// it — so it never reaches this struct.
	HasManifest bool
	// Skills are the plugin's skill directory names, sorted.
	Skills []string
	// Disabled says whether the user config turned the plugin off.
	Disabled bool
}

// InstalledPlugins lists the installed plugins, in the reference's
// order (directory name ascending).
//
// A directory without a readable plugin.json is not a plugin: the
// reference ignores it, and so does this.
//
// The stock fallback of InstalledRows is *not* used here: the Web UI's
// plugin list is a list of what is installed, and answering with the
// four plugins a fresh install ships would be a lie.
func InstalledPlugins() []InstalledPlugin {
	MaybeConsumeStaleness()
	entries, err := pluginscan.ReadEntries(StateDir())
	if err != nil {
		return nil
	}
	enabled := Enabled(ConfigPath())
	var out []InstalledPlugin
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name
		manifest, ok := readManifestFile(filepath.Join(Dir(name), "plugin.json"))
		if !ok {
			continue
		}
		if manifest.Name == "" {
			manifest.Name = name
		}
		// A plugin the user config does not name is enabled: only an
		// explicit "enabled": false turns a plugin off.
		disabled := false
		if on, ok := enabled[name]; ok && !on {
			disabled = true
		}
		out = append(out, InstalledPlugin{
			Name:        name,
			Path:        Dir(name),
			Manifest:    manifest,
			HasManifest: true,
			Skills:      Skills(name),
			Disabled:    disabled,
		})
	}
	return out
}

// FindInstalledPlugin returns the installed plugin with that name, and
// whether it was found.
func FindInstalledPlugin(name string) (InstalledPlugin, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return InstalledPlugin{}, false
	}
	for _, p := range InstalledPlugins() {
		if p.Name == name {
			return p, true
		}
	}
	return InstalledPlugin{}, false
}

// ConfigPath is the user configuration file the plugin tree hangs off:
// ~/.gemini/config/config.json. It is the file whose plugins.<name>
// entries carry the enabled flag.
func ConfigPath() string {
	return filepath.Join(filepath.Dir(StateDir()), "config.json")
}

// readManifestFile reads a plugin.json and reports whether it parsed.
//
// A missing file, an unreadable one and a malformed one are the same
// answer: no plugin.
func readManifestFile(path string) (Manifest, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, false
	}
	data = StripBOM(data)
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, false
	}
	return m, true
}

// SkillInfo is one skill's frontmatter.
//
// The reference's skills are SKILL.md files whose YAML frontmatter
// carries the name and the description. The frontmatter is what the
// Web UI's plugin surface shows for a skill.
type SkillInfo struct {
	// Name is the frontmatter's name. It falls back to the
	// directory's name when the frontmatter carries none.
	Name string
	// Description is the frontmatter's description.
	Description string
	// Path is the SKILL.md file.
	Path string
}

// PluginSkills reads the plugin's skills and their frontmatter.
//
// A skill directory without a readable SKILL.md frontmatter is not a
// skill: the reference skips it, and so does this.
func PluginSkills(pluginName string) []SkillInfo {
	dirs := SkillDirs(Dir(pluginName))
	out := make([]SkillInfo, 0, len(dirs))
	for _, dir := range dirs {
		entries, err := pluginscan.ReadEntries(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name, "SKILL.md")
			name, description, ok := skillFrontmatter(path)
			if !ok {
				continue
			}
			if name == "" {
				name = e.Name
			}
			out = append(out, SkillInfo{Name: name, Description: description, Path: path})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// skillFrontmatter reads the YAML block between the two `---` fences of
// a SKILL.md and returns its name and description.
//
// Block scalars are folded by the YAML decoder, and the description is
// trimmed — the same shape the prompt builder reads.
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

// AgentFrontmatter reads the YAML frontmatter of an agent's markdown
// definition and returns its name and description.
//
// It is the skill frontmatter contract applied to an agent file: the
// two files have the same shape, and the Web UI's agent picker shows
// the description the frontmatter carries.
//
// A file without a readable frontmatter yields ("", "", false).
func AgentFrontmatter(path string) (name, description string, ok bool) {
	return skillFrontmatter(path)
}

// MarketplaceEntry is one plugin inside a marketplace.json.
type MarketplaceEntry struct {
	Name             string   `json:"name"`
	DisplayName      string   `json:"displayName,omitempty"`
	Description      string   `json:"description,omitempty"`
	Version          string   `json:"version,omitempty"`
	Logo             string   `json:"logo,omitempty"`
	SuggestedPrompts []string `json:"suggestedPrompts,omitempty"`
	Author           Author   `json:"author,omitempty"`
	Homepage         string   `json:"homepage,omitempty"`
	Repository       string   `json:"repository,omitempty"`
	License          string   `json:"license,omitempty"`
	Keywords         []string `json:"keywords,omitempty"`
	Source           *Source  `json:"source,omitempty"`
}

// MarketplaceDoc is one marketplace.json.
type MarketplaceDoc struct {
	// Name is the marketplace's name — the directory name under the
	// cache root.
	Name string
	// Description is the marketplace's one-line description.
	Description string
	// Plugins are the marketplace's entries, in file order. The
	// reference renders them in file order, not sorted.
	Plugins []MarketplaceEntry
}

// MarketplaceDocs reads every <root>/<mkt>/marketplace.json in full.
//
// Unlike Marketplaces — which keeps only what the /plugin panel shows
// — this keeps every field the marketplace surfaces need: the logo,
// the author, the source, the suggested prompts.
func MarketplaceDocs() []MarketplaceDoc {
	MaybeConsumeStaleness()
	entries, err := pluginscan.ReadEntries(MarketplaceRoot())
	if err != nil {
		return nil
	}
	var out []MarketplaceDoc
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(MarketplaceRoot(), e.Name, "marketplace.json"))
		if err != nil {
			continue
		}
		var doc struct {
			Name        string             `json:"name"`
			Description string             `json:"description"`
			Plugins     []MarketplaceEntry `json:"plugins"`
		}
		if json.Unmarshal(StripBOM(data), &doc) != nil {
			continue
		}
		name := doc.Name
		if name == "" {
			name = e.Name
		}
		kept := doc.Plugins[:0]
		for _, p := range doc.Plugins {
			if strings.TrimSpace(p.Name) == "" {
				continue
			}
			kept = append(kept, p)
		}
		out = append(out, MarketplaceDoc{
			Name:        name,
			Description: doc.Description,
			Plugins:     kept,
		})
	}
	return out
}

// MarketplaceEntryByID finds one marketplace entry by its id, and the
// marketplace it came from.
//
// The id is the plugin's name — the reference addresses a marketplace
// entry by the name it carries in the catalogue.
func MarketplaceEntryByID(docs []MarketplaceDoc, id string) (MarketplaceEntry, MarketplaceDoc, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return MarketplaceEntry{}, MarketplaceDoc{}, false
	}
	for _, doc := range docs {
		for _, p := range doc.Plugins {
			if p.Name == id {
				return p, doc, true
			}
		}
	}
	return MarketplaceEntry{}, MarketplaceDoc{}, false
}

// SearchMarketplaceEntries searches the marketplace catalogues.
//
// The query is matched against the entry's name, its display name and
// its description, case-insensitively. An empty query matches
// everything — the reference's own search does.
//
// The results are ordered by marketplace name and then by the
// catalogue's own order, so the list is stable.
func SearchMarketplaceEntries(docs []MarketplaceDoc, query string) []MarketplaceEntry {
	q := strings.ToLower(strings.TrimSpace(query))
	docs = append([]MarketplaceDoc(nil), docs...)
	sort.SliceStable(docs, func(i, j int) bool { return docs[i].Name < docs[j].Name })
	var out []MarketplaceEntry
	for _, doc := range docs {
		for _, p := range doc.Plugins {
			if q == "" || marketplaceEntryMatches(p, q) {
				out = append(out, p)
			}
		}
	}
	return out
}

// marketplaceEntryMatches says whether a query hits an entry.
func marketplaceEntryMatches(p MarketplaceEntry, query string) bool {
	for _, field := range []string{p.Name, p.DisplayName, p.Description} {
		if field != "" && strings.Contains(strings.ToLower(field), query) {
			return true
		}
	}
	for _, kw := range p.Keywords {
		if strings.Contains(strings.ToLower(kw), query) {
			return true
		}
	}
	return false
}

// StripBOM removes a leading UTF-8 byte order mark.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xef && b[1] == 0xbb && b[2] == 0xbf {
		return b[3:]
	}
	return b
}
