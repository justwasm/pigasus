// http_error.go — the structured form of a non-2xx model-API response.
//
// The headless (`-p`) path reports a turn that dies on an agent or model API
// failure as a structured `AGY_ERROR: {...}` line on stderr plus exit code 3.
// Changelog 1.3.3: "the CLI now prints a structured `AGY_ERROR: {...}` JSON line
// on stderr with canonical status, HTTP or gRPC error code, retryability, and
// error ID". To do that the transport has to hand up the *raw* status and body
// rather than a pre-formatted string, which is what HTTPError is for.
//
// The body-to-message rules are recovered too:
//
//	Improved error messages: a request that fails with an HTML error page, for
//	example from a corporate proxy, now shows the page title instead of the raw
//	HTML, other long non-JSON error bodies are cut off after 500 bytes, and
//	reaching your individual quota is reported right away instead of being
//	retried.
//
// `... (truncated)` is a literal in the 1.3.6 string table.
//
// Changelog 1.3.3 line 47: "Fixed an expired or revoked sign-in showing an
// unclear error mid-session; the agent now says
// `Authentication expired. Please log back in.`, and a token refresh that was
// already in flight no longer restores the expired credentials." The Kind
// field below lets the user-facing formatter branch on 401 without parsing
// the body; AuthExpiredMessage is the verbatim canonical string.
package backend

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
)

// OAuthScopeHintMessage is the canonical user-facing hint the cycle
// 263 close-gap appends when an OAuth response is rejected for
// missing scopes. The full sentence is the verbatim string the user
// sees after the upstream message; do not reformat it without
// re-deriving the canonical form from a fresh AGY capture.
//
// AGY 1.4.x changelog (`internal/cli/testdata/changelog_reference.txt`
// line 271):
//
//	"Improved authentication error messages when OAuth tokens lack
//	required scopes by including explicit instructions to run `/logout`
//	and `/login`."
//
// The hint starts with the token "Run `/logout`" so a downstream
// user-facing formatter can grep for the leading token and slice
// it back off if it must reuse the message separately.
const OAuthScopeHintMessage = "Run `/logout` then `/login` to refresh your OAuth grant with the required scopes."

// MaxErrorBody is the recovered cap for a non-JSON error body. Changelog:
// "other long non-JSON error bodies are cut off after 500 bytes".
const MaxErrorBody = 500

// ErrorKind classifies a non-2xx response into the small set of cases the
// user-facing formatter branches on. The default for an unrecognised status
// is `KindUnknown`, which leaves the existing JSON / HTML title / truncated
// rules in Message() to do their work.
//
// Cycle auth-expired-message: 401 carries `KindAuthExpired` so the
// cmd/wagyu api_error formatter renders the verbatim "Authentication
// expired. Please log back in." string instead of the raw upstream body
// (which the recovered 1.3.3 binary suppresses as "unclear").
type ErrorKind int

const (
	// KindUnknown is the zero value. The existing body-shape rules apply.
	KindUnknown ErrorKind = iota
	// KindAuthExpired marks a 401 that represents an expired or revoked
	// sign-in — the canonical user-facing message is AuthExpiredMessage.
	KindAuthExpired
)

// AuthExpiredMessage is the verbatim user-facing string the 1.3.3 binary
// emits when a 401 surfaces mid-session. The trailing period is part of the
// recovered literal; do not edit without re-deriving it from a fresh probe.
const AuthExpiredMessage = "Authentication expired. Please log back in."

// HTTPError is a non-2xx response from a model API endpoint. It keeps the raw
// body so the headless error reporter can extract the user-facing message and
// classify the status.
//
// Kind is set by the constructor path; defaults to KindUnknown so existing
// call sites that build &HTTPError{StatusCode: ...} keep their semantics.
// The `Kind()` method below is the canonical accessor — when Kind has not
// been set explicitly it falls back to classifying on StatusCode, so a
// 401 returned by an upstream that did not use NewHTTPError still surfaces
// as KindAuthExpired.
type HTTPError struct {
	StatusCode int
	Body       []byte
	URL        string
	Kind       ErrorKind
}

// NewHTTPError constructs an HTTPError with the Kind field populated from
// StatusCode. Pre-existing `&HTTPError{StatusCode: …}` literals keep their
// zero-value Kind; callers that want auto-classification should switch to
// this constructor.
func NewHTTPError(statusCode int, body []byte, url string) *HTTPError {
	return &HTTPError{
		StatusCode: statusCode,
		Body:       body,
		URL:        url,
		Kind:       classifyStatus(statusCode),
	}
}

