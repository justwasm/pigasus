// plugin_cleanup.go — AGY 1.2.7 changelog line 215:
//
//	Fixed plugin upgrades leaving old MCP server and sidecar processes
//	running from deleted install directories, and pruned superseded
//	plugin versions and staging directories from the marketplace cache
//	on startup.
//
// The startup hook (PruneOrphanPluginProcesses + PruneMarketplaceCache)
// runs once per process launch from main(), before the parser dispatches.
// The upgrade hook (KillPluginProcessesForInstall) runs from
// runPluginInstall just before the old install directory is removed.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RunPluginCleanupOnStartup is the AGY 1.2.7 startup hook: kill any
// MCP server / sidecar processes whose install directory has been
// removed since the last launch, then prune the marketplace cache
// (staging leftovers + superseded plugin versions). It is best-effort:
// every error is non-fatal so a single corrupt registry entry cannot
// block the user from starting the CLI.
//
// The function is exported so cmd/wagyu/main.go can call it once per
// process launch — before the parser dispatches — without the cli
// package having to know about the binary's main().
func RunPluginCleanupOnStartup(antigravityHome string) error {
	if strings.TrimSpace(antigravityHome) == "" {
		return nil
	}
	reg := PluginProcessRegistryPath(antigravityHome)
	if _, err := PruneOrphanPluginProcesses(reg); err != nil {
		return fmt.Errorf("plugin process prune: %w", err)
	}
	cacheRoot := filepath.Join(antigravityHome, "cache", "marketplaces")
	if _, err := PruneMarketplaceCache(cacheRoot, installedPluginVersion); err != nil {
		return fmt.Errorf("marketplace cache prune: %w", err)
	}
	return nil
}

// installedPluginVersion resolves the version field from the plugin
// manifest at the legacy install path. Returns "" when the plugin is
// not installed locally — PruneMarketplaceCache treats that as
// "every cached version is superseded". Errors other than a missing
// file (corrupt JSON, permission denied) are swallowed and reported
// as "" because the prune must be best-effort.
func installedPluginVersion(name string) string {
	dir := legacyPluginsDir()
	if dir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(dir, name, "plugin.json"))
	if err != nil {
		return ""
	}
	var doc struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return ""
	}
	return strings.TrimSpace(doc.Version)
}

// antigravityHomeFromLegacy returns the antigravity home the rest of
// the package resolves from $ANTIGRAVITY_HOME / $HOME. Mirrors
// auth.AntigravityConfigHome but is local so this package does not
// have to import internal/auth (the auth package would otherwise
// drag in keyring/sqlite code that has no business in a CLI helper).
// Falls back to the legacyPluginsDir parent when the env vars are
// empty so a sandbox with no $HOME still resolves sensibly.
func antigravityHomeFromLegacy() string {
	if h := strings.TrimSpace(os.Getenv("ANTIGRAVITY_HOME")); h != "" {
		return h
	}
	if h := strings.TrimSpace(os.Getenv("HOME")); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	if dir := legacyPluginsDir(); dir != "" {
		return filepath.Join(filepath.Dir(dir), "antigravity-cli")
	}
	return ""
}

// PluginKillResult summarises a KillPluginProcessesForInstall run:
// Killed is the number of PIDs we managed to signal, Errors carries
// the os.FindProcess / Signal failures so the caller can log them
// without losing the rest of the batch.
type PluginKillResult struct {
	Killed int
	Errors []error
}

// KillPluginProcessesForInstall signals every process in the registry
// whose InstallDir equals installDir (canonicalised) and removes the
// matching rows. Empty installDir matches nothing — the caller
// almost certainly wanted PruneOrphanPluginProcesses for that case.
//
// SIGTERM is the first signal; SIGKILL is escalated automatically by
// the OS once the process exits. We do not block on the kill (the
// upgrade flow needs to keep going), but every signalled PID is also
// unregistered so the next startup does not see it as an orphan.
//
// Errors are returned per-record, not as a single fatal failure: a
// single pid that has already exited is normal, the whole batch
// should still be processed.
func KillPluginProcessesForInstall(path, installDir string) (PluginKillResult, error) {
	if strings.TrimSpace(installDir) == "" {
		return PluginKillResult{}, nil
	}
	want := canonicalInstallDir(installDir)
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return PluginKillResult{}, err
	}
	var kept []PluginProcessRecord
	res := PluginKillResult{}
	for _, r := range records {
		if canonicalInstallDir(r.InstallDir) != want {
			kept = append(kept, r)
			continue
		}
		if err := signalProcess(r.PID); err != nil {
			res.Errors = append(res.Errors,
				fmt.Errorf("pid %d from %s: %w", r.PID, installDir, err))
			kept = append(kept, r)
			continue
		}
		res.Killed++
	}
	if err := SavePluginProcessRegistry(path, kept); err != nil {
		return res, err
	}
	return res, nil
}

