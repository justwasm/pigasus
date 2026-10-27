// web_content_test.go — the catalogue's two web backends.
//
// Cycle 346: this file used to be stubs_test.go, which held eight tests
// for the v4 stub tools. Seven of them drove tools the catalogue never
// registered (`Status`, `Fetch`, `GrepSearch`, `Grep`, `FileSearch`,
// `Unzip`, `Wait`, `CreateTask`, `SendMessage`, `SkillSearch`); the
// eighth — `read_url_content` — is a live catalogue backend and stayed.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// TestReadURLContentSavesArticle — ReadURLContent via httptest.NewServer.
// We do NOT hit example.com directly so this test works in sandboxed /
// offline CI. The reference saves the article and returns Title /
// OG Description + path.
func TestReadURLContentSavesArticle(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html><head><title>Example Domain</title></head><body><p>body content for testing.</p></body></html>"))
	}))
	defer ts.Close()

	r := &ReadURLContent{}
	out, err := r.Run(context.Background(),
		json.RawMessage(fmt.Sprintf(`{"url":%q,"max_bytes":100}`, ts.URL)))
	if err != nil {
		t.Fatalf("ReadURLContent.Run: %v", err)
	}
	s, ok := out.(string)
	if !ok {
		t.Fatalf("expected string, got %T", out)
	}
	if !strings.Contains(s, "Title: Example Domain") {
		t.Errorf("expected the reference title line, got %q", s)
	}
	if !strings.Contains(s, "has been saved to: ") {
		t.Errorf("expected the saved-content line, got %q", s)
	}
	// max_bytes caps the saved article, not the result text.
	saved := s[strings.Index(s, "has been saved to: ")+len("has been saved to: "):]
	saved = strings.TrimSpace(saved[:strings.Index(saved, "\n")])
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("read saved article: %v", err)
	}
	if len(raw) > 100 {
		t.Errorf("article len = %d, want ≤ 100", len(raw))
	}
}
