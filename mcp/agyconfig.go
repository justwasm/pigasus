// Package mcp owns the clone's protocol-level surface for the Model
// Context Protocol.
//
// The package has three parts:
//
//   - agyconfig.go — the reference's `mcp.json`, parsed once for the whole
//     tree: the server entries, the per-server alwaysLoad hint and the
//     disableAllMCP switch (this file).
//   - client.go — the pool the clone's three MCP tools call through
//     (call_mcp_tool, list_resources, read_resource).
//   - header_substitute.go — the `${VAR}` / `$VAR` / `${VAR:-default}` /
//     `$$` expansion the AGY 1.4.0 per-server HTTP headers carry.
//
// The always-load feature
// -----------------------
// AGY 1.3.3 changelog line 42:
//
//	Added support for MCP servers to mark a tool as always loaded by
//	setting `_meta: {"antigravity/alwaysLoad": true}` on it in their
//	`tools/list` response, so the agent gets that tool directly instead
//	of discovering it on demand.
//
// The reference binary before 1.3.3 treated every tool an MCP server
// advertised through `tools/list` as discoverable — reachable only through
// the meta-tool `call_mcp_tool` aggregator. 1.3.3 lets a server flag a
// subset of its tools as core (always-loaded), so they appear directly in
// the model's tool list. The hint rides on the MCP-spec `_meta` field;
// only the AGY-specific key `antigravity/alwaysLoad` is meaningful, and
// only a JSON-true value opts the tool in. Anything else (false, "true"
// string, the number 1, null, missing key, malformed `_meta`) defaults
// to "not always-loaded".
//
// The decoder for that flag and the catalog it feeds live in
// internal/pigrt/alwaysload.go — the runtime reads the hints there, and
// this package used to carry a second, unreachable copy of the same
// decoder. The static half of the feature (the per-server `alwaysLoad`
// list in `mcp.json`) is agyconfig.go's, below.
//
// What this package does NOT own
// -------------------------------
//
//   - Dispatch. The reference's "always load" semantics in 1.3.3 is
//     "advertise that tool to the model as its own entry". The actual
//     dispatch path (mcp__<server>__<tool> → tools/call) is PiG's MCP
//     extension's responsibility in the print path.
//   - The PiG-shaped view of the configuration. internal/pigrt/mcpconfig.go
//     translates the same file into PiG's own shape; its types are aliases
//     of agyconfig.go's, so the two read the same bytes.
//
// agyconfig.go — the reference's MCP configuration file, in the shape the
// clone reads it.
//
// The reference keeps its MCP servers in
// `~/.gemini/antigravity-cli/mcp.json`:
//
//	{"servers": {"<name>": {"type": "stdio"|"http",
//	                        "command": "…", "args": ["…"],
//	                        "env": {"K": "V"},
//	                        "url": "https://…",
//	                        "headers": {"K": "V"},
//	                        "enabled": true,
//	                        "alwaysLoad": ["tool_name"]}},
//	 "disableAllMCP": false}
//
// Provenance
// ----------
// The file shape is the reference's own (【一手】, the compatibility layer's
// `auth.MCPConfig` was captured from the 1.3.5 binary and the two later
// fields were added by the reference's own changelog):
//
//   - `headers` — AGY 1.4.0 changelog line 21: "Added support for custom
//     HTTP headers when adding MCP servers." The values support `${VAR}` /
//     `$VAR` / `${VAR:-default}` / `$$` expansion (see
//     header_substitute.go).
//   - `alwaysLoad` — AGY 1.3.3 changelog line 42: a server may name the
//     tools it wants pinned (advertised directly to the model) rather than
//     left behind the `call_mcp_tool` aggregator.
//   - `disableAllMCP` — the top-level switch the reference honours
//     (`internal/auth/compat_mcp.go`, MCPsBlocked).
//
// 1.4.7 note (【strings】): the reference's own message "never loaded:
// mcp.json is read only for Agent Plugins plugins (plugin.json \"$schema\":
// %q). Move the servers to %s" says a workspace-level `mcp.json` is *not*
// read in 1.4.7 — servers come from Agent Plugins. The clone keeps reading
// the global `~/.gemini/antigravity-cli/mcp.json`, which the reference's
// `agy mcp add` still writes; the plugin path is not implemented yet.
//
// This file is the single parser for that file inside the clone: the
// PiG-shaped translation in internal/pigrt/mcpconfig.go is built on these
// types (they are aliases, so its API is unchanged) and the client in
// client.go reads the same file.
package mcp

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// ConfigFile is the on-disk shape of the reference's `mcp.json`.
type ConfigFile struct {
	// Servers maps a server name to its entry. The reference writes an
	// empty map by default, so an empty file is the "no servers
	// configured" steady state, not an error.
	Servers map[string]ServerFile `json:"servers"`
	// DisableAllMCP gates every server at once, independent of each
	// server's own `enabled` flag. The reference's env override
	// (`AGY_CLI_DISABLE_MCP`) wins over it — see
	// internal/auth.MCPsBlocked.
	DisableAllMCP bool `json:"disableAllMCP,omitempty"`
}

