package agyprompt

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// ruleManifest is the parsed rules.json — the file that registers the rules
// that live outside a rules/ directory and the nested rules inside one.
//
// The shape is the reference's own (official-docs/rules.md:120-131, and the
// reference's "JSON Configuration Files" document, which gives the same
// schema for skills.json and plugins.json):
//
//	{
//	  "inherits": [ { "path": "../shared-config/.agents/rules.json" } ],
//	  "entries": [
//	    { "path": "../shared-rules", "exclude": ["deprecated_rules.md", "legacy/"] },
//	    { "path": "rules", "include_only": ["frontend/react.md"] }
//	  ]
//	}
type ruleManifest struct {
	Inherits []ruleManifestEntry `json:"inherits"`
	Entries  []ruleManifestEntry `json:"entries"`
}

// ruleManifestEntry is one path entry of a manifest.
type ruleManifestEntry struct {
	Path        string   `json:"path"`
	IncludeOnly []string `json:"include_only"`
	Exclude     []string `json:"exclude"`
}

// manifestItem is one rule file a manifest entry names.
type manifestItem struct {
	path   string // the absolute path of the file
	rel    string // the path relative to the entry's directory, slash-separated
	nested bool   // the file lives in a subdirectory of the entry's directory
}

// maxManifestDepth is how deep an inherits chain may go. A manifest that
// inherits itself would otherwise loop forever.
const maxManifestDepth = 8

// readRuleManifest reads a rules.json.
func readRuleManifest(path string) (ruleManifest, bool) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ruleManifest{}, false
	}
	var m ruleManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return ruleManifest{}, false
	}
	return m, true
}

// resolveRuleManifest returns the rule files a manifest registers.
//
// The inherited manifests come first: their entries are merged with the
// local ones, and the inherited files are processed in the order they are
// listed (official-docs/rules.md:120-131).
func resolveRuleManifest(manifestPath string, bases []string) []string {
	manifest, ok := readRuleManifest(manifestPath)
	if !ok {
		return nil
	}
	var out []string
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" || seen[path] {
			return
		}
		seen[path] = true
		out = append(out, path)
	}
	for _, inherit := range manifest.Inherits {
		for _, item := range resolveInheritedManifest(inherit, bases, 0) {
			add(item.path)
		}
	}
	for _, entry := range manifest.Entries {
		for _, item := range resolveManifestEntry(entry, bases) {
			add(item.path)
		}
	}
	return out
}

// resolveInheritedManifest resolves the items an inherited manifest
// contributes, with the inheriting entry's own filters applied to them.
func resolveInheritedManifest(inherit ruleManifestEntry, bases []string, depth int) []manifestItem {
	if depth > maxManifestDepth {
		return nil
	}
	path := resolveManifestFile(inherit.Path, bases)
	if path == "" {
		return nil
	}
	manifest, ok := readRuleManifest(path)
	if !ok {
		return nil
	}
	inheritedBases := append([]string{filepath.Dir(path)}, bases...)
	var items []manifestItem
	for _, entry := range manifest.Entries {
		items = append(items, resolveManifestEntry(entry, inheritedBases)...)
	}
	for _, entry := range manifest.Inherits {
		items = append(items, resolveInheritedManifest(entry, inheritedBases, depth+1)...)
	}
	return filterManifestItems(items, inherit.IncludeOnly, inherit.Exclude)
}

