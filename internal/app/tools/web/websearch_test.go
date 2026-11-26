package web

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func withSearchBaseURLs(t *testing.T, ddgHTML, ddgLite, bing string) {
	t.Helper()
	origHTML := duckDuckGoSearchBaseURL
	origLite := duckDuckGoLiteBaseURL
	origBing := bingSearchBaseURL
	duckDuckGoSearchBaseURL = ddgHTML
	duckDuckGoLiteBaseURL = ddgLite
	bingSearchBaseURL = bing
	t.Cleanup(func() {
		duckDuckGoSearchBaseURL = origHTML
		duckDuckGoLiteBaseURL = origLite
		bingSearchBaseURL = origBing
	})
}

func TestFormatSearchWebResult(t *testing.T) {
	got := formatSearchWebResult("golang", "Go is an open source programming language.")
	wantPrefix := "The search for \"golang\" returned the following summary:\nGo is an open source programming language."
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("formatSearchWebResult prefix mismatch:\ngot:  %q\nwant: %q", got, wantPrefix)
	}
	wantSuffix := "\n\nNote: Source URLs are HTTP redirects; check where they resolve if you need the direct link."
	if !strings.HasSuffix(got, wantSuffix) {
		t.Fatalf("formatSearchWebResult suffix mismatch:\ngot:  %q\nwant: %q", got, wantSuffix)
	}
}

func TestPerformWebSearch_ValidHTML(t *testing.T) {
	var gotQuery string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body>
  <div class="result">
    <a class="result__url" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">go.dev/doc</a>
    <a class="result__snippet" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2Fdoc%2F">Official <b>Go</b> documentation &amp; guides.</a>
  </div>
</body></html>`))
	}))
	defer ts.Close()

	orig := duckDuckGoSearchBaseURL
	duckDuckGoSearchBaseURL = ts.URL
	defer func() { duckDuckGoSearchBaseURL = orig }()

	got, err := PerformWebSearch(context.Background(), "golang docs", "go.dev")
	if err != nil {
		t.Fatalf("PerformWebSearch failed: %v", err)
	}
	if gotQuery != "golang docs site:go.dev" {
		t.Errorf("query = %q, want %q", gotQuery, "golang docs site:go.dev")
	}
	if !strings.Contains(got, "1. go.dev/doc") {
		t.Errorf("expected title in output, got:\n%s", got)
	}
	if !strings.Contains(got, "URL: https://go.dev/doc/") {
		t.Errorf("expected unwrapped uddg URL in output, got:\n%s", got)
	}
	if !strings.Contains(got, "Official Go documentation & guides.") {
		t.Errorf("expected unescaped snippet in output, got:\n%s", got)
	}
}

func TestPerformWebSearch_EmptyResultsError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div class="no-results">No results found.</div></body></html>`))
	}))
	defer ts.Close()

	withSearchBaseURLs(t, ts.URL, ts.URL, ts.URL)

	_, err := PerformWebSearch(context.Background(), "nonexistentxyz123", "")
	if err == nil {
		t.Fatal("expected error for empty results, got nil")
	}
	if strings.Contains(err.Error(), "GenerateContent") {
		t.Fatalf("error must not mention GenerateContent, got: %v", err)
	}
	if !strings.Contains(err.Error(), "no results") {
		t.Fatalf("expected diagnostic no-results error, got: %v", err)
	}
}

func TestPerformWebSearch_ChallengeError(t *testing.T) {
	cases := []struct {
		name   string
		status int
		body   string
	}{
		{
			name:   "anomaly-modal 202",
			status: http.StatusAccepted,
			body:   `<html><body><div class="anomaly-modal__title">Unfortunately, bots use DuckDuckGo too.</div></body></html>`,
		},
		{
			name:   "captcha 200",
			status: http.StatusOK,
			body:   `<html><body><form id="captcha">Please solve the challenge</form></body></html>`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer ts.Close()

			withSearchBaseURLs(t, ts.URL, ts.URL, ts.URL)

			_, err := PerformWebSearch(context.Background(), "golang", "")
			if err == nil {
				t.Fatal("expected error for challenge response, got nil")
			}
			if strings.Contains(err.Error(), "GenerateContent") {
				t.Fatalf("error must not mention GenerateContent, got: %v", err)
			}
			if !strings.Contains(err.Error(), "blocked or challenged") {
				t.Fatalf("expected blocked/challenged diagnostic error, got: %v", err)
			}
		})
	}
}

