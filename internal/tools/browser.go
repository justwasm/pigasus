// Browser tools (CDP stubs). Source: google3/third_party/jetski/agent/tools/browser.
// Seven tools recovered in 1.3.3:
//
//   - browser_click
//   - browser_console
//   - browser_drag
//   - browser_press_key
//   - browser_screenshot
//   - browser_scroll
//   - browser_select
//
// The recovered 1.3.3 build does NOT bundle a Chrome instance, so
// these are placeholders. They log a structured message to stderr and
// return ErrNotImplemented. A real build would route them through the
// Chrome DevTools Protocol over a websocket.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
)

// browserStubBase carries the workspace and emits the same placeholder
// message every browser_* tool uses.
type browserStubBase struct {
	Workspace string
}

// browserUnavailable is the canonical "browser tools require Chrome
// DevTools Protocol" error returned by every browser_* tool.
var browserUnavailable = fmt.Errorf("browser tools require Chrome DevTools Protocol; not available in recovered build")

func (b *browserStubBase) reject(name string) (any, error) {
	fmt.Fprintf(os.Stderr, "[%s] %v\n", name, browserUnavailable)
	return nil, browserUnavailable
}

// BrowserClick — Source: tools.browser.browser_click. CDP Input.dispatchMouseEvent.
type BrowserClick struct{ Workspace string }

func (b *BrowserClick) Name() string { return "browser_click" }
func (b *BrowserClick) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"}},"required":["selector"]}`)
}
func (b *BrowserClick) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_click")
}

// BrowserConsole — Source: tools.browser.browser_console. Read console.log entries.
type BrowserConsole struct{ Workspace string }

func (b *BrowserConsole) Name() string            { return "browser_console" }
func (b *BrowserConsole) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer"}}}`) }
func (b *BrowserConsole) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_console")
}

// BrowserDrag — Source: tools.browser.browser_drag. CDP Input.dispatchMouseEvent (mouseMoved).
type BrowserDrag struct{ Workspace string }

func (b *BrowserDrag) Name() string { return "browser_drag" }
func (b *BrowserDrag) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"from":{"type":"string"},"to":{"type":"string"}},"required":["from","to"]}`)
}
func (b *BrowserDrag) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_drag")
}

// BrowserPressKey — Source: tools.browser.browser_press_key. CDP Input.dispatchKeyEvent.
type BrowserPressKey struct{ Workspace string }

func (b *BrowserPressKey) Name() string { return "browser_press_key" }
func (b *BrowserPressKey) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"key":{"type":"string"}},"required":["key"]}`)
}
func (b *BrowserPressKey) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_press_key")
}

// BrowserScreenshot — Source: tools.browser.browser_screenshot. CDP Page.captureScreenshot.
type BrowserScreenshot struct{ Workspace string }

func (b *BrowserScreenshot) Name() string            { return "browser_screenshot" }
func (b *BrowserScreenshot) Schema() json.RawMessage { return json.RawMessage(`{"type":"object","properties":{"format":{"type":"string","enum":["png","jpeg"]}}}`) }
func (b *BrowserScreenshot) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_screenshot")
}

// BrowserScroll — Source: tools.browser.browser_scroll. CDP Input.dispatchMouseEvent (mouseWheel).
type BrowserScroll struct{ Workspace string }

func (b *BrowserScroll) Name() string { return "browser_scroll" }
func (b *BrowserScroll) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"direction":{"type":"string","enum":["up","down"]},"amount":{"type":"integer"}}}`)
}
func (b *BrowserScroll) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_scroll")
}

// BrowserSelect — Source: tools.browser.browser_select. CDP Runtime.evaluate (form interaction).
type BrowserSelect struct{ Workspace string }

func (b *BrowserSelect) Name() string { return "browser_select" }
func (b *BrowserSelect) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"selector":{"type":"string"},"value":{"type":"string"}},"required":["selector","value"]}`)
}
func (b *BrowserSelect) Run(ctx context.Context, args json.RawMessage) (any, error) {
	return (&browserStubBase{Workspace: b.Workspace}).reject("browser_select")
}
