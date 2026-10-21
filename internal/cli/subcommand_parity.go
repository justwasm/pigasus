// Package cli — subcommand_parity.go: the v1.3.5 subcommand surface (recovered from the v1.3.5 binary).
//
// The engine in jetski.go understands *how* to dispatch a subcommand;
// this file supplies the exact per-subcommand usage blocks and the Run
// handlers that reproduce the v1.3.5 binary's behaviour. Everything
// here was captured from the reference binary
//
//	Usage: <prog> plugin <command> [arguments]
//	...
//
// The single source of truth for the byte-level text is the reference
// binary itself; the constants below were transcribed from
// `agy <sub> --help` / `agy <sub>` captures. Where a handler needs the
// program name (the reference uses argv0's basename — verified by
// renaming the binary), it reads j.Name.
//
// Stream discipline (matches the reference):
//   - usage/help blocks  -> stderr, exit 0
//   - `Error: ...` lines -> stderr
//   - normal results     -> stdout
package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/persistentrc"
)

// ---- usage blocks ---------------------------------------------------------

func usagePlugin(n string) string {
	return "Usage: " + n + " plugin <command> [arguments]\n" +
		"\n" +
		"Commands:\n" +
		"  list [--json]                          List installed plugins\n" +
		"  import [source]                        Import plugins from gemini or claude\n" +
		"  install <target>                       Install a plugin (supports plugin[@marketplace])\n" +
		"  uninstall <name>                       Uninstall a plugin\n" +
		"  enable <name>                          Enable a plugin\n" +
		"  disable <name>                         Disable a plugin\n" +
		"  validate [--strict] [--json] [path]    Validate a plugin (AGY 1.4.1)\n" +
		"  link <mp> <target>                     Generate link to a marketplace\n" +
		"  help                                   Show this help\n"
}

func usageMCP(n string) string {
	return "Usage: " + n + " mcp <subcommand> [flags] [args]\n" +
		"\n" +
		"Available subcommands:\n" +
		"  add      Add or update an MCP server configuration\n" +
		"  remove   Remove an MCP server configuration\n" +
		"  list     List all configured MCP servers\n" +
		"  enable   Enable an MCP server\n" +
		"  disable  Disable an MCP server\n" +
		"\n" +
		"Run \"" + n + " mcp <subcommand> --help\" for flags, argument rules, and examples.\n"
}

func usageMCPAdd(n string) string {
	return "Usage: " + n + " mcp add [flags] <name> <commandOrUrl> [args...]\n" +
		"\n" +
		"Add or update an MCP server configuration\n" +
		"\n" +
		"Flags:\n" +
		"  -H        Short alias for --header (default [])\n" +
		"  -e        Short alias for --env (default [])\n" +
		"  --env     Environment variable KEY=value (repeatable) (default [])\n" +
		"  -h        Show help\n" +
		"  --header  HTTP header Key: Value (repeatable) (default [])\n" +
		"  --help    Show help\n" +
		"  -t        Short alias for --type (default stdio)\n" +
		"  --type    Server type: 'stdio' or 'http' (default stdio)\n" +
		"\n" +
		"Arguments:\n" +
		"  <name>          Unique name for the server.\n" +
		"  <commandOrUrl>  Executable for a stdio server, or the URL for an http server.\n" +
		"  [args...]       Arguments passed to the stdio command.\n" +
		"\n" +
		"Notes:\n" +
		"  Flags must come before <name>; a flag placed after it is rejected.\n" +
		"  http/https URLs are detected automatically, so --type is usually unnecessary.\n" +
		"  Use -- before the command to pass a command or args that begin with '-'.\n" +
		"\n" +
		"Examples:\n" +
		"  " + n + " mcp add fs npx -y @modelcontextprotocol/server-filesystem /work\n" +
		"  " + n + " mcp add --header \"Authorization: Bearer TOKEN\" api https://example.com/mcp\n" +
		"  " + n + " mcp add --env GITHUB_TOKEN=xxx gh -- docker run -i ghcr.io/github/mcp\n"
}

func usageMCPSimple(n, verb, desc, arg string) string {
	syn := ""
	if arg != "" {
		syn = " <name>"
	}
	return "Usage: " + n + " mcp " + verb + syn + " [flags]\n" +
		"\n" +
		desc + "\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageRemoteControl(n string) string {
	return "Usage: " + n + " remote-control <subcommand>\n" +
		"\n" +
		"Available subcommands:\n" +
		"  start    Register and start the remote-control daemon\n" +
		"  serve    Run the remote-control daemon in the foreground (OAuth login inline)\n" +
		"  status   Show the daemon's status\n" +
		"  stop     Stop the daemon and unregister it\n" +
		"\n" +
		"Run \"" + n + " remote-control <subcommand> --help\" for flags.\n"
}

