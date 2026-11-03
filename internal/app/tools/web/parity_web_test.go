package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// stampRE matches the two stamp lines every file-tool result carries.
var stampRE = regexp.MustCompile(`^Created At: \S+\nCompleted At: \S+\n`)

// stripStamp removes the two leading stamp lines of a tool result.
//
// The helper is the web capability's own copy: the contract package's
// copy lives in its own test file, and a test helper cannot be shared
// across packages.
func stripStamp(t *testing.T, out any) string {
	t.Helper()
	s, ok := out.(string)
	if !ok {
		t.Fatalf("result type = %T, want string", out)
	}
	if !stampRE.MatchString(s) {
		t.Fatalf("result %q has no Created At / Completed At header", s)
	}
	return stampRE.ReplaceAllString(s, "")
}

// TestSearchWebWithoutProviderMatchesReferenceResult pins the no-provider
// path. The text it pins is the reference's own "no candidates" error, which
// the reference emits when the grounded GenerateContent answer carries no
// candidate - that is what the 1.4.4 capture recorded, because the mock then
// could not fake a grounded answer. The 1.4.7 capture records a real grounded
// summary (see internal/app/tools/testdata/result_texts_1.4.7.txt), so the clone's unbacked stub
// no longer matches a live reference run. This test claims the no-provider
// path only.
func TestSearchWebWithoutProviderMatchesReferenceResult(t *testing.T) {
	out, err := (&SearchWeb{}).Run(context.Background(), json.RawMessage(`{"query":"golang","domain":"go.dev"}`))
	if err != nil {
		t.Fatalf("search_web: %v", err)
	}
	if got := stripStamp(t, out); got != "Encountered error in tool execution: no candidates returned from GenerateContent" {
		t.Fatalf("search_web body = %q", got)
	}
}

// TestGenerateImageWithoutProviderMatchesReferenceResult pins the
// no-provider path. The text it pins is the reference's own error, which it
// emits when the generateContent answer carries no image - that is what the
// 1.4.4 capture recorded, because the mock then could not fake an image
// response. The 1.4.7 capture records a real generation: the reference calls
// the non-streaming generateContent RPC (requestType "image_gen", model
// "gemini-3.1-flash-image"), the capture harness forwards it to the real
// CloudCode endpoint, and the reference returns the brain path of the .jpg it
// saved (see internal/app/tools/testdata/result_texts_1.4.7.txt). The clone has no image backend
// wired, so its stub still returns the error text. This test claims the
// no-provider path only.
func TestGenerateImageWithoutProviderMatchesReferenceResult(t *testing.T) {
	out, err := (&GenerateImage{}).Run(context.Background(), json.RawMessage(`{"Prompt":"a red cube","ImageName":"cube"}`))
	if err != nil {
		t.Fatalf("generate_image: %v", err)
	}
	if got := stripStamp(t, out); got != "Encountered error in tool execution: no response in generate content result" {
		t.Fatalf("generate_image body = %q", got)
	}
}

func TestReadURLContentSavesArticleAndMatchesShape(t *testing.T) {
	page := `<html><head><title>Live Content</title>
<meta property="og:description" content="Fetched live"></head>
<body><script>ignore()</script><h1>Hello</h1><p>First para.</p><p>Second &amp; last.</p></body></html>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(page))
	}))
	defer srv.Close()

	home := t.TempDir()
	tool := &ReadURLContent{ConversationID: "conv-url", Home: home}
	out, err := tool.Run(context.Background(), json.RawMessage(`{"Url":"`+srv.URL+`/"}`))
	if err != nil {
		t.Fatalf("read_url_content: %v", err)
	}
	body := stripStamp(t, out)
	saved := filepath.Join(home, "brain", "conv-url", ".system_generated", "steps", "1", "content.md")
	want := "Title: Live Content\n\n" +
		"OG Description: Fetched live\n\n" +
		"The full content of the article at " + srv.URL + "/ has been saved to: " + saved + "\n\n" +
		"You can use the view_file tool to read specific sections if needed."
	if body != want {
		t.Fatalf("body =\n%q\nwant\n%q", body, want)
	}
	raw, err := os.ReadFile(saved)
	if err != nil {
		t.Fatalf("saved article: %v", err)
	}
	if strings.Contains(string(raw), "ignore()") {
		t.Errorf("script content survived: %q", raw)
	}
	if !strings.Contains(string(raw), "First para.") || !strings.Contains(string(raw), "Second & last.") {
		t.Errorf("article body = %q", raw)
	}
}
