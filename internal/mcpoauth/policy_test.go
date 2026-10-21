// policy_test.go — covers the AGY 1.3.4 changelog line:
//
//	"Fixed remote MCP servers that use OAuth signing you out after a
//	 temporary network failure, timeout, or server error; saved sign-ins
//	 are now cleared only when the server or its authorization server
//	 actually rejects them."
//
// The required tests are:
//
//  1. TestShouldClear_HTTP401      — HTTP 401 clears.
//  2. TestShouldClear_HTTP403      — HTTP 403 clears.
//  3. TestShouldClear_NetworkTimeout — deadline / net.Error preserves.
//  4. TestShouldClear_HTTP5xx      — 500/502/503/504 preserves.
//  5. TestShouldClear_OAuthRejection — OAuth `error="invalid_grant"`
//                                      (and friends) clears.
//
// The remaining tests below cover the boundary cases the cycle has to
// hold: nil / canceled errors, OAuth `server_error` (which is the
// provider-shaped 5xx and must NOT clear), insufficient_scope (which
// is a step-up, not a sign-out), DNS failure (preserves), EOF
// (preserves), connection refused (preserves), and a 401 wrapped in a
// fmt.Errorf with %w so the policy still finds the inner HTTPError.

package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"testing"
	"time"
)

// fakeHTTPStatusError is a wrapper that does NOT implement
// HTTPStatusError directly but unwraps to one. Exercises the
// errors.As path in HTTPStatus / ShouldClear.
type fakeHTTPStatusError struct {
	inner HTTPStatusError
	msg   string
}

func (e *fakeHTTPStatusError) Error() string { return e.msg }
func (e *fakeHTTPStatusError) Unwrap() error { return e.inner }

// TestShouldClear_HTTP401 — spec test #1: HTTP 401 clears.
func TestShouldClear_HTTP401(t *testing.T) {
	err := &HTTPError{StatusCode: http.StatusUnauthorized, URL: "https://mcp.example.com"}
	if !ShouldClear(err) {
		t.Fatalf("ShouldClear(401) = false, want true (AGY 1.3.4 line 28: server rejection must clear)")
	}
	if IsTransient(err) {
		t.Fatalf("IsTransient(401) = true, want false (401 is rejection, not transient)")
	}
	if IsRejection(err) != true {
		t.Fatalf("IsRejection(401) = false, want true")
	}
}

// TestShouldClear_HTTP403 — spec test #2: HTTP 403 clears.
//
// (403-with-insufficient_scope is a step-up; that branch is covered
// separately by TestStepUp_InsufficientScope. Here we test the plain
// 403 hard-deny path.)
func TestShouldClear_HTTP403(t *testing.T) {
	err := &HTTPError{StatusCode: http.StatusForbidden, URL: "https://mcp.example.com"}
	if !ShouldClear(err) {
		t.Fatalf("ShouldClear(403) = false, want true")
	}
	if IsTransient(err) {
		t.Fatalf("IsTransient(403) = true, want false")
	}
}

// TestShouldClear_NetworkTimeout — spec test #3: timeout preserves.
//
// The policy is the AGY 1.3.4 fix in action: a single 30-second
// timeout no longer signs the user out. Both context.DeadlineExceeded
// and a *net.OpError with Timeout()==true must preserve.
func TestShouldClear_NetworkTimeout(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"context.DeadlineExceeded", context.DeadlineExceeded},
		{"context.Canceled", context.Canceled},
		{"wrapped-DeadlineExceeded", fmt.Errorf("mcp dial: %w", context.DeadlineExceeded)},
		{"net.Error timeout=true", &timeoutErr{}},
		{"net.OpError dial timeout", &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &timeoutErr{},
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ShouldClear(tc.err) {
				t.Fatalf("ShouldClear(%s) = true, want false (network timeout must NOT sign the user out)", tc.name)
			}
			if !IsTransient(tc.err) {
				t.Fatalf("IsTransient(%s) = false, want true", tc.name)
			}
			if IsRejection(tc.err) {
				t.Fatalf("IsRejection(%s) = true, want false (timeout is not a rejection)", tc.name)
			}
		})
	}
}

// TestShouldClear_HTTP5xx — spec test #4: 5xx preserves.
//
// 500, 502, 503 and 504 are all server-side failures that have
// nothing to do with authorization. Clearing the saved grant would
// force the user through the OAuth flow on every blip.
func TestShouldClear_HTTP5xx(t *testing.T) {
	for _, status := range []int{500, 502, 503, 504, 599} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			err := &HTTPError{StatusCode: status, URL: "https://mcp.example.com"}
			if ShouldClear(err) {
				t.Fatalf("ShouldClear(%d) = true, want false (5xx is transient, must NOT clear)", status)
			}
			if !IsTransient(err) {
				t.Fatalf("IsTransient(%d) = false, want true", status)
			}
			if IsRejection(err) {
				t.Fatalf("IsRejection(%d) = true, want false (5xx is not a rejection)", status)
			}
		})
	}
}

