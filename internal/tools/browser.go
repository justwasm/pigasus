package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/justwasm/pigasus/internal/browsercdp"
)

// browserStubBase provides fallback and common helper methods for browser tools.
type browserStubBase struct {
	Workspace string
}

var browserUnavailable = browsercdp.ErrBrowserUnavailable

func (b *browserStubBase) reject(name string) (any, error) {
	fmt.Fprintf(os.Stderr, "[%s] %v\n", name, browserUnavailable)
	return nil, browserUnavailable
}

func getActions(ctx context.Context) (*browsercdp.Actions, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return nil, err
	}
	return browsercdp.NewActions(client), nil
}

// BrowserClick — CDP Input.dispatchMouseEvent / DOM click.
type BrowserClick struct{ Workspace string }

func (b *BrowserClick) Name() string { return "browser_click_element" }
func (b *BrowserClick) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"}},"required":["selector"]}`)
}
func (b *BrowserClick) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_click_element")
	}
	var p struct {
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.ClickElement(ctx, p.Selector)
}

// ClickBrowserPixel — Click a pixel coordinate.
type ClickBrowserPixel struct{ Workspace string }

func (b *ClickBrowserPixel) Name() string { return "click_browser_pixel" }
func (b *ClickBrowserPixel) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"},"button":{"type":"string"}},"required":["x","y"]}`)
}
func (b *ClickBrowserPixel) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("click_browser_pixel")
	}
	var p struct {
		X      int    `json:"x"`
		Y      int    `json:"y"`
		Button string `json:"button"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.ClickPixel(ctx, p.X, p.Y, p.Button)
}

// BrowserConsole — Read console.log entries.
type BrowserConsole struct{ Workspace string }

func (b *BrowserConsole) Name() string { return "capture_browser_console_logs" }
func (b *BrowserConsole) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`)
}
func (b *BrowserConsole) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("capture_browser_console_logs")
	}
	var p struct {
		Limit int `json:"limit"`
	}
	_ = json.Unmarshal(args, &p)
	logs := client.GetConsoleLogs(p.Limit)
	res, _ := json.MarshalIndent(logs, "", "  ")
	return string(res), nil
}

// BrowserDrag — CDP Input.dispatchMouseEvent drag.
type BrowserDrag struct{ Workspace string }

func (b *BrowserDrag) Name() string { return "browser_drag_pixel_to_pixel" }
func (b *BrowserDrag) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x1":{"type":"integer"},"y1":{"type":"integer"},"x2":{"type":"integer"},"y2":{"type":"integer"}},"required":["x1","y1","x2","y2"]}`)
}
func (b *BrowserDrag) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_drag_pixel_to_pixel")
	}
	var p struct {
		X1 int `json:"x1"`
		Y1 int `json:"y1"`
		X2 int `json:"x2"`
		Y2 int `json:"y2"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.DragPixelToPixel(ctx, p.X1, p.Y1, p.X2, p.Y2)
}

// BrowserPressKey — CDP Input.dispatchKeyEvent.
type BrowserPressKey struct{ Workspace string }

func (b *BrowserPressKey) Name() string { return "browser_press_key" }
func (b *BrowserPressKey) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`)
}
func (b *BrowserPressKey) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_press_key")
	}
	var p struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.PressKey(ctx, p.Key)
}

// BrowserScreenshot — CDP Page.captureScreenshot.
type BrowserScreenshot struct{ Workspace string }

func (b *BrowserScreenshot) Name() string { return "capture_browser_screenshot" }
func (b *BrowserScreenshot) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"format":{"type":"string","enum":["png","jpeg"]}}}`)
}
func (b *BrowserScreenshot) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("capture_browser_screenshot")
	}
	var p struct {
		Format string `json:"format"`
	}
	_ = json.Unmarshal(args, &p)
	return actions.CaptureScreenshot(ctx, b.Workspace, p.Format)
}

// BrowserScroll — CDP Input.dispatchMouseEvent (mouseWheel).
type BrowserScroll struct{ Workspace string }

func (b *BrowserScroll) Name() string { return "browser_scroll" }
func (b *BrowserScroll) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"direction":{"type":"string","enum":["up","down"]},"amount":{"type":"integer"}}}`)
}
func (b *BrowserScroll) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_scroll")
	}
	var p struct {
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
	}
	_ = json.Unmarshal(args, &p)
	return actions.Scroll(ctx, p.Direction, p.Amount)
}

