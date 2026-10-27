package refbackend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAssetsRedirectsTheDocumentToTheWebSocketTransport checks that the
// document is answered with a redirect to the WebSocket transport: the
// reference's bundle picks the transport from the URL, and on the fetch
// transport the app's own six long-lived streams take every connection
// Chrome keeps per origin, so the send never leaves the browser.
func TestAssetsRedirectsTheDocumentToTheWebSocketTransport(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302", rec.Code)
	}
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "useWebSocket=true") {
		t.Fatalf("location = %q, want the WebSocket transport", location)
	}
}

// TestAssetsKeepsTheSecretOnTheRedirect checks that the secret the
// operator shares with the LAN clients survives the redirect: it
// travels in the query string.
func TestAssetsKeepsTheSecretOnTheRedirect(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?token=s3cret", nil))
	location := rec.Header().Get("Location")
	if !strings.Contains(location, "token=s3cret") {
		t.Fatalf("location = %q, want the secret carried over", location)
	}
	if !strings.Contains(location, "useWebSocket=true") {
		t.Fatalf("location = %q, want the WebSocket transport", location)
	}
}

// TestAssetsKeepsThePathOnTheRedirect checks that a document requested
// under another path (the reference's SPA fallback) is redirected to
// the same path — the app routes on it.
func TestAssetsKeepsThePathOnTheRedirect(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/conversation/42", nil))
	location := rec.Header().Get("Location")
	if !strings.HasPrefix(location, "/conversation/42?") {
		t.Fatalf("location = %q, want the same path", location)
	}
}

// TestAssetsHonoursAnExplicitTransport checks that the operator's own
// choice wins: `useWebSocket=false` is the escape hatch back to the
// fetch transport, and the document is then served as it is.
func TestAssetsHonoursAnExplicitTransport(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/?useWebSocket=false", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ref-csrf-token") {
		t.Fatalf("body is not the reference's page: %s", rec.Body.String())
	}
}

// TestAssetsDoesNotRedirectTheBundle checks that the assets are served
// as they are: only the document carries the transport.
func TestAssetsDoesNotRedirectTheBundle(t *testing.T) {
	srv, _ := fakeAssets(t)
	assets, err := NewAssets(srv.URL)
	if err != nil {
		t.Fatalf("NewAssets: %v", err)
	}
	rec := httptest.NewRecorder()
	assets.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if loc := rec.Header().Get("Location"); loc != "" {
		t.Fatalf("location = %q, want no redirect", loc)
	}
}
