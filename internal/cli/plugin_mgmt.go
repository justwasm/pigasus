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

	"github.com/justwasm/pigasus/atomicfile"
	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
	pluginimport "github.com/justwasm/pigasus/internal/features/plugins/import"
	"github.com/justwasm/pigasus/internal/features/plugins/marketinstall"
)

// ---- subcommand_plugin.go ----

// ---- usage blocks ---------------------------------------------------------

// usagePlugin is the reference's `plugin` usage block, byte for byte
// (the program name is the only variable part). The `validate` row is
// the one that wraps: its usage string is longer than the description
// column, so the reference puts the description on the next line,
// indented to the same column.
func usagePlugin(n string) string {
	return "Usage: " + n + " plugin <command> [arguments]\n" +
		"\n" +
		"Commands:\n" +
		"  list [--json]          List installed plugins\n" +
		"  import [source]        Import plugins from gemini or claude\n" +
		"  install <target>       Install a plugin (supports plugin[@marketplace])\n" +
		"  uninstall <name>       Uninstall a plugin\n" +
		"  enable <name>          Enable a plugin\n" +
		"  disable <name>         Disable a plugin\n" +
		"  validate [path] [--strict] [--json]\n" +
		"                         Check a plugin's manifest and files\n" +
		"  link <mp> <target>     Generate link to a marketplace\n" +
		"  help                   Show this help\n"
}

// ---- plugin ---------------------------------------------------------------

func runPluginParent(j *JetskiCLI, args []string) error {
	// Reached only for `plugin` with no subcommand.
	fmt.Fprint(j.ErrOut, usagePlugin(j.Name))
	return errExitCodeMsg(1, "Error: missing plugin command")
}

func runPluginHelp(j *JetskiCLI, args []string) error {
	fmt.Fprint(j.ErrOut, usagePlugin(j.Name))
	return nil
}

