package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand/v2"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
	"github.com/justwasm/pigasus/mcp"
)

// ---- subcommand_agents.go ----

func usageACP(n string) string {
	return `Usage: ` + n + ` acp [flags]

Start the Agent Client Protocol (ACP) stdio server

Flags:
  --add-dir                         Add a directory to the workspace (repeatable) (default [])
  --agent                           Agent for the ACP session
  --app-data-dir                    Path where application data is stored (default antigravity-cli)
  --app_data_dir                    Path where application data is stored (default antigravity-cli)
  --cli_dir_name                    Subdirectory under app_data_dir for CLI settings and logs
  --cli_log_file                    Override ACP log file path
  --cloud_code_endpoint             CCPA API URL (default https://daily-cloudcode-pa.googleapis.com)
  --dangerously-skip-permissions    Auto-approve all tool permission requests
  --disable-telemetry               If true, disable telemetry
  --disable_telemetry               If true, disable telemetry
  --gemini-dir                      Path where Gemini files are stored (default .gemini)
  --gemini_dir                      Path where Gemini files are stored (default .gemini)
  -h                                Show help
  --help                            Show help
  --ide-mcp-port                    Port for host IDE tools MCP server (requires 'nc' in $PATH) (default 0)
  --ide_mcp_port                    Port for host IDE tools MCP server (requires 'nc' in $PATH) (default 0)
  --include-default-customizations  Include default customizations (default true)
  --is_google3_workspace            Whether running in Google environment
  --is_google_internal              Deprecated: use is_google3_workspace
  --log-file                        Override ACP log file path
  --login                           Log in with Google interactively, then exit (ACP terminal auth)
  --override_user_agent_name        Override user agent name for HTTP requests
  --process-per-session             Serve each session from an isolated worker process
  --sandbox                         Run in a sandbox with terminal restrictions enabled
  --stdio                           Use stdio JSON-RPC transport (default)
  --use_stubby_auth                 Use LOAS auth
  --worker                          Serve a single session for an ACP front process (internal)
`
}

func runACPCommand(j *JetskiCLI, args []string) error {
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Fprint(j.ErrOut, usageACP(j.Name))
			return nil
		}
	}
	if acpHandler != nil {
		acpArgs := ACPArgs{
			In:      os.Stdin,
			Out:     j.Out,
			Err:     j.ErrOut,
			Ctx:     context.Background(),
			RawArgs: args,
		}
		return acpHandler(j, acpArgs)
	}
	fmt.Fprintln(j.ErrOut, "acp: handler not bridged (build with cmd/pigasus to enable)")
	return nil
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

func legacyListAgentsFromMarkdown(pdir string, seen map[string]bool) []string {
	var names []string
	entries, err := os.ReadDir(filepath.Join(pdir, "agents"))
	if err != nil {
		return nil
	}
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
	return names
}

