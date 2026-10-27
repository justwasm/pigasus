package agentbrowser

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// answeringBroker returns a broker with one window that answers every
// request with the given payload.
func answeringBroker(t *testing.T, payload string) *Broker {
	t.Helper()
	b := NewBroker()
	b.timeout = 2 * time.Second
	sub := b.Subscribe(context.Background(), SubscribeRequest{
		WindowID:                    "win-1",
		DisplayedRootConversationID: "conv-1",
	})
	t.Cleanup(sub.Close)
	go func() {
		for req := range sub.Requests {
			_ = b.Fulfill(&Response{
				RequestID:  req.RequestID,
				OK:         true,
				ResultJSON: payload,
				WindowID:   "win-1",
			})
		}
	}()
	return b
}

func TestToolRunsOneAction(t *testing.T) {
	b := answeringBroker(t, `{"ok":true,"tabId":"agent-tab-1","url":"https://example.com"}`)
	tool := NewTool(b, Options{
		RootConversationID: func() string { return "conv-1" },
		CascadeID:          func() string { return "conv-1" },
	})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"click","uid":"e-1"}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK {
		t.Fatalf("the result is not ok: %+v", result)
	}
	if !strings.Contains(result.Text, "example.com") {
		t.Fatalf("the result is %q", result.Text)
	}
}

func TestToolRefusesAnUnknownAction(t *testing.T) {
	b := answeringBroker(t, `{"ok":true}`)
	tool := NewTool(b, Options{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"fly"}`))
	if err == nil || err.Error() != "unsupported webview_browser action \"fly\"" {
		t.Fatalf("the error is %v", err)
	}
}

func TestToolNeedsAnAction(t *testing.T) {
	b := answeringBroker(t, `{"ok":true}`)
	tool := NewTool(b, Options{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{}`))
	if err == nil || err.Error() != "webview_browser requires a non-empty 'action'" {
		t.Fatalf("the error is %v", err)
	}
}

func TestToolRefusesAnEmptyBatch(t *testing.T) {
	b := answeringBroker(t, `[]`)
	tool := NewTool(b, Options{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"batch"}`))
	if err == nil || err.Error() != "webview_browser action 'batch' requires a non-empty 'steps' array" {
		t.Fatalf("the error is %v", err)
	}
}

func TestToolRefusesABadBatchStep(t *testing.T) {
	b := answeringBroker(t, `[]`)
	tool := NewTool(b, Options{})
	_, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"batch","steps":[{"action":"fly"}]}`))
	if err == nil || err.Error() != "batch step 0 has invalid action \"fly\"" {
		t.Fatalf("the error is %v", err)
	}
	_, err = tool.Execute(context.Background(), json.RawMessage(`{"action":"batch","steps":[{"action":"click"}]}`))
	if err == nil || !strings.HasPrefix(err.Error(), "batch step 0 (click) invalid: ") {
		t.Fatalf("the error is %v", err)
	}
}

func TestToolRunsABatch(t *testing.T) {
	b := answeringBroker(t, `[{"ok":true},{"ok":true}]`)
	tool := NewTool(b, Options{})
	result, err := tool.Execute(context.Background(), json.RawMessage(
		`{"action":"batch","steps":[{"action":"click","uid":"e-1"},{"action":"snapshot"}]}`))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.OK {
		t.Fatalf("the result is not ok: %+v", result)
	}
}

func TestToolReportsAFailedAction(t *testing.T) {
	b := NewBroker()
	b.timeout = 2 * time.Second
	sub := b.Subscribe(context.Background(), SubscribeRequest{WindowID: "win-1"})
	t.Cleanup(sub.Close)
	go func() {
		req := <-sub.Requests
		_ = b.Fulfill(&Response{
			RequestID: req.RequestID,
			OK:        false,
			Error:     "PAGE_NOT_FOUND",
			ErrorCode: "PAGE_NOT_FOUND",
			WindowID:  "win-1",
		})
	}()
	tool := NewTool(b, Options{})
	result, err := tool.Execute(context.Background(), json.RawMessage(`{"action":"click","uid":"e-1"}`))
	if err == nil || err.Error() != "webview_browser action failed" {
		t.Fatalf("the error is %v", err)
	}
	if result.OK {
		t.Fatal("the result is ok")
	}
	if result.ErrorCode != "PAGE_NOT_FOUND" {
		t.Fatalf("the error code is %q", result.ErrorCode)
	}
}