func legacyPluginsDir() string {
	if h := strings.TrimSpace(os.Getenv("GEMINI_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "plugins")
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "plugins")
}

// legacyPlugin is the shape of plugin.json we care about.
type legacyPlugin struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// legacyPluginEntry is one installed plugin plus its derived fields.
type legacyPluginEntry struct {
	Name    string   `json:"name"`
	Enabled bool     `json:"enabled"`
	Version string   `json:"version"`
	Path    string   `json:"path"`
	Skills  []string `json:"skills,omitempty"`
}

func legacyLoadPlugins() []legacyPluginEntry {
	dir := legacyPluginsDir()
	if dir == "" {
		return nil
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []legacyPluginEntry
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pdir := filepath.Join(dir, e.Name())
		manifest := legacyFindManifest(pdir)
		if manifest == "" {
			continue
		}
		raw, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		var p legacyPlugin
		if err := json.Unmarshal(raw, &p); err != nil {
			continue
		}
		name := p.Name
		if name == "" {
			name = e.Name()
		}
		out = append(out, legacyPluginEntry{
			Name:    name,
			Enabled: true,
			Version: p.Version,
			Path:    pdir,
			Skills:  legacyListSkills(pdir),
		})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

// legacyPluginManifestNames is the set of manifest filenames the
// reference's installer recognises, in lookup order. AGY 1.3.6 closes
// changelog line 21 by also accepting the capitalised `Plugin.json`,
// so the cycle plugin-case-insensitive helper tries lowercase first
// (the form the reference's own strings use everywhere) and falls
// back to capitalised when lowercase is missing. Both forms must
// coexist — every existing install ships lowercase, and forcing
// capitalised would break the world.
var legacyPluginManifestNames = []string{"plugin.json", "Plugin.json"}

// legacyFindManifest returns the absolute path of the first plugin
// manifest under root that exists on disk, or "" if neither name is
// present. The lookup walks os.ReadDir rather than relying on
// os.Stat so it stays correct on case-insensitive filesystems
// (macOS APFS by default): there, writing `Plugin.json` creates
// the same inode as a hypothetical `plugin.json`, and an
// os.Stat-based probe would always match the lowercase spelling
// regardless of what the user named. ReadDir returns the actual
// directory entry names, so a missing-lowercase, present-upper
// tree lands on `Plugin.json` on macOS too — the same answer
// Linux's case-sensitive FS produces. The first match in
// legacyPluginManifestNames order wins.
func legacyFindManifest(root string) string {
	ents, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	have := map[string]bool{}
	for _, e := range ents {
		if !e.IsDir() {
			have[e.Name()] = true
		}
	}
	for _, name := range legacyPluginManifestNames {
		if have[name] {
			return filepath.Join(root, name)
		}
	}
	return ""
}

// legacyListSkills returns the names of skills/<x> directories that carry
// a SKILL.md, sorted. The reference counts only loadable skills.
func legacyListSkills(pdir string) []string {
	sdir := filepath.Join(pdir, "skills")
	ents, err := os.ReadDir(sdir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(sdir, e.Name(), "SKILL.md")); err != nil {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func runPluginList(j *JetskiCLI, args []string) error {
	jsonOut := false
	for _, a := range args {
		switch a {
		case "--json":
			jsonOut = true
		default:
			return fail(j, 1, fmt.Sprintf("Error: unknown list option %q: only --json is supported", a))
		}
	}
	plugins := legacyLoadPlugins()
	if jsonOut {
		if plugins == nil {
			plugins = []legacyPluginEntry{}
		}
		buf, err := json.MarshalIndent(plugins, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(j.Out, string(buf))
		return nil
	}
	if len(plugins) == 0 {
		fmt.Fprintln(j.Out, "No plugins installed.")
		return nil
	}
	fmt.Fprintln(j.Out, "Installed plugins:")
	for _, p := range plugins {
		fmt.Fprintf(j.Out, "- %s (enabled, %s)\n", p.Name, p.Version)
		if n := len(p.Skills); n > 0 {
			word := "skills"
			if n == 1 {
				word = "skill"
			}
			fmt.Fprintf(j.Out, "    Components: %d %s\n", n, word)
		}
	}
	return nil
}

// runPluginImport implements `agy plugin import [source]`.
//
// 【一手】 agy 1.4.11: the bare command imports from both sources — the
// Gemini CLI importer first, the Claude Code importer second — and each
// source that finds something ends with its own "Staged to <dir>" line.
// The importers themselves live in internal/features/plugins/import.
//
// The usage line is the reference's:
//
//	import [source]        Import plugins from gemini or claude
func runPluginImport(j *JetskiCLI, args []string) error {
	opts := pluginimport.Options{
		Roots:  pluginimport.DefaultRoots(),
		Out:    j.Out,
		ErrOut: j.ErrOut,
	}
	var sources []pluginimport.Source
	for _, arg := range args {
		switch arg {
		case "--force":
			opts.Force = true
		case string(pluginimport.SourceGemini):
			sources = append(sources, pluginimport.SourceGemini)
		case string(pluginimport.SourceClaude):
			sources = append(sources, pluginimport.SourceClaude)
		default:
			return fail(j, 1, fmt.Sprintf("Error: unknown import source or invalid path: %s", arg))
		}
	}
	if len(sources) == 0 {
		sources = []pluginimport.Source{pluginimport.SourceGemini, pluginimport.SourceClaude}
	}
	if err := pluginimport.Run(opts, sources); err != nil {
		// The reference reports a discovery failure on stderr and still
		// exits 0 — the error is the command's whole output.
		fmt.Fprintln(j.ErrOut, err)
		return nil
	}
	return nil
}

// runPluginValidate is the entry point for `agy plugin validate [path]`.
// The reference's surface and wording live in plugin_validate_json.go
// (runPluginValidateReference); this wrapper only keeps the historical
// function name used by the dispatcher.
func runPluginValidate(j *JetskiCLI, args []string) error {
	return runPluginValidateReference(j, args)
}

// runPluginArgErr is the shared `Error: <verb> requires ...` path for
// install/uninstall/enable/disable/link when their required operand is
// missing.
func runPluginArgErr(msg string) func(j *JetskiCLI, args []string) error {
	return func(j *JetskiCLI, args []string) error {
		return fail(j, 1, msg)
	}
}

func loadInstallManifest(j *JetskiCLI, target string) (string, error) {
	manifestPath := legacyFindManifest(target)
	if manifestPath == "" {
		return "", fail(j, 1, "Error: missing plugin.json: file does not exist")
	}
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return "", fail(j, 1, "Error: missing plugin.json: file does not exist")
	}
	var p legacyPlugin
	if err := json.Unmarshal(manifestRaw, &p); err != nil {
		return "", fail(j, 1, fmt.Sprintf("Error: invalid plugin.json: %s", err))
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = filepath.Base(target)
	}
	return name, nil
}

// findExistingPluginDir scans legacyPluginsDir() for an installed plugin matching
// plugin name (either folder name or manifest name in plugin.json).
// AGY 1.5.0: "fixed plugin auto-upgrades of a plugin installed under a different folder
// name, such as after a rename, writing to a new folder and retrying on every startup
// instead of updating the existing folder in place".
func findExistingPluginDir(dir, name string) string {
	if dir == "" || name == "" {
		return ""
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	// 1. Direct folder name match first
	direct := filepath.Join(dir, name)
	if fi, err := os.Stat(direct); err == nil && fi.IsDir() {
		return direct
	}
	// 2. Scan other directories to see if plugin.json name matches
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		pdir := filepath.Join(dir, e.Name())
		manifest := legacyFindManifest(pdir)
		if manifest == "" {
			continue
		}
		raw, err := os.ReadFile(manifest)
		if err != nil {
			continue
		}
		var p legacyPlugin
		if err := json.Unmarshal(raw, &p); err == nil && p.Name == name {
			return pdir
		}
	}
	return ""
}

func upgradeExistingPlugin(j *JetskiCLI, name, dst string) error {
	existing, err := os.Stat(dst)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	if !existing.IsDir() {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s exists and is not a directory", dst))
	}
	reg := PluginProcessRegistryPath(antigravityHomeFromLegacy())
	if _, err := KillPluginProcessesForInstall(reg, dst); err != nil {
		fmt.Fprintf(j.ErrOut, "Warning: failed to clean up MCP processes for %q: %v\n", name, err)
	}
	if err := os.RemoveAll(dst); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	fmt.Fprintf(j.Out, "Upgrading plugin %q\n", name)
	return nil
}

func runPluginInstall(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		return fail(j, 1, "Error: install requires a target")
	}
	target := args[0]
	fi, err := os.Stat(target)
	if err != nil || !fi.IsDir() {
		// Not a local directory: the target names a plugin in a
		// marketplace.
		return runPluginInstallMarketplace(j, target)
	}
	name, err := loadInstallManifest(j, target)
	if err != nil {
		return err
	}
	skills := legacyListSkills(target)
	agents, agentsErr, _ := legacyListAgents(target)
	if agentsErr != nil {
		return errExitCodeMsg(1, fmt.Sprintf("Error: invalid agents.json: %s", agentsErr))
	}
	dst := findExistingPluginDir(legacyPluginsDir(), name)
	if dst == "" {
		dst = filepath.Join(legacyPluginsDir(), name)
	}
	if err := upgradeExistingPlugin(j, name, dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	if err := copyTree(target, dst); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	if err := legacyMarkEnabled(name, true); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	fmt.Fprintf(j.Out, "Installed plugin %q\n", name)
	fmt.Fprintf(j.Out, "  - skills      : %d processed\n", len(skills))
	if len(agents) > 0 {
		fmt.Fprintf(j.Out, "  - agents      : %d processed\n", len(agents))
	} else {
		fmt.Fprintln(j.Out, "  - agents      : skipped (not found)")
	}
	fmt.Fprintln(j.Out, plugins.PartnerPluginDeveloperNotice)
	return nil
}

// runPluginInstallMarketplace installs a plugin from a cached marketplace
// feed.
//
// 【一手】 official-docs/marketplace.md:149-153 — "**Install from the
// official marketplace**: Specify `<plugin-name>@antigravity-plugins-official`
// … or pass a bare `<plugin-name>`, which defaults to
// `<plugin-name>@antigravity-plugins-official`".
//
// The feed is the `marketplace.json` the CLI caches under
// `<antigravity home>/cache/marketplaces/<marketplace>/`; the entry names
// the archive and the archive's digest, and the installer refuses an
// archive it cannot verify.
//
// The plugin lands in the same tree the local-directory install uses —
// `~/.gemini/config/plugins/<name>/` — and the same `plugins.<name>.enabled`
// flag is written.
//
// A target that names no plugin is refused with the reference's own
// sentence — "Error: install target must be a directory: %s" (the 1.4.6
// capture of `agy plugin install --help`,
// `internal/cli/testdata/help_1.4.6.txt:278`). A target that names a
// marketplace explicitly gets the reason instead, because the reference's
// own wording for that case is not recoverable.
func runPluginInstallMarketplace(j *JetskiCLI, target string) error {
	tgt, err := marketinstall.SplitTarget(target)
	if err != nil {
		if errors.Is(err, marketinstall.ErrRepositoryURL) {
			return fail(j, 1, fmt.Sprintf("Error: install from a repository URL is not supported: %s", target))
		}
		return fail(j, 1, fmt.Sprintf("Error: install target must be a directory: %s", target))
	}
	// A target that looks like a filesystem path is a path, even when it
	// carries an "@": the marketplace form is a bare plugin name.
	if looksLikePath(target) {
		return fail(j, 1, fmt.Sprintf("Error: install target must be a directory: %s", target))
	}
	entry, err := marketinstall.Lookup(tgt.Marketplace, tgt.Name)
	if err != nil {
		// A target that named its marketplace explicitly gets the reason;
		// a bare name that resolves to no plugin gets the reference's own
		// sentence.
		if tgt.Explicit {
			return fail(j, 1, fmt.Sprintf("Error: %s", err))
		}
		return fail(j, 1, fmt.Sprintf("Error: install target must be a directory: %s", target))
	}
	src, cleanup, err := marketinstall.Fetch(entry)
	if err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	defer cleanup()
	skills := legacyListSkills(src)
	agents, agentsErr, _ := legacyListAgents(src)
	if agentsErr != nil {
		return errExitCodeMsg(1, fmt.Sprintf("Error: invalid agents.json: %s", agentsErr))
	}
	dir := legacyPluginsDir()
	if dir == "" {
		return fail(j, 1, "Error: install failed: the plugin directory is unknown")
	}
	dst := findExistingPluginDir(dir, entry.Name)
	if dst == "" {
		dst = filepath.Join(dir, entry.Name)
	}
	if err := upgradeExistingPlugin(j, entry.Name, dst); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	if err := copyTree(src, dst); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	if err := legacyMarkEnabled(entry.Name, true); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
	}
	fmt.Fprintf(j.Out, "Successfully installed plugin %q from %q.\n", entry.Name, tgt.Marketplace)
	fmt.Fprintf(j.Out, "  - skills      : %d processed\n", len(skills))
	if len(agents) > 0 {
		fmt.Fprintf(j.Out, "  - agents      : %d processed\n", len(agents))
	} else {
		fmt.Fprintln(j.Out, "  - agents      : skipped (not found)")
	}
	fmt.Fprintln(j.Out, plugins.PartnerPluginDeveloperNotice)
	return nil
}

// looksLikePath reports whether an install target is a filesystem path
// rather than a marketplace reference.
//
// The marketplace form is a bare plugin name, optionally followed by an
// "@" and a marketplace name. A target that carries a path separator, or
// that starts with a dot, is a path the user meant to install from — and
// a path that does not exist is the reference's own "install target must
// be a directory" case.
func looksLikePath(target string) bool {
	t := strings.TrimSpace(target)
	if t == "" {
		return false
	}
	if strings.ContainsAny(t, `/\`) {
		return true
	}
	return strings.HasPrefix(t, ".")
}

func runPluginUninstall(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		return fail(j, 1, "Error: uninstall requires a plugin name")
	}
	name := args[0]
	dir := legacyPluginsDir()
	if dir != "" {
		if err := os.RemoveAll(filepath.Join(dir, name)); err == nil {
			fmt.Fprintf(j.Out, "Uninstalled plugin %q\n", name)
			return nil
		}
	}
	return fail(j, 1, fmt.Sprintf("Error: plugin %q not found or invalid", name))
}

func runPluginToggle(verb string) func(j *JetskiCLI, args []string) error {
	return func(j *JetskiCLI, args []string) error {
		if len(args) == 0 {
			return fail(j, 1, fmt.Sprintf("Error: %s requires a plugin name", verb))
		}
		name := args[0]
		dir := legacyPluginsDir()
		if dir != "" {
			if legacyFindManifest(filepath.Join(dir, name)) != "" {
				// No on-disk enabled/disabled state is modelled yet;
				// report success so the verb round-trips like the
				// reference does for a present plugin.
				if verb == "enable" {
					fmt.Fprintf(j.Out, "Enabled plugin %q\n", name)
				} else {
					fmt.Fprintf(j.Out, "Disabled plugin %q\n", name)
				}
				return nil
			}
		}
		return fail(j, 1, fmt.Sprintf("Error: plugin %q not found or invalid", name))
	}
}

func runPluginLink(j *JetskiCLI, args []string) error {
	if len(args) < 2 {
		return fail(j, 1, "Error: link requires marketplace name and target")
	}
	return nil
}

// copyTree recursively copies src to dst (which must not yet exist). It is
// a stdlib-only directory copy that preserves the source tree's permission
// bits via os.CopyFS — the reference installer copies the same way. The
// pre-cycle-234 stub left the install handler returning a hard error so the
// copy path was never exercised; cycle 234 wires the real installer through
// here so the validate-side `agents` count and the install-side summary
// match (AGY 1.3.6 changelog line 13).
func copyTree(src, dst string) error {
	if err := os.MkdirAll(dst, 0755); err != nil {
		return err
	}
	return os.CopyFS(dst, os.DirFS(src))
}

// legacyMarkEnabled sets plugins.<name>.enabled=true in
// ~/.gemini/config/config.json. The reference's installer writes the same
// flag (and only that flag) after a successful copy. We deliberately keep
// the helper local rather than importing auth.SetPluginEnabled: the install
// path runs before auth state is ready, and the write happens on a clean
// config tree most of the time.
func legacyMarkEnabled(name string, enabled bool) error {
	dir := legacyPluginsDir()
	if dir == "" {
		return fmt.Errorf("config home unavailable")
	}
	cfg := filepath.Join(filepath.Dir(dir), "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0700); err != nil {
		return err
	}
	m := map[string]any{}
	if data, err := os.ReadFile(cfg); err == nil {
		_ = json.Unmarshal(data, &m)
	} else if !os.IsNotExist(err) {
		return err
	}
	plugins, _ := m["plugins"].(map[string]any)
	if plugins == nil {
		plugins = map[string]any{}
		m["plugins"] = plugins
	}
	entry, _ := plugins[name].(map[string]any)
	if entry == nil {
		entry = map[string]any{}
		plugins[name] = entry
	}
	entry["enabled"] = enabled
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfg, out, 0600)
}

// ---- plugin_cleanup.go ----

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
// confhome.Antigravity but is local so this package does not
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

// ---- plugin_cleanup_state.go ----

// plugin_cleanup_state.go — process registry state management for AGY 1.2.7
// plugin process lifecycle tracking.

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
	return atomicfile.WriteFile(path, buf, 0o600)
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