// classifyStatus maps an HTTP status onto the ErrorKind. Only 401 has a
// distinct user-facing branch today; everything else stays KindUnknown so
// Message()'s existing rules apply.
func classifyStatus(code int) ErrorKind {
	if code == http.StatusUnauthorized {
		return KindAuthExpired
	}
	return KindUnknown
}

// Kind returns the error kind, populating it lazily from StatusCode when
// the caller left the field at its zero value. Safe to call repeatedly.
func (e *HTTPError) KindError() ErrorKind {
	if e == nil {
		return KindUnknown
	}
	if e.Kind != KindUnknown {
		return e.Kind
	}
	return classifyStatus(e.StatusCode)
}

// Error keeps the historical one-line shape so existing callers and logs are
// unchanged; Message() is the extracted, user-facing form.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("agw: upstream %d: %s", e.StatusCode, e.Message())
}

// Message is the user-facing message for the response body, in the reference's
// order of preference:
//
//  1. a JSON body contributes its `error.message`, then `message`;
//  2. an HTML body contributes its <title>;
//  3. anything else is used verbatim, cut to MaxErrorBody bytes with the
//     recovered `... (truncated)` marker.
//
// An empty body falls back to the status text.
func (e *HTTPError) Message() string {
	body := bytes.TrimSpace(e.Body)
	if len(body) == 0 {
		return http.StatusText(e.StatusCode)
	}
	if body[0] == '{' || body[0] == '[' {
		var v struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(body, &v); err == nil {
			if v.Error.Message != "" {
				return v.Error.Message
			}
			if v.Message != "" {
				return v.Message
			}
		}
	}
	if LooksHTML(body) {
		if title := HTMLTitle(body); title != "" {
			return title
		}
	}
	return truncateErrorBody(body)
}

// truncateErrorBody cuts a body to MaxErrorBody bytes and appends the recovered
// marker. The marker is not counted against the cap.
func truncateErrorBody(body []byte) string {
	s := strings.ToValidUTF8(string(body), "")
	if len(s) <= MaxErrorBody {
		return s
	}
	return s[:MaxErrorBody] + "... (truncated)"
}

// HTMLTitle returns the contents of the first <title> element, trimmed, or ""
// when the body carries none. Matching is case-insensitive; the recovered
// literals are `<title>` and `</title>`.
func HTMLTitle(body []byte) string {
	lower := bytes.ToLower(body)
	open := bytes.Index(lower, []byte("<title>"))
	if open < 0 {
		return ""
	}
	rest := body[open+len("<title>"):]
	lowerRest := bytes.ToLower(rest)
	close := bytes.Index(lowerRest, []byte("</title>"))
	if close < 0 {
		return ""
	}
	return strings.TrimSpace(string(rest[:close]))
}

// LooksHTML reports whether body starts with a doctype / <html tag (after
// trimming leading whitespace). The check is intentionally loose: anything
// that a corporate proxy is likely to return on a 5xx — "<!DOCTYPE html …",
// "<html …", "<HTML>…", etc. — should be classified as HTML so the
// <title>-extraction path runs. The full HTMLTitle call decides whether
// there is actually a <title> to extract.
func LooksHTML(body []byte) bool {
	b := bytes.TrimSpace(body)
	if len(b) == 0 {
		return false
	}
	low := bytes.ToLower(b)
	if bytes.HasPrefix(low, []byte("<!doctype")) {
		return true
	}
	return bytes.HasPrefix(low, []byte("<html"))
}

// quotaKeyword is the lower-case substring that tags a response as the
// "individual quota reached" case. The recovered AGY 1.3.6 capture
// (`{"error":{"message":"Individual quota reached. …"}}`, status 429)
// carries this token; we look for it case-insensitively over the body so
// non-canonical wrappers ("Your personal quota has been reached", etc.)
// still trip the rule.
const quotaKeyword = "quota"