func TestPerformWebSearch_FallbackToDDGLite(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`<html><body><div class="anomaly-modal">Bot check</div></body></html>`))
	}))
	defer primary.Close()

	lite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body><table>
  <tr>
    <td>1.&nbsp;</td>
    <td><a rel="nofollow" href="//duckduckgo.com/l/?uddg=https%3A%2F%2Fgo.dev%2F" class='result-link'>The Go Programming Language</a></td>
  </tr>
  <tr>
    <td>&nbsp;</td>
    <td class='result-snippet'>Build simple, <b>secure</b> &amp; scalable systems.</td>
  </tr>
</table></body></html>`))
	}))
	defer lite.Close()

	bingCalled := false
	bing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bingCalled = true
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bing.Close()

	withSearchBaseURLs(t, primary.URL, lite.URL, bing.URL)

	got, err := PerformWebSearch(context.Background(), "golang", "")
	if err != nil {
		t.Fatalf("PerformWebSearch failed: %v", err)
	}
	if bingCalled {
		t.Errorf("expected bing fallback not to be called when duckduckgo-lite succeeds")
	}
	if !strings.HasPrefix(got, `The search for "golang" returned the following summary:`+"\n") {
		t.Errorf("unexpected prefix:\n%s", got)
	}
	if !strings.Contains(got, "1. The Go Programming Language\n   URL: https://go.dev/\n   Build simple, secure & scalable systems.") {
		t.Errorf("unexpected DDG Lite formatted result:\n%s", got)
	}
	if !strings.HasSuffix(got, "\n\nNote: Source URLs are HTTP redirects; check where they resolve if you need the direct link.") {
		t.Errorf("unexpected suffix:\n%s", got)
	}
}

func TestPerformWebSearch_FallbackToDDGLiteTableRows(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()

	lite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body><table>
  <tr><td><a rel="nofollow" href="https://pkg.go.dev/net/http">net/http package</a></td></tr>
  <tr><td>Package http provides HTTP client and server implementations.</td></tr>
</table></body></html>`))
	}))
	defer lite.Close()

	withSearchBaseURLs(t, primary.URL, lite.URL, primary.URL)

	got, err := PerformWebSearch(context.Background(), "net/http", "")
	if err != nil {
		t.Fatalf("PerformWebSearch failed: %v", err)
	}
	if !strings.Contains(got, "1. net/http package\n   URL: https://pkg.go.dev/net/http\n   Package http provides HTTP client and server implementations.") {
		t.Errorf("unexpected DDG Lite table fallback result:\n%s", got)
	}
}

func TestPerformWebSearch_FallbackToBing(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer primary.Close()

	lite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`<html><body><div id="captcha">challenge</div></body></html>`))
	}))
	defer lite.Close()

	bing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`
<html><body><ol id="b_results">
  <li class="b_algo">
    <h2><a href="https://go.dev/doc/?a=1&amp;b=2">Documentation - The Go Programming Language</a></h2>
    <ul><li><a href="https://go.dev/tour/">Tour</a></li></ul>
    <div class="b_caption"><p>Official <b>Go</b> documentation &amp; tutorials.</p></div>
  </li>
  <li class="b_algo">
    <h2><a href="https://pkg.go.dev/">Go Packages</a></h2>
    <p>Discover packages for Go.</p>
  </li>
</ol></body></html>`))
	}))
	defer bing.Close()

	withSearchBaseURLs(t, primary.URL, lite.URL, bing.URL)

	got, err := PerformWebSearch(context.Background(), "go docs", "")
	if err != nil {
		t.Fatalf("PerformWebSearch failed: %v", err)
	}
	if !strings.Contains(got, "1. Documentation - The Go Programming Language\n   URL: https://go.dev/doc/?a=1&b=2\n   Official Go documentation & tutorials.") {
		t.Errorf("missing first Bing result:\n%s", got)
	}
	if !strings.Contains(got, "2. Go Packages\n   URL: https://pkg.go.dev/\n   Discover packages for Go.") {
		t.Errorf("missing second Bing result:\n%s", got)
	}
}

func TestPerformWebSearch_AllSourcesFail(t *testing.T) {
	primary := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`<html><body><div class="anomaly-modal">blocked</div></body></html>`))
	}))
	defer primary.Close()

	lite := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer lite.Close()

	bing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><ol id="b_results"></ol></body></html>`))
	}))
	defer bing.Close()

	withSearchBaseURLs(t, primary.URL, lite.URL, bing.URL)

	_, err := PerformWebSearch(context.Background(), "golang", "")
	if err == nil {
		t.Fatal("expected combined error when all sources fail, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"duckduckgo-html:",
		"blocked or challenged",
		"duckduckgo-lite:",
		"HTTP 500",
		"bing:",
		"no results",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("combined error %q missing %q", msg, want)
		}
	}
}
