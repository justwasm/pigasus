// plugin_data.go — the two on-disk sources behind /plugin's tabs.
//
// Cycle 198. The reference's /plugin picker reads (probed by driving 1.4.0 and
// diffing the filesystem):
//
//	Installed   ~/.gemini/config/plugins/<name>/plugin.json
//	            one directory per installed plugin; the name is the directory
//	            name and the description is the manifest's "description".
//	            ~/.gemini/config/config.json's plugins.<name>.enabled is the
//	            [enabled]/[disabled] badge (internal/auth.PluginEnabled).
//	            <dir>/skills/* supplies the expanded "Skills:" line.
//	            <dir>/plugin.json "version" supplies "Version:".
//
//	Discover    ~/.gemini/antigravity-cli/cache/marketplaces/<mkt>/marketplace.json
//	            {"name","description","plugins":[{name,description,...}]}.
//	            The picker lists every entry as "● <name> <description>", and
//	            the directory name is the "Marketplace:" field.
//
// Both trees are absent on a machine that has never installed a plugin; the
// Installed tab then falls back to the four stock rows the reference ships
// (ppref.plugin.raw.txt) so the panel still renders something truthful-ish
// instead of an empty list.
//
// Cycle plugin-source-symlink-resolve (AGY 1.2.5): every directory walk
// here now goes through internal/pluginscan, which follows symlinks.
// The prior os.ReadDir + e.IsDir() pattern reported the link's own
// mode and dropped symlinked plugin/skill directories silently —
// e.g. ~/.gemini/config/plugins/<name> being a symlink to
// ~/work/<name>/ was enough to hide the plugin from /plugin. The
// loaders below keep their semantics intact otherwise.
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/pluginscan"
	"github.com/justwasm/pigasus/internal/pluginwatch"
)

// pluginWatcherPtr is the active ~/.gemini plugin/skill/marketplace
// watcher. runLiveTUI installs a non-nil pointer on TUI entry; the
// deferred teardown swaps it back to nil so a subsequent TUI session
// (a future re-entry from tests, an embedded run) cannot pick up a
// stale watcher. Print mode leaves it nil — print turns have no UI
// loop to drain fsnotify events, and `agy -p /plugin` re-reads the
// disk on every call anyway, so an unobserved watcher is wasted
// overhead.
//
// atomic.Pointer keeps the loaders' IsStale() check race-free against
// the runLiveTUI install/deinstall paths. The pluginwatch methods
// themselves are goroutine-safe; the atomic is only here to protect
// the pointer read from the loaders.
var pluginWatcherPtr atomic.Pointer[pluginwatch.Watcher]

// maybeConsumeStaleness is the one-line hook the cycle plugin-fsnotify
// loaders call before they re-read ~/.gemini. When the watcher has
// fired (IsStale returns true), the helper clears the flag so the
// next scan does not have to. A nil watcher or a fresh watcher is a
// no-op — the function returns false and the loader proceeds with
// its cycle-198 on-disk walk.
//
// Splitting "read" and "clear" into one call lets us add the hook
// at the *top* of every loader without changing its body shape; the
// loader still does the actual work.
func maybeConsumeStaleness() {
	w := pluginWatcherPtr.Load()
	if w == nil {
		return
	}
	if w.IsStale() {
		w.MarkFresh()
	}
}

// pluginRow is one row of either tab: the name, its one-line description and
// the marketplace's declared version. `version` is only populated for the
// Discover rows read from marketplace.json (cycle 204: it is the Discover
// detail block's `Version:` value); the Installed rows leave it empty and read
// their version from plugin.json instead.
type pluginRow struct{ name, desc, version string }

// pluginInstalledFallback is the stock Installed tab — the four plugins a
// fresh reference install lists (measured; the descriptions come from each
// plugin.json).
var pluginInstalledFallback = []pluginRow{
	{name: "chrome-devtools-plugin", desc: "Reliable automation, in-depth debugging, and performance analysis in Chrome using Chrome DevTools and Puppeteer"},
	{name: "google-antigravity-sdk", desc: "Using the Google Antigravity Python SDK to build AI agents"},
	{name: "modern-web-guidance-plugin", desc: "Keep your coding agent up to date with the latest web best practices."},
	{name: "science", desc: "Curated collection of agent skills for science tasks."},
}

