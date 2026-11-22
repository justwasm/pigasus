// configpaths.go — where the clone reads the MCP configuration from, and
// how the files are merged.
//
// The official documentation names two configuration files for the CLI
// (official-docs/mcp.md, "Global and workspace server configs";
// official-docs/settings.md, "MCP servers"):
//
//	global     ~/.gemini/config/mcp_config.json        every workspace
//	workspace  <workspace>/.agents/mcp_config.json      one workspace
//
// Both carry the documented root member `mcpServers`.
//
// The reference binary keeps a third file of its own,
// `~/.gemini/antigravity-cli/mcp.json`, whose root member is `servers`
// — the shape the older `agy mcp add` writer and the `/mcp` panel use.
// The clone reads it too, so an installation that already has one keeps
// working.
//
// The writer and the reader used to disagree: `agy mcp add` wrote
// `~/.gemini/config/mcp_config.json` under the root member `mcpServers`,
// while the runtime read `<agent dir>/mcp.json` under the root member
// `servers`. Every server added through `agy mcp add` therefore never
// connected. This file is the one owner of the path set: the writer and
// the runtime both ask it, so the two cannot drift apart again.
//
// Precedence
// ----------
//
// The files are merged by server name, in this order:
//
//  1. the documented global file (`~/.gemini/config/mcp_config.json`),
//  2. the reference binary's own file (`<agent dir>/mcp.json`),
//  3. the workspace file (`<workspace>/.agents/mcp_config.json`).
//
// A later file overrides an earlier one for the same server name, so the
// workspace file wins over both global files and the reference file wins
// over the documented global file. The two global files are the same
// scope, and the reference's own file is the one the `/mcp` panel
// maintains, so it is the later of the two.
//
// Every file is read with both root members accepted: a file may carry
// `mcpServers`, `servers`, or both. The reader does not guess which
// shape a file "is" — the two genuinely exist in the wild.
package mcp

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ConfigScope names the scope one configuration file applies to.
type ConfigScope string

const (
	// ScopeGlobal is the file that applies to every workspace.
	ScopeGlobal ConfigScope = "global"
	// ScopeWorkspace is the file that applies to one workspace only.
	ScopeWorkspace ConfigScope = "workspace"
	// ScopeReference is the reference binary's own file. It applies to
	// every workspace, like the global one, and is kept so an existing
	// installation keeps working.
	ScopeReference ConfigScope = "reference"
)

// The two root members the clone reads. A file may carry either one, or
// both.
const (
	// RootMCPServers is the documented root member.
	RootMCPServers = "mcpServers"
	// RootServers is the reference binary's own root member.
	RootServers = "servers"
)

// ConfigSource is one MCP configuration file the clone reads.
type ConfigSource struct {
	// Path is the file's path. An empty path means the location does
	// not apply (no workspace, or no agent directory).
	Path string
	// Scope is what the file applies to.
	Scope ConfigScope
	// RootKey is the root member the file is expected to carry. Both
	// root members are read from every file regardless.
	RootKey string
}

// GlobalConfigPath returns the global MCP configuration file: the
// documented `~/.gemini/config/mcp_config.json`.
//
// $GEMINI_CONFIG_HOME replaces the `~/.gemini/config` directory. That is
// the clone's own convention for that variable, and it is the one the
// `agy mcp add` writer uses — so the writer and this reader always
// agree on where the file is.
//
// An empty string means the home directory is unknown and the file does
// not apply.
func GlobalConfigPath() string {
	if h := strings.TrimSpace(os.Getenv("GEMINI_CONFIG_HOME")); h != "" {
		return filepath.Join(h, "mcp_config.json")
	}
	home := strings.TrimSpace(os.Getenv("HOME"))
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".gemini", "config", "mcp_config.json")
}

// WorkspaceConfigPath returns the workspace MCP configuration file:
// `<workspace>/.agents/mcp_config.json`.
//
// An empty workspace yields an empty path: the caller has no workspace,
// so the workspace file does not apply.
func WorkspaceConfigPath(workspace string) string {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return ""
	}
	return filepath.Join(workspace, ".agents", "mcp_config.json")
}

// ReferenceConfigPath returns the reference binary's own MCP file:
// `<agentDir>/mcp.json`.
//
// An empty agent directory yields an empty path.
func ReferenceConfigPath(agentDir string) string {
	agentDir = strings.TrimSpace(agentDir)
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, "mcp.json")
}

// ConfigPaths returns every MCP configuration file the clone reads, in
// merge order: the documented global file, the reference binary's own
// file, then the workspace file. A later file overrides an earlier one
// for the same server name.
//
// A location that does not apply — no agent directory, no workspace — is
// left out of the list rather than returned with an empty path.
//
// The package header documents the precedence in full.
func ConfigPaths(agentDir, workspace string) []ConfigSource {
	candidates := []ConfigSource{
		{Path: GlobalConfigPath(), Scope: ScopeGlobal, RootKey: RootMCPServers},
		{Path: ReferenceConfigPath(agentDir), Scope: ScopeReference, RootKey: RootServers},
		{Path: WorkspaceConfigPath(workspace), Scope: ScopeWorkspace, RootKey: RootMCPServers},
	}
	out := make([]ConfigSource, 0, len(candidates))
	for _, src := range candidates {
		if src.Path == "" {
			continue
		}
		out = append(out, src)
	}
	return out
}

// MergedConfig is the union of the servers of every configuration file
// the clone reads.
type MergedConfig struct {
	// Servers maps a server name to its entry. A later file's entry
	// replaces an earlier file's entry for the same name.
	Servers map[string]ServerFile
	// DisableAllMCP is true when one of the files gates every server.
	DisableAllMCP bool
}

// LoadMergedConfig reads every source in order and merges the servers by
// name.
//
// A file that does not exist is not an error: it is the "no servers
// configured" state. A file that exists but cannot be read or parsed is
// dropped — the reference drops the whole file rather than half-loading
// it — and the first error is returned next to the servers of the files
// that did load, so the caller can surface it without losing the rest.
func LoadMergedConfig(sources []ConfigSource) (MergedConfig, error) {
	merged := MergedConfig{Servers: map[string]ServerFile{}}
	var firstErr error
	for _, src := range sources {
		if src.Path == "" {
			continue
		}
		cfg, err := LoadConfigFile(src.Path)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		if cfg.DisableAllMCP {
			merged.DisableAllMCP = true
		}
		for name, entry := range cfg.AllServers() {
			merged.Servers[name] = entry
		}
	}
	return merged, firstErr
}

// Specs returns the merged servers as connection specs, sorted by name.
//
// The same rules as ConfigFile.Specs apply: the `disableAllMCP` switch
// gates every server, and a disabled server is skipped.
func (m MergedConfig) Specs() []ServerSpec {
	if m.DisableAllMCP {
		return nil
	}
	names := make([]string, 0, len(m.Servers))
	for name := range m.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]ServerSpec, 0, len(names))
	for _, name := range names {
		entry := m.Servers[name]
		if !entry.Enabled {
			continue
		}
		specs = append(specs, entry.Spec(name))
	}
	return specs
}

// ServerNames returns the names of the merged servers, sorted, regardless
// of whether they are enabled.
func (m MergedConfig) ServerNames() []string {
	names := make([]string, 0, len(m.Servers))
	for name := range m.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