// BrowserScrollDOM — Scroll an element of the browser DOM.
type BrowserScrollDOM struct{ Workspace string }

func (b *BrowserScrollDOM) Name() string { return "browser_scroll_dom" }
func (b *BrowserScrollDOM) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"},"direction":{"type":"string","enum":["up","down"]},"amount":{"type":"integer"}},"required":["selector"]}`)
}
func (b *BrowserScrollDOM) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_scroll_dom")
	}
	var p struct {
		Selector  string `json:"selector"`
		Direction string `json:"direction"`
		Amount    int    `json:"amount"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.ScrollDOM(ctx, p.Selector, p.Direction, p.Amount)
}

// BrowserSelect — Dropdown select.
type BrowserSelect struct{ Workspace string }

func (b *BrowserSelect) Name() string { return "browser_select_option" }
func (b *BrowserSelect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"},"value":{"type":"string"}},"required":["selector","value"]}`)
}
func (b *BrowserSelect) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_select_option")
	}
	var p struct {
		Selector string `json:"selector"`
		Value    string `json:"value"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.SelectOption(ctx, p.Selector, p.Value)
}

// OpenBrowserURL — Open a URL in the browser.
type OpenBrowserURL struct{ Workspace string }

func (b *OpenBrowserURL) Name() string { return "open_browser_url" }
func (b *OpenBrowserURL) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}`)
}
func (b *OpenBrowserURL) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("open_browser_url")
	}
	var p struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.OpenURL(ctx, p.URL)
}

// ReadBrowserPage — Read the current browser page as plain text.
type ReadBrowserPage struct{ Workspace string }

func (b *ReadBrowserPage) Name() string { return "read_browser_page" }
func (b *ReadBrowserPage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (b *ReadBrowserPage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("read_browser_page")
	}
	return actions.ReadPage(ctx)
}

// BrowserGetDOM — Return the serialized DOM of the browser page.
type BrowserGetDOM struct{ Workspace string }

func (b *BrowserGetDOM) Name() string { return "browser_get_dom" }
func (b *BrowserGetDOM) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (b *BrowserGetDOM) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_get_dom")
	}
	return actions.GetDOM(ctx)
}

// BrowserInput — Type text into a focused field or element.
type BrowserInput struct{ Workspace string }

func (b *BrowserInput) Name() string { return "browser_input" }
func (b *BrowserInput) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"text":{"type":"string"},"selector":{"type":"string"}},"required":["text"]}`)
}
func (b *BrowserInput) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_input")
	}
	var p struct {
		Text     string `json:"text"`
		Selector string `json:"selector"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.InputText(ctx, p.Text, p.Selector)
}

// ExecuteBrowserJavaScript — Evaluate JavaScript in the browser.
type ExecuteBrowserJavaScript struct{ Workspace string }

func (b *ExecuteBrowserJavaScript) Name() string { return "execute_browser_javascript" }
func (b *ExecuteBrowserJavaScript) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"script":{"type":"string"}},"required":["script"]}`)
}
func (b *ExecuteBrowserJavaScript) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("execute_browser_javascript")
	}
	var p struct {
		Script string `json:"script"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	res, err := client.Evaluate(ctx, p.Script)
	if err != nil {
		return nil, err
	}
	out, _ := json.Marshal(res)
	return string(out), nil
}

// BrowserRefreshPage — Reload the browser page.
type BrowserRefreshPage struct{ Workspace string }

func (b *BrowserRefreshPage) Name() string { return "browser_refresh_page" }
func (b *BrowserRefreshPage) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (b *BrowserRefreshPage) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_refresh_page")
	}
	return actions.RefreshPage(ctx)
}

// BrowserResizeWindow — Resize the viewport.
type BrowserResizeWindow struct{ Workspace string }

