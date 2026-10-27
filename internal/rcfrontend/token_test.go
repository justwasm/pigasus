// Package rcfrontend — token_test.go: the page the web UI serves and
// the secret it carries (cycle rc-auth-bearer-required).
//
// The rules the tests pin:
//
//   - a loopback peer is handed the secret;
//   - a tunnel request is handed the secret;
//   - a remote peer is handed back the secret it presented in the
//     query string, and only when that secret is the right one;
//   - a remote peer that presented nothing gets the "token required"
//     page, and that page does not carry the secret.
package rcfrontend

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// testSecret is the secret the tests configure.
const testSecret = "test-secret-0123456789abcdef"

// loopbackPageRequest builds a page request from the loopback.
func loopbackPageRequest(target string) *http.Request {
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = "127.0.0.1:5555"
	return req
}

// remotePageRequest builds a page request from outside the loopback.
func remotePageRequest(target string) *http.Request {
	req := httptest.NewRequest("GET", target, nil)
	req.RemoteAddr = "192.168.1.5:5555"
	return req
}

// TestPage_LoopbackPeerGetsTheSecret pins that the operator's own
// machine is handed the secret: it is the machine that holds it.
func TestPage_LoopbackPeerGetsTheSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, loopbackPageRequest("/"))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), testSecret) {
		t.Errorf("the page does not carry the secret")
	}
}

// TestPage_RemotePeerWithoutTheSecret pins that a peer that cannot
// prove it knows the secret does not get the dashboard — and that the
// page it does get does not carry the secret.
func TestPage_RemotePeerWithoutTheSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, remotePageRequest("/"))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET / returned status %d, want 401", rr.Code)
	}
	if strings.Contains(rr.Body.String(), testSecret) {
		t.Errorf("the refused page carries the secret")
	}
	if !strings.Contains(rr.Body.String(), "Token required") {
		t.Errorf("the refused page does not say what to do")
	}
}

// TestPage_RemotePeerWithTheSecret pins the browser's channel: a peer
// that already knows the secret hands it back in the query string and
// gets the dashboard.
func TestPage_RemotePeerWithTheSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, remotePageRequest("/?token="+testSecret))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET /?token=... returned status %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), testSecret) {
		t.Errorf("the page does not carry the secret")
	}
}

// TestPage_RemotePeerWithTheWrongSecret pins that a wrong secret is
// refused, and that it is not laundered into the page.
func TestPage_RemotePeerWithTheWrongSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, remotePageRequest("/?token=wrong-secret-0123456789abcd"))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /?token=... returned status %d, want 401", rr.Code)
	}
	if strings.Contains(rr.Body.String(), "wrong-secret-0123456789abcd") {
		t.Errorf("the refused page carries the wrong secret")
	}
}

// TestPage_NoSecretConfigured pins the pre-cycle behaviour: a server
// without a secret serves the page to everybody.
func TestPage_NoSecretConfigured(t *testing.T) {
	handler := Handler(Options{})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, remotePageRequest("/"))

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}
}

// TestPage_TunnelRequest pins that the cloud's request is served: it
// is authenticated by the cloud and it never crossed the socket.
func TestPage_TunnelRequest(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	req := remotePageRequest("/")
	req.Header.Set("X-Jetski-Via-Remote-Control", "true")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / returned status %d, want 200", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), testSecret) {
		t.Errorf("the page does not carry the secret")
	}
}

// TestPage_SPAFallbackNeedsTheSecret pins that the single-page
// application's routes are gated too — a remote peer cannot reach the
// dashboard by asking for a route.
func TestPage_SPAFallbackNeedsTheSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	req := remotePageRequest("/cascade/c-123456")
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("SPA route returned status %d, want 401", rr.Code)
	}
}

// TestPage_ETagFollowsTheSecret pins that the cached page's ETag is
// computed over the substituted body: two peers that end up with the
// same body share one validator, and the repeat request answers 304.
func TestPage_ETagFollowsTheSecret(t *testing.T) {
	handler := Handler(Options{CSRFToken: testSecret})

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, loopbackPageRequest("/"))
	etag := rr.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("missing ETag on index.html")
	}

	// The same peer with the same secret gets the same validator.
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, loopbackPageRequest("/"))
	if got := rr2.Header().Get("ETag"); got != etag {
		t.Errorf("ETag = %q, want %q", got, etag)
	}

	// A peer that ends up with the same body (the same secret, from
	// the query string) shares the validator.
	rr3 := httptest.NewRecorder()
	handler.ServeHTTP(rr3, remotePageRequest("/?token="+testSecret))
	if got := rr3.Header().Get("ETag"); got != etag {
		t.Errorf("ETag = %q, want %q", got, etag)
	}

	// The repeat request answers 304.
	repeat := loopbackPageRequest("/")
	repeat.Header.Set("If-None-Match", etag)
	rr4 := httptest.NewRecorder()
	handler.ServeHTTP(rr4, repeat)
	if rr4.Code != http.StatusNotModified {
		t.Errorf("If-None-Match returned status %d, want 304", rr4.Code)
	}
}