// TestShouldClear_OAuthRejection — spec test #5: OAuth rejection
// clears.
//
// RFC 6749 §5.2 codes that imply the saved grant is invalid:
//   - invalid_grant      (refresh_token rejected)
//   - invalid_client     (client_id no longer recognised)
//   - unauthorized_client (client not allowed to use this grant)
//   - access_denied      (user denied the step-up)
//
// Plus the string-shaped path for providers that surface the error
// as a plain HTTP 400 with the OAuth code in the body.
func TestShouldClear_OAuthRejection(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"invalid_grant typed", &OAuthError{Code: "invalid_grant", Description: "refresh_token revoked", URL: "https://auth.example.com/token"}},
		{"invalid_client typed", &OAuthError{Code: "invalid_client", Description: "client_id not found", URL: "https://auth.example.com/token"}},
		{"unauthorized_client typed", &OAuthError{Code: "unauthorized_client", URL: "https://auth.example.com/token"}},
		{"access_denied typed", &OAuthError{Code: "access_denied", Description: "user denied", URL: "https://auth.example.com/authorize"}},
		{"invalid_grant in string body", errors.New("oauth invalid_grant: refresh token expired")},
		{"invalid_grant wrapped", fmt.Errorf("mcp: token exchange: %w", &OAuthError{Code: "invalid_grant"})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !ShouldClear(tc.err) {
				t.Fatalf("ShouldClear(%s) = false, want true (OAuth provider rejection must clear)", tc.name)
			}
			if code := OAuthCode(tc.err); code == "" {
				t.Fatalf("OAuthCode(%s) = \"\", want a non-empty code", tc.name)
			}
		})
	}
}

// TestShouldClear_OAuthServerError — boundary: `server_error` is the
// OAuth-shaped 5xx. Per RFC 6749 §5.2 it means "the authorization
// server encountered an unexpected condition that prevented it from
// fulfilling the request." Clearing on that is the same bug the
// changelog fixed: it would sign the user out on any provider blip.
func TestShouldClear_OAuthServerError(t *testing.T) {
	err := &OAuthError{Code: "server_error", Description: "authorization server unavailable", URL: "https://auth.example.com/token"}
	if ShouldClear(err) {
		t.Fatalf("ShouldClear(server_error) = true, want false (provider 5xx must not sign out)")
	}
}

// TestShouldClear_Other4xx — boundary: a 4xx that is not 401/403 is
// not a sign-in rejection (e.g. 400 bad request, 404 not found,
// 410 gone, 429 rate-limited). The clone should not sign the user
// out on those — the token may still be valid, the request was
// just malformed.
func TestShouldClear_Other4xx(t *testing.T) {
	for _, status := range []int{400, 404, 410, 418, 429} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			err := &HTTPError{StatusCode: status, URL: "https://mcp.example.com"}
			if ShouldClear(err) {
				t.Fatalf("ShouldClear(%d) = true, want false", status)
			}
		})
	}
}

// TestShouldClear_NilAndCanceled — boundary: nil error and context
// cancellation never sign anyone out.
func TestShouldClear_NilAndCanceled(t *testing.T) {
	if ShouldClear(nil) {
		t.Fatalf("ShouldClear(nil) = true, want false")
	}
	if IsTransient(nil) {
		t.Fatalf("IsTransient(nil) = true, want false")
	}
	if IsRejection(nil) {
		t.Fatalf("IsRejection(nil) = true, want false")
	}
	if ShouldClear(context.Canceled) {
		t.Fatalf("ShouldClear(context.Canceled) = true, want false")
	}
}

// TestShouldClear_OtherNetworkErrors — boundary: connection refused,
// DNS failure, EOF, TLS handshake failure. None of these are
// authorization rejections, so the saved sign-in must be preserved.
func TestShouldClear_OtherNetworkErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{"connection refused", &net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED}},
		{"no such host", &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "mcp.example.com"}}},
		{"EOF", errors.New("unexpected EOF")},
		{"tls handshake", errors.New("tls: handshake failure")},
		{"connection reset", &net.OpError{Op: "read", Net: "tcp", Err: syscall.ECONNRESET}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if ShouldClear(tc.err) {
				t.Fatalf("ShouldClear(%s) = true, want false (%s is transient)", tc.name, tc.name)
			}
			if !IsTransient(tc.err) {
				t.Fatalf("IsTransient(%s) = false, want true", tc.name)
			}
		})
	}
}