// pluginStateDir is ~/.gemini/config/plugins.
func pluginStateDir() string {
	return filepath.Join(auth.GeminiConfigHome(), "plugins")
}

// pluginDir is one installed plugin's directory.
func pluginDir(name string) string { return filepath.Join(pluginStateDir(), name) }

// pluginManifest is the subset of plugin.json the panel shows.
type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// readPluginManifest reads <plugins>/<name>/plugin.json. A missing or
// unparseable file yields the zero manifest (callers fall back to the static
// description table).
func readPluginManifest(name string) pluginManifest {
	var m pluginManifest
	data, err := os.ReadFile(filepath.Join(pluginDir(name), "plugin.json"))
	if err != nil {
		return m
	}
	if json.Unmarshal(data, &m) != nil {
		return pluginManifest{}
	}
	return m
}

// installedPluginRows lists ~/.gemini/config/plugins/* in the reference's
// order (directory name ascending — os.ReadDir sorts, and the reference's
// Installed list is alphabetical). Falls back to pluginInstalledFallback when
// the tree is absent or empty.
//
// Cycle plugin-source-symlink-resolve: the directory walk goes through
// pluginscan.ReadEntries so a symlinked <plugins>/<name>/ entry (e.g.
// `ln -s ~/work/my-plugin ~/.gemini/config/plugins/my-plugin`) shows up
// here. The prior os.ReadDir + e.IsDir() pattern reported the link's
// own FileMode and silently dropped the plugin.
func installedPluginRows() []pluginRow {
	// Cycle plugin-fsnotify: consume the watcher staleness flag so the
	// next /plugin render starts from a known-fresh state. On the TUI
	// path the watcher is wired; print mode leaves pluginWatcherPtr nil
	// and this is a single-pointer read.
	maybeConsumeStaleness()
	entries, err := pluginscan.ReadEntries(pluginStateDir())
	if err != nil {
		return pluginInstalledFallback
	}
	var out []pluginRow
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name
		desc := readPluginManifest(name).Description
		if desc == "" {
			desc = fallbackPluginDesc(name)
		}
		out = append(out, pluginRow{name: name, desc: desc})
	}
	if len(out) == 0 {
		return pluginInstalledFallback
	}
	return out
}

// fallbackPluginDesc returns the stock description for a name, or "".
func fallbackPluginDesc(name string) string {
	for _, p := range pluginInstalledFallback {
		if p.name == name {
			return p.desc
		}
	}
	return ""
}

// pluginSkills lists the sorted skill directory names under <plugin>/skills.
// The reference sorts them (probed: the on-disk order is debug-optimize-lcp,
// troubleshooting, chrome-devtools, … while the panel shows a11y-debugging
// first).
//
// Cycle plugin-source-symlink-resolve: the walk goes through
// pluginscan.ReadEntries so a symlinked <plugin>/skills/<x>/ directory
// (the AGY 1.2.5 case — symlinked plugin trees) appears in the Skills:
// line. e.IsDir() on a DirEntry reports the link's mode, not the
// resolved target's, so the prior implementation silently dropped
// every symlinked skill directory.
func pluginSkills(name string) []string {
	// Cycle plugin-fsnotify: see installedPluginRows. pluginSkills is
	// called once per /plugin row when the user opens the detail
	// panel, so the watcher hook costs nothing and keeps the
	// Skills: line aligned with whatever the user just installed
	// from another shell.
	maybeConsumeStaleness()
	entries, err := pluginscan.ReadEntries(filepath.Join(pluginDir(name), "skills"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name)
		}
	}
	sort.Strings(out)
	return out
}

// pluginMarketplace is one Discover source: the directory name under
// cache/marketplaces plus its entries in file order (the reference renders
// them in marketplace.json order, not sorted).
type pluginMarketplace struct {
	name  string
	items []pluginRow
}

