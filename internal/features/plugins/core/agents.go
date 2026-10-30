// Package plugins — agents.go: subagent discovery inside an installed plugin.
//
// Cycle 234: closes the AGY 1.3.6 changelog line 13 fix
//
//	Fixed `agy plugin validate`, the summary `agy plugin install` prints, and
//	marketplace previews in the `/plugin` Discover tab leaving out subagents
//	a plugin declares in its root `agents.json`.
//
// Plugins can declare subagents in two places (the AGY 1.3.3 changelog line 43
// added the JSON path; 1.3.6 just made every surface that walks the plugin
// directory actually load it):
//
//	<plugin>/agents/<name>.md        a Markdown subagent definition
//	<plugin>/agents.json             a root-level manifest, shape:
//	                                  {"agents": [{"name": "<subagent>",
//	                                              "path": "<relative-md-path>"},
//	                                             ...]}
//
// (The schema is not pinned to the byte by a 1.3.6 decompile — the only
// recovered constraint is `name` per entry and a `path` that resolves inside
// the plugin root. See `docs/research/agy-subagents-vs-wagyu.md` row 1
// (`agents.json`) and the 1.3.6 string table `{"path": "<rel>", ...}` shapes
// in `docs/decompiled-v1.3.4/strings-v1.3.4.txt`.)
//
// The two sources are merged: the union is what every surface below shows,
// deduped by name (later definition wins on tie), and sorted.
//
// The plugin loader in `internal/cli/subcommand_parity.go` mirrors this
// reader (legacyListAgents). The two stay byte-equivalent: the TUI panel
// reads installed plugins; the CLI subcommand reads an arbitrary path.
//
// Cycle plugin-source-symlink-resolve (AGY 1.2.5): the directory walks
// here go through internal/pluginscan, which follows symlinks.
// Symlinked plugin trees (the AGY 1.2.5 case — e.g. `ln -s
// ~/work/my-plugin ~/.gemini/config/plugins/my-plugin`) now surface
// their agents/*.md and agents.json the same way a non-symlinked
// plugin does. The prior os.ReadDir + e.IsDir() pattern reported the
// link's own mode (Lstat semantics) and silently dropped entries
// that were symlinks.
package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pluginscan"
)

// AgentEntry is one entry from a plugin's agents.json. We accept either
// the v1.3.3 flat shape `[{name, path}, ...]` or the v1.3.6 nested shape
// `{agents: [{name, path}, ...]}` — the only AGY release that ships a
// reference we can match against treats both as equivalent (the nested form
// is the documented one in 1.3.6; the flat form is what pre-1.3.3 fields
// accidentally produced).
type AgentEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// AgentsJSON is the doc shape we accept. We try the nested form first;
// failing that, an array at the root is treated as the list directly.
type AgentsJSON struct {
	Agents []AgentEntry `json:"agents"`
}

// ReadAgentsJSON parses <plugins>/<name>/agents.json and returns the
// declared entries, with parse errors surfaced as the zero slice + a non-nil
// error. A missing file yields (nil, nil) so callers can treat absent as
// "no JSON-declared subagents" without an extra error check.
//
// An entry's `path` is resolved relative to the plugin root; if the resolved
// path exists on disk it is recorded verbatim, otherwise the entry is still
// kept by name (some plugins ship the JSON without the markdown sidecar yet
// — the AGY 1.3.6 fix is that we count it, not that we validate it). A
// missing or empty name is the only thing that drops an entry silently —
// that's the same policy `skills.json` / `rules.json` use elsewhere.
func ReadAgentsJSON(name string) ([]AgentEntry, error) {
	path := filepath.Join(Dir(name), "agents.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	// Nested form first (the 1.3.6 canonical).
	var doc AgentsJSON
	if err := json.Unmarshal(data, &doc); err != nil || doc.Agents == nil {
		// Flat form fallback: a bare array of entries. This is what an
		// accidentally-unnested pre-1.3.3 file looks like, and the AGY
		// loader accepts it.
		var flat []AgentEntry
		if err2 := json.Unmarshal(data, &flat); err2 != nil {
			if err == nil {
				err = err2
			}
			return nil, err
		}
		return NormalizeAgentsJSON(flat), nil
	}
	return NormalizeAgentsJSON(doc.Agents), nil
}

// NormalizeAgentsJSON drops entries without a name and trims whitespace.
// `path` is preserved as-is; resolution happens at the call site so the TUI
// and the CLI share one read path.
func NormalizeAgentsJSON(in []AgentEntry) []AgentEntry {
	if in == nil {
		return nil
	}
	out := make([]AgentEntry, 0, len(in))
	for _, e := range in {
		e.Name = strings.TrimSpace(e.Name)
		e.Path = strings.TrimSpace(e.Path)
		if e.Name == "" {
			continue
		}
		out = append(out, e)
	}
	return out
}

// AgentsMarkdown lists the markdown-defined subagents of an installed
// plugin. The discovery walks <plugin>/agents/*.md and uses each file's stem
// as the subagent name (the same way AGY surfaces them in /agents — the
// Markdown frontmatter's `name:` wins when present, but the clone has no
// frontmatter parser and the on-disk filename is the source of truth in the
// shipped fixture set).
//
// A non-existent or empty directory yields nil. We deliberately do NOT
// recurse: the 1.3.6 changelog line 169 (in
// `docs/decompiled-v1.3.4/strings-v1.3.3.txt` around line 97435) made
// directory entries shallow by default and named `include_only` as the
// recursion opt-in. The clone reads only the top level here for parity.
//
// Cycle plugin-source-symlink-resolve: the walk goes through
// pluginscan.ReadEntries so a symlinked <plugin>/agents/<x>.md is
// surfaced. The prior os.ReadDir + e.IsDir() reported the link's own
// FileMode; a symlink to a .md file with the link's Lstat mode would
// be classified as a directory (because links have ModeSymlink, not
// ModeRegular) — but in practice the loader's `if e.IsDir() continue`
// check skipped exactly that case. The pluginscan fix swaps the
// stat for one that follows links, so a symlink to a file is now
// classified as a regular file and picked up.
func AgentsMarkdown(name string) []string {
	dir := filepath.Join(Dir(name), "agents")
	entries, err := pluginscan.ReadEntries(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name) != ".md" {
			continue
		}
		out = append(out, strings.TrimSuffix(e.Name, ".md"))
	}
	return out
}

