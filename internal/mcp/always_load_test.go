package mcp_test

import (
	"reflect"
	"testing"

	"github.com/justwasm/pigasus/internal/mcp"
)

// TestParseToolsList_AlwaysLoadTrue verifies that an entry whose
// _meta carries {"antigravity/alwaysLoad": true} is parsed with
// AlwaysLoad=true. This is the AGY 1.3.3 changelog line 42 happy
// path: the server marks the tool as core and the clone's parser
// reflects that.
func TestParseToolsList_AlwaysLoadTrue(t *testing.T) {
	// parseToolsList is package-internal, but the registry surface
	// (NewToolRegistry + RegisterToolsList) is the documented
	// entrypoint; drive the parser through it.
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{
				"name": "screenshot",
				"description": "Take a browser screenshot.",
				"inputSchema": {"type": "object", "properties": {"url": {"type": "string"}}},
				"_meta": {"antigravity/alwaysLoad": true}
			}
		]
	}`)
	if err := reg.RegisterToolsList("browser", payload); err != nil {
		t.Fatalf("RegisterToolsList: %v", err)
	}
	core := reg.CoreTools()
	if len(core) != 1 {
		t.Fatalf("CoreTools: got %d entries, want 1 (%+v)", len(core), core)
	}
	if !core[0].AlwaysLoad {
		t.Errorf("AlwaysLoad = false, want true (got=%+v)", core[0])
	}
	if core[0].Name != "screenshot" {
		t.Errorf("Name = %q, want %q", core[0].Name, "screenshot")
	}
	if core[0].Server != "browser" {
		t.Errorf("Server = %q, want %q (registry should attach the server name)", core[0].Server, "browser")
	}
	if core[0].Description != "Take a browser screenshot." {
		t.Errorf("Description = %q, want %q", core[0].Description, "Take a browser screenshot.")
	}
	if reg.MCPDiscoveredSize() != 0 {
		t.Errorf("MCP-discovered catalog: got %d entries, want 0", reg.MCPDiscoveredSize())
	}
}

// TestParseToolsList_AlwaysLoadAbsent verifies that an entry
// without a _meta field is parsed with AlwaysLoad=false. The
// default is the safe one: a tool with no flag at all is
// discoverable, not core. A model that pre-loaded every advertised
// tool would surprise its admin.
func TestParseToolsList_AlwaysLoadAbsent(t *testing.T) {
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{"name": "echo", "description": "Echo the text back."},
			{"name": "search", "inputSchema": {"type": "object"}}
		]
	}`)
	if err := reg.RegisterToolsList("docs", payload); err != nil {
		t.Fatalf("RegisterToolsList: %v", err)
	}
	if got := reg.CoreSize(); got != 0 {
		t.Errorf("CoreSize = %d, want 0 (no _meta means no always-load)", got)
	}
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 2 {
		t.Fatalf("MCPDiscoveredTools: got %d, want 2 (%+v)", len(disc), disc)
	}
	for i, tool := range disc {
		if tool.AlwaysLoad {
			t.Errorf("discovered[%d].AlwaysLoad = true, want false (tool=%+v)", i, tool)
		}
	}
	want := []string{"echo", "search"}
	got := make([]string, 0, len(disc))
	for _, tool := range disc {
		got = append(got, tool.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("discovered names = %v, want %v (sorted by name)", got, want)
	}
}

// TestParseToolsList_AlwaysLoadFalse verifies that an explicit
// `antigravity/alwaysLoad: false` keeps the tool in the
// MCP-discovered catalog. The reference's protocol treats anything
// other than literal true as "no flag"; false must not be honoured
// as "off and stay off" — it is functionally identical to a missing
// key.
func TestParseToolsList_AlwaysLoadFalse(t *testing.T) {
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{
				"name": "rare_tool",
				"description": "Rarely used.",
				"_meta": {"antigravity/alwaysLoad": false}
			}
		]
	}`)
	if err := reg.RegisterToolsList("docs", payload); err != nil {
		t.Fatalf("RegisterToolsList: %v", err)
	}
	if got := reg.CoreSize(); got != 0 {
		t.Errorf("CoreSize = %d, want 0 (false is not honoured)", got)
	}
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 1 {
		t.Fatalf("MCPDiscoveredTools: got %d, want 1", len(disc))
	}
	if disc[0].AlwaysLoad {
		t.Errorf("AlwaysLoad on a flag=false entry should be false, got true")
	}
}

// TestParseToolsList_MetaNotObject verifies that a `_meta` value
// that is not an object (e.g. a bare string) is treated as "no flag
// set". The parser must not panic, must default AlwaysLoad to false,
// and must log a warning so the misconfiguration is visible in
// audit. We do not assert on the log here — only on the parser's
// no-panic + default-to-false behaviour — because the log destination
// is set by the embedding program.
func TestParseToolsList_MetaNotObject(t *testing.T) {
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{
				"name": "broken_meta",
				"_meta": "not an object"
			}
		]
	}`)
	// No panic, no error from the surrounding registration.
	if err := reg.RegisterToolsList("docs", payload); err != nil {
		t.Fatalf("RegisterToolsList: unexpected error for string _meta: %v", err)
	}
	if got := reg.CoreSize(); got != 0 {
		t.Errorf("CoreSize = %d, want 0 (malformed _meta defaults to false)", got)
	}
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 1 {
		t.Fatalf("MCPDiscoveredTools: got %d, want 1", len(disc))
	}
	if disc[0].AlwaysLoad {
		t.Errorf("AlwaysLoad on malformed _meta should default to false, got true")
	}
	if disc[0].Name != "broken_meta" {
		t.Errorf("Name = %q, want %q (entry should still parse, just with AlwaysLoad=false)", disc[0].Name, "broken_meta")
	}
}

