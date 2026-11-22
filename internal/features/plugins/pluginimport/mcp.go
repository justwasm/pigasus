// mcp.go — the Claude Code MCP server conversion.
//
// 【一手】 agy 1.4.11: the importer reads the plugin's `.mcp.json`, converts
// the servers and writes them as the CLI's own `mcp_config.json`. The
// reference's conversion rules, as measured:
//
//	{"type":"http","url":…,"headers":{…}} → {"serverUrl":…,"headers":{…}}
//	{"type":"http"}                        → skipped: an http server needs a url
//	{"type":"sse","url":…}                 → skipped: the legacy SSE transport
//	                                        isn't supported
//	{"type":"stdio","command":…}           → {"command":…,"args":[…],"env":{…}}
//	{"type":"stdio"}                       → skipped: a stdio server needs a
//	                                        command
//	{"type":"other"}                       → skipped: unsupported type "other"
//	{"url":…}                              → {"serverUrl":…}
//	{"command":…}                          → {"command":…}
//	{}                                     → skipped: it has neither a command
//	                                        nor a url
//	"not an object"                        → skipped: it isn't a JSON object
//
// `${CLAUDE_PLUGIN_ROOT}` expands to the *staged* plugin directory, not to
// the source directory.
package pluginimport

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// claudePluginRoot is the placeholder Claude Code's plugin files use for the
// plugin's own directory.
const claudePluginRoot = "${CLAUDE_PLUGIN_ROOT}"

// mcpConfigFile is the name the CLI reads MCP servers from.
const mcpConfigFile = "mcp_config.json"

// claudeMCPFile is Claude Code's own file name.
const claudeMCPFile = ".mcp.json"

// mcpServer is one converted server.
type mcpServer struct {
	ServerURL string            `json:"serverUrl,omitempty"`
	Headers   map[string]string `json:"headers,omitempty"`
	Command   string            `json:"command,omitempty"`
	Args      []string          `json:"args,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
}

// mcpSpec is one server entry as Claude Code writes it.
type mcpSpec struct {
	Type    string            `json:"type"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Command string            `json:"command"`
	Args    []string          `json:"args"`
	Env     map[string]string `json:"env"`
}

// convertMCPServers converts the source plugin's .mcp.json into the staged
// plugin's mcp_config.json.
//
// It returns the converted server names and the warnings the reference
// prints for the servers it skipped. A malformed .mcp.json is an error —
// the reference reports it as "[error] <plugin>: staging MCP servers: …"
// and leaves the copied .mcp.json in place.
func convertMCPServers(srcDir, dest string) ([]string, []string, error) {
	path := filepath.Join(srcDir, claudeMCPFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, nil
		}
		return nil, nil, fmt.Errorf("staging MCP servers: reading %s: %w", path, err)
	}
	servers, warns, err := parseMCPServers(data, path, dest)
	if err != nil {
		return nil, warns, err
	}
	// The reference removes Claude Code's file name once the servers are
	// staged; the CLI only reads mcp_config.json.
	if err := os.Remove(filepath.Join(dest, claudeMCPFile)); err != nil && !os.IsNotExist(err) {
		return nil, warns, err
	}
	if len(servers) == 0 {
		return nil, warns, nil
	}
	out := struct {
		MCPServers map[string]mcpServer `json:"mcpServers"`
	}{MCPServers: servers}
	blob, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return nil, warns, err
	}
	if err := os.WriteFile(filepath.Join(dest, mcpConfigFile), blob, 0600); err != nil {
		return nil, warns, err
	}
	names := make([]string, 0, len(servers))
	for name := range servers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, warns, nil
}

