// Package cli — dispatch_mcp.go: MCP server management and listing handlers.
package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
)

// ---- 5b. runMcpList ----

// runMcpList is the `mcp list` implementation. Cycle 49 B
// rewrote it to byte-level 1:1 match the v1.3.5 binary: the
// output goes through legacyRenderMCPList / legacyRenderMCPListMissing
// (declared below) which render the 4-col table with NAME/TYPE/
// STATUS/COMMAND-URL columns. v1.3.5 binary path is
// $ANTIGRAVITY_HOME/mcp.json (or ~/.config/antigravity/mcp.json
// as a fallback) — see legacyMCPListConfigPath below.
func runMcpList(d *dispatchContext) error {
	cfg, path := legacyLoadMCPConfig()
	fmt.Fprint(d.Out, legacyRenderMCPList(cfg, path))
	return nil
}

// legacyMCPListConfigPath returns the v1.3.5 binary's MCP config path
// (cycle 48 legacy compat schema). $ANTIGRAVITY_HOME/mcp.json,
// $XDG_CONFIG_HOME/antigravity/mcp.json, or
// $HOME/.config/antigravity/mcp.json.
func legacyMCPListConfigPath() string {
	if h := strings.TrimSpace(os.Getenv("ANTIGRAVITY_HOME")); h != "" {
		return filepath.Join(h, "mcp.json")
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "antigravity", "mcp.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "antigravity", "mcp.json")
	}
	return ""
}

// legacyLoadMCPConfig reads the v1.3.5 binary's MCP config from the
// canonical path. Returns the config + path. The file may be
// missing — the caller can use the path to render the
// "no MCP servers configured (missing X/mcp.json)" line.
// Uses auth.MCPConfig (cycle 48 legacy compat schema).
func legacyLoadMCPConfig() (*auth.MCPConfig, string) {
	path := legacyMCPListConfigPath()
	cfg := &auth.MCPConfig{}
	if path == "" {
		return cfg, ""
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return cfg, path
	}
	_ = json.Unmarshal(raw, cfg)
	return cfg, path
}

// legacyRenderMCPList renders the v1.3.5 binary's mcp list output.
// Format (cycle 49 B sub-agent byte-level 1:1):
//
//	NAME   TYPE   STATUS   COMMAND/URL
//	dummy  http   enabled  http://localhost:8080
//	foo    stdio  enabled  bar
//
// Column widths are computed from the max field length across
// all servers (NAME, TYPE, STATUS), then padded with 2 spaces
// between fields. The COMMAND/URL column is the URL when type
// is "http", else the command (+ args joined with spaces).
func legacyRenderMCPList(cfg *auth.MCPConfig, path string) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		if path != "" {
			return fmt.Sprintf("no MCP servers configured (missing %s)\n", path)
		}
		return "no MCP servers configured\n"
	}
	// Fixed column widths matching the v1.3.5 binary:
	// NAME=7, TYPE=7, STATUS=9 (left-aligned, no literal space
	// between fields — the %-7s padding itself produces the
	// 2/3/2-space gap, and "enabled" (7) pads to 9 with 2
	// trailing spaces, while "disabled" (8) pads to 9 with 1).
	names := make([]string, 0, len(cfg.Servers))
	for n := range cfg.Servers {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", "NAME", "TYPE", "STATUS", "COMMAND/URL")
	for _, name := range names {
		s := cfg.Servers[name]
		enabled := "enabled"
		if !s.Enabled {
			enabled = "disabled"
		}
		switch s.Type {
		case "http":
			fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", name, s.Type, enabled, s.URL)
		default:
			cmdline := s.Command
			if len(s.Args) > 0 {
				cmdline += " " + strings.Join(s.Args, " ")
			}
			fmt.Fprintf(&b, "%-7s%-7s%-9s%s\n", name, s.Type, enabled, cmdline)
		}
	}
	return b.String()
}

// legacyRenderMCPListMissing is the v1.3.5 binary's output when the
// mcp.json file does not exist. Returns a "no MCP servers
// configured (missing PATH/mcp.json)" line.
func legacyRenderMCPListMissing(path string) string {
	return fmt.Sprintf("no MCP servers configured (missing %s)\n", path)
}

// ---- 6. runMcp ------------------------------------------------------------

// runMcp is the "MCP server status" entry point. The v4 binary's
// `mcp` subcommand has 5 children (add, remove, list, enable,
// disable); runMcp without a subcommand is the implicit "list"
// (alias for `mcp list`).
//
// For cycle 41 the "real" surface is mcp.ListConfigured which
// reads ~/.config/antigravity/mcp.json (the v4 binary's MCP
// config). We don't have a top-level mcp package yet, so the
// implementation is in this file: it does a JSON-parse of the
// config file and prints each server.
//
// When the user passes a sub-subcommand (e.g. `mcp add`, `mcp
// list`), the parser's dispatch loop in jetski.go routes through
// the sub-sub closures in parser_specs.go, which prepend the
// sub-name to args and call dispatchRunXxx("mcp", ...). So this
// function sees d.Args[0] as the sub-name when called from a
// sub-sub.
func runMcp(d *dispatchContext) error {
	// `mcp` with no subcommand: implicit list (matches v4).
	if len(d.Args) == 0 {
		return runMcpList(d)
	}
	sub := d.Args[0]
	switch sub {
	case "list":
		return runMcpList(d)
	case "add":
		return runMcpAdd(d, d.Args[1:])
	case "remove":
		return runMcpRemove(d, d.Args[1:])
	case "enable":
		return runMcpSetDisabled(d, d.Args[1:], false)
	case "disable":
		return runMcpSetDisabled(d, d.Args[1:], true)
	}
	fmt.Fprintf(d.Out, "mcp: unknown subcommand %q (expected: add | remove | list | enable | disable)\n", sub)
	return nil
}

