package backend

// http_error_test.go — cycle 112: the recovered body-to-message rules for a
// non-2xx model-API response.

import (
	"strings"
	"testing"
)

func TestHTTPErrorMessage_JSON(t *testing.T) {
	e := &HTTPError{StatusCode: 429, Body: []byte(`{"error":{"message":"Individual quota reached."}}`)}
	if got := e.Message(); got != "Individual quota reached." {
		t.Errorf("error.message = %q", got)
	}
	// The top-level `message` field is the fallback.
	e = &HTTPError{StatusCode: 503, Body: []byte(`{"message":"plain json message"}`)}
	if got := e.Message(); got != "plain json message" {
		t.Errorf("message = %q", got)
	}
}

func TestHTTPErrorMessage_HTMLTitle(t *testing.T) {
	// Changelog: "a request that fails with an HTML error page, for example
	// from a corporate proxy, now shows the page title instead of the raw
	// HTML".
	body := []byte("<html><head><TITLE>502 Bad Gateway</TITLE></head><body>" +
		strings.Repeat("a", 900) + "</body></html>")
	e := &HTTPError{StatusCode: 502, Body: body}
	if got := e.Message(); got != "502 Bad Gateway" {
		t.Errorf("html title = %q", got)
	}
}

func TestHTTPErrorMessage_Truncated(t *testing.T) {
	// Changelog: "other long non-JSON error bodies are cut off after 500
	// bytes". `... (truncated)` is a recovered literal.
	e := &HTTPError{StatusCode: 500, Body: []byte(strings.Repeat("x", 900))}
	got := e.Message()
	if !strings.HasSuffix(got, "... (truncated)") {
		t.Fatalf("marker missing: %q", got)
	}
	body := strings.TrimSuffix(got, "... (truncated)")
	if len(body) != MaxErrorBody {
		t.Errorf("cut to %d bytes, want %d", len(body), MaxErrorBody)
	}
	// A short body is used verbatim.
	e = &HTTPError{StatusCode: 500, Body: []byte("short")}
	if got := e.Message(); got != "short" {
		t.Errorf("short body = %q", got)
	}
}

func TestHTTPErrorMessage_Empty(t *testing.T) {
	e := &HTTPError{StatusCode: 503}
	if got := e.Message(); got != "Service Unavailable" {
		t.Errorf("empty body = %q", got)
	}
}

func TestHTTPErrorError(t *testing.T) {
	// Error() keeps the historical one-line shape.
	e := &HTTPError{StatusCode: 429, Body: []byte(`{"error":{"message":"quota"}}`)}
	if got := e.Error(); got != "gateway: upstream 429: quota" {
		t.Errorf("Error() = %q", got)
	}
}

func TestHTMLTitle_NoTitle(t *testing.T) {
	for _, in := range []string{"", "plain text", "<html><body>no title</body></html>", "<title>unclosed"} {
		if got := HTMLTitle([]byte(in)); got != "" {
			t.Errorf("HTMLTitle(%q) = %q, want empty", in, got)
		}
	}
}

// TestHTTPError_KindAuthExpired_Constructor pins the cycle
// auth-expired-message behaviour: NewHTTPError auto-classifies a 401
// response as KindAuthExpired. The literal AuthExpiredMessage is the
// verbatim canonical string the user-facing formatter renders.
func TestHTTPError_KindAuthExpired_Constructor(t *testing.T) {
	e := NewHTTPError(401, []byte(`{"error":{"message":"unclear backend noise"}}`), "https://example.test/v1/x")
	if e.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", e.StatusCode)
	}
	if e.KindError() != KindAuthExpired {
		t.Errorf("KindError() = %d, want KindAuthExpired (%d)", e.KindError(), KindAuthExpired)
	}
	if AuthExpiredMessage != "Authentication expired. Please log back in." {
		t.Errorf("AuthExpiredMessage = %q, want verbatim literal", AuthExpiredMessage)
	}
}

// TestHTTPError_KindAuthExpired_LazyClassification covers the path
// where existing callers (which still build the struct literally with
// `&HTTPError{StatusCode: 401, …}`) get auto-classification on first
// access — KindError() falls back to classifyStatus(StatusCode) when
// the explicit Kind field is left at its zero value.
func TestHTTPError_KindAuthExpired_LazyClassification(t *testing.T) {
	e := &HTTPError{StatusCode: 401, Body: []byte("upstream body"), URL: "x"}
	if e.KindError() != KindAuthExpired {
		t.Errorf("lazy KindError() = %d, want KindAuthExpired", e.KindError())
	}
	// A non-401 status stays KindUnknown.
	e = &HTTPError{StatusCode: 500, Body: []byte("oops")}
	if e.KindError() != KindUnknown {
		t.Errorf("500 KindError() = %d, want KindUnknown", e.KindError())
	}
}

// TestHTTPError_KindAuthExpired_OverridesMessage verifies the
// formatter contract: when Kind == KindAuthExpired, Message() is NOT
// used directly — the headless formatter renders AuthExpiredMessage.
// This test pins the contract that Message() still returns the
// upstream body (for the AGY_ERROR JSON envelope), so a regression
// that silently rewrites the body would surface here.
func TestHTTPError_KindAuthExpired_OverridesMessage(t *testing.T) {
	e := NewHTTPError(401, []byte(`{"error":{"message":"upstream says something cryptic"}}`), "x")
	if e.Message() != "upstream says something cryptic" {
		t.Errorf("Message() = %q, want upstream body verbatim", e.Message())
	}
	if AuthExpiredMessage == e.Message() {
		t.Errorf("AuthExpiredMessage must NOT collide with the upstream body — got %q", AuthExpiredMessage)
	}
}
