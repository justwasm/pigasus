// html_error_quota_test.go — cycle html-error-page-title.
//
// Changelog 1.3.3: "Improved error messages: a request that fails with an HTML
// error page, for example from a corporate proxy, now shows the page title
// instead of the raw HTML, other long non-JSON error bodies are cut off after
// 500 bytes, and reaching your individual quota is reported right away instead
// of being retried."
//
// These tests pin the three behaviours in one place:
//
//  1. TestHTMLError — an HTML body contributes its <title> as the user-facing
//     message (the corporate-proxy case).
//  2. TestErrorBody — a long non-JSON body is truncated at MaxErrorBody (500
//     bytes) with the recovered "... (truncated)" marker.
//  3. TestExtractTitle — the <title> tag content is recovered from several
//     shapes (uppercase tag, attribute soup, doctype prefix).
//  4. TestQuota — an HTTPError flagged as quota reached reports immediately
//     (IsQuotaReachedErr walks the error chain to the typed error).
//
// Plus a handful of negative / regression cases (HTML without a title,
// plain JSON unchanged, plain text falls through to the body truncation,
// non-429 quota mentions do not trip the rule).
package backend

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestHTMLError pins the corporate-proxy case from the changelog: the body
// is a full HTML page and the user-facing message is the <title> text, not
// the raw HTML.
func TestHTMLError(t *testing.T) {
	body := []byte("<html><head><title>Proxy Error: 502 Bad Gateway</title></head>" +
		"<body><p>Corporate proxy denied the request.</p>" +
		strings.Repeat("a", 900) + "</body></html>")
	e := &HTTPError{StatusCode: 502, Body: body, URL: "https://example.com/v1/chat"}
	got := e.Message()
	want := "Proxy Error: 502 Bad Gateway"
	if got != want {
		t.Fatalf("HTML body message = %q, want %q", got, want)
	}
	// Error() keeps the historical one-line shape on top of the extracted
	// message — so log scrapers continue to see the upstream status.
	if errStr := e.Error(); !strings.Contains(errStr, "gateway: upstream 502: ") ||
		!strings.Contains(errStr, want) {
		t.Errorf("Error() = %q, missing the upstream prefix or title", errStr)
	}
}

// TestHTMLErrorDoctype pins the alternate HTML shape many real proxies
// emit: a doctype prefix plus the same <title> recovery.
func TestHTMLErrorDoctype(t *testing.T) {
	body := []byte("<!DOCTYPE html><html><head><TITLE>403 Forbidden</TITLE></head></html>")
	e := &HTTPError{StatusCode: 403, Body: body}
	if got := e.Message(); got != "403 Forbidden" {
		t.Errorf("DOCTYPE HTML message = %q, want %q", got, "403 Forbidden")
	}
}

