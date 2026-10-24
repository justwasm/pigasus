package browsercdp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/justwasm/pigasus/internal/browsercdp"
)

func TestBrowserCDP_Actions_Mock(t *testing.T) {
	// Create mock CDP server
	mux := http.NewServeMux()
	mux.HandleFunc("/json/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"webSocketDebuggerUrl": "ws://" + r.Host + "/cdp",
		})
	})

	mux.HandleFunc("/cdp", func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close(websocket.StatusNormalClosure, "")

		for {
			_, msg, err := c.Read(r.Context())
			if err != nil {
				return
			}
			var req map[string]any
			if err := json.Unmarshal(msg, &req); err != nil {
				continue
			}

			id := req["id"]
			method, _ := req["method"].(string)

			var result any
			switch method {
			case "Page.navigate":
				result = map[string]any{"frameId": "F1"}
			case "Runtime.evaluate":
				params, _ := req["params"].(map[string]any)
				expr, _ := params["expression"].(string)
				val := "mock value"
				if strings.Contains(expr, "document.title") {
					val = "Test Title"
				} else if strings.Contains(expr, "outerHTML") {
					val = "<html><body>Hello</body></html>"
				} else if strings.Contains(expr, "innerText") {
					val = "Page Text Content"
				}
				result = map[string]any{
					"result": map[string]any{
						"type":  "string",
						"value": val,
					},
				}
			case "Page.captureScreenshot":
				result = map[string]any{
					"data": "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNk+A8AAQUBAScY42YAAAAASUVORK5CYII=", // 1x1 png
				}
			case "Target.getTargets":
				result = map[string]any{
					"targetInfos": []map[string]any{
						{
							"targetId": "T1",
							"type":     "page",
							"title":    "Example",
							"url":      "https://example.com",
						},
					},
				}
			default:
				result = map[string]any{}
			}

			resp := map[string]any{
				"id":     id,
				"result": result,
			}
			respBytes, _ := json.Marshal(resp)
			_ = c.Write(r.Context(), websocket.MessageText, respBytes)
		}
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Parse host:port
	port := strings.TrimPrefix(srv.URL, "http://127.0.0.1:")
	port = strings.TrimPrefix(port, "http://localhost:")
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", port)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := browsercdp.GetClient(ctx)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	defer client.Close()

	actions := browsercdp.NewActions(client)

	// Test OpenURL
	navMsg, err := actions.OpenURL(ctx, "https://example.com")
	if err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	if !strings.Contains(navMsg, "https://example.com") {
		t.Errorf("unexpected navMsg: %s", navMsg)
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
