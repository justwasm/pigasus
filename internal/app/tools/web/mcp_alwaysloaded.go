// mcp_alwaysloaded.go — the tools an MCP server marked as always loaded.
//
// AGY 1.3.3 changelog line 42: "Added support for MCP servers to mark a
// tool as always loaded by setting `_meta: {"antigravity/alwaysLoad": true}`
// on it in their `tools/list` response, so the agent gets that tool
// directly instead of discovering it on demand."
//
// The behaviour this file implements
// ----------------------------------
// Without the hint, every MCP tool is *lazy*: the model learns it exists
// through `call_mcp_tool`, whose schema carries no per-tool declaration, and
// calls it by naming the server and the tool. With the hint, the tool is
// advertised to the model as its own entry in the tool list — it appears in
// the request's `tools` array under its own name and schema, so the model
// sees it alongside `run_command` and the rest instead of having to discover
// it.
//
// The name the reference gives such a tool
// ----------------------------------------
// `mcp_<server>_<tool>` — a single underscore on each side, not the
// double-underscore spelling older notes in this tree claimed. Recovered
// from the 1.5.18 binary, which carries both the format string
//
//	mcp_%s_%s
//
// and real instances of it from its bundled chrome-devtools server
// (`mcp_chrome_devtools_list_pages`, `mcp_chrome_devtools_take_snapshot`,
// …). The reference can suppress the prefix per server
// (`McpServerSpec.skip_tool_name_prefix`), in which case the tool is
// advertised under its bare name; this file does the same.
//
// What is NOT implemented here
// ----------------------------
//   - `forceAllToolsEager` / `McpServerConfig.force_all_tools_eager`: the
//     later "eager" protocol (changelog: "Opt out of lazy tool-search
//     loading to force all tools from an MCP server into the model's eager
//     tool list"). It advertises *every* tool of a server rather than the
//     flagged subset. The binary carries the field but the config file
//     shape this package reads does not, so wiring it would mean inventing
//     a config key.
//   - The lazy half: `tools/list` is read only for the hint. A tool that is
//     not flagged stays reachable through `call_mcp_tool` exactly as
//     before.
package web

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// AlwaysLoadedTool is one tool a server flagged as always-loaded.
//
// It mirrors mcp.AlwaysLoadedTool; the tool layer keeps its own copy so
// internal/app/tools does not import the MCP client package (the same
// reason MCPProvider exists), and the caller — setup, which imports both —
// converts between them.
type AlwaysLoadedTool struct {
	// Server is the configured server name.
	Server string
	// Name is the tool's own name, as the server advertises it.
	Name string
	// Description is the server's description, if it sent one.
	Description string
	// InputSchema is the server's JSON Schema for the tool's arguments.
	InputSchema json.RawMessage
}

// AlwaysLoadedToolName returns the name the reference advertises a
// flagged tool under: `mcp_<server>_<tool>`.
//
// A server may suppress the prefix (the reference's
// `skip_tool_name_prefix`); the caller decides that, so this is the
// prefixed spelling only.
func AlwaysLoadedToolName(server, tool string) string {
	return "mcp_" + server + "_" + tool
}

// AlwaysLoadedTool is one flagged MCP tool advertised as its own entry.
//
// Running it forwards to the same provider `call_mcp_tool` uses, with the
// server and tool already bound — the model passes only the tool's own
// arguments, because the tool's declaration is the server's schema, not
// `call_mcp_tool`'s.
type AlwaysLoadedToolExecutor struct {
	// Server is the server the tool lives on.
	Server string
	// Tool is the tool's name on that server.
	Tool string
	// Advertised is the name the model sees (see AlwaysLoadedToolName).
	Advertised string
	// Desc is the server's description (Description() is the interface
	// method, so the field cannot carry that name).
	Desc string
	// SchemaJSON is the server's input schema.
	SchemaJSON json.RawMessage
	// Provider is the client the tool calls through. Nil uses the
	// process-wide provider.
	Provider MCPProvider
}

func (t *AlwaysLoadedToolExecutor) Name() string { return t.Advertised }

func (t *AlwaysLoadedToolExecutor) Description() string { return t.Desc }