// Agents is the union of <plugin>/agents.json and
// <plugin>/agents/*.md, deduped by name (later definition wins on tie — the
// JSON manifest is read second and treated as authoritative) and sorted. The
// return value is what every TUI surface uses.
func Agents(name string) []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range AgentsMarkdown(name) {
		if seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	entries, _ := ReadAgentsJSON(name)
	for _, e := range entries {
		if seen[e.Name] {
			continue
		}
		seen[e.Name] = true
		out = append(out, e.Name)
	}
	sort.Strings(out)
	return out
}

// AgentsAtPath is the on-disk reader for an arbitrary plugin directory
// (the CLI subcommand path: `agy plugin validate ./my-plugin`, `agy plugin
// install ./my-plugin`). It mirrors Agents but takes the root directly
// instead of going through Dir.
//
// Two layered errors that callers care about:
//
//   - jsonErr is non-nil iff agents.json was present AND malformed. The
//     listing still proceeds (the markdown sidecar may be intact), and the
//     caller is expected to surface the error to the user.
//   - missing is true when neither source contributed a single entry —
//     useful for `plugin validate` to print "skipped (not found)" instead
//     of silently doing nothing.
func AgentsAtPath(root string) (names []string, jsonErr error, missing bool) {
	seen := map[string]bool{}
	// Markdown sidecar first — it never errors.
	//
	// Cycle plugin-source-symlink-resolve: the walk goes through
	// pluginscan.ReadEntries so a symlinked plugin's <x>.md files are
	// surfaced to `agy plugin validate <path>` (the CLI subcommand
	// path that takes an arbitrary plugin directory).
	mdDir := filepath.Join(root, "agents")
	if entries, err := pluginscan.ReadEntries(mdDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name) != ".md" {
				continue
			}
			n := strings.TrimSuffix(e.Name, ".md")
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	// agents.json second; parse errors are returned separately.
	jsonNames, jerr := parseAgentsJSONFile(filepath.Join(root, "agents.json"), seen)
	names = append(names, jsonNames...)
	jsonErr = jerr
	sort.Strings(names)
	missing = len(names) == 0
	return names, jsonErr, missing
}

func parseAgentsJSONFile(jsonPath string, seen map[string]bool) ([]string, error) {
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		return nil, nil
	}
	var names []string
	var doc AgentsJSON
	if jerr := json.Unmarshal(data, &doc); jerr != nil || doc.Agents == nil {
		var flat []AgentEntry
		if err2 := json.Unmarshal(data, &flat); err2 != nil {
			if jerr == nil {
				jerr = err2
			}
			return nil, jerr
		}
		for _, e := range NormalizeAgentsJSON(flat) {
			if !seen[e.Name] {
				seen[e.Name] = true
				names = append(names, e.Name)
			}
		}
		return names, nil
	}
	for _, e := range NormalizeAgentsJSON(doc.Agents) {
		if !seen[e.Name] {
			seen[e.Name] = true
			names = append(names, e.Name)
		}
	}
	return names, nil
}

// UnpackedAgents is the Discover detail block's `Agents:` value. It
// reads the unpacked-cache mirror of the marketplace plugin directory the
// same way `pluginComponents` does: when the plugin is on disk under
// `<antigravity home>/cache/marketplaces/<mkt>/plugins/<name>/<version>/`,
// the agents come from there; otherwise the reference is mid-download and
// renders "loading...". The shipped marketplace.json does not carry agent
// names, so we have to read them off the unpacked tree — the same
// conservative read `Components:` already does for the directory listing.
//
// A missing marketplace mapping (the plugin is not in the catalog) yields
// "-" so the detail block stays at six rows regardless.
func UnpackedAgents(name string, mkts []Marketplace) string {
	mkt := MarketplaceOf(name, mkts)
	ver := MarketplaceVersion(name, mkts)
	if mkt == "" || ver == "" {
		return "-"
	}
	dir := filepath.Join(auth.AntigravityConfigHome(), "cache", "marketplaces",
		mkt, "plugins", name, ver)
	names, _, _ := AgentsAtPath(dir)
	if len(names) == 0 {
		// An unpacked-but-empty plugin directory is indistinguishable
		// from a not-yet-unpacked one here (we never downloaded); the
		// reference prints "loading..." in both cases, so we match.
		return "loading..."
	}
	return strings.Join(names, ", ")
}
