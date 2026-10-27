package browsersubagent

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/tools"
)

// TestSurfaceOfTellsTheTwoSurfacesApart pins how a tool set is
// recognised: the bridge's tab listing is the marker, and the managed
// surface has no such tool.
func TestSurfaceOfTellsTheTwoSurfacesApart(t *testing.T) {
	managed := BrowserTools(tools.Catalog(t.TempDir()))
	if got := SurfaceOf(managed); got != SurfaceManaged {
		t.Errorf("SurfaceOf(managed) = %v, want the managed surface", got)
	}
	live := BridgeTools(tools.BridgeCatalog())
	if got := SurfaceOf(live); got != SurfaceLive {
		t.Errorf("SurfaceOf(live) = %v, want the live surface", got)
	}
}

// TestBridgeToolsAreTheProjectsTools pins the live surface's tool set:
// the bridge's own tools, and nothing of the managed surface.
func TestBridgeToolsAreTheProjectsTools(t *testing.T) {
	got := BridgeTools(tools.BridgeCatalog())
	if len(got) != len(tools.BridgeToolNames) {
		t.Fatalf("BridgeTools returned %d tools, want %d", len(got), len(tools.BridgeToolNames))
	}
	for _, tool := range got {
		if tool.Name() == "browser_click_element" {
			t.Error("the live surface carries a managed-surface tool")
		}
	}
	if BridgeTools(nil) != nil {
		t.Error("BridgeTools(nil) must be nil")
	}
}

// TestPromptFollowsTheSurface pins that the prompt says what the agent
// is driving: the managed surface speaks of the Chrome DevTools Protocol,
// the live surface of the user's own browser.
func TestPromptFollowsTheSurface(t *testing.T) {
	managed := Prompt([]string{"browser_click_element — click a thing"}, SurfaceManaged)
	if !strings.Contains(managed, "Chrome DevTools Protocol") {
		t.Errorf("the managed prompt does not name the protocol: %q", managed)
	}
	if strings.Contains(managed, "Open Claude in Chrome") {
		t.Errorf("the managed prompt mentions the extension: %q", managed)
	}

	live := Prompt([]string{"tabs_context_mcp — list the tabs"}, SurfaceLive)
	if !strings.Contains(live, "Open Claude in Chrome") {
		t.Errorf("the live prompt does not name the extension: %q", live)
	}
	if strings.Contains(live, "Chrome DevTools Protocol") {
		t.Errorf("the live prompt speaks of the protocol: %q", live)
	}
	if !strings.HasPrefix(live, PromptLead) {
		t.Errorf("the live prompt does not open with the reference's sentence: %q", live)
	}
}

// TestLiveSurface drives the real extension, when one is running.
//
// It is skipped unless PIGASUS_BRIDGE_LIVE is set: the live bridge
// belongs to the user, and a test must not touch their browser without
// being asked to.
func TestLiveSurface(t *testing.T) {
	if os.Getenv("PIGASUS_BRIDGE_LIVE") == "" {
		t.Skip("set PIGASUS_BRIDGE_LIVE=1 to drive the user's own browser")
	}
	prev := browsercdp.CurrentConfig()
	cfg := prev
	cfg.UseLocalChrome = true
	browsercdp.Configure(cfg)
	t.Cleanup(func() { browsercdp.Configure(prev) })

	if got := CurrentSurface(); got != SurfaceLive {
		t.Fatalf("CurrentSurface() = %v, want the live surface", got)
	}
	got := Tools(nil)
	if len(got) != len(tools.BridgeToolNames) {
		t.Fatalf("the live tool set holds %d tools, want %d", len(got), len(tools.BridgeToolNames))
	}

	// One real call: the tabs of the user's own browser.
	tool, ok := tools.BridgeCatalog().Get("tabs_context_mcp")
	if !ok {
		t.Fatal("the bridge catalogue does not carry tabs_context_mcp")
	}
	out, err := tool.Run(context.Background(), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("tabs_context_mcp: %v", err)
	}
	text, _ := out.(string)
	if !strings.Contains(text, "availableTabs") {
		t.Fatalf("the bridge did not answer with the tabs: %q", text)
	}
	t.Logf("tabs: %s", text[:min(len(text), 160)])
}

// min is the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