// IsQuotaReached reports whether this error is the AGY "individual quota
// reached" case. The check is status 429 plus a body / message that
// carries the quotaKeyword substring. The status guard prevents a 200
// with the word "quota" in a legitimate payload from being misclassified.
//
// Changelog 1.3.3: "reaching your individual quota is reported right away
// instead of being retried" — this method is the predicate the auto-
// continue layers in internal/agent and internal/pigrt read to short-
// circuit their retry loops.
func (e *HTTPError) IsQuotaReached() bool {
	if e == nil || e.StatusCode != 429 {
		return false
	}
	if bytes.Contains(bytes.ToLower(bytes.TrimSpace(e.Body)), []byte(quotaKeyword)) {
		return true
	}
	// The Message() helper already strips the JSON `error.message` /
	// top-level `message` field; fall back to it so a body the JSON
	// parser rejected (or a future shape) still trips the rule as long as
	// the user-facing message mentions quota.
	if msg := strings.ToLower(e.Message()); strings.Contains(msg, quotaKeyword) {
		return true
	}
	return false
}

// IsQuotaReachedErr walks err's chain via errors.As looking for an
// *HTTPError, and reports whether it represents the "individual quota
// reached" case. Returns false for nil errs, non-API failures, and any
// HTTPError that does not trip IsQuotaReached. The error chain may be
// wrapped many times (agent.Loop wraps the provider error in
// "agent: open stream: %w"; the print path may wrap it again); the As
// walk handles every layer.
func IsQuotaReachedErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.IsQuotaReached()
}

// scopeKeywords is the case-insensitive substring list that tags an
// HTTP 403 response as the "OAuth scope mismatch" case. The keywords
// are the four shapes the recovered 1.4.x strings + probe bodies show:
//
//	insufficient_scope  — RFC 6750 §3.1 error code as OAuth providers emit it
//	missing scope       — Anthropic-style "missing scope: models.read"
//	requires scope      — generic "endpoint requires scope: …"
//	scope required      — alternate wording some proxies use
//
// All four collapse to a single substring scan over the lower-cased
// body. The status-403 guard ensures a 200 OK that happens to mention
// "scope" in a non-error payload (e.g. a session-create response that
// echoes the requested scope) never trips the rule.
var scopeKeywords = []string{
	"insufficient_scope",
	"missing scope",
	"requires scope",
	"scope required",
}

// IsScopeMismatch reports whether this HTTPError is the AGY "OAuth
// token lacks required scopes" case. The check is status 403 plus a
// body / message that carries one of the recovered scope keywords
// (case-insensitive).
//
// AGY 1.4.x changelog (`internal/cli/testdata/changelog_reference.txt`
// line 271): "Improved authentication error messages when OAuth
// tokens lack required scopes by including explicit instructions to
// run `/logout` and `/login`." This predicate is what the headless
// error reporter and the TUI error surface read to decide whether
// the OAuthScopeHintMessage should be appended.
//
// Unlike IsQuotaReached, this method is NOT projected onto a static
// status kind — three out of four shapes are status 403 *with* a
// keyword, but a 403 alone (e.g. an IP-allowlist denial or a model
// rate-limit) is not a scope mismatch and must not trigger the hint.
// Callers should always use this method, never `Kind == KindOAuthScopeMismatch`
// (no such kind is registered; see the note on classifyStatus).
func (e *HTTPError) IsScopeMismatch() bool {
	if e == nil || e.StatusCode != http.StatusForbidden {
		return false
	}
	lowerBody := bytes.ToLower(bytes.TrimSpace(e.Body))
	for _, kw := range scopeKeywords {
		if bytes.Contains(lowerBody, []byte(kw)) {
			return true
		}
	}
	// Fall back to Message(): a body the JSON parser rejected (or a
	// future shape) still trips the rule as long as the user-facing
	// message mentions a scope keyword — same defensive pattern as
	// IsQuotaReached.
	lowerMsg := strings.ToLower(e.Message())
	for _, kw := range scopeKeywords {
		if strings.Contains(lowerMsg, kw) {
			return true
		}
	}
	return false
}

// IsScopeMismatchErr walks err's chain via errors.As looking for an
// *HTTPError, and reports whether it represents the OAuth scope
// mismatch case. Returns false for nil errs, non-API failures, and any
// HTTPError that does not trip IsScopeMismatch. The error chain may be
// wrapped many times (agent.Loop wraps the provider error in
// "agent: open stream: %w"; the print path may wrap it again); the As
// walk handles every layer. Mirrors IsQuotaReachedErr verbatim so
// callers in internal/agent, internal/pigrt, and cmd/wagyu can use
// the same idiom.
func IsScopeMismatchErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.IsScopeMismatch()
}
