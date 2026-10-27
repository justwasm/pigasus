package rchost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/refbackend"
)

// fakeReferenceFrontend is a stand-in for the reference's own frontend:
// its index document (with its own window.__APP_CONFIG__) and one asset.
func fakeReferenceFrontend(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/", "/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><title>Antigravity</title><script>window.__APP_CONFIG__ = {"csrfToken":"ref-csrf-token"};</script></html>`))
		case "/main.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			_, _ = w.Write([]byte("console.log('reference')"))
		default:
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html>fallback</html>"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestReferenceAssetsHandlerServesTheReferencesOwnPage checks that the
// page comes from the reference — its own document, its own bundle.
//
// The document is requested the way the browser asks for it after the
// transport redirect (internal/refbackend/transport.go): without the
// parameter the proxy answers a 302 to the WebSocket transport.
func TestReferenceAssetsHandlerServesTheReferencesOwnPage(t *testing.T) {
	ref := fakeReferenceFrontend(t)
	assets, err := refbackend.NewAssets(ref.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	clone := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("clone"))
	})
	h := newReferenceAssetsHandler(clone, assets)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?useWebSocket=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ref-csrf-token") {
		t.Fatalf("body is not the reference's page: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if !strings.Contains(rec.Body.String(), "console.log('reference')") {
		t.Fatalf("body is not the reference's bundle: %s", rec.Body.String())
	}
}

// TestReferenceAssetsHandlerRedirectsTheDocumentToTheWebSocketTransport
// checks that the document is answered with a redirect to the
// WebSocket transport: the reference's bundle defaults to the fetch
// transport, on which the app's own six long-lived streams take every
// connection Chrome keeps per origin and the send never leaves the
// browser.
func TestReferenceAssetsHandlerRedirectsTheDocumentToTheWebSocketTransport(t *testing.T) {
	ref := fakeReferenceFrontend(t)
	assets, err := refbackend.NewAssets(ref.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	clone := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("clone"))
	})
	h := newReferenceAssetsHandler(clone, assets)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	if loc := rec.Header().Get("Location"); !strings.Contains(loc, "useWebSocket=true") {
		t.Fatalf("location = %q, want the WebSocket transport", loc)
	}
}

// TestReferenceAssetsHandlerKeepsTheClonesEndpoints checks that the
// clone's own endpoints stay with the clone: they belong to the process
// the browser talks to.
func TestReferenceAssetsHandlerKeepsTheClonesEndpoints(t *testing.T) {
	ref := fakeReferenceFrontend(t)
	assets, err := refbackend.NewAssets(ref.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	clone := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("clone"))
	})
	h := newReferenceAssetsHandler(clone, assets)

	for _, path := range []string{"/__agy/api/signin/status", "/__agy/api/debug/log", "/__agy/api/accounts"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Body.String() != "clone" {
			t.Fatalf("%s: body = %q, want the clone's answer", path, rec.Body.String())
		}
	}
}
