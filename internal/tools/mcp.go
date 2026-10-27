// mcp.go — the reference's three MCP tools.
//
// What the reference exposes
// --------------------------
// The reference advertises three MCP tools to the model:
//
//	call_mcp_tool   call a tool on a server whose tool list the model
//	                discovered lazily
//	list_resources  list a server's resources
//	read_resource   read one resource
//
// Their *declarations* are the reference's own (【反编译】, the 1.4.8
// binary's Go type metadata carries the reflected parameter structs
// verbatim — see the schema comments below), and their *descriptions* are
// the reference's own text (【strings】, read out of the binary and
// committed at testdata/tool_descriptions_1.4.7.json, layer "converter").
//
// The reference's parameter names
// ------------------------------
// All three tools use PascalCase parameter names, recovered from the
// reference's reflected schema structs:
//
//	call_mcp_tool   ServerName, ToolName, Arguments
//	list_resources  ServerName
//	read_resource   ServerName, Uri
//
// The clone's canonicaliser (tools_params.go) lower-cases a model's keys,
// so a `ServerName` argument arrives as `server_name`. These tools read
// the arguments themselves instead of going through argsOf, for two
// reasons:
//
//   - the reference's names must survive to the wire — `call_mcp_tool`'s
//     arguments are addressed by name on the MCP server side;
//   - `call_mcp_tool`'s `Arguments` payload belongs to the *server*, which
//     owns those names. The canonicaliser would rewrite them
//     (`camelToSnake`), and a server whose schema says `query_string`
//     would receive `query_string` — but a server whose schema says
//     `queryString` would receive `query_string` and reject the call.
//
// The MCP client underneath
// -------------------------
// The tools call through the MCPProvider seam rather than a concrete
// client, so internal/tools stays free of any MCP dependency; the concrete
// client lives in mcp and is installed with SetMCPProvider.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// MCPProvider is the seam the three MCP tools use to reach a server.
//
// It returns the *text the model sees*, not a structured value: the
// reference's result renderer is not recoverable from the binary (see
// mcp/client.go's header), so the rendering is done once, in the
// client, and the tools only forward it.
type MCPProvider interface {
	// CallTool calls a tool on a server. args is the tool's argument
	// object, verbatim.
	CallTool(ctx context.Context, server, tool string, args json.RawMessage) (string, error)
	// ListResources lists a server's resources.
	ListResources(ctx context.Context, server string) (string, error)
	// ReadResource reads one resource from a server.
	ReadResource(ctx context.Context, server, uri string) (string, error)
}

var (
	mcpProviderMu sync.RWMutex
	mcpProvider   MCPProvider
)

// SetMCPProvider installs the process-wide MCP provider the three MCP tools
// call through, and returns the previous one.
//
// The provider is process-wide because the tools are built by
// tools.Catalog, which takes no provider argument and is called from two
// places (the print path and the TUI) — and because the underlying
// connection pool is itself process-wide state (one connection per server,
// reused across turns). A test that needs its own provider should set the
// field on the executor instead.
func SetMCPProvider(p MCPProvider) MCPProvider {
	mcpProviderMu.Lock()
	defer mcpProviderMu.Unlock()
	prev := mcpProvider
	mcpProvider = p
	return prev
}

// DefaultMCPProvider returns the process-wide MCP provider, or nil when none
// is installed.
func DefaultMCPProvider() MCPProvider {
	mcpProviderMu.RLock()
	defer mcpProviderMu.RUnlock()
	return mcpProvider
}