// ServerFile is one server entry of `mcp.json`.
//
// Type is the reference's own hint ("http" or "stdio"). The reference
// resolves by *presence* rather than by the hint — a URL means HTTP, a
// command means stdio — so NormaliseType does the same and an entry
// without a type still works.
type ServerFile struct {
	Type    string            `json:"type"`
	URL     string            `json:"url,omitempty"`
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	// Headers is the AGY 1.4.0 per-server HTTP header map (http only).
	// The values support shell-style variable expansion; the keys never
	// do.
	Headers map[string]string `json:"headers,omitempty"`
	// Enabled is a plain bool. The reference always writes it, so an
	// absent field decodes as false — a server is enabled only when the
	// key is present and true.
	Enabled bool `json:"enabled"`
	// AlwaysLoad is the AGY 1.3.3 per-server static always-loaded-tool
	// hint: the named tools are advertised to the model directly instead
	// of being reachable only through `call_mcp_tool`.
	AlwaysLoad []string `json:"alwaysLoad,omitempty"`
}

// NormaliseType resolves the transport of a server entry: a URL means HTTP,
// a command means stdio, and the `type` hint breaks the tie when both are
// absent. The reference writes "http" for both the legacy SSE and the
// streamable-HTTP transports (the distinction lives in the URL).
func (s ServerFile) NormaliseType() string {
	switch s.Type {
	case "http", "sse", "streamable-http":
		return "http"
	case "stdio":
		return "stdio"
	}
	if s.URL != "" {
		return "http"
	}
	return "stdio"
}

// Spec converts the entry into the connection spec the client pool uses.
func (s ServerFile) Spec(name string) ServerSpec {
	return ServerSpec{
		Name:    name,
		Type:    s.NormaliseType(),
		Command: s.Command,
		Args:    append([]string(nil), s.Args...),
		Env:     s.Env,
		URL:     s.URL,
		Headers: s.Headers,
		Enabled: s.Enabled,
	}
}

// LoadConfigFile reads an agy `mcp.json` at path.
//
// A missing file is not an error: it is the reference's "no servers
// configured" state, and the caller gets the zero ConfigFile with a nil
// error. A file that exists but cannot be read, or whose JSON is malformed,
// returns the error verbatim so the caller can surface it — the reference
// drops the whole file in that case rather than half-loading it.
//
// A UTF-8 byte order mark is stripped first: a `mcp.json` saved on Windows
// through Notepad or PowerShell's `Set-Content` carries one, and
// encoding/json rejects it ("invalid character 'ï' looking for beginning
// of value"). The reference strips it too.
func LoadConfigFile(path string) (ConfigFile, error) {
	if path == "" {
		return ConfigFile{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ConfigFile{}, nil
		}
		return ConfigFile{}, err
	}
	raw = StripBOM(raw)
	var doc ConfigFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return ConfigFile{}, err
	}
	return doc, nil
}

// StripBOM drops a leading UTF-8 byte order mark (EF BB BF) if present.
func StripBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// Specs returns the file's servers as connection specs, sorted by name.
//
// A file with `disableAllMCP` set yields no specs at all: the reference's
// switch gates every server, so the pool has nothing to connect to.
//
// Disabled servers are skipped too — the reference only connects a server
// whose `enabled` flag is present and true.
func (c ConfigFile) Specs() []ServerSpec {
	if c.DisableAllMCP {
		return nil
	}
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]ServerSpec, 0, len(names))
	for _, name := range names {
		entry := c.Servers[name]
		if !entry.Enabled {
			continue
		}
		specs = append(specs, entry.Spec(name))
	}
	return specs
}

// ServerNames returns the names of the file's servers, sorted, regardless of
// whether they are enabled. It is the list the `/mcp` panel renders.
func (c ConfigFile) ServerNames() []string {
	names := make([]string, 0, len(c.Servers))
	for name := range c.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// MergedEnv returns the server's own env layered over the host env: the
// host's variables are the base, the server's own entries win. It is the
// namespace an HTTP header value's `${VAR}` reference resolves against.
func (s ServerFile) MergedEnv() map[string]string {
	merged := make(map[string]string, len(s.Env)+16)
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			merged[kv[:eq]] = kv[eq+1:]
		}
	}
	for k, v := range s.Env {
		merged[k] = v
	}
	return merged
}