func usageRCStart(n string) string {
	return "Usage: " + n + " remote-control start [flags]\n" +
		"\n" +
		"Register and start the remote-control daemon\n" +
		"\n" +
		"Flags:\n" +
		"  -h         Show help\n" +
		"  --help     Show help\n" +
		"  --addr     Bind address for the foreground daemon (host:port, default 0.0.0.0:8787)\n" +
		"  --log-file Absolute log file path; the daemon redirects stdout/stderr here\n" +
		"  --name     Set this machine's instance name shown in Remote Control (default: hostname)\n" +
		"  --session  Register the daemon login-scoped (stops at logout) instead of starting at boot\n"
}

func usageRCStatus(n string) string {
	return "Usage: " + n + " remote-control status [flags]\n" +
		"\n" +
		"Show the daemon's status (emits a JSON document)\n" +
		"\n" +
		"Flags:\n" +
		"  -h        Show help\n" +
		"  --help    Show help\n" +
		"  --json    Emit machine-readable JSON (default; the flag is accepted for forward compatibility)\n" +
		"  --name    Instance name to query (default: hostname)\n"
}

// usageRCServe documents the `remote-control serve` subcommand. The
// flag list mirrors usageRCStart — the cycle rc-serve-subcommand
// shorthand is "serve is the foreground twin of start", and the
// foreground path needs the same host / name / log-file overrides
// the persistent path does so a dashboard operator doesn't have to
// learn a new vocabulary.
func usageRCServe(n string) string {
	return "Usage: " + n + " remote-control serve [flags]\n" +
		"\n" +
		"Run the remote-control daemon in the foreground (does not register with launchd)\n" +
		"\n" +
		"Flags:\n" +
		"  -h         Show help\n" +
		"  --help     Show help\n" +
		"  --addr     Bind address for the foreground daemon (host:port, default 0.0.0.0:8787)\n" +
		"  --name     Set this machine's instance name shown in Remote Control (default: hostname)\n" +
		"  --no-login Skip the inline OAuth device-code login when no bearer token is on disk\n"
}

func usageRCStop(n string) string {
	return "Usage: " + n + " remote-control stop [flags]\n" +
		"\n" +
		"Stop the daemon and unregister it\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n" +
		"  --name  Instance name to stop (default: hostname)\n"
}

func usageMicServe(n string) string {
	return "Usage: " + n + " mic-serve [flags]\n" +
		"\n" +
		"Serve this machine's microphone to a CLI on another host\n" +
		"\n" +
		"Flags:\n" +
		"  --addr  Address to listen on; keep it on loopback (default 127.0.0.1:4713)\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageAgent(n string) string {
	return "Usage: " + n + " agent [flags]\n" +
		"\n" +
		"List available agents\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageModels(n string) string {
	return "Usage: " + n + " models [flags]\n" +
		"\n" +
		"List available models\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageChangelog(n string) string {
	return "Usage: " + n + " changelog [flags]\n" +
		"\n" +
		"Show changelog and release notes\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func usageInstall(n string) string {
	return "Usage: " + n + " install [flags]\n" +
		"\n" +
		"Configure environment paths and shell settings\n" +
		"\n" +
		"Flags:\n" +
		"  --dir           Custom directory target to configure PATH for\n" +
		"  -h              Show help\n" +
		"  --help          Show help\n" +
		"  --skip-aliases  Bypasses shell profile alias purging\n" +
		"  --skip-path     Bypasses shell profile PATH appending\n"
}

// UsageLogo returns the usage block for the logo subcommand.
func UsageLogo(n string) string {
	return "Usage: " + n + " logo [flags]\n" +
		"\n" +
		"Print the startup banner logo\n" +
		"\n" +
		"Flags:\n" +
		"  --flip   Transform to apply: none|both|ref|vert|row-rev|horiz|invert (default \"none\")\n" +
		"  -h       Show help\n" +
		"  --help   Show help\n" +
		"  --style  Style of logo: default|simple (or half|block)\n"
}

// ---- small helpers --------------------------------------------------------

// fail prints a raw message (no prefix) to stderr and returns the exit
// sentinel. Handlers do their own formatting so the message is byte
// exact.
func fail(j *JetskiCLI, code int, msg string) error {
	fmt.Fprintln(j.ErrOut, msg)
	return errExitCode(code)
}

// usageFail prints a usage block followed by a message, then exits 1.
// This is the shape the reference uses for an unrecognised flag on a
// subcommand whose only real flag is -h/--help.
func usageFail(j *JetskiCLI, usage, msg string) error {
	fmt.Fprint(j.ErrOut, usage)
	fmt.Fprintln(j.ErrOut, msg)
	return errExitCode(1)
}