// callMcpToolSchema is the reference's declaration for call_mcp_tool.
//
// The reference builds the schema by reflecting over
//
//	struct {
//	    ServerName string `jsonschema:"required" jsonschema_description:"Name of the MCP server."`
//	    ToolName   string `jsonschema:"required" jsonschema_description:"Name of the tool to call."`
//	    Arguments  encoding/json/jsontext.Value `jsonschema:"required" jsonschema_description:"Arguments to pass to the tool."`
//	}
//
// (【反编译】 the 1.4.8 binary's Go type metadata — the generic
// instantiation's own name carries the tags verbatim:
// google3/third_party/jetski/cortex/utils/utils.ToJsonSchemaString[go.shape.struct { ServerName string "jsonschema:\"required\" jsonschema_description:\"Name of the MCP server.\""; ToolName string …; Arguments encoding/json/jsontext.Value … }].)
// The reflection is the reference's own helper
// (cortex/utils/utils.ToJsonSchemaString), so the exact JSON it emits is
// not recoverable without decompiling it; the shape below is the clone's
// own, matching the house style of the other catalogue schemas.
//
// `Arguments` is a JSON value, not an object — the reference's type is
// encoding/json/jsontext.Value — so it carries no `type` constraint.
var callMcpToolSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"ServerName":{"type":"string","description":"Name of the MCP server."},` +
	`"ToolName":{"type":"string","description":"Name of the tool to call."},` +
	`"Arguments":{"description":"Arguments to pass to the tool."}` +
	`},"required":["ServerName","ToolName","Arguments"]}`)

// listResourcesSchema is the reference's declaration for list_resources.
//
// Reflected from
//
//	struct {
//	    ServerName string `jsonschema:"required,minLength=1" jsonschema_description:"Name of the server to list available resources from."`
//	}
//
// (【反编译】 the 1.4.8 binary's type metadata, same instantiation shape
// as above.) The `minLength=1` constraint is carried over.
var listResourcesSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"ServerName":{"type":"string","minLength":1,"description":"Name of the server to list available resources from."}` +
	`},"required":["ServerName"]}`)

// readResourceSchema is the reference's declaration for read_resource.
//
// Reflected from
//
//	struct {
//	    ServerName string `jsonschema_description:"Name of the server to read the resource from."`
//	    Uri        string `jsonschema_description:"Unique identifier for the resource."`
//	}
//
// (【反编译】 the 1.4.8 binary's type metadata.) Neither field carries a
// `jsonschema:"required"` tag, so the reference declares no required set —
// the clone therefore omits the `required` key rather than advertising an
// empty one.
var readResourceSchema = json.RawMessage(`{"type":"object","properties":{` +
	`"ServerName":{"type":"string","description":"Name of the server to read the resource from."},` +
	`"Uri":{"type":"string","description":"Unique identifier for the resource."}` +
	`}}`)

// CallMcpTool is the reference's call_mcp_tool.
//
// The reference calls a *lazy-loaded* MCP tool: the tool's schema lives in
// a file the model reads first ("Read the tool's schema file to understand
// the tool's arguments and usage"), and the call itself names the server
// and the tool.
type CallMcpTool struct {
	// Provider is the client the tool calls through. Nil uses the
	// process-wide provider (see SetMCPProvider).
	Provider MCPProvider
}

func (t *CallMcpTool) Name() string        { return "call_mcp_tool" }
func (t *CallMcpTool) Description() string { return refDesc("call_mcp_tool") }
func (t *CallMcpTool) Schema() json.RawMessage {
	return callMcpToolSchema
}

func (t *CallMcpTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	fields, err := mcpArgs(args)
	if err != nil {
		return nil, err
	}
	server := mcpArgString(fields, "ServerName")
	if server == "" {
		return nil, fmt.Errorf("ServerName is a required parameter. Follow the function call schema exactly.")
	}
	tool := mcpArgString(fields, "ToolName")
	if tool == "" {
		return nil, fmt.Errorf("ToolName is a required parameter. Follow the function call schema exactly.")
	}
	payload, ok := lookupMcpArg(fields, "Arguments")
	if !ok || len(payload) == 0 {
		payload = json.RawMessage(`{}`)
	}
	provider := t.Provider
	if provider == nil {
		provider = DefaultMCPProvider()
	}
	if provider == nil {
		return nil, fmt.Errorf("no MCP servers are configured")
	}
	if known, canTell := mcpServerKnown(provider, server); canTell && !known {
		return nil, mcpNoServerError(provider, server)
	}
	return provider.CallTool(ctx, server, tool, payload)
}

// ListResources is the reference's list_resources.
type ListResources struct {
	// Provider is the client the tool calls through. Nil uses the
	// process-wide provider (see SetMCPProvider).
	Provider MCPProvider
}

func (t *ListResources) Name() string        { return "list_resources" }
func (t *ListResources) Description() string { return refDesc("list_resources") }
func (t *ListResources) Schema() json.RawMessage {
	return listResourcesSchema
}

func (t *ListResources) Run(ctx context.Context, args json.RawMessage) (any, error) {
	fields, err := mcpArgs(args)
	if err != nil {
		return nil, err
	}
	server := mcpArgString(fields, "ServerName")
	if server == "" {
		return nil, fmt.Errorf("ServerName is a required parameter. Follow the function call schema exactly.")
	}
	provider := t.Provider
	if provider == nil {
		provider = DefaultMCPProvider()
	}
	if provider == nil {
		return nil, fmt.Errorf("no MCP servers are configured")
	}
	if known, canTell := mcpServerKnown(provider, server); canTell && !known {
		return nil, mcpNoServerError(provider, server)
	}
	return provider.ListResources(ctx, server)
}

