package browser

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/browsercdp"
	"github.com/justwasm/pigasus/internal/app/tools"
)

func TestBrowserTools_AnswerWithoutPanic(t *testing.T) {
	// The browser tools are real (cycle 311): they drive a Chrome over
	// the DevTools Protocol, and the browser starts on the first tool
	// call — the reference's rule ("Browser starts automatically on first
	// tool call using a persistent Chrome profile", the
	// chrome-devtools-mcp skill the reference ships).
	//
	// A unit test must not start a browser and must not drive the
	// user's own Chrome, so the client is pointed at a closed port, the
	// home directory is a scratch one (the DevToolsActivePort discovery
	// must not find the user's Chrome), and the binary cannot be run.
	// Every tool then fails to reach a browser, and the test pins the
	// contract that matters: the tool answers — a result or an error,
	// never a panic.
	browserTools := []tools.Tool{
		&BrowserClick{}, &BrowserConsole{}, &BrowserDrag{}, &BrowserPressKey{},
		&BrowserScreenshot{}, &BrowserScroll{}, &BrowserSelect{},
	}
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "1")
	t.Setenv("CHROME_BIN", t.TempDir())
	for _, tool := range browserTools {
		out, err := tool.Run(context.Background(), json.RawMessage(`{}`))
		if err == nil && out == nil {
			t.Errorf("%s: the tool returned neither a result nor an error", tool.Name())
		}
		if errors.Is(err, browsercdp.ErrBrowserUnavailable) &&
			!strings.Contains(err.Error(), "CHROME_BIN") {
			t.Errorf("%s: the browser-unavailable error does not name the overrides: %v", tool.Name(), err)
		}
	}
}
