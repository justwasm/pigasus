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
package apierror

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
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
	// KindPrepaidBalanceExhausted marks a Gemini Enterprise account whose
	// organization prepaid wallet is empty — the canonical user-facing
	// message is PrepaidBalanceMessage.
	//
	// Unlike KindAuthExpired this kind cannot be derived from the status
	// alone: it is carried by a structured `google.rpc.ErrorInfo` detail
	// (reason PREPAID_BALANCE_EXHAUSTED) on an otherwise ordinary 429/402
	// quota response (see IsPrepaidBalanceExhausted).
	KindPrepaidBalanceExhausted
)

// AuthExpiredMessage is the verbatim user-facing string the 1.3.3 binary
// emits when a 401 surfaces mid-session. The trailing period is part of the
// recovered literal; do not edit without re-deriving it from a fresh probe.
const AuthExpiredMessage = "Authentication expired. Please log back in."

// PrepaidBalanceMessage is the verbatim user-facing string AGY 1.5.17 emits
// when a Gemini Enterprise account's organization prepaid wallet is empty.
//
// AGY 1.5.17:
//
//	"Fixed turns on Gemini Enterprise accounts with an empty prepaid wallet
//	 retrying repeatedly and reporting `You have exhausted your quota on this
//	 model.`; the CLI now stops right away and tells you `Your organization's
//	 prepaid balance is insufficient. Contact your administrator to add
//	 funds.`"
//
// The sentence is the recovered literal from the same release's web bundle,
// whose notice for the same condition (PrepaidBalanceNotice) is the title
// above the body "Your organization has run out of Antigravity funds."
const PrepaidBalanceMessage = "Your organization's prepaid balance is insufficient. Contact your administrator to add funds."

// PrepaidBalanceNotice is the notice the Remote Control browser view shows
// for the same condition. The 1.5.17 web bundle renders it as the title of
// a non-dismissible warning:
//
//	{icon: "warning", title: "Organization prepaid wallet is empty",
//	 message: "Your organization has run out of Antigravity funds. …",
//	 dismissible: false}
//
// The title is the recovered literal; the browser view reuses it verbatim.
const PrepaidBalanceNotice = "Organization prepaid wallet is empty"

// prepaidBalanceReason is the `reason` of the `google.rpc.ErrorInfo` detail
// the reference matches. It is an enum-shaped token (upper snake case), not
// prose, so the comparison is case-insensitive on the whole token and not a
// substring scan over the body: a message that merely mentions a prepaid
// balance is not this condition.
//
// Recovered from the 1.5.17 web bundle, which classifies the failure as:
//
//	const oza = z.object({error: z.object({message: z.string(),
//	  details: z.array(z.object({"@type": z.string(),
//	    reason: z.string().optional(),
//	    metadata: z.object({…}).optional()}))})});
//	function hq(a, b) { … for (let c of a.data.error.details)
//	  if (c?.["@type"] === "type.googleapis.com/google.rpc.ErrorInfo") … }
//	function rza(a) { return hq(a, b => b.reason === "PREPAID_BALANCE_EXHAUSTED") === true }
//
// Note that the reference checks this *before* it looks at errorCode === 429,
// which is why the condition is not status-gated here either: an enterprise
// wallet failure that arrives with a non-429 status is exactly the case the
// old status-gated quota rule missed and retried.
const prepaidBalanceReason = "PREPAID_BALANCE_EXHAUSTED"

// errorInfoType is the `@type` that marks a Google RPC ErrorInfo detail.
const errorInfoType = "type.googleapis.com/google.rpc.ErrorInfo"

