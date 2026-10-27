// credentials.go — distinguish "definitively rejected" from "transient" auth
// failures and clear the on-disk token when rejection is the verdict.
//
// AGY 1.2.5 changelog: "Fixed stale credentials lingering after the sign-in
// server definitively rejects them (for example a revoked or invalidated
// enterprise account); the CLI now signs you out and clears the stale tokens
// so you can sign in again cleanly, while temporary network failures, server
// errors, and rate limits never cost you your session."
//
// Two layers of code drive this:
//
//  1. PostTokenForm / RefreshTokenReal* wrap non-2xx responses in *AuthError
//     when the OAuth token server replied with structured information
//     (so we know the HTTP status and the JSON `error` code). For those,
//     the *AuthError.Rejected flag tells callers whether the credentials
//     are definitively gone.
//
//  2. RefreshIfNeeded inspects that flag. When the server returned a
//     definitive rejection it deletes the unified token file (and the
//     legacy fallbacks), then surfaces the typed error so the caller's
//     error reporter can tell the user "your credentials were rejected,
//     please sign in again". On any other failure — network error, 5xx,
//     429 — the on-disk token is left alone so the next request can try
//     again without prompting for re-authentication.
//
// We deliberately do not clear the token for transient class because
// dropping a healthy refresh token on a flaky network would be worse than
// the bug the changelog is closing.
//
// Definitions (recovered / hand-rolled from RFC 6749 §5.2 + the cycle 224
// HTTP code tables in cmd/wagyu/api_error.go):
//
//   - Rejected = definitive; the server told us the credentials are gone.
//     HTTP status 400 with error=invalid_grant (refresh-token revoked),
//     error=invalid_client (client identity rejected), or any 401/403
//     with a JSON `error` field.
//
//   - Transient = recoverable; keep the token and retry on the next turn.
//     HTTP 408, 429, every 5xx, plus any network error (DNS, conn refused,
//     i/o timeout, …).
//
// Everything else (no structured response at all, JSON parse failure, …)
// is treated as transient so the next request can retry.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"

	"golang.org/x/oauth2"
)

// AuthError classifies a non-success response from the OAuth token / device
// endpoints. Callers can use errors.As to inspect the flags and decide
// whether to clear the credential store, surface the failure, or both.
//
// The fields are exported so callers can render their own messages; the
// canonical "Rejected vs Transient" boolean is accessible through
// IsAuthRejected / IsTransientAuthError.
type AuthError struct {
	// Rejection is true when the OAuth server definitively told us the
	// credentials are no longer valid (HTTP 401, HTTP 400 with the
	// invalid_grant / invalid_client error codes, revoked enterprise
	// accounts, …). The credential store should be cleared when this
	// flag is true. See ClassifyRefreshError for the exact rule.
	Rejection bool `json:"rejection"`
	// StatusCode is the HTTP status from the response, 0 when the error
	// was a transport failure (no response at all). Negative values are
	// reserved for the transport-failure classification (see IsTransient).
	StatusCode int `json:"status_code"`
	// OAuthCode is the JSON `error` field returned by the token endpoint
	// (e.g. "invalid_grant", "server_error", "temporarily_unavailable").
	// Empty when no JSON body was available or the body did not parse.
	OAuthCode string `json:"oauth_code"`
	// Endpoint is a short label for the call site that produced the error:
	// "refresh", "device_code", "device_token", "unclassified".
	Endpoint string `json:"endpoint"`
	// Cause is the wrapped original error so callers can dig deeper
	// (network errors etc.). For typed errors from the HTTP layer it
	// preserves the body bytes.
	Cause error `json:"-"`
}

func (e *AuthError) Error() string {
	if e == nil {
		return "<nil>"
	}
	var b strings.Builder
	b.WriteString("auth: ")
	if e.Rejection {
		b.WriteString("credentials rejected")
	} else {
		b.WriteString("auth failure")
	}
	if e.Endpoint != "" {
		b.WriteString(" (")
		b.WriteString(e.Endpoint)
		b.WriteString(")")
	}
	if e.StatusCode != 0 {
		fmt.Fprintf(&b, " status=%d", e.StatusCode)
	}
	if e.OAuthCode != "" {
		fmt.Fprintf(&b, " error=%q", e.OAuthCode)
	}
	if e.Cause != nil {
		fmt.Fprintf(&b, ": %v", e.Cause)
	}
	return b.String()
}