func (b *BrowserResizeWindow) Name() string { return "browser_resize_window" }
func (b *BrowserResizeWindow) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}},"required":["width","height"]}`)
}
func (b *BrowserResizeWindow) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_resize_window")
	}
	var p struct {
		Width  int `json:"width"`
		Height int `json:"height"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	return actions.ResizeWindow(ctx, p.Width, p.Height)
}

// ListBrowserPages — List active browser pages.
type ListBrowserPages struct{ Workspace string }

func (b *ListBrowserPages) Name() string { return "list_browser_pages" }
func (b *ListBrowserPages) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (b *ListBrowserPages) Run(ctx context.Context, args json.RawMessage) (any, error) {
	actions, err := getActions(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("list_browser_pages")
	}
	return actions.ListPages(ctx)
}

// BrowserListNetworkRequests — List recorded network requests.
type BrowserListNetworkRequests struct{ Workspace string }

func (b *BrowserListNetworkRequests) Name() string { return "browser_list_network_requests" }
func (b *BrowserListNetworkRequests) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{}}`)
}
func (b *BrowserListNetworkRequests) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_list_network_requests")
	}
	reqs := client.GetNetworkRequests()
	out, _ := json.MarshalIndent(reqs, "", "  ")
	return string(out), nil
}

// BrowserGetNetworkRequest — Inspect one captured network request.
type BrowserGetNetworkRequest struct{ Workspace string }

func (b *BrowserGetNetworkRequest) Name() string { return "browser_get_network_request" }
func (b *BrowserGetNetworkRequest) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"requestId":{"type":"string"}},"required":["requestId"]}`)
}
func (b *BrowserGetNetworkRequest) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_get_network_request")
	}
	var p struct {
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	for _, r := range client.GetNetworkRequests() {
		if r.RequestID == p.RequestID {
			out, _ := json.MarshalIndent(r, "", "  ")
			return string(out), nil
		}
	}
	return nil, fmt.Errorf("network request %q not found", p.RequestID)
}

// BrowserMouseMove — Move pointer.
type BrowserMouseMove struct{ Workspace string }

func (b *BrowserMouseMove) Name() string { return "browser_move_mouse" }
func (b *BrowserMouseMove) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"x":{"type":"integer"},"y":{"type":"integer"}},"required":["x","y"]}`)
}
func (b *BrowserMouseMove) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_move_mouse")
	}
	var p struct {
		X int `json:"x"`
		Y int `json:"y"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, err
	}
	_, err = client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type": "mouseMoved",
		"x":    p.X,
		"y":    p.Y,
	})
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("Moved pointer to (%d, %d)", p.X, p.Y), nil
}

// BrowserMouseDown — Press mouse button.
type BrowserMouseDown struct{ Workspace string }

func (b *BrowserMouseDown) Name() string { return "browser_mouse_down" }
func (b *BrowserMouseDown) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"button":{"type":"string"}}}`)
}
func (b *BrowserMouseDown) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_mouse_down")
	}
	var p struct {
		Button string `json:"button"`
	}
	_ = json.Unmarshal(args, &p)
	btn := p.Button
	if btn == "" {
		btn = "left"
	}
	_, err = client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":       "mousePressed",
		"button":     btn,
		"clickCount": 1,
	})
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("Mouse button %q pressed", btn), nil
}

// BrowserMouseUp — Release mouse button.
type BrowserMouseUp struct{ Workspace string }

func (b *BrowserMouseUp) Name() string { return "browser_mouse_up" }
func (b *BrowserMouseUp) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"button":{"type":"string"}}}`)
}
func (b *BrowserMouseUp) Run(ctx context.Context, args json.RawMessage) (any, error) {
	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		return (&browserStubBase{Workspace: b.Workspace}).reject("browser_mouse_up")
	}
	var p struct {
		Button string `json:"button"`
	}
	_ = json.Unmarshal(args, &p)
	btn := p.Button
	if btn == "" {
		btn = "left"
	}
	_, err = client.Call(ctx, "Input.dispatchMouseEvent", map[string]any{
		"type":   "mouseReleased",
		"button": btn,
	})
	if err != nil {
		return nil, err
	}
	return fmt.Sprintf("Mouse button %q released", btn), nil
}