// TestShouldClear_WrappedHTTPError — boundary: an HTTPError wrapped
// via fmt.Errorf("...: %w", err). errors.As must find it.
func TestShouldClear_WrappedHTTPError(t *testing.T) {
	inner := &HTTPError{StatusCode: 401, URL: "https://mcp.example.com"}
	wrapped := fmt.Errorf("mcp tool call: %w", inner)
	if !ShouldClear(wrapped) {
		t.Fatalf("ShouldClear(wrapped 401) = false, want true (errors.As must unwrap)")
	}
	var hse HTTPStatusError
	if !errors.As(wrapped, &hse) {
		t.Fatalf("errors.As did not unwrap to HTTPStatusError")
	}
	if hse.HTTPStatus() != 401 {
		t.Fatalf("HTTPStatus() = %d, want 401", hse.HTTPStatus())
	}
}

// TestShouldClear_IndirectHTTPStatus — boundary: an error that does
// not implement HTTPStatusError itself but unwraps to one. Exercises
// the errors.As path in ShouldClear / HTTPStatus.
func TestShouldClear_IndirectHTTPStatus(t *testing.T) {
	inner := &HTTPError{StatusCode: 403, URL: "https://mcp.example.com"}
	wrap := &fakeHTTPStatusError{inner: inner, msg: "mcp: forbidden access"}
	if !ShouldClear(wrap) {
		t.Fatalf("ShouldClear(indirect 403) = false, want true")
	}
}

// TestStepUp_InsufficientScope — boundary: a 403 with the
// insufficient_scope challenge is a step-up, not a sign-out.
func TestStepUp_InsufficientScope(t *testing.T) {
	err := &HTTPError{
		StatusCode: http.StatusForbidden,
		URL:        "https://mcp.example.com",
		Body:       []byte(`{"error":"insufficient_scope","error_description":"missing scope: write"}`),
	}
	if !StepUp(err) {
		t.Fatalf("StepUp(insufficient_scope 403) = false, want true")
	}
	// Step-up is a 403, so ShouldClear still returns true — the
	// clone uses StepUp() to decide whether to ask the user for
	// extra scopes while ShouldClear() decides whether to drop the
	// grant. The two answers can both be true.
	if !ShouldClear(err) {
		t.Fatalf("ShouldClear(insufficient_scope 403) = false, want true (403 is still a rejection)")
	}
}

// TestStepUp_NotInsufficientScope — boundary: a plain 403 with no
// insufficient_scope challenge is a hard deny, not a step-up.
func TestStepUp_NotInsufficientScope(t *testing.T) {
	err := &HTTPError{StatusCode: 403, URL: "https://mcp.example.com", Body: []byte(`{"error":"forbidden"}`)}
	if StepUp(err) {
		t.Fatalf("StepUp(plain 403) = true, want false")
	}
}

// TestSanitizeServerURL — boundary: the key used by the store must be
// stable across cosmetic URL changes (case in host, query string,
// fragment, scheme omission, trailing slash). The test pins the
// sanitized form so changing the rules is a deliberate decision.
func TestSanitizeServerURL(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://mcp.example.com/path", "https://mcp.example.com/path"},
		{"https://MCP.Example.com/path", "https://mcp.example.com/path"},
		{"https://mcp.example.com/path?foo=bar", "https://mcp.example.com/path"},
		{"https://mcp.example.com/path#frag", "https://mcp.example.com/path"},
		{"mcp.example.com/path", "https://mcp.example.com/path"},
		{"https://mcp.example.com/", "https://mcp.example.com"}, // trailing slash dropped
		{"https://mcp.example.com", "https://mcp.example.com"},
		{"", ""},
		{"://broken", ""},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got := SanitizeServerURL(tc.in)
			if got != tc.want {
				t.Fatalf("SanitizeServerURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestOAuthCode_StringMatch — boundary: the substring fallback in
// OAuthCode only matches an "oauth <code>" prefix so a generic error
// message that happens to contain "invalid_grant" inside other
// context does not produce a spurious match.
func TestOAuthCode_StringMatch(t *testing.T) {
	cases := []struct {
		name string
		in   error
		want string
	}{
		{"oauth invalid_grant", errors.New("oauth invalid_grant: refresh token expired"), "invalid_grant"},
		{"oauth invalid_client", errors.New("oauth invalid_client: client_id not found"), "invalid_client"},
		{"invalid_grant only", errors.New("invalid_grant"), ""},                                                        // no "oauth " prefix
		{"invalid_grant in hostname", errors.New("net: dial tcp: lookup invalid_grant.example.com: no such host"), ""}, // in hostname, not in code
		{"nil", nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := OAuthCode(tc.in); got != tc.want {
				t.Fatalf("OAuthCode(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// timeoutErr implements net.Error with Timeout()=true.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

// guard the imports we use through strings.HasPrefix / url.Parse so a
// future refactor cannot silently drop them.
var _ = strings.HasPrefix
var _ = (*url.URL)(nil)
var _ = time.Now
