package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/mcp"
)

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