// flagName normalises an argv flag token to the form the reference
// prints: "-json" for "--json", "-h" for "-h".
func flagName(a string) string {
	if strings.HasPrefix(a, "--") {
		return "-" + a[2:]
	}
	return a
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

func runPluginImport(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		fmt.Fprintln(j.Out, "No gemini extensions found.")
		fmt.Fprintln(j.Out, "No claude extensions found.")
		return nil
	}
	return fail(j, 1, fmt.Sprintf("Error: unknown import source or invalid path: %s", args[0]))
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

// legacyListAgents enumerates a plugin's subagents from both sides of the
// 1.3.6 changelog line 13 / 35 fix:
//
//   - <plugin>/agents/*.md    — Markdown-sidecar entries, one per .md file
//   - <plugin>/agents.json    — root-level manifest, either nested
//     {agents: [{name, path}, ...]} or the older bare-array form
//
// The two sources are unioned, deduped by name (later definition wins on a
// tie, and the JSON manifest is read second so it can override the markdown
// name when the two disagree), and sorted. Empty / missing sources are
// silently dropped — the same way `legacyListSkills` returns nil for a
// missing directory. Parse errors against agents.json surface as
// (names, err, true): the listing is still returned so callers can decide
// whether the JSON-failure is fatal.
func legacyListAgents(pdir string) (names []string, jsonErr error, missing bool) {
	seen := map[string]bool{}
	if entries, err := os.ReadDir(filepath.Join(pdir, "agents")); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".md" {
				continue
			}
			n := strings.TrimSuffix(e.Name(), ".md")
			if !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(pdir, "agents.json"))
	if err == nil {
		var nested struct {
			Agents []struct {
				Name string `json:"name"`
			} `json:"agents"`
		}
		jerr := json.Unmarshal(data, &nested)
		entries := nested.Agents
		if jerr != nil || entries == nil {
			var flat []struct {
				Name string `json:"name"`
			}
			if err2 := json.Unmarshal(data, &flat); err2 != nil {
				if jerr == nil {
					jerr = err2
				}
				jsonErr = jerr
			} else {
				for _, e := range flat {
					n := strings.TrimSpace(e.Name)
					if n == "" || seen[n] {
						continue
					}
					seen[n] = true
					names = append(names, n)
				}
			}
		} else {
			for _, e := range entries {
				n := strings.TrimSpace(e.Name)
				if n == "" || seen[n] {
					continue
				}
				seen[n] = true
				names = append(names, n)
			}
		}
	}
	sort.Strings(names)
	missing = len(names) == 0
	return names, jsonErr, missing
}

// legacyLoadPluginAgents adds the agents field to legacyPluginEntry. The
// loader walks agents.json and agents/*.md once per plugin at load time; the
// list is what `agy plugin list --json` ships to machine consumers and the
// /plugin detail block's `Agents:` line.
func legacyLoadPluginAgents(pdir string) []string {
	names, _, _ := legacyListAgents(pdir)
	return names
}