// resolveManifestEntry returns the rule files one entry names.
//
// The entry's directory is scanned one level deep: its own *.md files are
// the rules, and the files inside its subdirectories are loaded only when
// the entry names them in include_only (official-docs/rules.md:32, :129-130).
func resolveManifestEntry(entry ruleManifestEntry, bases []string) []manifestItem {
	dir := resolveManifestDir(entry.Path, bases)
	if dir == "" {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var items []manifestItem
	for _, e := range entries {
		if e.IsDir() {
			items = append(items, nestedManifestItems(dir, e.Name())...)
			continue
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		items = append(items, manifestItem{path: filepath.Join(dir, e.Name()), rel: e.Name()})
	}
	return filterManifestItems(items, entry.IncludeOnly, entry.Exclude)
}

// nestedManifestItems returns the *.md files of a subdirectory of a
// manifest entry's directory. They are candidates only: the entry's
// include_only decides whether they are loaded.
func nestedManifestItems(dir, name string) []manifestItem {
	sub, err := os.ReadDir(filepath.Join(dir, name))
	if err != nil {
		return nil
	}
	var items []manifestItem
	for _, e := range sub {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		items = append(items, manifestItem{
			path:   filepath.Join(dir, name, e.Name()),
			rel:    name + "/" + e.Name(),
			nested: true,
		})
	}
	return items
}

// filterManifestItems applies the entry's include_only and exclude filters.
//
// include_only names the items that are loaded; when it is set, every other
// item is skipped — including the files of the subdirectories, which are
// never loaded without it. exclude skips the items it names; a pattern that
// ends with a slash names a directory of the entry.
func filterManifestItems(items []manifestItem, includeOnly, exclude []string) []manifestItem {
	var out []manifestItem
	for _, item := range items {
		if manifestExcluded(item, exclude) {
			continue
		}
		if item.nested {
			if !manifestIncluded(item, includeOnly) {
				continue
			}
		} else if len(includeOnly) > 0 && !manifestIncluded(item, includeOnly) {
			continue
		}
		out = append(out, item)
	}
	return out
}

// manifestIncluded reports whether one of the patterns names the item.
func manifestIncluded(item manifestItem, patterns []string) bool {
	for _, pattern := range patterns {
		if manifestPatternMatch(pattern, item.rel) {
			return true
		}
	}
	return false
}

// manifestExcluded reports whether one of the patterns skips the item.
func manifestExcluded(item manifestItem, patterns []string) bool {
	for _, pattern := range patterns {
		if manifestPatternMatch(pattern, item.rel) {
			return true
		}
	}
	return false
}

// manifestPatternMatch matches a pattern against an item's path, relative to
// the entry's directory.
//
// A pattern that ends with a slash names a directory: it matches the item
// when one of the item's directories carries that name. Every other pattern
// matches the item's own path or its file name.
func manifestPatternMatch(pattern, rel string) bool {
	pattern = strings.TrimSpace(pattern)
	if pattern == "" {
		return false
	}
	if strings.HasSuffix(pattern, "/") {
		dirPattern := strings.TrimSuffix(pattern, "/")
		segments := strings.Split(rel, "/")
		for i := 0; i < len(segments)-1; i++ {
			if matchGlobPath(dirPattern, segments[i]) {
				return true
			}
		}
		return false
	}
	if matchGlobPath(pattern, rel) {
		return true
	}
	return matchGlobPath(pattern, filepath.Base(rel))
}

// resolveManifestDir resolves an entry's path against the bases a relative
// path may resolve against, and returns the first base that holds it.
//
// The bases are, in the reference's order: the directory that holds the
// manifest (the .agents/ directory), the directory that holds the dot-dir
// that declares it, and the project root
// (official-docs/rules.md:129-130 and the reference's "JSON Configuration
// Files" document).
func resolveManifestDir(path string, bases []string) string {
	if path == "" {
		return ""
	}
	if resolved, ok := resolveHomePath(path); ok {
		return resolved
	}
	if filepath.IsAbs(path) {
		return path
	}
	candidates := manifestCandidates(path, bases)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// resolveManifestFile resolves the path of an inherited manifest.
func resolveManifestFile(path string, bases []string) string {
	if path == "" {
		return ""
	}
	if resolved, ok := resolveHomePath(path); ok {
		return resolved
	}
	if filepath.IsAbs(path) {
		return path
	}
	candidates := manifestCandidates(path, bases)
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	return candidates[0]
}

// manifestCandidates returns the paths a relative path resolves to, one per
// base.
func manifestCandidates(path string, bases []string) []string {
	var out []string
	for _, base := range bases {
		if base == "" {
			continue
		}
		out = append(out, filepath.Join(base, path))
	}
	return out
}

// resolveHomePath expands a path that starts with "~/" against the user's
// home directory.
func resolveHomePath(path string) (string, bool) {
	if !strings.HasPrefix(path, "~/") {
		return "", false
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", false
	}
	return filepath.Join(home, path[2:]), true
}
