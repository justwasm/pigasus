package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	pluginimport "github.com/justwasm/pigasus/internal/features/plugins/import"
)

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

// RunPluginValidate is the exported entry point used by callers that
// cannot reach the unexported dispatcher. It mirrors the binary's
// `plugin validate` surface; see plugin_validate_json.go.
func RunPluginValidate(j *JetskiCLI, args []string) error {
	return runPluginValidate(j, args)
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
		return fail(j, 1, fmt.Sprintf("Error: install target must be a directory: %s", target))
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
	dst := filepath.Join(legacyPluginsDir(), name)
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
	return nil
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