func (e *AuthError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// IsAuthRejected reports whether err carries a definitive rejection from
// the sign-in server. Returns true only when the error has been classified
// as a non-transient, terminal rejection (revoked token, invalidated
// enterprise account, etc.). Returns false for transport-level failures,
// 5xx, rate limits, and unknown errors — every one of those should leave
// the credential store alone.
func IsAuthRejected(err error) bool {
	var ae *AuthError
	if errors.As(err, &ae) {
		return ae.Rejection
	}
	return false
}

// IsTransientAuthError reports whether err looks like a transient
// (retryable) failure. Network errors are transient by default; HTTP
// 408, 429, every 5xx, and the OAuth `server_error` /
// `temporarily_unavailable` codes count too. Returns false only when
// the underlying type explicitly says the failure is terminal
// (i.e. IsAuthRejected == true). Unknown error shapes are treated as
// transient so the credential store stays intact and the next request
// can try again.
func IsTransientAuthError(err error) bool {
	var ae *AuthError
	if errors.As(err, &ae) {
		// Rejected-class errors are by definition not transient.
		if ae.Rejection {
			return false
		}
		// A typed error explicitly tagged non-transient.
		return !ae.Rejection
	}
	if err == nil {
		return false
	}
	// Untyped: best guess based on Go stdlib network error shapes so the
	// caller does not clear on a flaky network.
	if isNetworkError(err) {
		return true
	}
	return false
}

// isNetworkError reports whether err originates from the net stack:
// DNS resolution failure, connection refused, i/o timeout, broken pipe,
// etc. The check is intentionally conservative — anything that survives
// past this returns to the default "transient" path above when wrapped.
func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return true
		}
	}
	if errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.EHOSTUNREACH) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.ETIMEDOUT) ||
		errors.Is(err, syscall.EPIPE) {
		return true
	}
	// Fallback: substring probe against the most common stdlib strings
	// so callers don't have to import net for a one-liner check.
	// This is the documented "recognised" surface for cycle-225.
	msg := err.Error()
	for _, needle := range []string{
		"connection refused",
		"connection reset",
		"no such host",
		"i/o timeout",
		"network is unreachable",
		"broken pipe",
		"EOF", // a closed connection before the server could reply
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

// ClassifyRefreshError wraps err into *AuthError when the failure carries
// enough information to decide between "rejected" and "transient". It
// never invents a rejection on its own: an error with no parseable
// status / body returns nil, and the caller falls back to the
// "untyped → transient" rule.
//
// endpoint is a short label attached to the returned *AuthError so the
// log line at the call site can tell refresh failures from device-flow
// failures. Common values: "refresh", "device_token", "device_code".
//
// The function is best-effort: it does NOT itself clear credentials. The
// RefreshIfNeeded / device-flow callers do, when the returned *AuthError
// has Rejection == true.
func ClassifyRefreshError(err error) *AuthError {
	if err == nil {
		return nil
	}
	// Already classified: preserve the verdict.
	var existing *AuthError
	if errors.As(err, &existing) {
		return existing
	}
	// No structured info: we cannot distinguish rejected from transient.
	// The caller treats untyped as transient via IsTransientAuthError.
	ae := &AuthError{
		Endpoint: "refresh",
		Cause:    err,
	}
	status, oauthCode, body := parseRefreshError(err)
	if status == 0 && oauthCode == "" {
		return nil
	}
	ae.StatusCode = status
	ae.OAuthCode = oauthCode
	ae.Rejection = isRejection(status, oauthCode, body)
	return ae
}

// ClassifyDeviceError wraps an error from /device/code or /token into
// the same *AuthError type. Used by runLogin / device_flow for error
// reporting; we don't clear credentials from the device-flow path since
// the user has not signed in yet.
func ClassifyDeviceError(err error, endpoint string) *AuthError {
	if err == nil {
		return nil
	}
	var existing *AuthError
	if errors.As(err, &existing) {
		return existing
	}
	if endpoint == "" {
		endpoint = "device"
	}
	ae := &AuthError{
		Endpoint: endpoint,
		Cause:    err,
	}
	status, oauthCode, _ := parseRefreshError(err)
	ae.StatusCode = status
	ae.OAuthCode = oauthCode
	// Only `access_denied` and `expired_token` matter here, and both are
	// the user's call (denied / didn't approve in time) — NOT a stale
	// credential. We set Rejection=false on both; the caller's job is to
	// surface a "please try again" message.
	if oauthCode == "access_denied" || oauthCode == "expired_token" {
		// Permanent in the sense that retrying with the same device
		// code won't work, but it's not a credential rejection.
		ae.Rejection = false
	}
	return ae
}

// parseRefreshError pulls the status code, OAuth `error` code, and raw
// body out of an error returned by a refresh (see unified_config.go
// refreshTokenGrant).
//
// The refresh fails with a *oauth2.RetrieveError: the token endpoint's
// status and its RFC 6749 `error` field travel with the error, so both
// are read structurally. A transport failure (no response at all)
// carries neither.
//
// The body is decoded as JSON as a fallback for the endpoints that
// answer an error with a JSON body under a non-JSON content type:
// x/oauth2 only fills ErrorCode from a JSON content type, and the
// rejection table below needs the code.
func parseRefreshError(err error) (status int, oauthCode string, body []byte) {
	if err == nil {
		return 0, "", nil
	}
	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		// Transport-level failure: no status, no body.
		return 0, "", nil
	}
	if retrieve.Response != nil {
		status = retrieve.Response.StatusCode
	}
	body = retrieve.Body
	oauthCode = retrieve.ErrorCode
	if oauthCode == "" && len(body) > 0 && body[0] == '{' {
		// Try to decode as JSON {"error":"<code>","error_description":"..."}.
		var probe struct {
			Error            string `json:"error"`
			ErrorDescription string `json:"error_description"`
		}
		if json.Unmarshal(body, &probe) == nil {
			oauthCode = probe.Error
		}
	}
	return status, oauthCode, body
}

