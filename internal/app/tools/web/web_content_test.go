// web_content_test.go — the catalogue's two web backends.
//
// Cycle 346: this file used to be stubs_test.go, which held eight tests
// for the v4 stub tools. Seven of them drove tools the catalogue never
// registered (`Status`, `Fetch`, `GrepSearch`, `Grep`, `FileSearch`,
// `Unzip`, `Wait`, `CreateTask`, `SendMessage`, `SkillSearch`); the
// eighth — `read_url_content` — is a live catalogue backend and stayed.
package web

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

func TestNormalizeFetchURL(t *testing.T) {
	tests := []struct {
		raw      string
		wantURL  string
		wantFrag string
	}{
		{
			raw:      `  "https://github.com/google-antigravity/antigravity-cli/blob/main/CHANGELOG.md#1510"  `,
			wantURL:  "https://raw.githubusercontent.com/google-antigravity/antigravity-cli/main/CHANGELOG.md",
			wantFrag: "1510",
		},
		{
			raw:      "(https://example.com/docs/page.html#getting-started)",
			wantURL:  "https://example.com/docs/page.html",
			wantFrag: "getting-started",
		},
		{
			raw:      "https://example.com/plain.txt",
			wantURL:  "https://example.com/plain.txt",
			wantFrag: "",
		},
	}

	for _, tc := range tests {
		gotURL, gotFrag := NormalizeFetchURL(tc.raw)
		if gotURL != tc.wantURL || gotFrag != tc.wantFrag {
			t.Errorf("NormalizeFetchURL(%q) = (%q, %q), want (%q, %q)", tc.raw, gotURL, gotFrag, tc.wantURL, tc.wantFrag)
		}
	}
}

func TestFetchURLText_FragmentExtraction(t *testing.T) {
	md := `# Changelog

## 1.5.11
- Newer item

## 1.5.10
### Added
- Feature A
- Feature B

## 1.5.9
- Older item
`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/markdown")
		_, _ = w.Write([]byte(md))
	}))
	defer ts.Close()

	got, err := FetchURLText(context.Background(), ts.URL+"/CHANGELOG.md#1510", FetchURLOptions{
		ExtractFragment: true,
	})
	if err != nil {
		t.Fatalf("FetchURLText failed: %v", err)
	}
	if !strings.Contains(got, "## 1.5.10") || !strings.Contains(got, "Feature A") || !strings.Contains(got, "Feature B") {
		t.Errorf("expected 1.5.10 section with subsections, got:\n%s", got)
	}
	if strings.Contains(got, "1.5.11") || strings.Contains(got, "1.5.9") {
		t.Errorf("unexpected adjacent sections in output:\n%s", got)
	}
}

func TestFetchURLText_HTMLStrippingAndMaxBytes(t *testing.T) {
	page := `<!DOCTYPE html><html><head><title>Test</title><style>body{color:red}</style></head>
<body><script>alert(1)</script><noscript>enable js</noscript><h1>Heading</h1><p>Hello &amp; welcome to the test page.</p></body></html>`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(page))
	}))
	defer ts.Close()

	got, err := FetchURLText(context.Background(), ts.URL, FetchURLOptions{})
	if err != nil {
		t.Fatalf("FetchURLText failed: %v", err)
	}
	if strings.Contains(got, "alert(1)") || strings.Contains(got, "color:red") || strings.Contains(got, "enable js") {
		t.Errorf("script/style/noscript survived: %q", got)
	}
	if !strings.Contains(got, "Heading") || !strings.Contains(got, "Hello & welcome to the test page.") {
		t.Errorf("expected stripped text, got: %q", got)
	}

	truncated, err := FetchURLText(context.Background(), ts.URL, FetchURLOptions{MaxBytes: 15})
	if err != nil {
		t.Fatalf("FetchURLText with MaxBytes failed: %v", err)
	}
	if len(truncated) > 15 {
		t.Errorf("len(truncated) = %d, want <= 15", len(truncated))
	}
}

func TestFetchURLText_HTTP404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer ts.Close()

	_, err := FetchURLText(context.Background(), ts.URL+"/missing", FetchURLOptions{})
	if err == nil {
		t.Fatal("expected error for HTTP 404, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("expected 404 in error, got: %v", err)
	}
}

func TestExtractHTMLText_StripVsParagraphs(t *testing.T) {
	raw := `<html><head><style>.x{display:none}</style></head><body>
<script>console.log("x")</script>
<noscript>no js</noscript>
<h1>Title</h1>
<p>First &amp; foremost.</p>


<p>Second paragraph.</p>
</body></html>`

	stripped := StripHTML(raw)
	wantStripped := "Title\nFirst & foremost.\nSecond paragraph."
	if stripped != wantStripped {
		t.Errorf("StripHTML =\n%q\nwant\n%q", stripped, wantStripped)
	}

	article := extractHTMLText(raw, true)
	wantArticle := "Title\n\nFirst & foremost.\n\nSecond paragraph.\n"
	if article != wantArticle {
		t.Errorf("extractHTMLText(..., true) =\n%q\nwant\n%q", article, wantArticle)
	}
}

func TestReadURLContent_SharedFetchHeadersAndHTTPError(t *testing.T) {
	var gotUA, gotAccept string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		if r.URL.Path == "/missing" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>Shared</title></head><body><p>Para 1</p><br><br><p>Para 2</p></body></html>`))
	}))
	defer ts.Close()

	home := t.TempDir()
	tool := &ReadURLContent{ConversationID: "conv-shared", Home: home}

	out, err := tool.Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"url":%q}`, ts.URL+"/ok")))
	if err != nil {
		t.Fatalf("ReadURLContent.Run(/ok): %v", err)
	}
	if gotUA == "" || !strings.Contains(gotUA, "Pigasus") {
		t.Errorf("expected shared User-Agent header, got %q", gotUA)
	}
	if !strings.Contains(gotAccept, "text/html") {
		t.Errorf("expected shared Accept header, got %q", gotAccept)
	}
	s, _ := out.(string)
	if !strings.Contains(s, "Title: Shared") {
		t.Errorf("expected Title: Shared in output, got %q", s)
	}

	errOut, err := tool.Run(context.Background(), json.RawMessage(fmt.Sprintf(`{"url":%q}`, ts.URL+"/missing")))
	if err != nil {
		t.Fatalf("ReadURLContent.Run(/missing) returned Go error: %v", err)
	}
	errStr, _ := errOut.(string)
	if !strings.Contains(errStr, "Encountered error in tool execution: HTTP 404 for "+ts.URL+"/missing") {
		t.Errorf("expected HTTP 404 tool execution error, got %q", errStr)
	}
}

func TestFetchURLText_CustomUserAgent(t *testing.T) {
	var gotUA string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	got, err := FetchURLText(context.Background(), ts.URL, FetchURLOptions{UserAgent: "CustomAgent/2.0"})
	if err != nil {
		t.Fatalf("FetchURLText failed: %v", err)
	}
	if got != "ok" {
		t.Errorf("got %q, want %q", got, "ok")
	}
	if gotUA != "CustomAgent/2.0" {
		t.Errorf("User-Agent = %q, want %q", gotUA, "CustomAgent/2.0")
	}
}