func legacyListAgentsFromJSON(pdir string, seen map[string]bool) ([]string, error) {
	data, err := os.ReadFile(filepath.Join(pdir, "agents.json"))
	if err != nil {
		return nil, nil
	}
	var nested struct {
		Agents []struct {
			Name string `json:"name"`
		} `json:"agents"`
	}
	var names []string
	jerr := json.Unmarshal(data, &nested)
	if jerr == nil && nested.Agents != nil {
		for _, e := range nested.Agents {
			n := strings.TrimSpace(e.Name)
			if n == "" || seen[n] {
				continue
			}
			seen[n] = true
			names = append(names, n)
		}
		return names, nil
	}
	var flat []struct {
		Name string `json:"name"`
	}
	if err2 := json.Unmarshal(data, &flat); err2 != nil {
		if jerr == nil {
			jerr = err2
		}
		return nil, jerr
	}
	for _, e := range flat {
		n := strings.TrimSpace(e.Name)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		names = append(names, n)
	}
	return names, nil
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
	names = append(names, legacyListAgentsFromMarkdown(pdir, seen)...)
	jNames, err := legacyListAgentsFromJSON(pdir, seen)
	if err != nil {
		jsonErr = err
	}
	names = append(names, jNames...)
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

// ---- subcommand_mcp.go ----

// ---- usage blocks ---------------------------------------------------------

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

// ---- mcp ------------------------------------------------------------------

// runMCPParent handles `mcp` with no subcommand: the reference prints the
// parent usage block to stderr and exits 0 (distinct from `plugin`, which
// prints usage + `Error: missing plugin command` and exits 1).
func runMCPParent(j *JetskiCLI, args []string) error {
	fmt.Fprint(j.ErrOut, usageMCP(j.Name))
	return nil
}

// The configuration file's path belongs to the `mcp` package — it is the
// one owner of the path set, so the writer and the runtime that reads the
// file back cannot drift apart (the runtime also reads the reference
// binary's own file and the workspace file; see mcp/configpaths.go).
//
// The file's shape belongs to the `mcp` package too: it owns the
// documented schema and its omit rules (see mcp/documented.go), and it
// owns the file itself (see mcp/document.go). The entries are kept as raw
// maps: the file is the user's own, and the writer must not drop a member
// it does not know (the documented schema carries `cwd`,
// `authProviderType`, `oauth`, `enabledTools`, `disabledTools` and
// `timeoutSeconds` beside the transport members).
//
// The *new* members a writer sets are not raw either: they come from the
// `mcp` package, which owns the documented member names. The map only
// carries what is already in the file and what the package produced.

// loadMCPConfigDoc reads the documented MCP configuration file.
//
// A file that does not exist is the "no servers configured" state, not an
// error. A file that exists but cannot be parsed is an error: the
// reference refuses to work with a file it could not read, and it names
// the file in the message.
func loadMCPConfigDoc(j *JetskiCLI) (*mcp.Document, error) {
	path := mcp.GlobalConfigPath()
	doc, err := mcp.LoadDocument(path)
	if err != nil {
		return nil, fail(j, 1, "Error: failed to parse "+path+": "+err.Error())
	}
	return doc, nil
}

func saveMCPConfigDoc(doc *mcp.Document) error {
	path := mcp.GlobalConfigPath()
	if path == "" {
		return fmt.Errorf("mcp: no config path")
	}
	return doc.Save(path)
}

func runMCPList(j *JetskiCLI, args []string) error {
	for _, a := range args {
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usageMCPSimple(j.Name, "list", "List all configured MCP servers", ""),
				"Error: flags provided but not defined: "+flagName(a))
		}
	}
	doc, err := loadMCPConfigDoc(j)
	if err != nil {
		return err
	}
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
		t := mcpServerType(s)
		if len(t) > maxType {
			maxType = len(t)
		}
		st := mcpServerStatus(s)
		if len(st) > maxStatus {
			maxStatus = len(st)
		}
	}
	sort.Strings(names)
	nw, tw, sw := maxName+2, maxType+2, maxStatus+2
	fmt.Fprintf(j.Out, "%-*s%-*s%-*s%s\n", nw, "NAME", tw, "TYPE", sw, "STATUS", "COMMAND/URL")
	for _, n := range names {
		s := doc.Servers[n]
		fmt.Fprintf(j.Out, "%-*s%-*s%-*s%s\n", nw, n, tw, mcpServerType(s), sw, mcpServerStatus(s), mcpServerTarget(s))
	}
	return nil
}

func mcpServerType(s map[string]any) string {
	if _, ok := s["serverUrl"]; ok {
		return "http"
	}
	return "stdio"
}

func mcpServerStatus(s map[string]any) string {
	if d, _ := s["disabled"].(bool); d {
		return "disabled"
	}
	return "enabled"
}

