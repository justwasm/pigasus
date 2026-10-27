package rcfrontend

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/rctunnel/patches"
)

func TestFrontend_IndexHTML(t *testing.T) {
	handler := Handler(Options{
		EnableTouchPatches: true,
	})

	req := httptest.NewRequest("GET", "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}

	body := rr.Body.String()
	if !strings.Contains(body, "<title>Jetski Web</title>") {
		t.Errorf("body missing Jetski Web title")
	}

	// Verify touch patch injection
	if !strings.Contains(body, "visualViewport") {
		t.Errorf("expected visualViewport touch patch in index.html")
	}

	// Test ETag / 304
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Errorf("missing ETag on index.html")
	}

	req2 := httptest.NewRequest("GET", "/", nil)
	req2.Header.Set("If-None-Match", etag)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusNotModified {
		t.Errorf("If-None-Match returned status %d, want 304", rr2.Code)
	}
}

func TestFrontend_MainJS(t *testing.T) {
	handler := Handler(Options{
		EnableTouchPatches: true,
	})

	req := httptest.NewRequest("GET", "/main.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /main.js returned status %d, want 200", rr.Code)
	}

	ct := rr.Header().Get("Content-Type")
	if !strings.HasPrefix(ct, "application/javascript") {
		t.Errorf("Content-Type = %q, want application/javascript", ct)
	}

	if rr.Body.Len() < 1000000 {
		t.Errorf("main.js size = %d, expected > 1MB", rr.Body.Len())
	}
}

func TestFrontend_StaticCSS(t *testing.T) {
	handler := Handler(Options{})

	for _, cssPath := range []string{"/jetbox.css", "/compiled_tailwind.css"} {
		req := httptest.NewRequest("GET", cssPath, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s returned status %d, want 200", cssPath, rr.Code)
		}

		ct := rr.Header().Get("Content-Type")
		if !strings.HasPrefix(ct, "text/css") {
			t.Errorf("Content-Type for %s = %q, want text/css", cssPath, ct)
		}
	}
}

func TestFrontend_SPAFallback(t *testing.T) {
	handler := Handler(Options{EnableTouchPatches: true})

	req := httptest.NewRequest("GET", "/cascade/c-123456", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("SPA route returned status %d, want 200", rr.Code)
	}

	if !strings.Contains(rr.Body.String(), "<title>Jetski Web</title>") {
		t.Errorf("SPA route should fall back to index.html")
	}
}

func TestFrontend_PatchesApplied(t *testing.T) {
	rawHTML, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatal(err)
	}
	rawMainJS, err := fs.ReadFile(FS(), "main.js")
	if err != nil {
		t.Fatal(err)
	}

	opts := patches.Options{
		MobileUX: true,
	}

	_, htmlReport := patches.Apply(patches.HTML, rawHTML, opts)
	_, jsReport := patches.Apply(patches.MainJS, rawMainJS, opts)

	var appliedHTML, appliedJS []string
	for _, r := range htmlReport {
		if r.Status == patches.StatusApplied {
			appliedHTML = append(appliedHTML, r.ID)
		}
	}
	for _, r := range jsReport {
		if r.Status == patches.StatusApplied {
			appliedJS = append(appliedJS, r.ID)
		}
	}

	t.Logf("Applied HTML patches (%d): %v", len(appliedHTML), appliedHTML)
	t.Logf("Applied MainJS patches (%d): %v", len(appliedJS), appliedJS)

	if len(appliedHTML) == 0 {
		t.Error("expected at least 1 HTML patch applied")
	}
	if len(appliedJS) == 0 {
		t.Error("expected at least 1 MainJS patch applied")
	}
}
