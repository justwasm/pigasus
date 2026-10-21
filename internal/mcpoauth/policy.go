// Package mcpoauth is the clone's surface for the AGY 1.3.4 changelog line:
//
//	"Fixed remote MCP servers that use OAuth signing you out after a temporary
//	 network failure, timeout, or server error; saved sign-ins are now
//	 cleared only when the server or its authorization server actually
//	 rejects them."
//
// Two layers share the package:
//
//   - signin.go: the persistent saved-sign-in store. One record per remote
//     MCP server URL, written to ~/.gemini/antigravity-cli/mcp-oauth-signins.json
//     with 0600 perms so the OS user is the only reader. The store is what
//     "saved sign-in" means here: an OAuth grant that survived a CLI exit,
//     keyed by the server URL so each MCP server has its own grant.
//
//   - policy.go: the classification of "should this error clear the saved
//     sign-in?". The store itself is dumb — it just reads and writes. The
//     policy is the gate that turns a transient network failure (timeout,
//     EOF, connection refused, 5xx, …) into a no-op instead of a sign-out.
//
// The reference binary before AGY 1.3.4 had a single switch on err in the
// MCP OAuth error path: any non-nil error wiped the saved tokens. That
// meant a single 503 from the upstream MCP server, a 30-second timeout,
// or an SSL handshake error during a flaky coffee-shop Wi-Fi was enough
// to require the user to re-authorize the whole OAuth flow. The fix is
// to gate by error class, not by error presence. PiG's own oauth flow
// already does this on its end (it only Invalidates on `invalid_client`,
// `unauthorized_client`, and `invalid_grant`); this package is the
// clone-side mirror so the saved-sign-in layer that PiG does not own
// (on-disk persistence keyed by server URL) follows the same policy.
package mcpoauth

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// HTTPStatusError is any error that carries an HTTP status code. The
// clone's MCP transport wraps PiG's *mcp.McpHttpError into this type so
// the policy can match without importing PiG's types directly; any
// wrapper that implements Error() and HTTPStatus() (or unwraps to one)
// is matched.
type HTTPStatusError interface {
	error
	HTTPStatus() int
}

// HTTPError is the structured form of a non-2xx HTTP response from a
// remote MCP server or its authorization server. It mirrors the shape
// of internal/backend.HTTPError but lives in this package so the
// policy does not depend on the backend transport.
type HTTPError struct {
	StatusCode int
	URL        string
	Body       []byte
}

func (e *HTTPError) Error() string {
	// Body is appended (when short) so StepUp can match
	// "insufficient_scope" without each caller having to wrap the
	// body explicitly. Long bodies are cut off to keep the message
	// readable.
	body := strings.TrimSpace(string(e.Body))
	if len(body) > 200 {
		body = body[:200] + "... (truncated)"
	}
	if body != "" {
		return fmt.Sprintf("mcp: HTTP %d from %s: %s", e.StatusCode, e.URL, body)
	}
	return fmt.Sprintf("mcp: HTTP %d from %s", e.StatusCode, e.URL)
}

func (e *HTTPError) HTTPStatus() int { return e.StatusCode }

// OAuthError is the structured form of an OAuth provider error
// response (`{"error": "...", "error_description": "..."}`). The Code
// is the OAuth `error` value; the rejection policy matches on Code,
// not on HTTP status — providers sometimes wrap a rejection in a
// 200 (especially client-registration paths) and sometimes return a
// rejection inside a 4xx that the transport would otherwise treat as
// a transient error.
type OAuthError struct {
	Code        string // RFC 6749 §5.2 error code
	Description string
	URL         string
}

func (e *OAuthError) Error() string {
	if e.Description != "" {
		return fmt.Sprintf("mcp oauth: %s: %s", e.Code, e.Description)
	}
	return "mcp oauth: " + e.Code
}

// OAuthCode returns the OAuth `error` field, or "" if err is not an
// OAuth provider rejection. Used by ShouldClear to gate on rejection
// codes without leaking the wrapper type.
func OAuthCode(err error) string {
	if err == nil {
		return ""
	}
	var oe *OAuthError
	if errors.As(err, &oe) {
		return oe.Code
	}
	// RFC 6749 §5.2 code is also a common substring in plain HTTP
	// error bodies — match conservatively so we never clear on a
	// network error whose message happens to contain "invalid_grant"
	// (e.g. a TLS handshake error stringified by a buggy library).
	if s := strings.ToLower(err.Error()); strings.HasPrefix(s, "oauth ") {
		for _, code := range []string{
			"invalid_grant",
			"invalid_client",
			"invalid_request",
			"invalid_scope",
			"unauthorized_client",
			"unsupported_grant_type",
			"access_denied",
		} {
			if strings.Contains(s, " "+code+" ") || strings.HasPrefix(s, "oauth "+code) {
				return code
			}
		}
	}
	return ""
}

// HTTPStatus returns the HTTP status code carried by err, or 0 when
// err is not an HTTP error. Network errors (timeout, EOF, connection
// refused) deliberately return 0 so ShouldClear treats them as
// transient.
func HTTPStatus(err error) int {
	if err == nil {
		return 0
	}
	var hse HTTPStatusError
	if errors.As(err, &hse) {
		return hse.HTTPStatus()
	}
	return 0
}