// parseMCPServers parses the file's servers. The file's top-level object
// either is the server map or wraps it in an "mcpServers" member.
func parseMCPServers(data []byte, path, dest string) (map[string]mcpServer, []string, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, nil, fmt.Errorf("staging MCP servers: parsing %s: %s", path, unmarshalErr(jsonKindOf(data), "Go value of type map[string]jsontext.Value"))
	}
	raw := json.RawMessage(data)
	if wrapper, ok := top["mcpServers"]; ok {
		raw = wrapper
		if kind := jsonKind(wrapper); kind != "object" {
			return nil, nil, fmt.Errorf("staging MCP servers: parsing %s: mcpServers: %s", path, unmarshalErr(kind, "Go value of type map[string]jsontext.Value"))
		}
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, nil, fmt.Errorf("staging MCP servers: parsing %s: %s", path, unmarshalErr(jsonKind(raw), "Go value of type map[string]jsontext.Value"))
	}
	out := map[string]mcpServer{}
	var warns []string
	for _, name := range sortedRawKeys(entries) {
		entry := entries[name]
		if kind := jsonKind(entry); kind != "object" {
			warns = append(warns, fmt.Sprintf("MCP server %q skipped: it isn't a JSON object", name))
			continue
		}
		var spec mcpSpec
		if err := json.Unmarshal(entry, &spec); err != nil {
			warns = append(warns, fmt.Sprintf("MCP server %q skipped: it isn't a JSON object", name))
			continue
		}
		server, ok := convertServer(spec, dest)
		if !ok {
			warns = append(warns, fmt.Sprintf("MCP server %q skipped: %s", name, serverSkipReason(spec)))
			continue
		}
		out[name] = server
	}
	return out, warns, nil
}

// convertServer turns one Claude server into the CLI's own shape. The
// second result is false when the server has to be skipped.
func convertServer(spec mcpSpec, dest string) (mcpServer, bool) {
	switch spec.Type {
	case "http":
		if spec.URL == "" {
			return mcpServer{}, false
		}
		return mcpServer{ServerURL: spec.URL, Headers: spec.Headers}, true
	case "sse":
		return mcpServer{}, false
	case "stdio":
		if spec.Command == "" {
			return mcpServer{}, false
		}
		return mcpServer{
			Command: expandPluginRoot(spec.Command, dest),
			Args:    expandArgs(spec.Args, dest),
			Env:     expandEnv(spec.Env, dest),
		}, true
	case "":
		switch {
		case spec.URL != "":
			return mcpServer{ServerURL: spec.URL, Headers: spec.Headers}, true
		case spec.Command != "":
			return mcpServer{
				Command: expandPluginRoot(spec.Command, dest),
				Args:    expandArgs(spec.Args, dest),
				Env:     expandEnv(spec.Env, dest),
			}, true
		default:
			return mcpServer{}, false
		}
	default:
		return mcpServer{}, false
	}
}

// serverSkipReason explains why a server was skipped.
func serverSkipReason(spec mcpSpec) string {
	switch spec.Type {
	case "http":
		return "an http server needs a url"
	case "sse":
		return "the legacy SSE transport isn't supported"
	case "stdio":
		return "a stdio server needs a command"
	case "":
		return "it has neither a command nor a url"
	default:
		return fmt.Sprintf("unsupported type %q", spec.Type)
	}
}

// expandPluginRoot replaces ${CLAUDE_PLUGIN_ROOT} with the staged plugin
// directory.
func expandPluginRoot(s, dest string) string {
	return strings.ReplaceAll(s, claudePluginRoot, dest)
}

// expandArgs expands the placeholder in every argument.
func expandArgs(args []string, dest string) []string {
	if len(args) == 0 {
		return nil
	}
	out := make([]string, 0, len(args))
	for _, a := range args {
		out = append(out, expandPluginRoot(a, dest))
	}
	return out
}

// expandEnv expands the placeholder in every environment value.
func expandEnv(env map[string]string, dest string) map[string]string {
	if len(env) == 0 {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		out[k] = expandPluginRoot(v, dest)
	}
	return out
}

// jsonKindOf names the JSON kind of a raw document.
func jsonKindOf(data []byte) string {
	return jsonKind(json.RawMessage(data))
}