// PruneOrphanPluginProcesses is the on-startup half of the gap: walk
// the registry, find every record whose InstallDir no longer exists
// on disk, signal those processes, and drop the rows. A registry row
// whose install dir has been deleted is the canonical "MCP server
// from a previous install we no longer have a plugin for" case, and
// the reference binary prunes them at startup so the orphan cannot
// keep running indefinitely.
//
// Returns the number of orphans signalled. A missing registry yields
// (0, nil) — there is nothing to prune.
func PruneOrphanPluginProcesses(path string) (int, error) {
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	var kept []PluginProcessRecord
	killed := 0
	for _, r := range records {
		if r.InstallDir != "" && dirExists(r.InstallDir) {
			kept = append(kept, r)
			continue
		}
		if err := signalProcess(r.PID); err == nil {
			killed++
		}
	}
	if err := SavePluginProcessRegistry(path, kept); err != nil {
		return killed, err
	}
	return killed, nil
}

// PluginCachePruneStats is what PruneMarketplaceCache returns so the
// startup hook can log a one-line summary without re-walking the tree.
type PluginCachePruneStats struct {
	StagingRemoved      int
	SupersededRemoved   int
	MarketplacesScanned int
	PluginsScanned      int
}

// PruneMarketplaceCache sweeps the marketplace cache under root. Two
// kinds of entries are removed:
//
//   - Staging directories: anything named "staging" at any depth under
//     <root>. The reference uses these as scratch space while a
//     download is in flight; an interrupted download leaves a stale
//     staging dir behind that the next launch should drop.
//
//   - Superseded plugin versions: under
//     <root>/<mkt>/plugins/<plugin>/<version>/, any version directory
//     whose <version> differs from the installed plugin's manifest
//     version (the JSON plugin.json file under
//     <installed plugin root>/). The currently-installed version is
//     kept; every other version on disk is removed.
//
// installedVersionFn is a hook so the caller can resolve "what version
// is installed?" without this package having to know the installed
// plugin layout. The signature returns "" when the plugin is not
// installed (in which case every cached version is superseded).
//
// The walk is read-only on the registry: the cache layout is owned by
// the marketplace downloader, this function only removes entries.
func PruneMarketplaceCache(root string, installedVersion func(pluginName string) string) (PluginCachePruneStats, error) {
	stats := PluginCachePruneStats{}
	if strings.TrimSpace(root) == "" {
		return stats, nil
	}
	stagingRemoved, err := removeStagingDirs(root)
	if err != nil {
		return stats, err
	}
	stats.StagingRemoved = stagingRemoved

	mkts, err := os.ReadDir(root)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return stats, nil
		}
		return stats, err
	}
	for _, mkt := range mkts {
		if !mkt.IsDir() {
			continue
		}
		stats.MarketplacesScanned++
		if err := pruneMarketplacePlugins(root, mkt.Name(), &stats, installedVersion); err != nil {
			return stats, err
		}
	}
	return stats, nil
}

func pruneMarketplacePlugins(root, mktName string, stats *PluginCachePruneStats, installedVersion func(string) string) error {
	pluginsDir := filepath.Join(root, mktName, "plugins")
	pluginEntries, err := os.ReadDir(pluginsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	for _, plugin := range pluginEntries {
		if !plugin.IsDir() {
			continue
		}
		stats.PluginsScanned++
		kept := installedVersion(plugin.Name())
		versionsDir := filepath.Join(pluginsDir, plugin.Name())
		removed, err := removeSupersededVersions(versionsDir, kept)
		if err != nil {
			return err
		}
		stats.SupersededRemoved += removed
	}
	return nil
}

// removeStagingDirs walks root and removes any directory named
// "staging" it finds. The walk is bounded by filepath.WalkDir so
// symlink loops (rare, but possible in the marketplace cache) cannot
// stall the startup hook.
func removeStagingDirs(root string) (int, error) {
	count := 0
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A deleted-out-from-under-us entry is uninteresting;
			// permission errors get reported so the operator can
			// triage without the startup hook crashing.
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if d.Name() != "staging" {
			return nil
		}
		if err := os.RemoveAll(path); err != nil {
			return err
		}
		count++
		// Skip the rest of the (now-removed) subtree.
		return filepath.SkipDir
	})
	return count, err
}

// removeSupersededVersions deletes every version directory under
// versionsDir whose name != keep. Returns the number of directories
// removed. An empty keep removes every version (the plugin is not
// installed locally, so nothing is current).
func removeSupersededVersions(versionsDir, keep string) (int, error) {
	entries, err := os.ReadDir(versionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if e.Name() == keep {
			continue
		}
		if err := os.RemoveAll(filepath.Join(versionsDir, e.Name())); err != nil {
			return removed, err
		}
		removed++
	}
	return removed, nil
}