// isRejection is the single source of truth for the "this status + this
// OAuth code means credentials are definitively gone" decision. The
// logic is hand-rolled; it is intentionally stricter than
// retryableStatus in cmd/wagyu/api_error.go because that table also
// handles model-API errors that have nothing to do with token rejection.
func isRejection(status int, oauthCode string, body []byte) bool {
	switch status {
	case 401, 403:
		// 401/403 from the token endpoint is always definitive.
		return true
	case 400:
		// 400 with a structured OAuth error code: only the credential
		// rejection codes count. "invalid_request",
		// "unsupported_grant_type", "invalid_scope" are programmer
		// errors and stay retriable.
		switch oauthCode {
		case "invalid_grant",
			"invalid_client",
			"unauthorized_client",
			"account_disabled",
			"account_locked",
			"revoked":
			return true
		}
		// 400 with no parseable error field: treat as definitive, since
		// the token endpoint never returns 200 with a non-JSON body and
		// 400 without a body almost always means the request itself was
		// permanently malformed for this client_id.
		if oauthCode == "" && len(body) > 0 {
			return false
		}
	case 408, 429:
		return false
	}
	if status >= 500 && status <= 599 {
		return false
	}
	return false
}

// ClearTokenUnified deletes the on-disk OAuth tokens at every path
// AllTokenPaths reports, plus the OS keychain entry. It is safe to call
// when no token file exists — the missing-file errors are swallowed so
// the caller (runLogin's sign-out hook, RefreshIfNeeded on rejection,
// etc.) can use the function as an idempotent "ensure no credentials are
// cached" primitive.
//
// Returns the count of token paths that were successfully removed. A
// count of 0 is normal on a freshly-installed machine; the caller does
// not need to flag it as an error.
func ClearTokenUnified() (int, error) {
	paths := AllTokenPaths()
	cleared := 0
	var firstErr error
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			if firstErr == nil {
				firstErr = fmt.Errorf("clear %s: %w", p, err)
			}
			continue
		}
		cleared++
	}
	// Best-effort keychain purge. Errors here are non-fatal because
	// the next save will overwrite the same entry on macOS / Linux /
	// Windows, and "no entry" is the expected steady state after a
	// successful clear.
	_ = ClearKeychainToken()
	return cleared, firstErr
}
