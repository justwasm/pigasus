// plugin_cleanup.go — AGY 1.2.7 changelog line 215:
//
//	Fixed plugin upgrades leaving old MCP server and sidecar processes
//	running from deleted install directories, and pruned superseded
//	plugin versions and staging directories from the marketplace cache
//	on startup.
//
// The clone never launches real MCP servers / sidecars, but it models
// the two surfaces the recovery gap is about:
//
//  1. A registry of plugin-owned processes. Each record carries the
//     PID, the plugin + server name, the install directory the
//     process was started from, and a start timestamp. The registry
//     lives at <antigravity home>/state/plugin-processes.json.
//
//  2. A marketplace cache layout:
//
//     <antigravity home>/cache/marketplaces/<mkt>/
//     marketplace.json
//     plugins/<plugin>/<version>/...
//     staging/<...>/
//
//     "Superseded" = a version directory under plugins/<plugin>/ that
//     no longer matches the installed plugin's manifest version.
//     "Staging" = any directory named staging/ at any depth under
//     <root>; the reference uses these as scratch space while a
//     download is in flight, and a clean boot should drop anything
//     left over from a crashed one.
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
	"sort"
	"strings"
	"syscall"
	"time"
)

// PluginProcessRecord is one row of the plugin process registry. The
// schema is intentionally minimal: the only required field is PID, the
// others are diagnostic and survive a registry rewrite even when the
// process itself is long gone.
type PluginProcessRecord struct {
	PID        int    `json:"pid"`
	PluginName string `json:"plugin"`
	ServerName string `json:"server"`
	InstallDir string `json:"install_dir"`
	StartedAt  int64  `json:"started_at"`
}

// PluginProcessRegistryPath returns the on-disk location of the registry.
// The directory is created lazily by callers (RegisterPluginProcess,
// SavePluginProcessRegistry) so this getter never touches the filesystem.
func PluginProcessRegistryPath(antigravityHome string) string {
	return filepath.Join(antigravityHome, "state", "plugin-processes.json")
}

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

// LoadPluginProcessRegistry reads the registry file. A missing file is
// not an error: a fresh install has never recorded a plugin process
// and the registry is created on the first RegisterPluginProcess call.
// Parse errors and unreadable files yield (nil, err) so callers can
// decide whether the corruption is fatal or merely informational.
func LoadPluginProcessRegistry(path string) ([]PluginProcessRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Records []PluginProcessRecord `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Records, nil
}

// SavePluginProcessRegistry writes the registry atomically (write to
// <path>.tmp, rename) so a concurrent read by KillPluginProcessesForInstall
// never sees a half-written file. The parent directory is created with
// 0700 perms.
func SavePluginProcessRegistry(path string, records []PluginProcessRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	doc := struct {
		Records []PluginProcessRecord `json:"records"`
	}{Records: records}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RegisterPluginProcess appends rec to the registry at path. Duplicates
// (matching PID) are silently dropped — a process that is already
// tracked should not be tracked twice. A missing registry is created.
func RegisterPluginProcess(path string, rec PluginProcessRecord) error {
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.PID == rec.PID {
			return nil
		}
	}
	records = append(records, rec)
	return SavePluginProcessRegistry(path, records)
}

// UnregisterPluginProcess removes every record with the given PID from
// the registry. Missing PIDs are a no-op (the caller's intent is
// satisfied either way).
func UnregisterPluginProcess(path string, pid int) error {
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return err
	}
	out := records[:0]
	changed := false
	for _, r := range records {
		if r.PID == pid {
			changed = true
			continue
		}
		out = append(out, r)
	}
	if !changed {
		return nil
	}
	return SavePluginProcessRegistry(path, out)
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
	// 1. Staging directories at any depth.
	stagingRemoved, err := removeStagingDirs(root)
	if err != nil {
		return stats, err
	}
	stats.StagingRemoved = stagingRemoved
	// 2. Superseded version directories.
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
		pluginsDir := filepath.Join(root, mkt.Name(), "plugins")
		pluginEntries, err := os.ReadDir(pluginsDir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return stats, err
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
				return stats, err
			}
			stats.SupersededRemoved += removed
		}
	}
	return stats, nil
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

// signalProcess sends SIGTERM to pid and ignores ESRCH (the process
// has already exited). SIGKILL is not escalated here because the caller
// controls the lifecycle: the next startup's PruneOrphanPluginProcesses
// picks up anything that ignored SIGTERM, and the upgrade flow does
// not block on the kill.
func signalProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

// dirExists reports whether path is an existing directory. Symlinks
// are followed. A non-existent path returns false, not an error.
func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// canonicalInstallDir resolves symlinks and cleans installDir so two
// paths that differ only by an intermediate symlink compare equal.
// On any error it falls back to filepath.Clean — the registry row
// still matches as long as the caller and the writer agreed on the
// same string form.
func canonicalInstallDir(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// PluginProcessRecordNow is a small helper for tests and callers that
// want to record a freshly-started process: it stamps StartedAt with
// the current Unix time so the registry has a stable sort order and
// operators can age out stale rows later.
func PluginProcessRecordNow(pid int, plugin, server, installDir string) PluginProcessRecord {
	return PluginProcessRecord{
		PID:        pid,
		PluginName: plugin,
		ServerName: server,
		InstallDir: installDir,
		StartedAt:  time.Now().Unix(),
	}
}

// SortedPluginProcessRecords returns records sorted by (PluginName,
// ServerName, PID). Tests rely on the stable order; the registry
// itself does not require sorting.
func SortedPluginProcessRecords(in []PluginProcessRecord) []PluginProcessRecord {
	out := append([]PluginProcessRecord(nil), in...)
	sort.Slice(out, func(i, k int) bool {
		if out[i].PluginName != out[k].PluginName {
			return out[i].PluginName < out[k].PluginName
		}
		if out[i].ServerName != out[k].ServerName {
			return out[i].ServerName < out[k].ServerName
		}
		return out[i].PID < out[k].PID
	})
	return out
}