// runMcpAdd records a new MCP server entry in the on-disk
// config. The shape mirrors the v4 binary's `mcp add` (we just
// append to the servers array and rewrite the file).
func runMcpAdd(d *dispatchContext, args []string) error {
	if len(args) < 2 {
		fmt.Fprintln(d.Out, "usage: mcp add <name> <command> [args...]")
		return nil
	}
	name := args[0]
	command := args[1]
	extra := args[2:]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	// Reject duplicate names so we don't silently drop the
	// previous entry.
	for _, s := range doc.Servers {
		if s.Name == name {
			fmt.Fprintf(d.Out, "mcp: server %q already exists (use `mcp remove` first)\n", name)
			return nil
		}
	}
	doc.Servers = append(doc.Servers, mcpServer{Name: name, Command: command, Args: extra})
	sort.Slice(doc.Servers, func(i, j int) bool { return doc.Servers[i].Name < doc.Servers[j].Name })
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "mcp: added %q -> %s %s\n", name, command, strings.Join(extra, " "))
	return nil
}

// runMcpRemove deletes an MCP server entry by name.
func runMcpRemove(d *dispatchContext, args []string) error {
	if len(args) < 1 {
		fmt.Fprintln(d.Out, "usage: mcp remove <name>")
		return nil
	}
	name := args[0]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	before := len(doc.Servers)
	out := doc.Servers[:0]
	for _, s := range doc.Servers {
		if s.Name != name {
			out = append(out, s)
		}
	}
	doc.Servers = out
	if len(doc.Servers) == before {
		fmt.Fprintf(d.Out, "mcp: no server named %q\n", name)
		return nil
	}
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	fmt.Fprintf(d.Out, "mcp: removed %q\n", name)
	return nil
}

// runMcpSetDisabled toggles the disabled flag for a server.
func runMcpSetDisabled(d *dispatchContext, args []string, disabled bool) error {
	if len(args) < 1 {
		verb := "enable"
		if disabled {
			verb = "disable"
		}
		fmt.Fprintf(d.Out, "usage: mcp %s <name>\n", verb)
		return nil
	}
	name := args[0]
	cfgPath := defaultMCPConfigPath()
	doc, err := loadOrInitMCPDoc(cfgPath)
	if err != nil {
		return err
	}
	found := false
	for i := range doc.Servers {
		if doc.Servers[i].Name == name {
			doc.Servers[i].Disabled = disabled
			found = true
			break
		}
	}
	if !found {
		fmt.Fprintf(d.Out, "mcp: no server named %q\n", name)
		return nil
	}
	if err := saveMCPDoc(cfgPath, doc); err != nil {
		return err
	}
	verb := "enabled"
	if disabled {
		verb = "disabled"
	}
	fmt.Fprintf(d.Out, "mcp: %q %s\n", name, verb)
	return nil
}

// mcpServer is the JSON shape used by ~/.config/antigravity/mcp.json.
// Kept unexported; the public surface is runMcp*.
type mcpServer struct {
	Name     string   `json:"name"`
	Command  string   `json:"command"`
	Args     []string `json:"args,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// mcpDoc is the on-disk document; the servers array is what
// mcp add/remove/list/enable/disable all mutate.
type mcpDoc struct {
	Servers []mcpServer `json:"servers"`
}

// loadOrInitMCPDoc reads (or creates) the MCP config file. A
// missing file is treated as an empty document.
func loadOrInitMCPDoc(path string) (*mcpDoc, error) {
	doc := &mcpDoc{}
	if path == "" {
		return doc, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, fmt.Errorf("mcp: read %s: %w", path, err)
	}
	if len(raw) == 0 {
		return doc, nil
	}
	if err := json.Unmarshal(raw, doc); err != nil {
		return nil, fmt.Errorf("mcp: parse %s: %w", path, err)
	}
	return doc, nil
}

// saveMCPDoc writes the doc back atomically (temp + rename).
func saveMCPDoc(path string, doc *mcpDoc) error {
	if path == "" {
		return fmt.Errorf("mcp: no config path (set $ANTIGRAVITY_MCP_CONFIG or $XDG_CONFIG_HOME)")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// defaultMCPConfigPath returns the fallback MCP config path when none is configured.
func defaultMCPConfigPath() string {
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "antigravity", "mcp.json")
	}
	if home := strings.TrimSpace(os.Getenv("HOME")); home != "" {
		return filepath.Join(home, ".config", "antigravity", "mcp.json")
	}
	return ""
}
