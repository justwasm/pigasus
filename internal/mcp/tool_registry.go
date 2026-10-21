// Package mcp owns the clone's protocol-level surface for the Model
// Context Protocol — currently a single gap: AGY 1.3.3 changelog line 42
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
// What this file owns
// -------------------
//
//  1. The single JSON-decoder entrypoint for a `tools/list` payload
//     (parseToolsList).
//  2. The constant AntigravityAlwaysLoadMetaKey — the AGY project's
//     single declaration of the spelling it reads. A misspelling here
//     would silently make the flag unobservable.
//  3. The Tool descriptor with the computed AlwaysLoad bool field.
//  4. A ToolRegistry that splits tools into a core catalog
//     (always-loaded, advertised directly to the model) and an
//     MCP-discovered catalog (reachable only through call_mcp_tool).
//
// What this file does NOT own
// ---------------------------
//
//   - Server config. The `mcp.json` per-server static hint lives in
//     internal/pigrt/mcpconfig.go (agyMCPServer.AlwaysLoad). This
//     package only handles the protocol-side `_meta` flag and the
//     in-memory registration surface.
//
//   - Dispatch. The reference's "always load" semantics in 1.3.3 is
//     "advertise that tool to the model as its own entry". The actual
//     dispatch path (mcp__<server>__<tool> → tools/call) is PiG's MCP
//     extension's responsibility; this package only decides which
//     catalog a parsed tool ends up in.
package mcp

import (
	"encoding/json"
	"log"
)

// AntigravityAlwaysLoadMetaKey is the AGY-defined key inside a
// `tools/list` entry's `_meta` object that opts a tool into being
// advertised to the model directly, instead of remaining reachable
// only through the discoverable call_mcp_tool surface.
//
// MCP's `_meta` field is intentionally open-ended; this constant is
// the AGY project's single declaration of the spelling it reads. A
// misspelling here would silently make the flag unobservable.
const AntigravityAlwaysLoadMetaKey = "antigravity/alwaysLoad"

// Tool is one tool an MCP server returned in a `tools/list`
// response, after parsing. The AlwaysLoad field is computed from the
// `_meta` bag; the rest is the MCP-spec subset the registry surfaces.
type Tool struct {
	// Name is the tool's MCP name (the part after the
	// `mcp__<server>__` prefix the runtime adds in front).
	Name string
	// Description is the human-readable description the server
	// supplied. Empty when the server omitted one.
	Description string
	// InputSchema is the JSON Schema the server returned for the
	// tool's arguments. May be empty (the MCP spec permits it);
	// callers should treat a missing schema as
	// `{"type":"object"}`.
	InputSchema json.RawMessage
	// AlwaysLoad is true iff the entry's `_meta` carried
	// {"antigravity/alwaysLoad": true}. Computed; callers do not
	// set it directly. Defaults to false on missing keys, wrong
	// types, or non-object `_meta` bags.
	AlwaysLoad bool
	// Server is the MCP server name the registry attached when
	// the tool was registered. Set by RegisterToolsList from the
	// surrounding server context; empty when RegisterCoreTool /
	// RegisterDiscoveredTool are called directly.
	Server string
}

// toolsListResponse is the JSON shape of an MCP `tools/list` response.
// The MCP spec puts the array under `tools` (alongside `nextCursor`,
// which we ignore — our spec is single-shot).
type toolsListResponse struct {
	Tools []toolsListEntry `json:"tools"`
}