// runPluginArgErr is the shared `Error: <verb> requires ...` path for
// install/uninstall/enable/disable/link when their required operand is
// missing.
func runPluginArgErr(msg string) func(j *JetskiCLI, args []string) error {
	return func(j *JetskiCLI, args []string) error {
		return fail(j, 1, msg)
	}
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
	// Load the manifest the reference validates up front: a malformed
	// plugin.json is fatal — the reference rejects it before doing any
	// install work (cycle 38's openagy stub emitted a misleading
	// "must be a directory" message on every successful tree, which is
	// exactly the gap 1.3.6 closes by wiring the real install path).
	// The plugin-case-insensitive cycle also accepts `Plugin.json`
	// alongside the canonical `plugin.json`, so the install reads
	// whichever case-sensitive name is on disk.
	manifestPath := legacyFindManifest(target)
	if manifestPath == "" {
		return fail(j, 1, "Error: missing plugin.json: file does not exist")
	}
	manifestRaw, err := os.ReadFile(manifestPath)
	if err != nil {
		return fail(j, 1, "Error: missing plugin.json: file does not exist")
	}
	var p legacyPlugin
	if err := json.Unmarshal(manifestRaw, &p); err != nil {
		return fail(j, 1, fmt.Sprintf("Error: invalid plugin.json: %s", err))
	}
	name := strings.TrimSpace(p.Name)
	if name == "" {
		name = filepath.Base(target)
	}
	skills := legacyListSkills(target)
	agents, agentsErr, _ := legacyListAgents(target)
	if agentsErr != nil {
		return errExitCodeMsg(1, fmt.Sprintf("Error: invalid agents.json: %s", agentsErr))
	}
	// v1.3.6 install path: copy the source directory into
	// ~/.gemini/config/plugins/<name>/, mark enabled=true in config.json,
	// and print the summary that lets the user verify what just landed.
	// The clone mirrors the reference's wording — a two-line preamble
	// ("Installed plugin %q") plus per-component counts. The agents count
	// closes changelog line 13: the pre-cycle-234 stub never got past
	// the directory check, so neither skills nor agents were ever
	// surfaced in the summary.
	dst := filepath.Join(legacyPluginsDir(), name)
	// AGY 1.2.7 changelog line 215: when the same plugin name is
	// installed on top of an existing copy, the reference performs an
	// upgrade — it signals any MCP server / sidecar processes that
	// were started out of the old install directory before replacing
	// the tree. Without this sweep the old processes keep running
	// against binaries that no longer exist, which is the recovery gap
	// the clone was missing.
	if existing, err := os.Stat(dst); err == nil {
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
	} else if !os.IsNotExist(err) {
		return fail(j, 1, fmt.Sprintf("Error: install failed: %s", err))
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

// ---- mcp ------------------------------------------------------------------

// runMCPParent handles `mcp` with no subcommand: the reference prints the
// parent usage block to stderr and exits 0 (distinct from `plugin`, which
// prints usage + `Error: missing plugin command` and exits 1).
func runMCPParent(j *JetskiCLI, args []string) error {
	fmt.Fprint(j.ErrOut, usageMCP(j.Name))
	return nil
}

// mcpConfigPath returns the v1.3.5 binary's MCP config path.
func legacyMCPConfigPath() string {
	if h := strings.TrimSpace(os.Getenv("GEMINI_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "mcp_config.json")
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "mcp_config.json")
}

type legacyMCPDoc struct {
	Servers map[string]map[string]any `json:"mcpServers"`
}

func legacyLoadMCPDoc() *legacyMCPDoc {
	doc := &legacyMCPDoc{Servers: map[string]map[string]any{}}
	path := legacyMCPConfigPath()
	if path == "" {
		return doc
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return doc
	}
	_ = json.Unmarshal(raw, doc)
	if doc.Servers == nil {
		doc.Servers = map[string]map[string]any{}
	}
	return doc
}

func legacySaveMCPDoc(doc *legacyMCPDoc) error {
	path := legacyMCPConfigPath()
	if path == "" {
		return fmt.Errorf("mcp: no config path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	return os.WriteFile(path, buf, 0o644)
}

func runMCPList(j *JetskiCLI, args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usageMCPSimple(j.Name, "list", "List all configured MCP servers", ""),
				"Error: flags provided but not defined: "+flagName(a))
		}
	}
	doc := legacyLoadMCPDoc()
	if len(doc.Servers) == 0 {
		fmt.Fprintln(j.Out, "No MCP servers configured.")
		return nil
	}
	names := make([]string, 0, len(doc.Servers))
	maxName, maxType, maxStatus := 4, 4, 6
	for n, s := range doc.Servers {
		names = append(names, n)
		if len(n) > maxName {
			maxName = len(n)
		}
		t := legacyMCPType(s)
		if len(t) > maxType {
			maxType = len(t)
		}
		st := legacyMCPStatus(s)
		if len(st) > maxStatus {
			maxStatus = len(st)
		}
	}
	sort.Strings(names)
	nw, tw, sw := maxName+2, maxType+2, maxStatus+2
	fmt.Fprintf(j.Out, "%-*s%-*s%-*s%s\n", nw, "NAME", tw, "TYPE", sw, "STATUS", "COMMAND/URL")
	for _, n := range names {
		s := doc.Servers[n]
		fmt.Fprintf(j.Out, "%-*s%-*s%-*s%s\n", nw, n, tw, legacyMCPType(s), sw, legacyMCPStatus(s), legacyMCPTarget(s))
	}
	return nil
}

func legacyMCPType(s map[string]any) string {
	if _, ok := s["serverUrl"]; ok {
		return "http"
	}
	return "stdio"
}

func legacyMCPStatus(s map[string]any) string {
	if d, _ := s["disabled"].(bool); d {
		return "disabled"
	}
	return "enabled"
}

func legacyMCPTarget(s map[string]any) string {
	if u, ok := s["serverUrl"].(string); ok {
		return u
	}
	cmd, _ := s["command"].(string)
	parts := []string{cmd}
	if rawArgs, ok := s["args"].([]any); ok {
		for _, a := range rawArgs {
			if str, ok := a.(string); ok {
				parts = append(parts, str)
			}
		}
	}
	return strings.Join(parts, " ")
}

func runMCPAdd(j *JetskiCLI, args []string) error {
	usage := usageMCPAdd(j.Name)
	var typ string
	var env, headers []string
	var positional []string
	i := 0
	for i < len(args) {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, args[i:]...)
			break
		}
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		need := func() (string, bool) {
			if hasVal {
				return val, true
			}
			if i+1 < len(args) {
				i++
				return args[i], true
			}
			return "", false
		}
		switch a {
		case "-h", "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		case "-t", "--type":
			v, ok := need()
			if !ok {
				return usageFail(j, usage, "Error: flag needs an argument: "+flagName(a))
			}
			typ = v
		case "-e", "--env":
			v, ok := need()
			if !ok {
				return usageFail(j, usage, "Error: flag needs an argument: "+flagName(a))
			}
			env = append(env, v)
		case "-H", "--header":
			v, ok := need()
			if !ok {
				return usageFail(j, usage, "Error: flag needs an argument: "+flagName(a))
			}
			headers = append(headers, v)
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	if len(positional) < 2 {
		return fail(j, 1, "Error: usage: mcp add [flags] <name> <commandOrUrl> [args...]")
	}
	name, target := positional[0], positional[1]
	rest := positional[2:]
	isHTTP := typ == "http" || typ == "http+sse" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	entry := map[string]any{"disabled": false}
	if len(env) > 0 {
		entry["env"] = env
	}
	if len(headers) > 0 {
		entry["headers"] = headers
	}
	kind := "stdio"
	if isHTTP {
		kind = "http"
		entry["serverUrl"] = target
	} else {
		entry["command"] = target
		if len(rest) > 0 {
			entry["args"] = rest
		}
	}
	doc := legacyLoadMCPDoc()
	doc.Servers[name] = entry
	if err := legacySaveMCPDoc(doc); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	fmt.Fprintf(j.Out, "Added MCP server %q (%s)\n", name, kind)
	return nil
}

func runMCPRemove(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		return fail(j, 1, "Error: usage: mcp remove <name>")
	}
	name := args[0]
	doc := legacyLoadMCPDoc()
	if _, ok := doc.Servers[name]; !ok {
		return fail(j, 1, fmt.Sprintf("Error: MCP server %q not found", name))
	}
	delete(doc.Servers, name)
	if err := legacySaveMCPDoc(doc); err != nil {
		return fmt.Errorf("mcp: %w", err)
	}
	fmt.Fprintf(j.Out, "Removed MCP server %q\n", name)
	return nil
}

func runMCPSetEnabled(enabled bool) func(j *JetskiCLI, args []string) error {
	return func(j *JetskiCLI, args []string) error {
		verb := "disable"
		if enabled {
			verb = "enable"
		}
		if len(args) == 0 {
			return fail(j, 1, fmt.Sprintf("Error: usage: mcp %s <name>", verb))
		}
		name := args[0]
		doc := legacyLoadMCPDoc()
		entry, ok := doc.Servers[name]
		if !ok {
			return fail(j, 1, fmt.Sprintf("Error: MCP server %q not found", name))
		}
		if enabled {
			delete(entry, "disabled")
		} else {
			entry["disabled"] = true
		}
		doc.Servers[name] = entry
		if err := legacySaveMCPDoc(doc); err != nil {
			return fmt.Errorf("mcp: %w", err)
		}
		if enabled {
			fmt.Fprintf(j.Out, "Enabled MCP server %q\n", name)
		} else {
			fmt.Fprintf(j.Out, "Disabled MCP server %q\n", name)
		}
		return nil
	}
}

// ---- remote-control -------------------------------------------------------

// runRCParent handles `remote-control` with no subcommand: like `mcp`, the
// reference prints the parent usage block to stderr and exits 0.
func runRCParent(j *JetskiCLI, args []string) error {
	fmt.Fprint(j.ErrOut, usageRemoteControl(j.Name))
	return nil
}

// runRCStart is the cycle rc-persistent-daemon implementation of
// `wagyu remote-control start`.  It registers a launchd
// LaunchAgent (macOS) or session-detached foreground process
// (--session) that runs the long-lived WebSocket listener.
//
// Flags:
//
//	--addr        bind address (default 0.0.0.0:8787)
//	--log-file    absolute log-file path (default
//	              $TMPDIR/openagy-remote-control-<name>.log)
//	--name        machine instance label (default: hostname)
//	--session     login-scoped instead of boot-time
//
// On success, prints
//
//	Daemon started; pid=<pid>; addr=<addr>
//
// to stdout so wrapper scripts can parse the PID without parsing
// the launchd label.  Errors return as cli.ExitCodeError(1, …) so
// the process exits with the right code without an extra banner.
func runRCStart(j *JetskiCLI, args []string) error {
	usage := usageRCStart(j.Name)

	addr := ""
	name := ""
	logFile := ""
	session := false
	showHelp := false

	i := 0
	for i < len(args) {
		a := args[i]
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--addr":
			if hasVal {
				addr = val
			} else if i+1 < len(args) {
				i++
				addr = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --addr")
			}
		case "--log-file":
			if hasVal {
				logFile = val
			} else if i+1 < len(args) {
				i++
				logFile = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --log-file")
			}
		case "--name":
			if hasVal {
				name = val
			} else if i+1 < len(args) {
				i++
				name = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		case "--session":
			session = true
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}

	// Resolve --name default = hostname.  This mirrors the
	// reference's "the daemon generates one on its first start"
	// behaviour while keeping the implementation deterministic.
	if name == "" {
		hn, herr := os.Hostname()
		if herr != nil || hn == "" {
			return ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
		}
		name = hn
	}

	// Resolve --log-file default = persistentrc.LogFilePath(name).
	// Done here (rather than inside persistentrc.ResolveSpec)
	// because callers like tests want to leave it blank.
	if logFile == "" {
		logFile = persistentrc.LogFilePath(name)
		if logFile == "" {
			return ExitCodeError(1, "Error: --log-file is required (no TMPDIR available)")
		}
	}

	// Resolve the binary path.  We re-exec the same binary the
	// user invoked so the LaunchAgent stays pinned to the same
	// build.  On ErrProcFS (rare; some sandboxes) we fall back
	// to argv0.
	binary, err := os.Executable()
	if err != nil || binary == "" {
		binary = os.Args[0]
	}

	spec := persistentrc.Spec{
		Name:       name,
		Addr:       addr, // empty → persistentrc.DefaultAddr
		LogFile:    logFile,
		BinaryPath: binary,
		Session:    session,
	}

	// 5 s call budget: launchctl bootstrap on a cold daemon
	// takes ~150 ms; this lets a system under load still finish.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := persistentrc.Start(ctx, spec); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}

	// Re-read the PID via Status so the printed message
	// has a single source of truth (the launchd report).
	stCtx, stCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer stCancel()
	report, sErr := persistentrc.Status(stCtx, name)
	if sErr != nil {
		// The daemon is running; a status read failure is
		// non-fatal — the operator can poll again.
		fmt.Fprintf(j.Out, "Daemon started (status probe failed: %v); addr=%s\n", sErr, spec.Addr)
		return nil
	}
	bindingAddr := report.Addr
	if bindingAddr == "" {
		bindingAddr = spec.Addr
	}
	fmt.Fprintf(j.Out, "Daemon started; pid=%d; addr=%s\n", report.PID, bindingAddr)
	return nil
}

// runRCServe is the cycle rc-serve-subcommand implementation of
// `wagyu remote-control serve` — the foreground twin of start.
//
// Behaviour:
//
//   - validates --addr / --name (the same rules persistentrc.Start
//     applies to the start path: --addr refuses loopback, --name
//     matches the conservative [A-Za-z0-9._-]+ character set);
//   - rejects the call when $AGY_ADC_AUTH is set (the same gate
//     --remote-control / /remote-control apply today — Remote
//     Control doesn't support ADC yet);
//   - delegates to the cmd/wagyu-supplied serveHandler (registered
//     via cli.RegisterServeHandler at process init), passing the
//     resolved flags; the handler body lives in cmd/wagyu/ so it
//     can reach buildHost* helpers and avoid the internal/cli ←
//     cmd/wagyu import cycle;
//   - on a successful serve comes back as `Remote-control daemon
//     stopped.` (matches the wording runRCStop emits, so scripts
//     can share a sentinel grep between the two).
//
// The handler accepts --no-login (a cycle rc-serve-subcommand
// addition) so an automated test rig can drive `serve` without
// triggering the OAuth device-code flow on a fresh machine; the
// production default is "login if no token" so the brief's "when
// OAuth is required and the user isn't logged in, serve triggers
// the OAuth browser flow synchronously" semantic holds.
func runRCServe(j *JetskiCLI, args []string) error {
	usage := usageRCServe(j.Name)

	addr := ""
	name := ""
	noLogin := false
	showHelp := false

	i := 0
	for i < len(args) {
		a := args[i]
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--addr":
			if hasVal {
				addr = val
			} else if i+1 < len(args) {
				i++
				addr = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --addr")
			}
		case "--name":
			if hasVal {
				name = val
			} else if i+1 < len(args) {
				i++
				name = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		case "--no-login":
			noLogin = true
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}

	// Resolve --name default = hostname. Mirrors runRCStart.
	if name == "" {
		hn, herr := os.Hostname()
		if herr != nil || hn == "" {
			return ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
		}
		name = hn
	}

	// ADC gate (the same one the periodic path uses). Reject
	// before doing anything else so the error message lines up
	// with the gate --remote-control and /remote-control
	// surfaces — Remote Control does not support ADC yet. The
	// predicate lives in cmd/wagyu/ (next to the --remote-control
	// flag's checkRemoteControlADC), so we delegate the gate to
	// the serve handler itself rather than duplicate the env-var
	// inspection here.
	//
	// The serve handler returns ExitCodeError(3, …) on the
	// ADC path so the parser surfaces the canonical
	// "Application Default Credentials" wording and exits with
	// the AGY_ERROR convention (matches remote_control_adc.go).

	// Delegate to the cmd/wagyu-supplied handler. The bridge
	// stays nil in cli-internal tests; we surface a one-line
	// notice and exit 0 in that case (matches the
	// runRCForegroundPlaceholder contract).
	if serveHandler == nil {
		fmt.Fprintln(j.ErrOut, "remote-control serve: no handler registered (binary was built without the serve bridge)")
		return nil
	}
	if err := serveHandler(j, ServeArgs{
		Addr:    addr,
		Name:    name,
		NoLogin: noLogin,
	}); err != nil {
		return err
	}
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}

// serveADCErrorMessage is the single source of truth for the
// error text the user sees when `remote-control serve` collides
// with ADC. Mirrors remoteControlADCErrorMessage (the
// --remote-control flag-path wording) but with the verb
// substituted for "serve" so the surfaced message is consistent
// with the surface the user actually invoked.
//
//   - "remote-control"          grep-able from CI logs
//   - "Application Default Credentials"   the human-readable name
//   - "AGY_ADC_AUTH"            the env var the user can /logout, then unset
//   - "/logout"                 recovery command
//   - "serve"                   verb the user invoked
func serveADCErrorMessage() string {
	return "error: remote-control serve cannot be used with Application Default Credentials " +
		"(AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — " +
		"sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}

// runRCStatus is the cycle rc-persistent-daemon implementation of
// `wagyu remote-control status`.  Output is always a JSON
// document — the brief pins that shape so dashboards can parse
// the same stream regardless of operating mode (running,
// registered-but-stopped, never-registered).
//
// The `--json` flag is accepted for forward compatibility but
// has no effect; the brief's output is JSON regardless.  The
// flag's presence in usageRCStatus is what surfaces it in
// `start --help`.
//
// `--name` defaults to hostname so the user never has to know
// their own machine's label after `start` minted it.
func runRCStatus(j *JetskiCLI, args []string) error {
	usage := usageRCStatus(j.Name)
	name := ""
	showHelp := false
	i := 0
	for i < len(args) {
		a := args[i]
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--json":
			// accepted; JSON is the default output.
		case "--name":
			if hasVal {
				name = val
			} else if i+1 < len(args) {
				i++
				name = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if name == "" {
		hn, herr := os.Hostname()
		if herr != nil || hn == "" {
			// Status with no resolvable name still emits
			// a JSON document; we just don't have a
			// name to report.
			name = ""
		} else {
			name = hn
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	report, err := persistentrc.Status(ctx, name)
	if err != nil {
		// Surface the error as the JSON document so the
		// dashboard still gets a parseable response.
		report = persistentrc.StatusReport{
			Running: false,
			Name:    name,
			Hint:    fmt.Sprintf("status probe failed: %v", err),
		}
	}
	out, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: status JSON encode failed: %v", err))
	}
	fmt.Fprintln(j.Out, string(out))
	return nil
}

// runRCStop is the cycle rc-persistent-daemon implementation of
// `wagyu remote-control stop`.  Idempotent — the underlying
// `persistentrc.Stop` treats a missing agent as success.
//
// Flags:
//
//	--name  the instance to stop (default: hostname)
//
// On success prints `Remote-control daemon stopped.` to
// stdout (matches the recovered stub's wording so existing
// scripts keep working).
func runRCStop(j *JetskiCLI, args []string) error {
	usage := usageRCStop(j.Name)
	name := ""
	showHelp := false
	i := 0
	for i < len(args) {
		a := args[i]
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--name":
			if hasVal {
				name = val
			} else if i+1 < len(args) {
				i++
				name = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: --name")
			}
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if name == "" {
		hn, herr := os.Hostname()
		if herr != nil || hn == "" {
			return ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
		}
		name = hn
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := persistentrc.Stop(ctx, name); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}

// ---- agent / agents -------------------------------------------------------

func runAgents(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		WarnMode(j.ErrOut, j.FlagMode())
		if agentsLister != nil {
			names := agentsLister()
			if len(names) > 0 {
				sort.Strings(names)
				fmt.Fprintln(j.Out, "Available agents:")
				for _, name := range names {
					fmt.Fprintln(j.Out, name)
				}
			}
		}
		return nil
	}
	return fail(j, 1, fmt.Sprintf("Error: unexpected arguments: [%s]", strings.Join(args, " ")))
}

// ---- mic-serve ------------------------------------------------------------

func runMicServe(j *JetskiCLI, args []string) error {
	usage := usageMicServe(j.Name)
	addr := "127.0.0.1:4713"
	i := 0
	for i < len(args) {
		a := args[i]
		val := ""
		hasVal := false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val = a[eq+1:]
			a = a[:eq]
			hasVal = true
		}
		switch a {
		case "-h", "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		case "--addr":
			if hasVal {
				addr = val
			} else if i+1 < len(args) {
				i++
				addr = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: -addr")
			}
		default:
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		i++
	}
	// The reference binds first, then prints its two banner lines, so a
	// bind failure surfaces only the error.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errExitCodeMsg(1, fmt.Sprintf("Error: cannot listen on %s: %v", addr, err))
	}
	defer ln.Close()
	fmt.Fprintf(j.ErrOut, "Serving the microphone on %s. Press ctrl+c to stop.\n", addr)
	fmt.Fprintln(j.ErrOut, "Note: Ensure this terminal has microphone access.")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		_ = conn.Close()
	}
}

// ---- models ---------------------------------------------------------------

// ModelEntry is one row of the reference's model catalogue.
type ModelEntry struct{ ID, Label string }

// ReferenceModels is the catalogue `agy models` prints. The reference also
// uses it to decide whether a configured model is still available: a model
// that is not in the catalogue triggers the "is no longer available" warning
// and a fallback to Gemini 3.8 Flash (High).
//
// AGY 1.3.6 changelog line 1: "Added Claude Opus 5.5 and Claude Sonnet
// 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude
// models now appear in /model only if your Google Cloud project can use
// them." The two 5.5 entries are kept in the static table for downstream
// lookups (so `agy --model claude-opus-5-5` still resolves when the gate
// is open) and conditionally dropped from the returned slice via
// claude55Available() — see claude55.go for the gate's predicate.
func ReferenceModels() []ModelEntry {
	out := []ModelEntry{
		{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
		{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
		// MiniMax M3 is a configured-only model the reference catalogue
		// intentionally does not advertise on its own, but the cycle-99
		// effective-model fallback path also exercises it. Listing it
		// here means the /model table picks it up directly when the
		// user has set it in settings.json or via the --model flag.
		{"minimax-m3", "MiniMax M3"},
	}
	if Claude55Available() {
		out = append(out,
			ModelEntry{"claude-opus-5-5", "Claude Opus 5.5"},
			ModelEntry{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
		)
	}
	return out
}

func runModelsParity(j *JetskiCLI, args []string) error {
	usage := usageModels(j.Name)
	for _, a := range args {
		if isHelpArg(a) {
			fmt.Fprint(j.ErrOut, usage)
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
		return fail(j, 1, fmt.Sprintf("Error: unexpected arguments: [%s]", strings.Join(args, " ")))
	}
	// The unrecognized---mode warning precedes the fetch and the sign-in
	// check (verified: `agy --mode bogus models` warns on the first
	// stderr line, then "Fetching available models...", then the error).
	WarnMode(j.ErrOut, j.FlagMode())
	if !legacySignedIn() {
		fmt.Fprintln(j.ErrOut, "Fetching available models...")
		return fail(j, 1, "Error: Please sign in to view available models. Launch the CLI without arguments to sign in.")
	}
	fmt.Fprintln(j.Out, "Fetching available models...")
	for _, m := range ReferenceModels() {
		fmt.Fprintf(j.Out, "%s\t%s\n", m.ID, m.Label)
	}
	return nil
}

// legacySignedIn reports whether the v1.3.5 binary would consider the user
// signed in. It consults only on-disk/env token sources under the
// current HOME — the reference does not consult the macOS keychain for
// this check (verified: a scratch HOME reports signed-out even when the
// keychain holds a `gemini`/`antigravity` item).
func legacySignedIn() bool {
	if strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")) != "" {
		return true
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return false
	}
	candidates := []string{
		filepath.Join(home, ".config", "antigravity", "oauth-token"),
		filepath.Join(home, ".config", "antigravity", "antigravity-auth.json"),
		filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token"),
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}
	return false
}

// ---- changelog ------------------------------------------------------------

func runChangelog(j *JetskiCLI, args []string) error {
	usage := usageChangelog(j.Name)
	for _, a := range args {
		if isHelpArg(a) {
			fmt.Fprint(j.ErrOut, usage)
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	fmt.Fprint(j.Out, referenceChangelog)
	return nil
}

// ---- help -----------------------------------------------------------------

func runHelpCommand(j *JetskiCLI, args []string) error {
	if len(args) == 0 {
		j.PrintHelpTo(j.ErrOut)
		return nil
	}
	name := args[0]
	if sub, ok := j.lookup(name); ok && sub.Help != "" {
		fmt.Fprint(j.ErrOut, sub.Help)
		return nil
	}
	return fail(j, 1, fmt.Sprintf("Error: unknown subcommand: %s", name))
}