// TestParseToolsList_AlwaysLoadNotBool verifies that a non-bool
// value under the antigravity/alwaysLoad key (a string in this
// case) is treated as "no flag set". Same defaults: AlwaysLoad=false,
// no panic, warning logged.
func TestParseToolsList_AlwaysLoadNotBool(t *testing.T) {
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{
				"name": "string_flag",
				"_meta": {"antigravity/alwaysLoad": "yes"}
			}
		]
	}`)
	if err := reg.RegisterToolsList("docs", payload); err != nil {
		t.Fatalf("RegisterToolsList: unexpected error for string flag: %v", err)
	}
	if got := reg.CoreSize(); got != 0 {
		t.Errorf("CoreSize = %d, want 0 (string flag defaults to false)", got)
	}
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 1 {
		t.Fatalf("MCPDiscoveredTools: got %d, want 1", len(disc))
	}
	if disc[0].AlwaysLoad {
		t.Errorf("AlwaysLoad on string flag should default to false, got true")
	}
}

// TestRegisterAlwaysLoad_AddsToCoreCatalog verifies the
// end-to-end registration path: a tool with AlwaysLoad=true in its
// _meta lands in the core catalog after RegisterToolsList. The
// previous tests verify that parseToolsList produces the right
// bool; this test verifies the dispatcher wires it to the right
// map.
func TestRegisterAlwaysLoad_AddsToCoreCatalog(t *testing.T) {
	reg := mcp.NewToolRegistry()
	payload := []byte(`{
		"tools": [
			{"name": "first",  "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "second", "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "third"}
		]
	}`)
	if err := reg.RegisterToolsList("browser", payload); err != nil {
		t.Fatalf("RegisterToolsList: %v", err)
	}
	core := reg.CoreTools()
	if len(core) != 2 {
		t.Fatalf("CoreTools: got %d, want 2 (%+v)", len(core), core)
	}
	// Sorted by name; first then second.
	wantCore := []string{"first", "second"}
	gotCore := make([]string, 0, len(core))
	for _, tool := range core {
		gotCore = append(gotCore, tool.Name)
	}
	if !reflect.DeepEqual(gotCore, wantCore) {
		t.Errorf("CoreTools names = %v, want %v", gotCore, wantCore)
	}
	for _, tool := range core {
		if !tool.AlwaysLoad {
			t.Errorf("core tool %q: AlwaysLoad=false, want true", tool.Name)
		}
		if tool.Server != "browser" {
			t.Errorf("core tool %q: Server=%q, want %q (registry should attach the server name)", tool.Name, tool.Server, "browser")
		}
	}
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 1 {
		t.Fatalf("MCPDiscoveredTools: got %d, want 1 (%+v)", len(disc), disc)
	}
	if disc[0].Name != "third" {
		t.Errorf("discovered tool name = %q, want %q", disc[0].Name, "third")
	}
	if disc[0].AlwaysLoad {
		t.Errorf("discovered tool AlwaysLoad=true, want false")
	}
}

// TestRegisterAlwaysLoad_PreservesExistingCoreTools verifies that
// pre-loaded core tools (registered before a tools/list arrives)
// are not dropped when a tools/list is processed. The clone's
// reference surface has always-loaded core tools that exist
// independently of MCP discovery; the MCP layer must not stomp on
// them.
func TestRegisterAlwaysLoad_PreservesExistingCoreTools(t *testing.T) {
	reg := mcp.NewToolRegistry()
	// Pre-load a core tool that does not come from MCP.
	reg.RegisterCoreTool(mcp.Tool{
		Name:        "core_static",
		Description: "Statically-defined core tool.",
		AlwaysLoad:  true,
	})
	// Now process a tools/list payload that brings in MCP-discovered
	// core + discoverable tools. None of them collide with
	// core_static, but a future refactor that resets the core map
	// would silently drop the static one.
	payload := []byte(`{
		"tools": [
			{"name": "mcp_core",      "_meta": {"antigravity/alwaysLoad": true}},
			{"name": "mcp_discovered"}
		]
	}`)
	if err := reg.RegisterToolsList("docs", payload); err != nil {
		t.Fatalf("RegisterToolsList: %v", err)
	}
	core := reg.CoreTools()
	wantCore := map[string]bool{
		"core_static": true,
		"mcp_core":    true,
	}
	if len(core) != len(wantCore) {
		t.Fatalf("CoreTools: got %d, want %d (%+v)", len(core), len(wantCore), core)
	}
	for _, tool := range core {
		if !wantCore[tool.Name] {
			t.Errorf("unexpected core tool %q (got=%+v)", tool.Name, tool)
		}
		if !tool.AlwaysLoad {
			t.Errorf("core tool %q: AlwaysLoad=false, want true", tool.Name)
		}
	}
	// And the static tool still has the description it was
	// registered with — the MCP tools/list handler did not
	// overwrite it.
	for _, tool := range core {
		if tool.Name == "core_static" && tool.Description != "Statically-defined core tool." {
			t.Errorf("core_static Description = %q, want %q (pre-loaded core tool must be preserved)",
				tool.Description, "Statically-defined core tool.")
		}
	}
	// Discoverable: only the MCP one.
	disc := reg.MCPDiscoveredTools()
	if len(disc) != 1 || disc[0].Name != "mcp_discovered" {
		t.Errorf("MCPDiscoveredTools = %+v, want one entry named mcp_discovered", disc)
	}
}
