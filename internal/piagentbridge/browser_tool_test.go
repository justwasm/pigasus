// browser_tool_test.go: the reference's `webview_browser` tool on PiG's
// wire.
//
// 【一手】 2026-10-27, the reference's language server ships exactly one
// browser tool — `webview_browser` — with a twenty-value `action` enum.
package piagentbridge

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/agentbrowser"
)

// browserBroker returns a broker with one window that answers every
// request with the given payload.
func browserBroker(t *testing.T, payload string) *agentbrowser.Broker {
	t.Helper()
	broker := agentbrowser.NewBroker()
	sub := broker.Subscribe(context.Background(), agentbrowser.SubscribeRequest{
		WindowID:                    "win-1",
		DisplayedRootConversationID: "conv-1",
	})
	t.Cleanup(sub.Close)
	go func() {
		for req := range sub.Requests {
			_ = broker.Fulfill(&agentbrowser.Response{
				RequestID:  req.RequestID,
				OK:         true,
				ResultJSON: payload,
				WindowID:   "win-1",
			})
		}
	}()
	return broker
}

func TestBrowserToolAdvertisesTheReferenceDeclaration(t *testing.T) {
	tool := BrowserTool(browserBroker(t, `{"ok":true}`), agentbrowser.Options{})
	if tool.Name() != "webview_browser" {
		t.Fatalf("the name is %q", tool.Name())
	}
	if tool.Label() != "webview_browser" {
		t.Fatalf("the label is %q", tool.Label())
	}
	if tool.ExecutionMode() != "sequential" {
		t.Fatalf("the execution mode is %q", tool.ExecutionMode())
	}
	schema := tool.Schema()
	if schema.Name != "webview_browser" {
		t.Fatalf("the schema's name is %q", schema.Name)
	}
	if len(schema.Description) != 611 {
		t.Fatalf("the description is %d bytes, want 611", len(schema.Description))
	}
	params, ok := schema.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no properties: %#v", schema.Parameters)
	}
	action, ok := params["action"].(map[string]any)
	if !ok {
		t.Fatalf("the schema has no action property: %#v", params)
	}
	enum, ok := action["enum"].([]any)
	if !ok || len(enum) != 20 {
		t.Fatalf("the action enum is %#v", action["enum"])
	}
}

func TestBrowserDeclarationMatchesTheTool(t *testing.T) {
	decl := BrowserDeclaration()
	if decl.Name != "webview_browser" {
		t.Fatalf("the name is %q", decl.Name)
	}
	if len(decl.Parameters) == 0 {
		t.Fatal("the declaration carries no parameters")
	}
}

func TestBrowserToolRunsOneAction(t *testing.T) {
	broker := browserBroker(t, `{"ok":true,"tabId":"agent-tab-1","url":"https://example.com"}`)
	tool := BrowserTool(broker, agentbrowser.Options{
		RootConversationID: func() string { return "conv-1" },
		CascadeID:          func() string { return "conv-1" },
	})
	result, err := tool.Execute(context.Background(), "call-1",
		json.RawMessage(`{"action":"click","uid":"e-1"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("the result has %d blocks", len(result.Content))
	}
	text, ok := result.Content[0].(pi.TextContent)
	if !ok {
		t.Fatalf("the block is %T", result.Content[0])
	}
	if !strings.Contains(text.Text, "example.com") {
		t.Fatalf("the text is %q", text.Text)
	}
	if result.IsError {
		t.Fatal("the result is an error")
	}
}

func TestBrowserToolReportsAFailedAction(t *testing.T) {
	broker := agentbrowser.NewBroker()
	sub := broker.Subscribe(context.Background(), agentbrowser.SubscribeRequest{WindowID: "win-1"})
	t.Cleanup(sub.Close)
	go func() {
		req := <-sub.Requests
		_ = broker.Fulfill(&agentbrowser.Response{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "PAGE_NOT_FOUND",
			WindowID:  "win-1",
		})
	}()
	tool := BrowserTool(broker, agentbrowser.Options{})
	result, err := tool.Execute(context.Background(), "call-1",
		json.RawMessage(`{"action":"click","uid":"e-1"}`), nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.IsError {
		t.Fatal("the result is not an error")
	}
}

func TestBrowserToolSendsTheRenderUpdate(t *testing.T) {
	broker := browserBroker(t, `{"ok":true}`)
	tool := BrowserTool(broker, agentbrowser.Options{})
	var updates int
	_, err := tool.Execute(context.Background(), "call-1",
		json.RawMessage(`{"action":"snapshot"}`),
		func(partial piagent.AgentToolResult) { updates++ })
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if updates != 1 {
		t.Fatalf("the tool sent %d updates, want 1", updates)
	}
}
