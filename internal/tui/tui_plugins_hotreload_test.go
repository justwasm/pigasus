package tui

import (
	"context"
	"encoding/json"
	"flag"
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestRootModel_PluginReloadMsg_RefreshesTools(t *testing.T) {
	ws := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", ws, false)
	setupTUIToolsAndPolicy(m, fs)

	initialTools := len(m.tools.decls)
	if initialTools == 0 {
		t.Fatalf("expected initial tools to be populated, got %d", initialTools)
	}

	// Send pluginReloadMsg to Update via updateMouseAndMiscMsg
	_, cmd, handled := m.updateMouseAndMiscMsg(pluginReloadMsg{})
	if !handled {
		t.Fatalf("expected pluginReloadMsg to be handled by updateMouseAndMiscMsg")
	}
	if cmd != nil {
		t.Fatalf("expected cmd to be nil, got %v", cmd)
	}

	// Verify tools catalog is valid and active
	if m.tools.catalog == nil {
		t.Fatalf("expected tools catalog to be non-nil after reload")
	}
	if len(m.tools.decls) == 0 {
		t.Fatalf("expected tools decls to be non-empty after reload")
	}
}

type testDummyTool struct {
	name string
}

func (d *testDummyTool) Name() string            { return d.name }
func (d *testDummyTool) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (d *testDummyTool) Description() string     { return "test tool description" }
func (d *testDummyTool) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return "dynamic_plugin_executed", nil
}

func TestRootModel_HotReload_PreservesPointerAndReflectsDynamicTools(t *testing.T) {
	ws := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", ws, false)
	setupTUIToolsAndPolicy(m, fs)

	origCatalogPtr := m.tools.catalog
	origDeclsCount := len(m.tools.decls)

	// Simulate a newly installed plugin adding a dynamic tool
	surface.RegisterPluginTool(&testDummyTool{name: "custom_plugin_tool"})
	defer surface.UnregisterPluginTool("custom_plugin_tool")

	// Trigger hot reload via message
	_, _, handled := m.updateMouseAndMiscMsg(pluginReloadMsg{})
	if !handled {
		t.Fatal("expected pluginReloadMsg to be handled")
	}

	// Verify catalog pointer was preserved via ReplaceWith (in-place update)
	if m.tools.catalog != origCatalogPtr {
		t.Errorf("catalog pointer changed; expected in-place preservation for active turn runners")
	}

	// Verify the new tool is invokable through runner
	res, err := m.tools.runner.Run(context.Background(), backend.ToolCall{
		Name:      "custom_plugin_tool",
		Arguments: json.RawMessage(`{}`),
	})
	if err != nil {
		t.Fatalf("runner failed to execute dynamic tool: %v", err)
	}
	if res != "dynamic_plugin_executed" {
		t.Errorf("expected 'dynamic_plugin_executed', got %q", res)
	}

	// Verify tool declaration count reflects dynamic tool
	var found bool
	for _, d := range m.tools.decls {
		if d.Name == "custom_plugin_tool" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'custom_plugin_tool' to be advertised in m.tools.decls (total decls: %d, orig: %d)",
			len(m.tools.decls), origDeclsCount)
	}
}