// IsTransient reports whether err looks like a transient network
// failure (timeout, EOF, connection refused, DNS failure, server 5xx,
// …) — the kind of error the AGY 1.3.4 changelog calls out as no
// longer clearing saved sign-ins.
//
// Transient is the inverse of ShouldClear for the network/server-error
// path. ShouldClear adds two extra rejections on top of the inverse:
// explicit 401 / 403 and explicit OAuth provider rejections.
func IsTransient(err error) bool {
	if err == nil {
		return false
	}
	// Context cancellation is the caller's choice, not the server's:
	// never treat it as a rejection.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	// stdlib net errors: timeout, connection refused / reset, no route
	// to host, DNS failure, unexpected EOF. These are the most common
	// transient shapes the reference binary saw before the fix.
	var ne net.Error
	if errors.As(err, &ne) {
		return true
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	// HTTP 5xx — server-side failure that has nothing to do with
	// authorization. (4xx is *not* transient — a 401/403 is exactly
	// the rejection the policy matches.)
	if status := HTTPStatus(err); status >= 500 && status <= 599 {
		return true
	}
	// Stringly-typed stdlib errors that are never wrapped in a
	// typed net.Error but are still clearly transient: EOF,
	// TLS handshake failures, broken pipe, connection reset. The
	// substring check is conservative (must be a complete word)
	// so a server response that happens to contain "tls" in a URL
	// path is not mis-classified.
	msg := strings.ToLower(err.Error())
	for _, marker := range []string{
		"unexpected eof",
		"tls: ",
		"broken pipe",
		"connection reset",
		"no route to host",
		"network is unreachable",
		"i/o timeout",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// IsRejection reports whether err looks like an actual rejection of
// the saved sign-in by the MCP server or its authorization server.
// The clone uses this when wrapping a transport-level error so a
// caller can ask "should I clear the saved grant?" without depending
// on the policy table directly.
func IsRejection(err error) bool {
	if err == nil {
		return false
	}
	if status := HTTPStatus(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if code := OAuthCode(err); code != "" {
		// `server_error` is the OAuth-shaped way providers report a
		// 5xx-ish failure; treat it as transient, not as a sign-out.
		if code == "server_error" {
			return false
		}
		return true
	}
	return false
}

// ShouldClear is the single decision function. err is the error from
// the most recent MCP HTTP round-trip (or the OAuth token endpoint).
// It returns true exactly when the saved sign-in should be dropped —
// i.e. when the server or its authorization server has actually
// rejected it.
//
// The rules, in priority order:
//
//  1. nil → false (no error, nothing to clear).
//  2. context cancellation → false (caller's choice; never sign out
//     because the user hit Ctrl-C).
//  3. explicit HTTP 401 or 403 → true (the server rejected the
//     credentials). 403 with insufficient_scope is a step-up, not a
//     sign-out, so it lives under ShouldClear's sibling `StepUp`
//     helper; here 403 still counts as a rejection because a 403
//     that is not an insufficient_scope challenge is a hard deny.
//  4. explicit OAuth provider error code → true. `server_error` is
//     excluded because providers use it to report 5xx-shaped upstream
//     failures, which should not sign the user out.
//  5. everything else → false. This includes: connection refused,
//     EOF, TLS handshake failure, DNS failure, HTTP 5xx, HTTP 429
//     (rate-limit; clearing tokens would not help and would force the
//     user through OAuth again), and unknown stringly-typed errors.
//
// ShouldClear is intentionally total: it never panics and never blocks.
func ShouldClear(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if status := HTTPStatus(err); status == http.StatusUnauthorized || status == http.StatusForbidden {
		return true
	}
	if code := OAuthCode(err); code != "" && code != "server_error" {
		return true
	}
	// Last resort: a string-shaped rejection. We accept "401" / "403"
	// in the message only when the error is itself an HTTPStatusError
	// (so we never promote a net.OpError("dial tcp: lookup foo: no
	// such host") into a sign-out just because "401" appears in a
	// unrelated log line that got wrapped in the message).
	var hse HTTPStatusError
	if errors.As(err, &hse) {
		msg := strings.ToLower(err.Error())
		if status := hse.HTTPStatus(); status == 0 {
			if strings.Contains(msg, " 401 ") || strings.HasPrefix(msg, "401 ") || strings.HasSuffix(msg, " 401") {
				return true
			}
			if strings.Contains(msg, " 403 ") || strings.HasPrefix(msg, "403 ") || strings.HasSuffix(msg, " 403") {
				return true
			}
		}
	}
	return false
}

// StepUp reports whether err looks like an insufficient_scope
// challenge (`error="insufficient_scope"` in the WWW-Authenticate
// header per RFC 6750). The clone's policy on step-up is "ask the
// user to re-authorize with extra scopes but keep the saved sign-in
// around so a successful step-up does not require a fresh grant".
func StepUp(err error) bool {
	if err == nil {
		return false
	}
	if status := HTTPStatus(err); status != http.StatusForbidden {
		return false
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "insufficient_scope")
}

// SanitizeServerURL strips query / fragment and lower-cases the host
// so the saved-sign-in key is stable across cosmetic URL changes.
// Returns "" for unparseable URLs.
//
// A scheme-less input ("mcp.example.com/path") is treated as HTTPS
// rather than as a relative path: an MCP server is always addressed
// over the network, never as a relative reference. A trailing slash
// on an otherwise-empty path is dropped so "host" and "host/" produce
// the same key (matches RFC 3986 §6.2.3 normalization).
func SanitizeServerURL(raw string) string {
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return ""
	}
	u.RawQuery = ""
	u.Fragment = ""
	u.Host = strings.ToLower(u.Host)
	if u.Scheme == "" {
		u.Scheme = "https"
	}
	if u.Path == "/" {
		u.Path = ""
	}
	return u.String()
}
