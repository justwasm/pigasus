package tools

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBridgeCatalogCarriesTheProjectsTools pins the bridge surface: the
// project's own tool names, each with a schema, each reachable out of the
// registry.
func TestBridgeCatalogCarriesTheProjectsTools(t *testing.T) {
	reg := BridgeCatalog()
	if reg.Size() != len(BridgeToolNames) {
		t.Fatalf("BridgeCatalog holds %d tools, want %d", reg.Size(), len(BridgeToolNames))
	}
	for _, name := range BridgeToolNames {
		tool, ok := reg.Get(name)
		if !ok || tool == nil {
			t.Fatalf("the bridge catalogue does not carry %q", name)
		}
		var schema map[string]any
		if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
			t.Fatalf("%s: schema is not JSON: %v", name, err)
		}
		if schema["type"] != "object" {
			t.Errorf("%s: schema is not an object schema: %s", name, tool.Schema())
		}
	}
}

// TestNavigateDescriptionMatchesTheBridgesOwnSemantics pins the one
// description that used to narrow the surface.
//
// The bridge accepts a URL with or without a protocol, and "forward"
// and "back" to move through the browser's history. A description that
// says the URL "must be absolute" sends the model away from two of the
// three forms the tool really takes — and the model's only source for
// that is this text.
func TestNavigateDescriptionMatchesTheBridgesOwnSemantics(t *testing.T) {
	reg := BridgeCatalog()
	tool, ok := reg.Get("navigate")
	if !ok || tool == nil {
		t.Fatal("the bridge catalogue does not carry the navigate tool")
	}
	bridge, ok := tool.(*bridgeTool)
	if !ok {
		t.Fatalf("the navigate tool is a %T, want the bridge's own", tool)
	}
	desc := bridge.Description()
	if strings.Contains(desc, "must be absolute") {
		t.Errorf("the navigate description still demands an absolute URL: %s", desc)
	}
	for _, word := range []string{"protocol", "forward", "back"} {
		if !strings.Contains(desc, word) {
			t.Errorf("the navigate description does not mention %q: %s", word, desc)
		}
	}

	var schema map[string]any
	if err := json.Unmarshal(tool.Schema(), &schema); err != nil {
		t.Fatalf("schema is not JSON: %v", err)
	}
	props, _ := schema["properties"].(map[string]any)
	url, _ := props["url"].(map[string]any)
	text, _ := url["description"].(string)
	if strings.Contains(text, "absolute") {
		t.Errorf("the url property still demands an absolute URL: %s", text)
	}
}

// TestBridgeToolsAreNotAdvertised pins the advertised surface: the
// catalogue is the reference's own list of tool names, and the bridge
// tools are the browser agent's alone.
func TestBridgeToolsAreNotAdvertised(t *testing.T) {
	reg := Catalog(t.TempDir())
	for _, name := range BridgeToolNames {
		if _, ok := reg.Get(name); ok {
			t.Errorf("the advertised catalogue carries the bridge tool %q", name)
		}
	}
}

// TestRenderBridgeResult pins the rendering: the project's tools answer
// with an MCP-shaped content array, and the model reads the text.
func TestRenderBridgeResult(t *testing.T) {
	raw := json.RawMessage(`{"content":[{"type":"text","text":"first"},{"type":"text","text":"second"}]}`)
	if got := renderBridgeResult(raw); got != "first\nsecond" {
		t.Errorf("renderBridgeResult = %q, want the two texts", got)
	}
}

// TestRenderBridgeResultKeepsUnknownShapes pins that an answer the clone
// does not recognise is handed over as it came: the model can read JSON,
// and guessing would lose information.
func TestRenderBridgeResultKeepsUnknownShapes(t *testing.T) {
	raw := json.RawMessage(`{"availableTabs":[{"tabId":1}]}`)
	if got := renderBridgeResult(raw); got != string(raw) {
		t.Errorf("renderBridgeResult = %q, want the raw answer", got)
	}
	if got := renderBridgeResult(nil); got != "" {
		t.Errorf("renderBridgeResult(nil) = %q, want the empty string", got)
	}
}

// TestBridgeToolNamesAreTheBrowsingOnes pins the list: the browser half
// of the project's tool set, and none of its recorder or plan tools.
func TestBridgeToolNamesAreTheBrowsingOnes(t *testing.T) {
	want := map[string]bool{
		"tabs_context_mcp":      true,
		"tabs_create_mcp":       true,
		"tabs_close_mcp":        true,
		"navigate":              true,
		"computer":              true,
		"find":                  true,
		"form_input":            true,
		"get_page_text":         true,
		"read_page":             true,
		"javascript_tool":       true,
		"read_console_messages": true,
		"read_network_requests": true,
		"resize_window":         true,
		"upload_image":          true,
		"file_upload":           true,
		"set_tab_focus":         true,
		"switch_browser":        true,
	}
	for _, name := range BridgeToolNames {
		if !want[name] {
			t.Errorf("%q is not part of the bridge surface", name)
		}
		delete(want, name)
	}
	for name := range want {
		t.Errorf("the bridge surface is missing %q", name)
	}
	for _, name := range BridgeToolNames {
		if strings.HasPrefix(name, "shortcuts_") || strings.HasPrefix(name, "recording") {
			t.Errorf("%q belongs to a Claude Code session, not to a browsing task", name)
		}
	}
}
