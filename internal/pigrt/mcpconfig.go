// mcpconfig.go — the agy → PiG MCP configuration bridge.
//
// PiG's built-in `mcp` extension reads `mcp.json` from its own agent
// directory, in its own schema:
//
//	{"mcpServers": {"<name>": {"type": "stdio"|"http", "command": …, "args": […],
//	                           "env": {…}, "url": …, "headers": {…},
//	                           "enabled": true, "exposure": "direct"}}}
//
// The clone reads the reference's file instead — `~/.gemini/antigravity-cli/
// mcp.json`, written by `agy mcp add` and by the `/mcp` panel:
//
//	{"servers": {"<name>": {"type": "stdio"|"http", "command": …, "args": […],
//	                        "env": {…}, "url": …, "headers": {…},
//	                        "enabled": bool}}}
//
// (auth.MCPConfig, internal/auth/compat.go.) The `headers` field on
// HTTP servers was added in 1.4.0 (changelog line 21); its values
// support `${VAR}` / `$VAR` / `${VAR:-default}` / `$$` substitution
// against the merged server-env ∪ host-env, applied here in
// agyToPiGServer.
//
// Rather than fork the file, the runtime points PiG's extension at a
// `LoadConfig` that reads the agy file and returns PiG's in-memory shape. That
// keeps a single source of truth on disk — the same file `wagyu mcp list` and
// the `/mcp` panel already render — while letting PiG own the transport,
// OAuth, tool listing and tool calls.
//
// The two schemas differ in three places:
//
//  1. the top-level key (`servers` vs `mcpServers`);
//  2. `env`/`headers`: agy stores a plain object, PiG an ordered map, so the
//     keys are sorted to give a stable order (Go map iteration is randomised,
//     and PiG renders the map back to the server, where order is cosmetic but
//     determinism keeps captures/diffs readable);
//  3. exposure: agy has no notion of it, and its `call_mcp_tool` surface treats
//     every configured server's tools as eagerly declared, so we map to
//     "direct" rather than PiG's default "codemode" (which would hide the
//     tools behind the codemode sandbox and change what the model sees).
package pigrt

import (
	"encoding/json"
	"os"
	"sort"
	"strings"

	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/mcpext"

	"github.com/justwasm/pigasus/mcp"
)

// stripUTF8BOM drops a leading UTF-8 byte order mark (EF BB BF) if present.
// AGY 1.2.16 changelog line 13 + line 376 (cycle config-bom-strip):
// mcp.json saved on Windows via Notepad / PowerShell `Set-Content` carries
// a BOM and makes json.Unmarshal reject the file with
// "invalid character 'ï' looking for beginning of value". The reference
// strips before parsing; we mirror that locally here so this package does
// not have to import internal/auth (it is meant to be usable in isolation,
// see the file header).
func stripUTF8BOM(b []byte) []byte {
	return mcp.StripBOM(b)
}

// AgyMCPFile mirrors auth.MCPConfig's on-disk JSON. It is an alias of the
// clone's single parser (mcp.ConfigFile) so the reference's file
// has exactly one reader in the tree: the PiG-shaped translation below and
// the client in mcp/client.go see the same bytes.
type AgyMCPFile = mcp.ConfigFile

// AgyMCPServer mirrors auth.MCPServer: "http" uses URL, "stdio" uses
// Command/Args/Env. Enabled is a plain bool (the reference always writes it),
// so an absent field decodes as false — matching `auth.LoadMCPConfig`, and
// matching the reference's own behaviour of treating a server as enabled only
// when the key is present and true.
type AgyMCPServer = mcp.ServerFile

// LoadAgyMCP translates an agy `mcp.json` at path into PiG's loaded-config
// shape. A missing or unreadable file yields an empty config (no servers), the
// same "no servers configured" state the reference starts in. Parse errors are
// surfaced in LoadedMcpConfig.Errors so PiG logs them instead of dropping
// every other server.
func LoadAgyMCP(path string) mcpext.LoadedMcpConfig {
	loaded := mcpext.LoadedMcpConfig{}
	if path == "" {
		return loaded
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			loaded.Errors = append(loaded.Errors, err.Error())
		}
		return loaded
	}
	raw = stripUTF8BOM(raw)
	var doc AgyMCPFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		loaded.Errors = append(loaded.Errors, err.Error())
		return loaded
	}
	names := make([]string, 0, len(doc.Servers))
	for name := range doc.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		loaded.Servers = append(loaded.Servers, mcpext.McpServerEntry{
			Name:   name,
			Config: agyToPiGServer(doc.Servers[name]),
			Source: path,
			Scope:  "global",
		})
	}
	return loaded
}