// toolsListEntry is the JSON shape one item in a `tools/list`
// response's `tools` array takes. The MCP spec permits additional
// fields; only the four we read are decoded and the rest is dropped
// (json.Unmarshal ignores unknown fields by default), so a server
// extending its own entries with new fields does not break parsing.
type toolsListEntry struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"inputSchema,omitempty"`
	Meta        json.RawMessage `json:"_meta,omitempty"`
}

// parseToolsList parses a `tools/list` JSON response and returns the
// parsed tools. An empty payload is not an error: it yields an empty
// slice — every server that has not yet connected its tools/list
// produces this state, and downstream code should treat it as "no
// tools from this server", not as an error. Nil and zero-length
// payloads take the same path (json.Unmarshal returns "unexpected
// end of JSON input" on a nil byte slice, which we silence here).
//
// A malformed payload (bad top-level JSON, or `tools` carrying a
// non-array value) returns the unmarshal error. A payload whose
// entries each parse correctly is never an error — including a
// payload whose entries are missing `_meta`, missing `name`, or have
// a malformed `_meta`. We surface "this entry is not always-load"
// rather than failing the whole parse, because the MCP spec lets a
// server degrade one entry without breaking the rest.
func parseToolsList(payload []byte) ([]Tool, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	var resp toolsListResponse
	if err := json.Unmarshal(payload, &resp); err != nil {
		return nil, err
	}
	out := make([]Tool, 0, len(resp.Tools))
	for _, entry := range resp.Tools {
		out = append(out, Tool{
			Name:        entry.Name,
			Description: entry.Description,
			InputSchema: entry.InputSchema,
			AlwaysLoad:  entryAlwaysLoad(entry.Meta, entry.Name),
		})
	}
	return out, nil
}

// entryAlwaysLoad reports whether the AGY-defined alwaysLoad meta
// flag is set on an entry. The MCP spec defines `_meta` as an object
// with implementation-defined keys; we look only under our own key
// and require a JSON-true value — anything else (false, null, "true"
// string, the number 1, a missing key) yields false. A malformed
// `_meta` (not an object at all) is treated as "no flag set" and a
// warning is logged so a misconfigured MCP server is visible in
// audit.
//
// The bool-only check is intentional: the reference uses a strict
// bool true in the decompiled source. Anything weaker than that risks
// surprising the model with a tool that the admin did not actually
// flag.
func entryAlwaysLoad(meta json.RawMessage, name string) bool {
	if len(meta) == 0 {
		return false
	}
	var lookup struct {
		AlwaysLoad json.RawMessage `json:"antigravity/alwaysLoad"`
	}
	if err := json.Unmarshal(meta, &lookup); err != nil {
		log.Printf("mcp: tool %q: _meta is not an object, ignoring alwaysLoad: %v", name, err)
		return false
	}
	if len(lookup.AlwaysLoad) == 0 {
		return false
	}
	var flag bool
	if err := json.Unmarshal(lookup.AlwaysLoad, &flag); err != nil {
		log.Printf("mcp: tool %q: antigravity/alwaysLoad is not a bool, defaulting to false: %v", name, err)
		return false
	}
	return flag
}

// ToolRegistry is the in-memory split of MCP-discovered tools into
// the two catalogs the runtime advertises to the model:
//
//   - Core (always-loaded): tools whose _meta carried
//     {"antigravity/alwaysLoad": true}. These are exposed directly
//     to the model under the mcp__<server>__<tool> name.
//
//   - MCPDiscovered: the rest. The model reaches these only through
//     the call_mcp_tool aggregator.
//
// The two maps are kept separate so a caller can tell at a glance
// which surface a tool is on. Insertion order is not preserved; the
// model-facing list is sorted by name by Core() and
// MCPDiscovered().
type ToolRegistry struct {
	core     map[string]Tool
	discover map[string]Tool
}

// NewToolRegistry returns an empty ToolRegistry. The returned value
// is the zero-state a fresh session starts in.
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{
		core:     map[string]Tool{},
		discover: map[string]Tool{},
	}
}

// RegisterCoreTool adds a tool to the core (always-loaded) catalog
// under its Name. A pre-existing entry with the same name is
// overwritten; the model-facing set is keyed by name so a duplicate
// always-loaded tool from two sources collapses to one entry.
//
// RegisterCoreTool is the explicit path; RegisterToolsList uses it
// for every entry whose _meta carried the flag.
func (r *ToolRegistry) RegisterCoreTool(t Tool) {
	if r == nil {
		return
	}
	if r.core == nil {
		r.core = map[string]Tool{}
	}
	r.core[t.Name] = t
}

// RegisterDiscoveredTool adds a tool to the MCP-discovered catalog
// under its Name. Same overwrite semantics as RegisterCoreTool.
func (r *ToolRegistry) RegisterDiscoveredTool(t Tool) {
	if r == nil {
		return
	}
	if r.discover == nil {
		r.discover = map[string]Tool{}
	}
	r.discover[t.Name] = t
}

// RegisterToolsList parses a `tools/list` JSON payload and routes
// each entry to the right catalog based on its `_meta` flag. The
// server name is attached to every tool so the runtime can compose
// the mcp__<server>__<tool> canonical name later.
//
// A malformed payload returns the json.Unmarshal error and leaves
// the existing catalogs untouched. A payload whose entries each
// parse correctly is never an error — including entries with
// missing names (the spec requires names but a misbehaving server
// should not break the rest of the catalog). The default for a
// missing-name entry is the empty string; the model-facing name
// composition will fail to produce a useful tool name, which is
// the right outcome (the misconfigured tool is reachable through
// the discoverable path but won't accidentally collide with a
// well-named sibling).
func (r *ToolRegistry) RegisterToolsList(server string, payload []byte) error {
	if r == nil {
		return nil
	}
	parsed, err := parseToolsList(payload)
	if err != nil {
		return err
	}
	for _, t := range parsed {
		t.Server = server
		if t.AlwaysLoad {
			r.RegisterCoreTool(t)
		} else {
			r.RegisterDiscoveredTool(t)
		}
	}
	return nil
}

// CoreTools returns the always-loaded tools, sorted by Name.
func (r *ToolRegistry) CoreTools() []Tool {
	if r == nil {
		return nil
	}
	return sortedTools(r.core)
}

// MCPDiscoveredTools returns the discoverable tools, sorted by Name.
func (r *ToolRegistry) MCPDiscoveredTools() []Tool {
	if r == nil {
		return nil
	}
	return sortedTools(r.discover)
}

// CoreSize returns the number of tools in the core catalog.
func (r *ToolRegistry) CoreSize() int {
	if r == nil {
		return 0
	}
	return len(r.core)
}

// MCPDiscoveredSize returns the number of tools in the
// MCP-discovered catalog.
func (r *ToolRegistry) MCPDiscoveredSize() int {
	if r == nil {
		return 0
	}
	return len(r.discover)
}

// AllTools returns core tools followed by MCP-discovered tools,
// each block sorted by name. Used by callers that want a single
// list (e.g. an audit log dump) without collapsing the two
// surfaces.
func (r *ToolRegistry) AllTools() []Tool {
	if r == nil {
		return nil
	}
	all := make([]Tool, 0, len(r.core)+len(r.discover))
	all = append(all, sortedTools(r.core)...)
	all = append(all, sortedTools(r.discover)...)
	return all
}

func sortedTools(m map[string]Tool) []Tool {
	if len(m) == 0 {
		return nil
	}
	out := make([]Tool, 0, len(m))
	for _, t := range m {
		out = append(out, t)
	}
	// Insertion sort over a small map (handful to a few dozen tools
	// in a typical MCP server); stdlib sort.Slice would also work
	// but the smaller allocation is cheap and keeps the function
	// dependency-free.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
