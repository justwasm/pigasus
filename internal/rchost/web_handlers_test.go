package rchost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebHandlers_NewUIHandler(t *testing.T) {
	tmpDir := t.TempDir()
	handler, err := NewUIHandler(WebHandlerOptions{
		Workspace: tmpDir,
		CSRFToken: "test-token-ui",
	})
	if err != nil {
		t.Fatalf("NewUIHandler failed: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/?token=test-token-ui", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from NewUIHandler at root, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "html") && !strings.Contains(body, "DOCTYPE") {
		t.Errorf("expected HTML body from NewUIHandler, got: %s", body)
	}
}

func TestWebHandlers_NewAPIHandler(t *testing.T) {
	tmpDir := t.TempDir()
	handler, cleanup, err := NewAPIHandler(WebHandlerOptions{
		Workspace: tmpDir,
		CSRFToken: "test-token-api",
	})
	if err != nil {
		t.Fatalf("NewAPIHandler failed: %v", err)
	}
	defer cleanup()

	// 1. Root / UI path should return 404 in API-only mode
	req := httptest.NewRequest(http.MethodGet, "/?token=test-token-api", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Errorf("expected 404 for root path in API-only mode, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "API-only mode") {
		t.Errorf("expected API-only message in 404 response, got %s", rr.Body.String())
	}

	// 2. Connect-RPC path should be routed
	rpcPath := "/exa.language_server_pb.LanguageServerService/GetLanguageServerInfo"
	rpcReq := httptest.NewRequest(http.MethodPost, rpcPath, strings.NewReader(`{}`))
	rpcReq.Header.Set("Content-Type", "application/json")
	rpcReq.Header.Set("Authorization", "Bearer test-token-api")
	rpcRR := httptest.NewRecorder()
	handler.ServeHTTP(rpcRR, rpcReq)

	if rpcRR.Code != http.StatusOK {
		t.Errorf("expected 200 for Connect-RPC path, got %d: %s", rpcRR.Code, rpcRR.Body.String())
	}
}

func TestWebHandlers_NewWebHandler(t *testing.T) {
	tmpDir := t.TempDir()
	handler, cleanup, err := NewWebHandler(WebHandlerOptions{
		Workspace: tmpDir,
		CSRFToken: "test-token-web",
	})
	if err != nil {
		t.Fatalf("NewWebHandler failed: %v", err)
	}
	defer cleanup()

	// 1. Root / UI path should serve HTML
	req := httptest.NewRequest(http.MethodGet, "/?token=test-token-web", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 from NewWebHandler for root UI, got %d", rr.Code)
	}

	// 2. Connect-RPC path should also work
	rpcPath := "/exa.language_server_pb.LanguageServerService/GetLanguageServerInfo"
	rpcReq := httptest.NewRequest(http.MethodPost, rpcPath, strings.NewReader(`{}`))
	rpcReq.Header.Set("Content-Type", "application/json")
	rpcReq.Header.Set("Authorization", "Bearer test-token-web")
	rpcRR := httptest.NewRecorder()
	handler.ServeHTTP(rpcRR, rpcReq)

	if rpcRR.Code != http.StatusOK {
		t.Errorf("expected 200 for Connect-RPC path, got %d: %s", rpcRR.Code, rpcRR.Body.String())
	}
}