// marketplaceRoot is ~/.gemini/antigravity-cli/cache/marketplaces.
func marketplaceRoot() string {
	return filepath.Join(auth.AntigravityConfigHome(), "cache", "marketplaces")
}

// pluginMarketplaces reads every <root>/<mkt>/marketplace.json. Entries
// without a name are dropped. Unreadable or empty marketplaces are skipped.
//
// Cycle plugin-source-symlink-resolve: symlinked marketplace trees
// (a common shape for shared cache directories on NFS / cross-machine
// mounts) are now visible to the Discover tab.
func pluginMarketplaces() []pluginMarketplace {
	// Cycle plugin-fsnotify: see installedPluginRows. The Discover tab
	// is the one most likely to render a stale list when the user
	// edits a marketplace cache (e.g. a CI job refreshing the cache
	// outside the TUI), so the staleness flip is most valuable here.
	maybeConsumeStaleness()
	entries, err := pluginscan.ReadEntries(marketplaceRoot())
	if err != nil {
		return nil
	}
	var out []pluginMarketplace
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(marketplaceRoot(), e.Name, "marketplace.json"))
		if err != nil {
			continue
		}
		var doc struct {
			Plugins []struct {
				Name        string `json:"name"`
				Description string `json:"description"`
				Version     string `json:"version"`
			} `json:"plugins"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		var items []pluginRow
		for _, p := range doc.Plugins {
			if strings.TrimSpace(p.Name) == "" {
				continue
			}
			items = append(items, pluginRow{name: p.Name, desc: p.Description, version: p.Version})
		}
		if len(items) > 0 {
			out = append(out, pluginMarketplace{name: e.Name, items: items})
		}
	}
	return out
}

// marketplaceItems flattens every marketplace's entries — the Discover list is
// one list across sources (only one marketplace exists on the reference
// machine this was probed on, so a merge order across several is unverified).
func marketplaceItems(mkts []pluginMarketplace) []pluginRow {
	var out []pluginRow
	for _, mk := range mkts {
		out = append(out, mk.items...)
	}
	return out
}

// marketplaceOf reports which marketplace a plugin came from, or "".
func marketplaceOf(name string, mkts []pluginMarketplace) string {
	for _, mk := range mkts {
		for _, p := range mk.items {
			if p.name == name {
				return mk.name
			}
		}
	}
	return ""
}

// marketplaceDesc returns the marketplace entry's description, or "".
func marketplaceDesc(name string, mkts []pluginMarketplace) string {
	for _, mk := range mkts {
		for _, p := range mk.items {
			if p.name == name && p.desc != "" {
				return p.desc
			}
		}
	}
	return ""
}

// marketplaceVersion returns the marketplace entry's declared version, or "".
// It is the Discover detail block's `Version:` value and, with the name and
// marketplace, the cache path the Components line reads (cycle 204).
func marketplaceVersion(name string, mkts []pluginMarketplace) string {
	for _, mk := range mkts {
		for _, p := range mk.items {
			if p.name == name {
				return p.version
			}
		}
	}
	return ""
}

// pluginDescFor is the one-line description the reference shows for an
// installed plugin: the marketplace entry's when the plugin has one, else the
// installed plugin.json's, else the stock table.
//
// Measured cycle 198: modern-web-guidance-plugin's shipped plugin.json says
// "Curated collection of agent skills for modern web development." while both
// the row and the expanded Description: line show the marketplace text "Keep
// your coding agent up to date with the latest web best practices." — so the
// marketplace wins.
func pluginDescFor(name string, mkts []pluginMarketplace) string {
	if d := marketplaceDesc(name, mkts); d != "" {
		return d
	}
	if d := readPluginManifest(name).Description; d != "" {
		return d
	}
	return fallbackPluginDesc(name)
}

// installedNames is the set of installed plugin directory names.
func installedNames() map[string]bool {
	out := map[string]bool{}
	for _, p := range installedPluginRows() {
		out[p.name] = true
	}
	return out
}