// containsFold reports whether the lower-case ASCII needle occurs in body,
// comparing case-insensitively without allocating. The needle is a constant
// ASCII token, so a byte-at-a-time fold is enough and keeps this cheap enough
// for the transport's error-construction path.
func containsFold(body []byte, needle string) bool {
	n := len(needle)
	if n == 0 {
		return true
	}
	first := lowerASCII(needle[0])
	for i := 0; i+n <= len(body); i++ {
		if lowerASCII(body[i]) != first {
			continue
		}
		match := true
		for j := 1; j < n; j++ {
			if lowerASCII(body[i+j]) != lowerASCII(needle[j]) {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

// lowerASCII folds an ASCII letter to lower case and leaves every other byte
// alone, which is what containsFold needs.
func lowerASCII(b byte) byte {
	if b >= 'A' && b <= 'Z' {
		return b + ('a' - 'A')
	}
	return b
}

// errorInfoDetail is one entry of a Google RPC `error.details` array. Only
// the two fields the classification reads are modelled; an unknown detail
// contributes nothing.
type errorInfoDetail struct {
	Type   string `json:"@type"`
	Reason string `json:"reason"`
}

// errorInfo holds the `reason` values of the response body's ErrorInfo
// details. The parse is deliberately narrow — an unparsable body, a non-JSON
// body, or a body with no details yields an empty list, which classifies as
// "not this condition" rather than as an error.
func (e *HTTPError) errorInfo() []errorInfoDetail {
	body := bytes.TrimSpace(e.Body)
	if len(body) == 0 || (body[0] != '{' && body[0] != '[') {
		return nil
	}
	// Cheap gate before the parse: this runs for every 429/5xx the transports
	// build, and the detail token is a fixed ASCII string, so a case-folded
	// byte scan settles the common case without allocating a decoder or a
	// lowered copy of the body.
	if !containsFold(body, "prepaid_balance_exhausted") {
		return nil
	}
	var v struct {
		Error struct {
			Details []errorInfoDetail `json:"details"`
		} `json:"error"`
		Details []errorInfoDetail `json:"details"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		return nil
	}
	if len(v.Error.Details) > 0 {
		return v.Error.Details
	}
	return v.Details
}

// IsPrepaidBalanceExhausted reports whether this error is the Gemini
// Enterprise "organization prepaid wallet is empty" condition.
//
// The signal is the structured `google.rpc.ErrorInfo` detail with
// `reason: "PREPAID_BALANCE_EXHAUSTED"`. It is *not* a keyword scan: the
// upstream message for this failure is the ordinary quota sentence
// ("You have exhausted your quota on this model."), so the body text cannot
// tell the two apart.
//
// The check is deliberately not gated on a status code. The reference
// evaluates the detail before it branches on the status (see
// prepaidBalanceReason), and the old status-gated quota rule is what let this
// condition retry: a 429 that carried only the generic quota sentence tripped
// the keyword list, but a prepaid-wallet failure that arrived with any other
// status fell through to the retry loop.
func (e *HTTPError) IsPrepaidBalanceExhausted() bool {
	if e == nil {
		return false
	}
	for _, d := range e.errorInfo() {
		if d.Type != errorInfoType {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(d.Reason), prepaidBalanceReason) {
			return true
		}
	}
	return false
}

// IsPrepaidBalanceExhaustedErr walks err's chain via errors.As looking for an
// *HTTPError and reports whether it carries the enterprise prepaid-wallet
// condition. Mirrors IsQuotaReachedErr so callers in the transports and the
// turn loop can use the same idiom.
func IsPrepaidBalanceExhaustedErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.IsPrepaidBalanceExhausted()
}

// PrepaidBalanceMessageErr returns the canonical prepaid-wallet sentence when
// err carries the condition, or "" otherwise. Callers that own a user-facing
// surface use this so the wording lives in one place.
func PrepaidBalanceMessageErr(err error) string {
	if !IsPrepaidBalanceExhaustedErr(err) {
		return ""
	}
	return PrepaidBalanceMessage
}

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
	Header     http.Header
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
//
// A body-carried kind (KindPrepaidBalanceExhausted) wins over the status-only
// classification when the field was not set explicitly: the status cannot
// express it, and the response body the receiver already holds can.
func (e *HTTPError) KindError() ErrorKind {
	if e == nil {
		return KindUnknown
	}
	if e.Kind != KindUnknown {
		return e.Kind
	}
	if e.IsPrepaidBalanceExhausted() {
		return KindPrepaidBalanceExhausted
	}
	return classifyStatus(e.StatusCode)
}

// Error keeps the historical one-line shape so existing callers and logs are
// unchanged; Message() is the extracted, user-facing form.
func (e *HTTPError) Error() string {
	return fmt.Sprintf("gateway: upstream %d: %s", e.StatusCode, e.Message())
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
	if e.IsPrepaidBalanceExhausted() {
		return PrepaidBalanceMessage
	}
	if msg := vertexModelAccessMessage(e.StatusCode, body); msg != "" {
		return msg
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

// VertexModelAccessMessage is the reference's own sentence for the Vertex
// AI model-not-found / no-access failure.
//
// AGY 1.5.3: "Changed the error shown when Vertex AI reports that the
// selected model does not exist or that your Google Cloud project cannot
// use it, which now reads `The requested model was not found, or your
// project does not have access to it.` instead of the raw API error."
const VertexModelAccessMessage = "The requested model was not found, or your project does not have access to it."

// vertexModelAccessMessage recognises Vertex AI's own publisher-model
// failure. The phrase "publisher model" is Vertex-specific, so the
// mapping does not swallow an unrelated 404 from another provider.
func vertexModelAccessMessage(status int, body []byte) string {
	if status < 400 {
		return ""
	}
	low := strings.ToLower(string(body))
	if !strings.Contains(low, "publisher model") {
		return ""
	}
	if strings.Contains(low, "not found") ||
		strings.Contains(low, "does not have access") ||
		strings.Contains(low, "cannot use") ||
		strings.Contains(low, "cannot be used") {
		return VertexModelAccessMessage
	}
	return ""
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

// quotaKeywords are the lower-case substrings that tag a response as an
// exhausted quota, spend cap, or prepaid balance error.
// AGY 1.3.6 added "quota". AGY 1.5.10 added "spend cap", "prepaid balance", and "weekly quota".
var quotaKeywords = []string{
	"quota",
	"spend cap",
	"prepaid balance",
	"weekly quota",
}

// IsQuotaReached reports whether this error is the AGY "individual quota reached",
// spend cap exhausted, prepaid balance exhausted, or weekly quota error.
// The check is status 429, 402, or 403 plus a body / message that carries
// any of the quotaKeywords substrings.
//
// Changelog 1.3.3: "reaching your individual quota is reported right away
// instead of being retried"
// Changelog 1.5.10: "Upstream errors indicating an exhausted spend cap, prepaid
// balance, or weekly quota now abort immediately instead of retrying."
func (e *HTTPError) IsQuotaReached() bool {
	if e == nil {
		return false
	}
	if e.StatusCode != http.StatusTooManyRequests && e.StatusCode != http.StatusPaymentRequired && e.StatusCode != http.StatusForbidden {
		return false
	}
	bodyLower := bytes.ToLower(bytes.TrimSpace(e.Body))
	for _, kw := range quotaKeywords {
		if bytes.Contains(bodyLower, []byte(kw)) {
			return true
		}
	}
	// The Message() helper already strips the JSON `error.message` /
	// top-level `message` field; fall back to it so a body the JSON
	// parser rejected (or a future shape) still trips the rule as long as
	// the user-facing message mentions any of the quota keywords.
	msgLower := strings.ToLower(e.Message())
	for _, kw := range quotaKeywords {
		if strings.Contains(msgLower, kw) {
			return true
		}
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

// IsRetryableStatus reports whether this HTTP error is considered retryable
// (429 rate limit or 5xx server error, unless personal quota is exhausted).
// AGY 1.5.0: "Fixed certain models failing a turn on rate-limit (429) and
// server (5xx) errors instead of retrying them, now honoring the server's
// Retry-After header".
//
// AGY 1.5.17: an empty organization prepaid wallet is never retryable. The
// failure can arrive as an ordinary 429, so the status alone would mark it
// retryable and the turn would spend its budget on retries that cannot
// succeed; the ErrorInfo detail is what distinguishes it.
func (e *HTTPError) IsRetryableStatus() bool {
	if e == nil {
		return false
	}
	if e.IsPrepaidBalanceExhausted() {
		return false
	}
	if e.IsQuotaReached() {
		return false
	}
	return e.StatusCode == http.StatusTooManyRequests || (e.StatusCode >= 500 && e.StatusCode <= 599) || e.StatusCode == http.StatusRequestTimeout
}

// IsRetryableStatusErr walks err's chain via errors.As looking for an
// *HTTPError and reports whether it represents a retryable 429 or 5xx status.
func IsRetryableStatusErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.IsRetryableStatus()
}

// RetryAfterDuration returns the delay specified by the Retry-After header,
// or parsed from Google RPC RetryInfo in the response body if present.
// Returns 0 if none is specified or invalid.
func (e *HTTPError) RetryAfterDuration() time.Duration {
	if e == nil {
		return 0
	}
	if e.Header != nil {
		if raw := strings.TrimSpace(e.Header.Get("Retry-After")); raw != "" {
			if sec, err := strconv.Atoi(raw); err == nil && sec >= 0 {
				return time.Duration(sec) * time.Second
			}
			if t, err := http.ParseTime(raw); err == nil {
				if d := time.Until(t); d > 0 {
					return d
				}
			}
		}
	}
	// Fallback to Google RPC RetryInfo in JSON body:
	// {"error":{"details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","retryDelay":"1.5s"}]}}
	if len(e.Body) > 0 && bytes.Contains(e.Body, []byte("retryDelay")) {
		var payload struct {
			Error struct {
				Details []struct {
					Type       string `json:"@type"`
					RetryDelay any    `json:"retryDelay"`
				} `json:"details"`
			} `json:"error"`
		}
		if err := json.Unmarshal(e.Body, &payload); err == nil {
			for _, d := range payload.Error.Details {
				if strings.Contains(d.Type, "RetryInfo") {
					switch v := d.RetryDelay.(type) {
					case string:
						if dur, err := time.ParseDuration(v); err == nil && dur > 0 {
							return dur
						}
					case map[string]any:
						sec, _ := v["seconds"].(float64)
						nanos, _ := v["nanos"].(float64)
						total := time.Duration(sec)*time.Second + time.Duration(nanos)*time.Nanosecond
						if total > 0 {
							return total
						}
					}
				}
			}
		}
	}
	return 0
}

// RetryAfterDurationErr walks err's chain via errors.As looking for an
// *HTTPError and returns its RetryAfterDuration.
func RetryAfterDurationErr(err error) time.Duration {
	if err == nil {
		return 0
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return 0
	}
	return he.RetryAfterDuration()
}

// accountDisabledKeywords are substrings that identify an account as being
// disabled, suspended, or flagged for Terms of Service violations by Google.
var accountDisabledKeywords = []string{
	"service has been disabled",
	"account has been disabled",
	"account_disabled",
	"account is suspended",
	"account has been suspended",
	"account_suspended",
	"account_banned",
	"user_disabled",
	"terms of service",
	"terms_of_service_violation",
	"violation of terms of service",
	"violation of the terms of service",
	"tos_violation",
	"policy_violation",
	"abuse_detected",
}

var reAppealURL = regexp.MustCompile(`https?://[^\s"'<>]+appeal[^\s"'<>]*|https?://support\.google\.com/[^\s"'<>]+`)

// AppealURL extracts an account appeal URL from the upstream error body, if present.
func AppealURL(err error) string {
	if err == nil {
		return ""
	}
	var he *HTTPError
	var body string
	if errors.As(err, &he) {
		body = string(he.Body)
	} else {
		body = err.Error()
	}
	url := reAppealURL.FindString(body)
	url = strings.ReplaceAll(url, `\u0026`, "&")
	url = strings.TrimRight(url, `.,;\)`)
	return url
}

// IsAccountDisabled reports whether this HTTPError represents an account
// service disabled or Terms of Service violation from upstream.
func (e *HTTPError) IsAccountDisabled() bool {
	if e == nil {
		return false
	}
	lowerBody := bytes.ToLower(bytes.TrimSpace(e.Body))
	for _, kw := range accountDisabledKeywords {
		if bytes.Contains(lowerBody, []byte(kw)) {
			return true
		}
	}
	lowerMsg := strings.ToLower(e.Message())
	for _, kw := range accountDisabledKeywords {
		if strings.Contains(lowerMsg, kw) {
			return true
		}
	}
	return false
}

// IsAccountDisabledErr walks err's chain via errors.As looking for an
// *HTTPError or inspects err.Error() for Terms of Service / disabled keywords.
func IsAccountDisabledErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		if he.IsAccountDisabled() {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range accountDisabledKeywords {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// AccountDisabledReason returns a human-readable explanation if err
// represents an account disabled / ToS error, or empty string otherwise.
func AccountDisabledReason(err error) string {
	if !IsAccountDisabledErr(err) {
		return ""
	}
	return "Terms of Service violation / account service disabled"
}

// IsAuthFailure reports whether an error represents an authentication failure
// (401 Unauthorized, unauthenticated, invalid_grant, or revoked token).
func (e *HTTPError) IsAuthFailure() bool {
	if e == nil {
		return false
	}
	if e.StatusCode == http.StatusUnauthorized || e.Kind == KindAuthExpired {
		return true
	}
	low := strings.ToLower(string(e.Body))
	return strings.Contains(low, "unauthenticated") ||
		strings.Contains(low, "invalid_grant") ||
		strings.Contains(low, "token has been expired or revoked") ||
		strings.Contains(low, "auth failure") ||
		strings.Contains(low, "refresh failed") ||
		strings.Contains(low, "stale token")
}

// IsAuthFailureErr reports whether err represents an authentication failure.
func IsAuthFailureErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.IsAuthFailure()
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unauthenticated") ||
		strings.Contains(msg, "invalid_grant") ||
		strings.Contains(msg, "token has been expired or revoked") ||
		strings.Contains(msg, "authentication expired") ||
		strings.Contains(msg, "auth failure") ||
		strings.Contains(msg, "refresh failed") ||
		strings.Contains(msg, "stale token")
}

// IsConnectionDroppedErr reports whether err represents a dropped transport connection.
func IsConnectionDroppedErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "connection dropped") ||
		(strings.Contains(msg, "connection to ") && strings.Contains(msg, "dropped:")) ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "connection reset by peer") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "operation timed out") ||
		strings.Contains(msg, "connection timed out") ||
		strings.Contains(msg, "unexpected eof")
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
// wrapped many times (a turn loop wraps the provider error in
// "open stream: %w"; the print path may wrap it again); the As walk
// handles every layer. Mirrors IsQuotaReachedErr verbatim so callers
// can use the same idiom.
//
// Note the production callers test the method form instead —
// (*HTTPError).IsScopeMismatch at internal/core/apierror/api_error.go
// (the hint appending) and internal/app/agentturn/local.go (the
// turn-error text) — because they already hold the *HTTPError from
// their own errors.As. This helper's only callers are its tests.
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

var modelAccessKeywords = []string{
	"publisher model",
	"does not have access",
	"cannot use",
	"cannot be used",
	"not supported",
	"not available",
	"ineligible",
	"permission_denied",
	"model_not_found",
	"model not found",
}

// IsModelAccess reports whether this HTTPError indicates the current account
// or project lacks access/eligibility for the requested model (403/404, or 400
// with model-access keywords), excluding quota/auth/disabled errors.
func (e *HTTPError) IsModelAccess() bool {
	if e == nil || e.IsQuotaReached() || e.IsAccountDisabled() || e.IsAuthFailure() {
		return false
	}
	if e.StatusCode == http.StatusForbidden || e.StatusCode == http.StatusNotFound {
		return true
	}
	if e.StatusCode == http.StatusBadRequest {
		low := strings.ToLower(string(e.Body) + " " + e.Message())
		for _, kw := range modelAccessKeywords {
			if strings.Contains(low, kw) {
				return true
			}
		}
	}
	return false
}

// IsModelAccessErr walks err's chain via errors.As looking for an *HTTPError
// and reports whether it indicates a model-access / eligibility failure.
func IsModelAccessErr(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if !errors.As(err, &he) {
		return false
	}
	return he.IsModelAccess()
}

// FormatRetryReason returns a concise, single-line summary of err suitable for
// displaying alongside a retry countdown in the TUI.
func FormatRetryReason(err error) string {
	if err == nil {
		return ""
	}
	var raw string
	var he *HTTPError
	if errors.As(err, &he) {
		msg := strings.TrimSpace(he.Message())
		if msg != "" && !strings.EqualFold(msg, http.StatusText(he.StatusCode)) {
			raw = fmt.Sprintf("HTTP %d: %s", he.StatusCode, msg)
		} else if he.StatusCode > 0 {
			raw = fmt.Sprintf("HTTP %d %s", he.StatusCode, http.StatusText(he.StatusCode))
		}
	}
	if raw == "" {
		raw = err.Error()
	}
	raw = strings.Join(strings.Fields(raw), " ")
	const maxRunes = 72
	runes := []rune(raw)
	if len(runes) > maxRunes {
		return strings.TrimSpace(string(runes[:maxRunes-3])) + "..."
	}
	return raw
}