func (t *AlwaysLoadedToolExecutor) Schema() json.RawMessage {
	if len(t.SchemaJSON) == 0 {
		// A server that sent no schema still gets an object-typed
		// declaration: the model must be able to pass an empty object.
		return json.RawMessage(`{"type":"object","properties":{}}`)
	}
	return t.SchemaJSON
}

func (t *AlwaysLoadedToolExecutor) Run(ctx context.Context, args json.RawMessage) (any, error) {
	provider := t.Provider
	if provider == nil {
		provider = DefaultMCPProvider()
	}
	if provider == nil {
		return nil, fmt.Errorf("no MCP servers are configured")
	}
	if len(args) == 0 {
		args = json.RawMessage(`{}`)
	}
	// The arguments go to the server verbatim: the schema the model was
	// given is the server's own, so the names are already the server's.
	return provider.CallTool(ctx, t.Server, t.Tool, args)
}

// AlwaysLoadedExecutors builds one executor per flagged tool.
//
// The caller does the listing — setup imports both this package and the MCP
// client, so it converts the client's result into the slice this takes. An
// empty slice is the common case (no server flagged anything), and there
// the clone must keep advertising exactly the reference's 60 names.
//
// The caller decides where the executors land: setup registers them with
// surface.RegisterPluginTool, which is the seam the advertised declaration
// list already merges, so the tool the model is shown and the tool the
// registry can run are the same object.
//
// A tool whose advertised name must not shadow one of the reference's own
// names is skipped: a server named `run` with a tool `command` would
// otherwise advertise `mcp_run_command`, which is distinct — but a server
// that suppresses its prefix could collide, and the caller filters those.
func AlwaysLoadedExecutors(flagged []AlwaysLoadedTool, provider MCPProvider) []tools.Tool {
	out := make([]tools.Tool, 0, len(flagged))
	for _, t := range flagged {
		if t.Server == "" || t.Name == "" {
			continue
		}
		out = append(out, &AlwaysLoadedToolExecutor{
			Server:     t.Server,
			Tool:       t.Name,
			Advertised: AlwaysLoadedToolName(t.Server, t.Name),
			Desc:       alwaysLoadedDescription(t),
			SchemaJSON: t.InputSchema,
			Provider:   provider,
		})
	}
	return out
}

// alwaysLoadedAdvertised records the names this process has advertised, so a
// re-install can withdraw the ones a server stopped flagging.
var (
	alwaysLoadedAdvertisedMu sync.Mutex
	alwaysLoadedAdvertised   = map[string]bool{}
)

// NoteAlwaysLoadedAdvertised records a name as advertised by this process.
//
// The installer calls it for every tool it registers, so
// AlwaysLoadedToolNames can later report the full set to withdraw.
func NoteAlwaysLoadedAdvertised(name string) {
	alwaysLoadedAdvertisedMu.Lock()
	defer alwaysLoadedAdvertisedMu.Unlock()
	alwaysLoadedAdvertised[name] = true
}

// ForgetAlwaysLoadedAdvertised records a name as no longer advertised.
func ForgetAlwaysLoadedAdvertised(name string) {
	alwaysLoadedAdvertisedMu.Lock()
	defer alwaysLoadedAdvertisedMu.Unlock()
	delete(alwaysLoadedAdvertised, name)
}

// AlwaysLoadedToolNames returns every name this process has advertised and
// not yet withdrawn.
func AlwaysLoadedToolNames() []string {
	alwaysLoadedAdvertisedMu.Lock()
	defer alwaysLoadedAdvertisedMu.Unlock()
	out := make([]string, 0, len(alwaysLoadedAdvertised))
	for name := range alwaysLoadedAdvertised {
		out = append(out, name)
	}
	return out
}

// alwaysLoadedDescription falls back to a generated line when the server
// sent no description, so the model is not handed a nameless tool.
func alwaysLoadedDescription(t AlwaysLoadedTool) string {
	if strings.TrimSpace(t.Description) != "" {
		return t.Description
	}
	return fmt.Sprintf("The %s tool on the %s MCP server.", t.Name, t.Server)
}
