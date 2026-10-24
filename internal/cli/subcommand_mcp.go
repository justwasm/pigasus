package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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

// legacyMCPConfigPath returns the v1.3.5 binary's MCP config path.
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

type mcpAddOpts struct {
	typ        string
	env        []string
	headers    []string
	positional []string
	showHelp   bool
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
	var opts mcpAddOpts
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
		case "-e", "--env":
			v, err := parseMCPAddFlagValue(j, flagName(a), val, hasVal, &i, args, usage)
			if err != nil {
				return opts, err
			}
			opts.env = append(opts.env, v)
		case "-H", "--header":
			v, err := parseMCPAddFlagValue(j, flagName(a), val, hasVal, &i, args, usage)
			if err != nil {
				return opts, err
			}
			opts.headers = append(opts.headers, v)
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

func buildMCPEntry(opts mcpAddOpts, target string, rest []string) (map[string]any, string) {
	isHTTP := opts.typ == "http" || opts.typ == "http+sse" || strings.HasPrefix(target, "http://") || strings.HasPrefix(target, "https://")
	entry := map[string]any{"disabled": false}
	if len(opts.env) > 0 {
		entry["env"] = opts.env
	}
	if len(opts.headers) > 0 {
		entry["headers"] = opts.headers
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
	return entry, kind
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
	entry, kind := buildMCPEntry(opts, target, opts.positional[2:])
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
