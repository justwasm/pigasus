package browsercdp_test

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
)

// fakeCDP is a CDP transport that answers from a table instead of a browser.
// It records the traffic, so the test can pin what the actions send.
type fakeCDP struct {
	mu      sync.Mutex
	calls   []string
	params  []map[string]any
	results map[string]json.RawMessage
}

func newFakeCDP() *fakeCDP {
	return &fakeCDP{
		results: map[string]json.RawMessage{
			"Page.navigate": json.RawMessage(`{"frameId":"F1"}`),
			// 1x1 png
			"Page.captureScreenshot": json.RawMessage(`{"data":"iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII="}`),
			"Target.getTargets":      json.RawMessage(`{"targetInfos":[{"targetId":"T1","type":"page","title":"Example","url":"https://example.com"}]}`),
		},
	}
}

func (f *fakeCDP) Call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	f.record(method, params)
	if res, ok := f.results[method]; ok {
		return res, nil
	}
	return json.RawMessage(`{}`), nil
}

func (f *fakeCDP) Evaluate(ctx context.Context, expr string) (any, error) {
	f.record("Runtime.evaluate", map[string]any{"expression": expr})
	switch {
	case strings.Contains(expr, "document.title"):
		return "Test Title", nil
	case strings.Contains(expr, "outerHTML"):
		return "<html><body>Hello</body></html>", nil
	case strings.Contains(expr, "innerText"):
		return "Page Text Content", nil
	default:
		return "mock value", nil
	}
}

func (f *fakeCDP) record(method string, params any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, method)
	m, _ := params.(map[string]any)
	f.params = append(f.params, m)
}

// methods returns the CDP methods the actions sent, in order.
func (f *fakeCDP) methods() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

// param returns the parameters of the nth call.
func (f *fakeCDP) param(n int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if n < 0 || n >= len(f.params) {
		return nil
	}
	return f.params[n]
}

func TestBrowserCDP_Actions_Fake(t *testing.T) {
	fake := newFakeCDP()
	actions := browsercdp.NewActions(fake)
	ctx := context.Background()

	// Test OpenURL
	navMsg, err := actions.OpenURL(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	if !strings.Contains(navMsg, "https://example.com") {
		t.Errorf("unexpected navMsg: %s", navMsg)
	}
	if !strings.Contains(navMsg, "Test Title") {
		t.Errorf("navMsg carries no title: %s", navMsg)
	}

	// Test ReadPage
	text, err := actions.ReadPage(ctx)
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if text != "Page Text Content" {
		t.Errorf("got %q, want %q", text, "Page Text Content")
	}

	// Test GetDOM
	dom, err := actions.GetDOM(ctx)
	if err != nil {
		t.Fatalf("GetDOM failed: %v", err)
	}
	if !strings.Contains(dom, "<html>") {
		t.Errorf("unexpected dom: %s", dom)
	}

	// Test ClickPixel
	clickMsg, err := actions.ClickPixel(ctx, 100, 200, "left")
	if err != nil {
		t.Fatalf("ClickPixel failed: %v", err)
	}
	if !strings.Contains(clickMsg, "(100, 200)") {
		t.Errorf("unexpected clickMsg: %s", clickMsg)
	}

	// Test PressKey
	keyMsg, err := actions.PressKey(ctx, "Enter")
	if err != nil {
		t.Fatalf("PressKey failed: %v", err)
	}
	if !strings.Contains(keyMsg, "Enter") {
		t.Errorf("unexpected keyMsg: %s", keyMsg)
	}

	// Test Scroll
	scrollMsg, err := actions.Scroll(ctx, "down", 300)
	if err != nil {
		t.Fatalf("Scroll failed: %v", err)
	}
	if !strings.Contains(scrollMsg, "down") {
		t.Errorf("unexpected scrollMsg: %s", scrollMsg)
	}

	// Test ResizeWindow
	resizeMsg, err := actions.ResizeWindow(ctx, 1024, 768)
	if err != nil {
		t.Fatalf("ResizeWindow failed: %v", err)
	}
	if !strings.Contains(resizeMsg, "1024x768") {
		t.Errorf("unexpected resizeMsg: %s", resizeMsg)
	}

	// Test Screenshot
	tmpDir := t.TempDir()
	snapMsg, err := actions.CaptureScreenshot(ctx, tmpDir, "png")
	if err != nil {
		t.Fatalf("CaptureScreenshot failed: %v", err)
	}
	if !strings.Contains(snapMsg, "Screenshot saved") {
		t.Errorf("unexpected snapMsg: %s", snapMsg)
	}

	// Test ListPages
	pagesMsg, err := actions.ListPages(ctx)
	if err != nil {
		t.Fatalf("ListPages failed: %v", err)
	}
	if !strings.Contains(pagesMsg, "https://example.com") {
		t.Errorf("unexpected pagesMsg: %s", pagesMsg)
	}
}

// TestBrowserCDP_Actions_Traffic pins the CDP traffic the actions produce.
func TestBrowserCDP_Actions_Traffic(t *testing.T) {
	fake := newFakeCDP()
	actions := browsercdp.NewActions(fake)
	ctx := context.Background()

	if _, err := actions.OpenURL(ctx, "example.com"); err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	if _, err := actions.ClickPixel(ctx, 10, 20, ""); err != nil {
		t.Fatalf("ClickPixel failed: %v", err)
	}
	if _, err := actions.PressKey(ctx, "Enter"); err != nil {
		t.Fatalf("PressKey failed: %v", err)
	}
	if _, err := actions.Scroll(ctx, "up", 0); err != nil {
		t.Fatalf("Scroll failed: %v", err)
	}
	if _, err := actions.RefreshPage(ctx); err != nil {
		t.Fatalf("RefreshPage failed: %v", err)
	}

	want := []string{
		"Page.navigate",
		"Runtime.evaluate",
		"Input.dispatchMouseEvent",
		"Input.dispatchMouseEvent",
		"Input.dispatchMouseEvent",
		"Input.dispatchKeyEvent",
		"Input.dispatchKeyEvent",
		"Input.dispatchMouseEvent",
		"Page.reload",
	}
	got := fake.methods()
	if len(got) != len(want) {
		t.Fatalf("got %d calls %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("call %d: got %q, want %q", i, got[i], want[i])
		}
	}

	// The navigation carries the URL, with the scheme the action adds.
	if url, _ := fake.param(0)["url"].(string); url != "https://example.com" {
		t.Errorf("navigate url: got %q", url)
	}
	// The click is moved, pressed and released.
	for i, wantType := range []string{"mouseMoved", "mousePressed", "mouseReleased"} {
		if got, _ := fake.param(2 + i)["type"].(string); got != wantType {
			t.Errorf("click %d: got %q, want %q", i, got, wantType)
		}
	}
	// The scroll direction is the sign of the wheel delta.
	if dy, _ := fake.param(7)["deltaY"].(int); dy != -300 {
		t.Errorf("scroll deltaY: got %d, want -300", dy)
	}
}