// TestErrorBody pins the long-non-JSON truncation from the changelog:
// bodies over MaxErrorBody (500) bytes are cut off with the recovered
// "... (truncated)" marker, which the 1.3.6 string-table capture pins
// verbatim.
func TestErrorBody(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string // expected Message() output
	}{
		{
			name: "long plain text truncated",
			body: []byte(strings.Repeat("x", 900)),
			want: strings.Repeat("x", MaxErrorBody) + "... (truncated)",
		},
		{
			name: "exactly cap preserved",
			body: []byte(strings.Repeat("y", MaxErrorBody)),
			want: strings.Repeat("y", MaxErrorBody),
		},
		{
			name: "short plain text verbatim",
			body: []byte("connection refused"),
			want: "connection refused",
		},
		{
			name: "long with invalid utf-8",
			body: append([]byte("ok\xff"), bytesRepeat(0x41, 600)...),
			want: "ok" + strings.Repeat("A", MaxErrorBody-2) + "... (truncated)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &HTTPError{StatusCode: 500, Body: tc.body}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

// bytesRepeat is a tiny stdlib-free byte-repeater so the test above does
// not import bytes just for one helper.
func bytesRepeat(b byte, n int) []byte {
	out := make([]byte, n)
	for i := range out {
		out[i] = b
	}
	return out
}

// TestExtractTitle pins the <title>-extraction path against several real
// shapes the corporate-proxy / captive-portal cases produce.
func TestExtractTitle(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"lowercase", "<html><title>hello</title></html>", "hello"},
		{"uppercase", "<HTML><TITLE>Hi</TITLE></HTML>", "Hi"},
		{"mixed case", "<html><Title>MiX</title></html>", "MiX"},
		{"doctype prefix", "<!doctype html><html><title>Page Title</title></html>", "Page Title"},
		{"trailing whitespace", "<html><title>  Spaced Out  </title></html>", "Spaced Out"},
		{"newlines inside title", "<html><title>Line1\nLine2</title></html>", "Line1\nLine2"},
		{"attributes before tag", `<html><head lang="en"><title>Header</title></head></html>`, "Header"},
		{"empty title", "<html><title></title></html>", ""},
		{"unclosed tag", "<title>no closing", ""},
		{"no title at all", "<html><body>plain body</body></html>", ""},
		{"plain text", "the body has no html markers at all", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := HTMLTitle([]byte(tc.body)); got != tc.want {
				t.Errorf("HTMLTitle(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

// TestExtractTitleLooksHTML pins the LooksHTML gate the changelog implies:
// only bodies that actually look like HTML (start with <!doctype or
// <html, after whitespace trimming) feed the <title> extractor. A body
// that just contains a stray <title> tag in plain text falls through to
// the truncation path instead.
func TestExtractTitleLooksHTML(t *testing.T) {
	for _, in := range []string{"", "plain text", `{"error":{"message":"x"}}`} {
		if LooksHTML([]byte(in)) {
			t.Errorf("LooksHTML(%q) = true, want false", in)
		}
	}
	for _, in := range []string{
		"<!DOCTYPE html><html>...",
		"<!doctype html><html>...",
		"<html>...",
		"<HTML>...",
		"   \n\t<html>...",
		"<!DOCTYPE\nhtml><html>...",
	} {
		if !LooksHTML([]byte(in)) {
			t.Errorf("LooksHTML(%q) = false, want true", in)
		}
	}
}

// TestExtractTitleHTMLWithoutTitleReturnsBody: an HTML body without a
// <title> falls through to the truncated body — the user still gets
// something, just not the title. This is the captioned "fallback" the
// changelog mentions obliquely.
func TestExtractTitleHTMLWithoutTitleReturnsBody(t *testing.T) {
	body := []byte("<html><body>no title here " + strings.Repeat("z", 600) + "</body></html>")
	e := &HTTPError{StatusCode: 502, Body: body}
	got := e.Message()
	// No <title> was found; the fallback returns the truncated body
	// (which is still HTML markup but cut to 500 bytes with the
	// recovered "... (truncated)" marker so a long captive-portal page
	// does not flood the terminal).
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Fatalf("HTML-without-title fallback missing truncation marker: %q", got)
	}
	if strings.Contains(got, "<title>") {
		t.Errorf("title tag leaked into the fallback: %q", got)
	}
}

// TestPlainJSONUnchanged pins the JSON body path: a plain JSON body
// contributes its `error.message` (preferred) or top-level `message`
// field; HTMLTitle is NOT consulted because LooksHTML is false.
func TestPlainJSONUnchanged(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			"error.message wins",
			`{"error":{"message":"structured error"}}`,
			"structured error",
		},
		{
			"top-level message fallback",
			`{"message":"plain json message"}`,
			"plain json message",
		},
		{
			"nested error.code plus message",
			`{"error":{"code":429,"message":"nested"}}`,
			"nested",
		},
		{
			"empty JSON falls through to body",
			// Valid JSON but no recoverable string fields; the
			// fallback path returns the truncated body. The point
			// of the test is that the JSON shape does not panic.
			`{"foo":"bar"}`,
			`{"foo":"bar"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &HTTPError{StatusCode: 500, Body: []byte(tc.body)}
			if got := e.Message(); got != tc.want {
				t.Errorf("Message() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNormalErrorStillSurfaces: a *non-API* error (no HTTPError in the
// chain) is not classified as a quota error. This is the negative case
// that keeps the no-retry rule from misclassifying tool failures,
// iteration-cap panics, etc.
func TestNormalErrorStillSurfaces(t *testing.T) {
	for _, err := range []error{
		nil,
		errors.New("usage: --print requires a prompt"),
		fmt.Errorf("agent: exceeded tool-iteration budget (8)"),
		errors.New("i/o timeout"),
		// A 429 that does NOT mention quota is a normal retryable
		// 429 (rate limit), not the individual-quota case.
		fmt.Errorf("wrapped: %w", &HTTPError{StatusCode: 429, Body: []byte(`{"error":{"message":"rate limit"}}`)}),
		// A non-429 status that mentions quota is also not the
		// individual-quota case (the recovered predicate requires
		// status 429 plus the keyword).
		&HTTPError{StatusCode: 503, Body: []byte(`{"error":{"message":"quota exceeded"}}`)},
	} {
		if IsQuotaReachedErr(err) {
			t.Errorf("IsQuotaReachedErr(%v) = true, want false", err)
		}
	}
}

// TestQuota pins the "individual quota reached → reported immediately,
// not retried" case from the changelog. The predicate is HTTPError
// status 429 + body containing the "quota" keyword (case-insensitive).
//
// This is the test the auto-continue layers in internal/agent and
// internal/pigrt rely on: their no-retry rule is "if
// backend.IsQuotaReachedErr(err) { return err }". The wiring tests in
// the two packages exercise the rule end-to-end; this one pins the
// predicate directly.
func TestQuota(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "canonical 429 quota JSON",
			err: &HTTPError{
				StatusCode: 429,
				Body:       []byte(`{"error":{"message":"Individual quota reached."}}`),
			},
			want: true,
		},
		{
			name: "wrapped quota error",
			err: fmt.Errorf("agent: open stream: %w", &HTTPError{
				StatusCode: 429,
				Body:       []byte(`{"error":{"message":"quota exhausted"}}`),
			}),
			want: true,
		},
		{
			name: "uppercase QUOTA in body",
			err: &HTTPError{
				StatusCode: 429,
				Body:       []byte(`{"error":{"message":"YOUR QUOTA HAS BEEN REACHED"}}`),
			},
			want: true,
		},
		{
			name: "non-JSON quota body still trips",
			err: &HTTPError{
				StatusCode: 429,
				Body:       []byte("quota exceeded for user"),
			},
			want: true,
		},
		{
			name: "rate-limit 429 does NOT trip",
			err: &HTTPError{
				StatusCode: 429,
				Body:       []byte(`{"error":{"message":"rate limit"}}`),
			},
			want: false,
		},
		{
			name: "non-429 quota does NOT trip",
			err: &HTTPError{
				StatusCode: 500,
				Body:       []byte(`{"error":{"message":"quota exhausted"}}`),
			},
			want: false,
		},
		{
			name: "empty body does NOT trip",
			err:  &HTTPError{StatusCode: 429, Body: nil},
			want: false,
		},
		{
			name: "nil error is not quota",
			err:  nil,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsQuotaReachedErr(tc.err); got != tc.want {
				t.Errorf("IsQuotaReachedErr = %v, want %v (err=%v)", got, tc.want, tc.err)
			}
		})
	}
}

// TestIsQuotaReachedNilReceiver pins the nil-safe behaviour: a typed
// nil *HTTPError returns false, not a panic.
func TestIsQuotaReachedNilReceiver(t *testing.T) {
	var e *HTTPError
	if e.IsQuotaReached() {
		t.Error("nil *HTTPError should not be a quota error")
	}
	if IsQuotaReachedErr(e) {
		t.Error("IsQuotaReachedErr(nil *HTTPError) should be false")
	}
}