// ReadResource is the reference's read_resource.
type ReadResource struct {
	// Provider is the client the tool calls through. Nil uses the
	// process-wide provider (see SetMCPProvider).
	Provider MCPProvider
}

func (t *ReadResource) Name() string        { return "read_resource" }
func (t *ReadResource) Description() string { return refDesc("read_resource") }
func (t *ReadResource) Schema() json.RawMessage {
	return readResourceSchema
}

func (t *ReadResource) Run(ctx context.Context, args json.RawMessage) (any, error) {
	fields, err := mcpArgs(args)
	if err != nil {
		return nil, err
	}
	server := mcpArgString(fields, "ServerName")
	if server == "" {
		return nil, fmt.Errorf("ServerName is a required parameter. Follow the function call schema exactly.")
	}
	uri := mcpArgString(fields, "Uri")
	if uri == "" {
		return nil, fmt.Errorf("Uri is a required parameter. Follow the function call schema exactly.")
	}
	provider := t.Provider
	if provider == nil {
		provider = DefaultMCPProvider()
	}
	if provider == nil {
		return nil, fmt.Errorf("no MCP servers are configured")
	}
	if known, canTell := mcpServerKnown(provider, server); canTell && !known {
		return nil, mcpNoServerError(provider, server)
	}
	return provider.ReadResource(ctx, server, uri)
}

// mcpArgs decodes an MCP tool call's arguments into their raw values.
//
// The payload is an object; a non-object (or an empty payload) is an empty
// object. Unlike argsOf, the *values* are kept verbatim — the nested keys
// belong to the MCP server.
func mcpArgs(raw json.RawMessage) (map[string]json.RawMessage, error) {
	if len(raw) == 0 {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("invalid tool arguments: %w", err)
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	return fields, nil
}

// lookupMcpArg finds an argument by any of its accepted spellings.
//
// The exact spelling wins; otherwise the argument is matched on its
// canonical (snake_case) form, so `ServerName`, `serverName` and
// `server_name` all resolve to the same field. When several keys
// canonicalise to the same name the lexicographically smallest wins, so
// the lookup is deterministic.
func lookupMcpArg(fields map[string]json.RawMessage, names ...string) (json.RawMessage, bool) {
	for _, name := range names {
		if v, ok := fields[name]; ok {
			return v, true
		}
	}
	for _, name := range names {
		want := camelToSnake(name)
		var bestKey string
		var best json.RawMessage
		found := false
		for key, value := range fields {
			if camelToSnake(key) != want {
				continue
			}
			if !found || key < bestKey {
				bestKey, best, found = key, value, true
			}
		}
		if found {
			return best, true
		}
	}
	return nil, false
}

// mcpArgString reads an argument as a string. A missing argument, or one
// whose value is not a string, yields "".
func mcpArgString(fields map[string]json.RawMessage, names ...string) string {
	raw, ok := lookupMcpArg(fields, names...)
	if !ok {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// mcpKnownServers returns the server names a provider knows, for an error
// message that names them. It returns nil when the provider cannot tell.
func mcpKnownServers(p MCPProvider) []string {
	np, ok := p.(namedProvider)
	if !ok {
		return nil
	}
	names := np.Servers()
	sort.Strings(names)
	return names
}

// namedProvider is a provider that can list the servers it knows.
type namedProvider interface {
	Servers() []string
}

// mcpServerKnown reports whether the provider knows a server. The second
// result is false when the provider cannot tell — in that case the call is
// made and the provider decides.
func mcpServerKnown(p MCPProvider, server string) (known bool, canTell bool) {
	np, ok := p.(namedProvider)
	if !ok {
		return true, false
	}
	for _, name := range np.Servers() {
		if name == server {
			return true, true
		}
	}
	return false, true
}

// mcpNoServerError is the message the three tools use when the model names
// a server the provider does not know. It lists the known servers when the
// provider can tell, so the model can correct the name in one step.
func mcpNoServerError(p MCPProvider, server string) error {
	known := mcpKnownServers(p)
	if len(known) == 0 {
		return fmt.Errorf("MCP server %q not found", server)
	}
	return fmt.Errorf("MCP server %q not found (known servers: %s)", server, strings.Join(known, ", "))
}
