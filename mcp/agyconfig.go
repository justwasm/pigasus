// Package mcp owns the clone's protocol-level surface for the Model
// Context Protocol.
//
// The package has three parts:
//
//   - agyconfig.go — the MCP configuration files, parsed once for the
//     whole tree: the server entries, the per-server alwaysLoad hint and
//     the disableAllMCP switch (this file).
//   - documented.go — the documented `mcpServers` entry: the shape a
//     writer emits, with the documented omit rules.
//   - configpaths.go — the path set the clone reads (the documented
//     global file, the reference binary's own file, the workspace file)
//     and the merge of their servers.
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
// agyconfig.go — the MCP configuration files, in the shape the clone
// reads them.
//
// Two file shapes exist, and both are read:
//
// The reference binary's own file, `~/.gemini/antigravity-cli/mcp.json`:
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
// The documented file, `~/.gemini/config/mcp_config.json` and
// `<workspace>/.agents/mcp_config.json` (official-docs/mcp.md,
// "MCP configuration structure"):
//
//	{"mcpServers": {"<name>": {"command": "…", "args": ["…"],
//	                           "env": {"K": "V"},
//	                           "cwd": "/work",
//	                           "serverUrl": "https://…",
//	                           "headers": {"K": "V"},
//	                           "authProviderType": "google_credentials",
//	                           "oauth": {"clientId": "…"},
//	                           "disabled": false,
//	                           "enabledTools": ["…"],
//	                           "disabledTools": ["…"],
//	                           "timeoutSeconds": 180}}}
//
// configpaths.go owns *where* those files are; this file owns what is in
// them.
//
// Provenance
// ----------
// The reference's own file shape is 【一手】 (the compatibility layer's
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
// The documented shape is the official documentation's own (【一手】,
// official-docs/mcp.md): the root member `mcpServers`, the transport
// members `command` / `serverUrl`, the object-shaped `env` and
// `headers`, and the six members `cwd`, `authProviderType`, `oauth`,
// `enabledTools`, `disabledTools` and `timeoutSeconds`.
//
// The six members' spelling and their types are the reference binary's
// own protocol buffer descriptor (【一手】,
// out/local-macos/protos/cortex_1.4.6.txt, the message `McpServerSpec`):
// `cwd` (TYPE_STRING), `auth_provider_type` (TYPE_ENUM, json_name
// "authProviderType"), `oauth` (TYPE_MESSAGE, json_name "oauth"),
// `enabled_tools` (LABEL_REPEATED, json_name "enabledTools"),
// `disabled_tools` (LABEL_REPEATED, json_name "disabledTools") and
// `timeout_seconds` (TYPE_INT32, json_name "timeoutSeconds"). `env` and
// `headers` are map entries there — objects on the wire, never arrays.
// The same names are in the reference's own recovered function names
// (【反编译】, out/local-macos/seeds/seeds.txt:
// cortex_go_proto.(*McpServerSpec).GetCwd, .GetAuthProviderType,
// .GetOauth, .GetEnabledTools, .GetDisabledTools and .GetTimeoutSeconds).
//
// The write shape — the members a writer emits, and the omit rules — is
// documented.go's.
//
// 1.4.7 note (【strings】): the reference's own message "never loaded:
// mcp.json is read only for Agent Plugins plugins (plugin.json \"$schema\":
// %q). Move the servers to %s" says a workspace-level `mcp.json` is *not*
// read in 1.4.7 — servers come from Agent Plugins. The clone keeps reading
// the global `~/.gemini/antigravity-cli/mcp.json`, which the reference's
// `agy mcp add` still writes; the plugin path is not implemented yet.
//
// This file is the single parser for those files inside the clone: the
// PiG-shaped translation in internal/pigrt/mcpconfig.go is built on these
// types (they are aliases, so its API is unchanged) and the client in
// client.go reads the same bytes.
package mcp

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