func mcpServerTarget(s map[string]any) string {
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

type mcpAddOpts struct {
	typ string
	// typSet records that the `--type` flag was given at all: the
	// reference refuses an empty value, so an absent flag and an empty
	// one are not the same thing.
	typSet bool
	// env and headers are the documented key-value maps: the file
	// stores them as JSON objects, not as arrays.
	env        map[string]string
	headers    map[string]string
	positional []string
	showHelp   bool
}

// splitEnvPair splits an `--env KEY=value` argument on its first "=".
//
// A value with no "=" at all is a key with an empty value: the split is
// on the first "=", so there is nothing to split.
func splitEnvPair(arg string) (string, string) {
	key, value, _ := strings.Cut(arg, "=")
	return key, value
}

// splitHeaderPair splits an `--header` argument into its name and its
// value.
//
// The flag's own usage block spells the pair "Key: Value", and the
// reference's own example writes `--header "Authorization: Bearer TOKEN"`,
// so a "=" is tried first (the documented object form) and the colon of
// the usage block's own form is the fallback.
func splitHeaderPair(arg string) (string, string) {
	if key, value, ok := strings.Cut(arg, "="); ok {
		return key, value
	}
	key, value, ok := strings.Cut(arg, ":")
	if !ok {
		return arg, ""
	}
	return strings.TrimSpace(key), strings.TrimSpace(value)
}

func parseMCPAddFlagValue(j *JetskiCLI, flag, val string, hasVal bool, i *int, args []string, usage string) (string, error) {
	if hasVal {
		return val, nil
	}
	if *i+1 < len(args) {
		*i++
		return args[*i], nil
	}
	return "", usageFail(j, usage, "Error: flag needs an argument: "+flag)
}

func parseMCPAddFlags(j *JetskiCLI, args []string, usage string) (mcpAddOpts, error) {
	opts := mcpAddOpts{env: map[string]string{}, headers: map[string]string{}}
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			opts.positional = append(opts.positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			opts.positional = append(opts.positional, args[i:]...)
			break
		}
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		switch a {
		case "-h", "--help":
			opts.showHelp = true
			return opts, nil
		case "-t", "--type":
			v, err := parseMCPAddFlagValue(j, flagName(a), val, hasVal, &i, args, usage)
			if err != nil {
				return opts, err
			}
			opts.typ = v
			opts.typSet = true
		case "-e", "--env":
			v, err := parseMCPAddFlagValue(j, flagName(a), val, hasVal, &i, args, usage)
			if err != nil {
				return opts, err
			}
			key, value := splitEnvPair(v)
			opts.env[key] = value
		case "-H", "--header":
			v, err := parseMCPAddFlagValue(j, flagName(a), val, hasVal, &i, args, usage)
			if err != nil {
				return opts, err
			}
			key, value := splitHeaderPair(v)
			opts.headers[key] = value
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

// buildMCPEntry builds the documented entry for the server a command line
// describes.
//
// The transport is the reference's own: the `--type` flag names it, an
// `http://` or `https://` endpoint is an http server whatever the flag
// says, and the flag that belongs to the other transport is refused.
//
// The entry's shape belongs to the `mcp` package — it owns the
// documented member names — so this function spells none of them.
func buildMCPEntry(opts mcpAddOpts, target string, rest []string) (map[string]any, string, error) {
	kind := mcp.ServerTypeStdio
	if opts.typSet {
		resolved, err := mcp.ParseServerType(opts.typ)
		if err != nil {
			return nil, "", err
		}
		kind = resolved
	}
	if mcp.IsHTTPEndpoint(target) {
		kind = mcp.ServerTypeHTTP
	}
	if err := mcp.ValidateTransportFlags(kind, len(opts.env) > 0, len(opts.headers) > 0); err != nil {
		return nil, "", err
	}
	entry := mcp.ServerFile{}
	if kind == mcp.ServerTypeHTTP {
		entry.ServerURL = target
		if len(opts.headers) > 0 {
			entry.Headers = opts.headers
		}
	} else {
		entry.Command = target
		if len(rest) > 0 {
			entry.Args = rest
		}
		if len(opts.env) > 0 {
			entry.Env = opts.env
		}
	}
	return entry.DocumentedMembers(), kind, nil
}

func runMCPAdd(j *JetskiCLI, args []string) error {
	usage := usageMCPAdd(j.Name)
	opts, err := parseMCPAddFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if len(opts.positional) < 2 {
		return fail(j, 1, "Error: usage: mcp add [flags] <name> <commandOrUrl> [args...]")
	}
	name, target := opts.positional[0], opts.positional[1]
	entry, kind, err := buildMCPEntry(opts, target, opts.positional[2:])
	if err != nil {
		return fail(j, 1, "Error: "+err.Error())
	}
	doc, err := loadMCPConfigDoc(j)
	if err != nil {
		return err
	}
	// The reference *replaces* the entry the command names: a re-add
	// drops the members the `agy mcp add` flags cannot express — `cwd`,
	// `authProviderType`, `oauth`, `enabledTools`, `disabledTools` and
	// `timeoutSeconds` — and any member beside them that the file
	// carried.
	//
	// 【一手】 the reference binary (1.4.12), under a temporary HOME: a
	// file whose `gh` entry carried all six documented members and a
	// member the schema does not list was rewritten by `agy mcp add gh
	// node server.js` with an entry carrying only `args`, `command` and
	// `disabled`. The sibling entries and the unknown root members of
	// the same file survived.
	//
	// 【反编译】 the reference's own `cli/mcp/mcp` package carries a
	// `mergeServerEntry` (out/local-macos/seeds/seeds.txt), but that
	// package serves the reference binary's own `mcp.json` — the root
	// member `servers` — which the `agy mcp add` writer does not touch.
	doc.Servers[name] = entry
	if err := saveMCPConfigDoc(doc); err != nil {
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
	doc, err := loadMCPConfigDoc(j)
	if err != nil {
		return err
	}
	if _, ok := doc.Servers[name]; !ok {
		return fail(j, 1, fmt.Sprintf("Error: MCP server %q not found", name))
	}
	delete(doc.Servers, name)
	if err := saveMCPConfigDoc(doc); err != nil {
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
		doc, err := loadMCPConfigDoc(j)
		if err != nil {
			return err
		}
		// The documented `disabled` flag is the package's member; the
		// writer asks for it rather than spelling it here.
		//
		// The reference does not refuse a server the file does not know:
		// suspending one creates the entry, and resuming one that is not
		// there is a no-op.
		//
		// 【一手】 the reference binary (1.4.12): `agy mcp disable zzz` on
		// a file that did not know `zzz` wrote the entry
		// `{"disabled": true}` and exited 0.
		doc.SetServerDisabled(name, !enabled)
		if err := saveMCPConfigDoc(doc); err != nil {
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

// ---- subcommand_misc.go ----

// ---- mic-serve ------------------------------------------------------------

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

func parseMicServeFlags(j *JetskiCLI, args []string, usage string) (addr string, showHelp bool, err error) {
	addr = "127.0.0.1:4713"
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
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
				return "", false, usageFail(j, usage, "Error: flag needs an argument: -addr")
			}
		default:
			return "", false, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return addr, showHelp, nil
}

func runMicServe(j *JetskiCLI, args []string) error {
	usage := usageMicServe(j.Name)
	addr, showHelp, err := parseMicServeFlags(j, args, usage)
	if err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
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

// ---- changelog ------------------------------------------------------------

func usageChangelog(n string) string {
	return "Usage: " + n + " changelog [flags]\n" +
		"\n" +
		"Show changelog and release notes\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

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

// ---- install --------------------------------------------------------------

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

// ---- subcommand_parity.go ----

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

// ---- models ---------------------------------------------------------------

func usageModels(n string) string {
	return "Usage: " + n + " models [flags]\n" +
		"\n" +
		"List available models\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
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
	// 【一手】 The fetch banner goes to stderr, not stdout: a pty capture and
	// a plain redirect of agy 1.4.5 both put "Fetching available models..."
	// on stderr, and leave stdout carrying nothing but the catalogue rows.
	fmt.Fprintln(j.ErrOut, "Fetching available models...")
	return writeModelTable(j.Out, catalog.ModelCatalogue())
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

// ---- subcommand_server.go ----

// usageServer documents the `pigasus server` subcommand.
func usageServer(prog string) string {
	return "Usage: " + prog + " server [flags]\n" +
		"\n" +
		"Host the agent and serve it over a websocket, so a client can attach to\n" +
		"this machine's workspace, tools and credentials.\n" +
		"\n" +
		"The server always demands a bearer token: it runs shell commands against\n" +
		"its own workspace. A token is minted when --token is not given, and it is\n" +
		"printed when the server starts.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --addr            Bind address for the server (host:port, default 127.0.0.1:7777)\n" +
		"  -p, --port        Port to listen on (convenience override, e.g. 7777, 8080)\n" +
		"  --host            Host interface to bind on (default 127.0.0.1)\n" +
		"  -w, --workspace   Root workspace folder to serve (default: current directory)\n" +
		"  --dir             Alias for --workspace\n" +
		"  --model           Model the served turns run under\n" +
		"  --effort          Reasoning effort the served turns ask for\n" +
		"  --mode            Permission mode: auto, plan or acceptEdits\n" +
		"  --dangerously-skip-permissions  Run the served tools without the permission gate\n" +
		"  --token           Bearer token a client has to present (default: minted)\n" +
		"  --agent           Custom agent the served turns run under\n" +
		"  --backend         Transport the served turns run on (gateway, cloudcode, shim)\n" +
		"  --add-dir         Extra workspace the served tools may act on (repeatable)\n" +
		"  --acp-version     ACP protocol version (\"auto\", \"1\", \"2\", default \"auto\")\n"
}

// ServerSubcommand returns the SubcommandSpec for `pigasus server`.
func ServerSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "server",
		Short:  "Host the agent and serve it over a websocket",
		HelpFn: usageServer,
		RunJ:   runServer,
	}
}

type serverOpts struct {
	addr            string
	port            int
	host            string
	workspace       string
	model           string
	effort          string
	mode            string
	skipPermissions bool
	token           string
	agent           string
	backend         string
	addDirs         []string
	acpVersion      string
	showHelp        bool
}

func parseServerFlags(j *JetskiCLI, args []string, usage string) (serverOpts, error) {
	var opts serverOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		consume := func(target *string, flag string) error {
			if hasVal {
				*target = val
			} else if i+1 < len(args) {
				i++
				*target = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: "+flag)
			}
			return nil
		}
		switch a {
		case "-h", "--help", "-help":
			opts.showHelp = true
		case "-addr", "--addr":
			if err := consume(&opts.addr, a); err != nil {
				return opts, err
			}
		case "-p", "--port", "-port":
			var portStr string
			if err := consume(&portStr, a); err != nil {
				return opts, err
			}
			p, err := strconv.Atoi(strings.TrimSpace(portStr))
			if err != nil || p <= 0 || p > 65535 {
				return opts, usageFail(j, usage, fmt.Sprintf("Error: invalid port: %q", portStr))
			}
			opts.port = p
		case "-host", "--host":
			if err := consume(&opts.host, a); err != nil {
				return opts, err
			}
		case "-w", "--workspace", "-workspace", "--dir", "-dir":
			if err := consume(&opts.workspace, a); err != nil {
				return opts, err
			}
		case "-model", "--model":
			if err := consume(&opts.model, a); err != nil {
				return opts, err
			}
		case "-effort", "--effort":
			if err := consume(&opts.effort, a); err != nil {
				return opts, err
			}
		case "-mode", "--mode":
			if err := consume(&opts.mode, a); err != nil {
				return opts, err
			}
		case "-dangerously-skip-permissions", "--dangerously-skip-permissions":
			opts.skipPermissions = true
		case "-token", "--token":
			if err := consume(&opts.token, a); err != nil {
				return opts, err
			}
		case "-agent", "--agent":
			if err := consume(&opts.agent, a); err != nil {
				return opts, err
			}
		case "-backend", "--backend":
			if err := consume(&opts.backend, a); err != nil {
				return opts, err
			}
		case "-add-dir", "--add-dir":
			var dir string
			if err := consume(&dir, a); err != nil {
				return opts, err
			}
			opts.addDirs = append(opts.addDirs, dir)
		case "-acp-version", "--acp-version", "-acp-protocol-version", "--acp-protocol-version":
			if err := consume(&opts.acpVersion, a); err != nil {
				return opts, err
			}
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

func runServer(j *JetskiCLI, args []string) error {
	usage := usageServer(j.Name)
	opts, err := parseServerFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if serverHandler == nil {
		fmt.Fprintln(j.ErrOut, "server: no handler registered (binary was built without the server bridge)")
		return nil
	}
	return serverHandler(j, ServerArgs{
		Addr:            opts.addr,
		Port:            opts.port,
		Host:            opts.host,
		Workspace:       opts.workspace,
		Model:           opts.model,
		Effort:          opts.effort,
		Mode:            opts.mode,
		SkipPermissions: opts.skipPermissions,
		Token:           opts.token,
		Agent:           opts.agent,
		Backend:         opts.backend,
		AddDirs:         opts.addDirs,
		ACPVersion:      opts.acpVersion,
	})
}

// ---- subcommand_tui.go ----

// usageTUI documents the `pigasus tui` subcommand.
func usageTUI(prog string) string {
	return "Usage: " + prog + " tui [flags]\n" +
		"\n" +
		"Run the interactive session.\n" +
		"\n" +
		"With --server the session attaches to a remote pigasus server: the turn\n" +
		"runs there, on that machine's workspace, tools and credentials, and the\n" +
		"conversation renders here. Without it the session is the ordinary local\n" +
		"one.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --backend         Agent backend to drive (\"local\", \"pigasus\", \"agy\", \"grok\", \"opencode\", \"codex\", \"crush\", \"claude\", \"pigasus-acp\", \"codex-acp\", \"claude-acp\", \"agy-acp\", default \"local\")\n" +
		"  --pigasus         Shortcut for --backend pigasus (Pigasus subprocess via stdio stream-json)\n" +
		"  --agy             Shortcut for --backend agy (Antigravity subprocess via stdio stream-json)\n" +
		"  --grok            Shortcut for --backend grok (Grok CLI via streaming-json NDJSON)\n" +
		"  --opencode        Shortcut for --backend opencode (OpenCode CLI via NDJSON)\n" +
		"  --pi              Shortcut for --backend pi (Pi coding agent CLI via NDJSON)\n" +
		"  --pig             Shortcut for --backend pig (PiG coding agent CLI via NDJSON)\n" +
		"  --codex           Shortcut for --backend codex\n" +
		"  --crush           Shortcut for --backend crush (Crush app-server via stdio JSON-RPC)\n" +
		"  --claude          Shortcut for --backend claude\n" +
		"  --pigasus-acp     Shortcut for --backend pigasus-acp (Pigasus via ACP: pigasus acp)\n" +
		"  --codex-acp       Shortcut for --backend codex-acp (OpenAI Codex via ACP)\n" +
		"  --claude-acp      Shortcut for --backend claude-acp (Claude Agent via ACP)\n" +
		"  --agy-acp         Shortcut for --backend agy-acp (Antigravity via ACP: agy acp)\n" +
		"  --acp-cmd         Command to spawn a custom ACP agent subprocess over stdio\n" +
		"  --acp-manifest    Path to an ACP agent.json manifest file or directory\n" +
		"  --server          Address of the pigasus server to attach to (ws://host:port)\n" +
		"  --token           Bearer token the server demands\n" +
		"  --protocol        Protocol to speak with the server (\"acp\", \"agentlink\", or \"ahp\", default \"acp\")\n" +
		"  --ahp             Shortcut for --protocol ahp\n" +
		"  --session         Remote session URI or ID to attach to (e.g. ahp-session:/<uuid>)\n" +
		"  --acp-version     ACP protocol version (\"auto\", \"1\", \"2\", default \"auto\")\n"
}

// TUISubcommand returns the SubcommandSpec for `pigasus tui`.
func TUISubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "tui",
		Short:  "Run the interactive session, locally or attached to a server",
		HelpFn: usageTUI,
		RunJ:   runTUI,
	}
}

type tuiOpts struct {
	backend     string
	server      string
	token       string
	protocol    string
	session     string
	acpVersion  string
	acpCmd      string
	acpManifest string
	showHelp    bool
	// rest holds the arguments the subcommand does not own. They are
	// the session's own flags, and the session parses them.
	rest []string
}

func parseTUIFlags(j *JetskiCLI, args []string, usage string) (tuiOpts, error) {
	opts := tuiOpts{protocol: "acp", acpVersion: "auto"}
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		consume := func(target *string, flag string) error {
			if hasVal {
				*target = val
			} else if i+1 < len(args) {
				i++
				*target = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: "+flag)
			}
			return nil
		}
		switch a {
		case "-h", "--help", "-help":
			opts.showHelp = true
		case "-backend", "--backend":
			if err := consume(&opts.backend, a); err != nil {
				return opts, err
			}
		case "-pigasus", "--pigasus":
			opts.backend = "pigasus"
		case "-codex", "--codex":
			opts.backend = "codex"
		case "-crush", "--crush":
			opts.backend = "crush"
		case "-claude", "--claude":
			opts.backend = "claude"
		case "-agy", "--agy":
			opts.backend = "agy"
		case "-grok", "--grok":
			opts.backend = "grok"
		case "-opencode", "--opencode":
			opts.backend = "opencode"
		case "-pi", "--pi":
			opts.backend = "pi"
		case "-pig", "--pig":
			opts.backend = "pig"
		case "-pigasus-acp", "--pigasus-acp":
			opts.backend = "pigasus-acp"
		case "-codex-acp", "--codex-acp":
			opts.backend = "codex-acp"
		case "-claude-acp", "--claude-acp":
			opts.backend = "claude-acp"
		case "-agy-acp", "--agy-acp":
			opts.backend = "agy-acp"
		case "-acp-cmd", "--acp-cmd":
			if err := consume(&opts.acpCmd, a); err != nil {
				return opts, err
			}
		case "-acp-manifest", "--acp-manifest":
			if err := consume(&opts.acpManifest, a); err != nil {
				return opts, err
			}
		case "-server", "--server":
			if err := consume(&opts.server, a); err != nil {
				return opts, err
			}
		case "-token", "--token":
			if err := consume(&opts.token, a); err != nil {
				return opts, err
			}
		case "-protocol", "--protocol":
			if err := consume(&opts.protocol, a); err != nil {
				return opts, err
			}
		case "-ahp", "--ahp":
			opts.protocol = "ahp"
		case "-session", "--session":
			if err := consume(&opts.session, a); err != nil {
				return opts, err
			}
		case "-acp-version", "--acp-version", "-acp-protocol-version", "--acp-protocol-version":
			if err := consume(&opts.acpVersion, a); err != nil {
				return opts, err
			}
		default:
			// The session's own flags are the root command's flags, and
			// the session parses them — so they are passed through
			// untouched.
			opts.rest = append(opts.rest, args[i])
		}
	}
	return opts, nil
}

func runTUI(j *JetskiCLI, args []string) error {
	usage := usageTUI(j.Name)
	opts, err := parseTUIFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if tuiHandler == nil {
		fmt.Fprintln(j.ErrOut, "tui: no handler registered (binary was built without the tui bridge)")
		return nil
	}
	return tuiHandler(j, TUIArgs{
		Backend:     opts.backend,
		Server:      opts.server,
		Token:       opts.token,
		Protocol:    opts.protocol,
		Session:     opts.session,
		ACPVersion:  opts.acpVersion,
		ACPCmd:      opts.acpCmd,
		ACPManifest: opts.acpManifest,
		Args:        opts.rest,
	})
}

// ---- subcommand_web.go ----

// usageWeb documents the `pigasus web` subcommand.
func usageWeb(prog string) string {
	return "Usage: " + prog + " web [flags]\n" +
		"\n" +
		"Start the standalone web UI server for local browser and LAN access without\n" +
		"requiring remote-control, cloud tunnels, or Google account login.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --addr            Bind address for the web server (host:port, default 0.0.0.0:8787)\n" +
		"  -p, --port        Port to listen on (convenience override, e.g. 8787, 8080)\n" +
		"  --host            Host interface to bind on (default 0.0.0.0)\n" +
		"  -w, --workspace   Root workspace folder to serve (default: current directory)\n" +
		"  --dir             Alias for --workspace\n" +
		"  --open            Automatically open the web UI in default browser upon startup\n" +
		"  --api-only        Serve only backend Connect-RPC and WebSocket APIs (no web UI assets)\n" +
		"  --ui-only         Serve only frontend static UI assets (no local agent RPC backend)\n" +
		"  --reference-backend  Proxy the RPCs to a reference language_server instead of\n" +
		"                     answering them, and serve the reference's own frontend\n" +
		"                     assets: an endpoint (host:port) of a running reference, or\n" +
		"                     the path of a reference binary to start\n" +
		"  --desktop         Serve the desktop UI: the voice input, the jetbox\n" +
		"                    terminal, the redesigned aux pane. The page and the\n" +
		"                    bundle are the same either way — the switch is the\n" +
		"                    identity the page's Mendel read carries\n"
}

// WebSubcommand returns the SubcommandSpec for `pigasus web`.
func WebSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "web",
		Short:  "Start the standalone web UI server for local and LAN access",
		HelpFn: usageWeb,
		RunJ:   runWeb,
	}
}

type webOpts struct {
	addr             string
	port             int
	host             string
	workspace        string
	open             bool
	referenceBackend string
	desktop          bool
	apiOnly          bool
	uiOnly           bool
	showHelp         bool
}

func parseWebFlags(j *JetskiCLI, args []string, usage string) (webOpts, error) {
	var opts webOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		consume := func(target *string, flag string) error {
			if hasVal {
				*target = val
			} else if i+1 < len(args) {
				i++
				*target = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: "+flag)
			}
			return nil
		}
		switch a {
		case "-h", "--help":
			opts.showHelp = true
		case "--addr":
			if err := consume(&opts.addr, "--addr"); err != nil {
				return opts, err
			}
		case "-p", "--port":
			var portStr string
			if err := consume(&portStr, a); err != nil {
				return opts, err
			}
			p, err := strconv.Atoi(strings.TrimSpace(portStr))
			if err != nil || p <= 0 || p > 65535 {
				return opts, usageFail(j, usage, fmt.Sprintf("Error: invalid port: %q", portStr))
			}
			opts.port = p
		case "--host":
			if err := consume(&opts.host, "--host"); err != nil {
				return opts, err
			}
		case "-w", "--workspace", "--dir":
			if err := consume(&opts.workspace, a); err != nil {
				return opts, err
			}
		case "--open":
			opts.open = true
		case "--api-only":
			opts.apiOnly = true
		case "--ui-only":
			opts.uiOnly = true
		case "--reference-backend":
			if err := consume(&opts.referenceBackend, a); err != nil {
				return opts, err
			}
		case "--desktop":
			opts.desktop = true
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	if opts.apiOnly && opts.uiOnly {
		return opts, usageFail(j, usage, "Error: --api-only and --ui-only cannot be used together")
	}
	return opts, nil
}

func runWeb(j *JetskiCLI, args []string) error {
	usage := usageWeb(j.Name)
	opts, err := parseWebFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if webHandler == nil {
		fmt.Fprintln(j.ErrOut, "web: no handler registered (binary was built without the web bridge)")
		return nil
	}
	return webHandler(j, WebArgs{
		Addr:             opts.addr,
		Port:             opts.port,
		Host:             opts.host,
		Workspace:        opts.workspace,
		Open:             opts.open,
		ReferenceBackend: opts.referenceBackend,
		Desktop:          opts.desktop,
		ApiOnly:          opts.apiOnly,
		UiOnly:           opts.uiOnly,
	})
}

// ---- halloween_cmd.go ----

// usageHalloween returns the usage block for the halloween subcommand.
//
// The command is the clone's own — the reference has no such
// subcommand — but the art it prints is the reference's own, decoded
// from its truecolor captures (out/changelog-147/ref/seasonal-scenes/).
func usageHalloween(n string) string {
	return "Usage: " + n + " halloween [scene...]\n" +
		"\n" +
		"Print the seasonal banner art\n" +
		"\n" +
		"The reference's Halloween banner is not one picture: it draws one of\n" +
		"five scenes per launch — a pumpkin, a moon, a ghost, a witch's hat and\n" +
		"a bat — each with its own colours. Without an argument every scene is\n" +
		"printed; with one, that scene is printed.\n" +
		"\n" +
		"Scenes:\n" +
		"  all       print every scene\n" +
		"  random    print one scene, the way the reference picks it at startup\n" +
		"  pumpkin   the pumpkin scene\n" +
		"  moon      the moon scene\n" +
		"  ghost     the ghost scene\n" +
		"  witch-hat the witch's hat scene\n" +
		"  bat       the bat scene\n" +
		"\n" +
		"Flags:\n" +
		"  -h       Show help\n" +
		"  --help   Show help\n"
}

// runHalloween prints the seasonal banner art.
//
// 【一手】 agy 1.4.7 draws one of five scenes at startup, each with its
// own colour set; the rasters are the reference's own truecolor output.
func runHalloween(j *JetskiCLI, args []string) error {
	usage := usageHalloween(j.Name)
	var names []string
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		default:
			names = append(names, a)
		}
	}

	ev := seasonal.Halloween(time.Now().Year())
	if len(names) == 1 && strings.EqualFold(names[0], "random") {
		names = []string{ev.ArtNames()[rand.IntN(len(ev.Arts))]}
	}
	if err := ev.PrintScenes(j.Out, names); err != nil {
		return errExitCodeMsg(1, "Error: "+err.Error())
	}
	return nil
}