// LoadAgyMCPFile reads an agy `mcp.json` at path into the typed
// AgyMCPFile view used by always-load hint extraction. A missing file
// is the empty (no servers) state — the same steady state the
// reference starts in — and yields a nil error. A present-but-bad
// file returns the parse error verbatim, so callers can surface it
// in their own diagnostic (the runtime does, see Options.MCPConfigPath
// during New()).
//
// This is a sibling of LoadAgyMCP rather than a refactor of it: the
// existing PiG-shaped view continues to flow through LoadAgyMCP so
// callers using just the PiG side (tests, the CLI bridge) are not
// forced to change. New code that needs the typed view (the
// always-load reader, any future schema-level mutation) should call
// LoadAgyMCPFile.
func LoadAgyMCPFile(path string) (AgyMCPFile, error) {
	if path == "" {
		return AgyMCPFile{}, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return AgyMCPFile{}, nil
		}
		return AgyMCPFile{}, err
	}
	raw = stripUTF8BOM(raw)
	var doc AgyMCPFile
	if err := json.Unmarshal(raw, &doc); err != nil {
		return AgyMCPFile{}, err
	}
	return doc, nil
}

// agyToPiGServer converts one server entry. Type is normalised: the reference
// writes "http" for both the legacy SSE and the streamable-HTTP transports
// (the distinction lives in the URL), and PiG accepts "http" and
// "streamable-http" alike (mcpext validates a URL is present).
func agyToPiGServer(s AgyMCPServer) extension.McpServerConfig {
	enabled := s.Enabled
	cfg := extension.McpServerConfig{
		Type:     normaliseMCPType(s.Type, s.URL),
		Exposure: extension.McpExposureDirect,
		Enabled:  &enabled,
	}
	// AGY 1.4.0 (changelog line 21): header values support
	// `${VAR}` / `$VAR` / `${VAR:-default}` / `$$` against the
	// merged server-env ∪ host-env (server-env wins). Compute
	// the merged env once and reuse it for env, so a header can
	// reference the same vars a sibling env entry exposes.
	mergedEnv := mergeServerEnv(s.Env)
	if cfg.Type == "http" {
		cfg.URL = s.URL
		if len(s.Headers) > 0 {
			substituted := mcp.SubstituteHeaderVars(s.Headers, mergedEnv)
			keys := make([]string, 0, len(substituted))
			for k := range substituted {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			pairs := make([]string, 0, len(keys)*2)
			for _, k := range keys {
				pairs = append(pairs, k, substituted[k])
			}
			cfg.Headers = extension.NewOrderedStrings(pairs...)
		}
	} else {
		cfg.Command = s.Command
		cfg.Args = append([]string(nil), s.Args...)
		if len(s.Env) > 0 {
			keys := make([]string, 0, len(s.Env))
			for k := range s.Env {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			pairs := make([]string, 0, len(keys)*2)
			for _, k := range keys {
				pairs = append(pairs, k, s.Env[k])
			}
			cfg.Env = extension.NewOrderedStrings(pairs...)
		}
	}
	return cfg
}

// mergeServerEnv returns serverEnv with host-env (os.Environ) as the
// base; entries in serverEnv override the host's. The result is the
// substitution namespace for `${VAR}` references in HTTP header
// values. nil serverEnv falls through to the host env unchanged.
func mergeServerEnv(serverEnv map[string]string) map[string]string {
	merged := make(map[string]string, len(serverEnv)+16)
	for _, kv := range os.Environ() {
		if eq := strings.IndexByte(kv, '='); eq >= 0 {
			merged[kv[:eq]] = kv[eq+1:]
		}
	}
	for k, v := range serverEnv {
		merged[k] = v
	}
	return merged
}

// normaliseMCPType picks "http" or "stdio". A URL always means HTTP (the
// reference uses `type` as a hint but resolves by presence of url/command);
// anything else defaults to stdio, which is what an entry with a command is.
func normaliseMCPType(typ, url string) string {
	switch typ {
	case "http", "sse", "streamable-http":
		return "http"
	case "stdio":
		return "stdio"
	}
	if url != "" {
		return "http"
	}
	return "stdio"
}
