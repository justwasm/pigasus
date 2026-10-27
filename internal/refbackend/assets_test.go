package refbackend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeAssets is a stand-in for the reference's frontend: it serves the
// index document, one asset with an ETag, and counts the fetches so the
// test can tell a cached answer from a fetched one.
func fakeAssets(t *testing.T) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		switch r.URL.Path {
		case "/", "/index.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<html><script>window.__APP_CONFIG__={"csrfToken":"ref-csrf-token"};</script></html>`))
		case "/main.js":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
			w.Header().Set("ETag", `W/"9fa115-bb169a04402c03c6"`)
			_, _ = w.Write([]byte("console.log(1)"))
		default:
			// The reference's SPA fallback: an unknown path answers
			// with the index document.
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html>fallback</html>"))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// TestAssetsServesTheReferencesOwnPage checks that the proxy answers
// with the reference's own index document — the one that carries the
// reference's own window.__APP_CONFIG__.
//
// The document is requested the way the browser asks for it after the
// transport redirect (internal/refbackend/transport.go): the redirect
// itself is covered by the tests next door.
func TestAssetsServesTheReferencesOwnPage(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?useWebSocket=true", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ref-csrf-token") {
		t.Fatalf("body is not the reference's page: %s", rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "text/html") {
		t.Fatalf("content type = %q, want text/html", ct)
	}
}

// TestAssetsCachesTheBundleAndAnswers304 checks that the bundle is
// fetched once, that the reference's ETag is passed through, and that a
// conditional request answers 304 without a body.
func TestAssetsCachesTheBundleAndAnswers304(t *testing.T) {
	srv, hits := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("ETag"); got != `W/"9fa115-bb169a04402c03c6"` {
		t.Fatalf("etag = %q, want the reference's", got)
	}
	rec2 := httptest.NewRecorder()
	assets.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if hits.Load() != 1 {
		t.Fatalf("reference hits = %d, want 1 (the bundle is cached)", hits.Load())
	}
	req := httptest.NewRequest(http.MethodGet, "/main.js", nil)
	req.Header.Set("If-None-Match", `W/"9fa115-bb169a04402c03c6"`)
	rec3 := httptest.NewRecorder()
	assets.ServeHTTP(rec3, req)
	if rec3.Code != http.StatusNotModified {
		t.Fatalf("conditional status = %d, want 304", rec3.Code)
	}
	if rec3.Body.Len() != 0 {
		t.Fatalf("conditional body = %q, want empty", rec3.Body.String())
	}
}

// TestAssetsDoesNotCacheTheDocument checks that the HTML document is
// fetched every time: it carries the reference's live csrf token.
func TestAssetsDoesNotCacheTheDocument(t *testing.T) {
	srv, hits := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	for i := 0; i < 2; i++ {
		rec := httptest.NewRecorder()
		assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	}
	if hits.Load() != 2 {
		t.Fatalf("reference hits = %d, want 2 (the document is not cached)", hits.Load())
	}
}

// TestAssetsRefusesOtherMethods checks that only GET and HEAD are
// served: an asset is read-only.
func TestAssetsRefusesOtherMethods(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/main.js", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

// TestAssetsAnswers502WhenTheReferenceIsDown checks that a reference
// that cannot be reached is a bad gateway, not a panic.
func TestAssetsAnswers502WhenTheReferenceIsDown(t *testing.T) {
	assets, err := NewAssets("http://127.0.0.1:1")
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}