// ConfigFile is the on-disk shape of an MCP configuration file.
//
// Two root members are read, because two file shapes exist in the wild:
//
//	servers      the reference binary's own `mcp.json`
//	mcpServers   the documented `mcp_config.json`
//
// A file may carry either one, or both. AllServers returns the union.
type ConfigFile struct {
	// Servers maps a server name to its entry, in the reference
	// binary's own shape (the root member `servers`). The reference
	// writes an empty map by default, so an empty file is the "no
	// servers configured" steady state, not an error.
	Servers map[string]ServerFile `json:"servers,omitempty"`
	// MCPServers maps a server name to its entry, in the documented
	// shape (the root member `mcpServers`). Its entries carry the
	// documented member names — `serverUrl`, `disabled` — which
	// AllServers normalises onto the reference's own entry shape.
	MCPServers map[string]ServerFile `json:"mcpServers,omitempty"`
	// DisableAllMCP gates every server at once, independent of each
	// server's own `enabled` flag. The reference's env override
	// (`AGY_CLI_DISABLE_MCP`) wins over it — see
	// internal/auth.MCPsBlocked.
	DisableAllMCP bool `json:"disableAllMCP,omitempty"`
}

// ServerFile is one server entry of an MCP configuration file.
//
// Type is the reference's own hint ("http" or "stdio"). The reference
// resolves by *presence* rather than by the hint — a URL means HTTP, a
// command means stdio — so NormaliseType does the same and an entry
// without a type still works.
type ServerFile struct {
	Type string `json:"type"`
	// URL is the reference's own name of the remote endpoint.
	URL string `json:"url,omitempty"`
	// ServerURL is the documented name of the remote endpoint: the
	// documentation says "Set the server's endpoint in `serverUrl`
	// (`url` also works)", and the `agy mcp add` writer writes
	// `serverUrl`. Both are read; Endpoint prefers `url`.
	ServerURL string            `json:"serverUrl,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	// CWD is the documented working directory of a local stdio server
	// process. An empty value means the clone's own working directory.
	CWD string `json:"cwd,omitempty"`
	// Headers is the AGY 1.4.0 per-server HTTP header map (http only).
	// The values support shell-style variable expansion; the keys never
	// do.
	Headers map[string]string `json:"headers,omitempty"`
	// AuthProviderType names the built-in authentication provider. The
	// documentation names one value, "google_credentials", which
	// attaches Google Application Default Credentials.
	//
	// The entry records it and the client carries it, but the clone
	// runs no authentication flow: the SDK's transport has an
	// `OAuthHandler` slot, and filling it needs a token source that
	// neither this value nor the `oauth` member below describes (the
	// documentation names no scope and no token endpoint). See
	// ServerSpec.AuthProviderType.
	AuthProviderType string `json:"authProviderType,omitempty"`
	// OAuth carries the documented manual OAuth 2.0 client credentials
	// (`clientId` and an optional `clientSecret`) for a remote server
	// that does not support Dynamic Client Registration.
	//
	// The entry records it and the client carries it, but the clone
	// runs no OAuth flow — see AuthProviderType above.
	OAuth map[string]any `json:"oauth,omitempty"`
	// Enabled is a plain bool. The reference always writes it, so an
	// absent field decodes as false — a server is enabled only when the
	// key is present and true.
	Enabled bool `json:"enabled"`
	// Disabled is the documented suspension flag: "When true, suspends
	// the server connection without removing its configuration". It is
	// the inverse of Enabled, and it is what the documented
	// `mcpServers` entries carry.
	//
	// The flag is written unconditionally — the reference always writes
	// its own `enabled` flag, and the documented `disabled` flag is its
	// inverse. The write shape is documented.go's DocumentedMembers.
	Disabled bool `json:"disabled"`
	// AlwaysLoad is the AGY 1.3.3 per-server static always-loaded-tool
	// hint: the named tools are advertised to the model directly instead
	// of being reachable only through `call_mcp_tool`.
	AlwaysLoad []string `json:"alwaysLoad,omitempty"`
	// EnabledTools is the documented allowlist: "Allowlist of tool
	// names exposed to the agent from this server. When set, all other
	// tools on the server are hidden."
	EnabledTools []string `json:"enabledTools,omitempty"`
	// DisabledTools is the documented denylist: "Denylist of tool
	// names withheld from the agent."
	DisabledTools []string `json:"disabledTools,omitempty"`
	// TimeoutSeconds is the documented per-server timeout: "Timeout, in
	// seconds, for connecting to the server and listing its tools, and
	// for each tool call. A negative value, such as -1, turns both off."
	// Zero — or an absent member — keeps the defaults.
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

// Endpoint returns the remote endpoint of the entry: the reference's own
// `url` when it is there, the documented `serverUrl` otherwise.
func (s ServerFile) Endpoint() string {
	if s.URL != "" {
		return s.URL
	}
	return s.ServerURL
}

// UnmarshalJSON decodes one server entry.
//
// The `env` and `headers` members are decoded leniently: the documented
// shape is a key-value object, but an older build of the clone wrote an
// array of "KEY=value" strings, and a configuration file that cannot be
// decoded is dropped whole — so the array form is read as the object it
// stands for.
func (s *ServerFile) UnmarshalJSON(raw []byte) error {
	type plain ServerFile
	doc := struct {
		*plain
		Env     json.RawMessage `json:"env,omitempty"`
		Headers json.RawMessage `json:"headers,omitempty"`
	}{plain: (*plain)(s)}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return err
	}
	env, err := decodeKeyValues(doc.Env)
	if err != nil {
		return err
	}
	s.Env = env
	headers, err := decodeKeyValues(doc.Headers)
	if err != nil {
		return err
	}
	s.Headers = headers
	return nil
}

// decodeKeyValues decodes a key-value member of a server entry.
//
// The documented shape is a JSON object. The array of "KEY=value" strings
// an older build of the clone wrote is read as the object it stands for.
func decodeKeyValues(raw json.RawMessage) (map[string]string, error) {
	body := strings.TrimSpace(string(raw))
	if body == "" || body == "null" {
		return nil, nil
	}
	if strings.HasPrefix(body, "[") {
		var list []string
		if err := json.Unmarshal(raw, &list); err != nil {
			return nil, err
		}
		out := make(map[string]string, len(list))
		for _, item := range list {
			key, value, _ := strings.Cut(item, "=")
			out[key] = value
		}
		return out, nil
	}
	var out map[string]string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	if s.Endpoint() != "" {
		return "http"
	}
	return "stdio"
}

// Spec converts the entry into the connection spec the client pool uses.
func (s ServerFile) Spec(name string) ServerSpec {
	return ServerSpec{
		Name:             name,
		Type:             s.NormaliseType(),
		Command:          s.Command,
		Args:             append([]string(nil), s.Args...),
		Env:              s.Env,
		CWD:              s.CWD,
		URL:              s.Endpoint(),
		Headers:          s.Headers,
		Enabled:          s.Enabled,
		AuthProviderType: s.AuthProviderType,
		OAuth:            s.OAuth,
		EnabledTools:     append([]string(nil), s.EnabledTools...),
		DisabledTools:    append([]string(nil), s.DisabledTools...),
		TimeoutSeconds:   s.TimeoutSeconds,
	}
}

// AllServers returns the file's servers from both root members, merged by
// name.
//
// The documented root member (`mcpServers`) wins over the reference
// binary's own (`servers`) for the same name: the documented file is the
// one the `agy mcp add` writer maintains.
//
// An entry under the documented root is normalised onto the reference's
// own entry shape: `serverUrl` becomes the endpoint, and the documented
// `disabled` flag (default false) becomes the reference's `enabled` flag.
// An entry under the reference's own root keeps its own `enabled` flag —
// the reference always writes it, and an absent flag means "disabled"
// there.
func (c ConfigFile) AllServers() map[string]ServerFile {
	out := make(map[string]ServerFile, len(c.Servers)+len(c.MCPServers))
	for name, entry := range c.Servers {
		out[name] = entry
	}
	for name, entry := range c.MCPServers {
		out[name] = normaliseDocumentedEntry(entry)
	}
	return out
}

// normaliseDocumentedEntry maps a documented `mcpServers` entry onto the
// reference's own entry shape.
func normaliseDocumentedEntry(s ServerFile) ServerFile {
	if s.URL == "" {
		s.URL = s.ServerURL
	}
	s.Enabled = !s.Disabled
	return s
}

// LoadConfigFile reads an MCP configuration file at path.
//
// Both file shapes are read: the reference's own `mcp.json` (the root
// member `servers`) and the documented `mcp_config.json` (the root member
// `mcpServers`). See AllServers.
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
// Both root members are read (see AllServers), so a file written in the
// documented shape contributes its servers too.
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
	servers := c.AllServers()
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	specs := make([]ServerSpec, 0, len(names))
	for _, name := range names {
		entry := servers[name]
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
	servers := c.AllServers()
	names := make([]string, 0, len(servers))
	for name := range servers {
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
