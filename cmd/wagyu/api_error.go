// api_error.go — cycle 112: the recovered structured headless error report.
//
// Changelog 1.3.3: "Improved headless (`-p` / `--prompt`) error reporting when a
// turn terminates on an agent or model API failure: the CLI now prints a
// structured `AGY_ERROR: {...}` JSON line on stderr with canonical status, HTTP
// or gRPC error code, retryability, and error ID (including HTTP status mapping
// for `GEMINI_API_KEY` SDK errors) and exits with code `3` instead of `1`."
//
// Captured verbatim from 1.3.6 on this machine (quota exhausted, rc 3):
//
//	error: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.
//	AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): Individual quota reached. …","status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http","retryable":true,"error_id":"890cbab5-0640-47b4-8f5e-b3a650115e7a-1"}
//
// `AGY_ERROR: ` and `AGY_ERROR: {"short_error":%q}` are both literals in the
// 1.3.6 string table; the field names come from the `ShortError json:"short_error"`
// struct tag pair recovered alongside them.
package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// headlessAPIExitCode is the exit status the reference uses for a turn that
// terminates on an agent or model API failure. Changelog: "exits with code `3`
// instead of `1`".
const headlessAPIExitCode = 3

// apiError is the structured form of a headless model/agent API failure. The
// field order is the order the reference emits: `short_error` first, then
// `status`, `error_code`, `code_kind`, `retryable`, `error_id`. Cycle 263
// adds `remediation` (omitempty) after `error_id` — the field is only
// emitted when the typed HTTPError trips the OAuth scope-mismatch
// predicate, and stays absent otherwise so the canonical envelope
// shape does not change for existing failure modes.
type apiError struct {
	// Message is the user-facing text, emitted as the `error: <msg>` line and
	// reused inside short_error. It is not a JSON field. When the typed
	// HTTPError trips IsScopeMismatch (cycle 263), Message also carries the
	// appended OAuthScopeHintMessage — the user-visible `error:` line is
	// what the AGY 1.4.x changelog contract pins.
	Message string `json:"-"`

	ShortError  string `json:"short_error"`
	Status      string `json:"status"`
	ErrorCode   int    `json:"error_code"`
	CodeKind    string `json:"code_kind"`
	Retryable   bool   `json:"retryable"`
	ErrorID     string `json:"error_id"`
	Remediation string `json:"remediation,omitempty"`
}

// httpStatusNames maps an HTTP status onto the canonical status name the
// reference reports. Only 429 -> RESOURCE_EXHAUSTED is 【一手】 (the capture
// above); the rest follow the usual HTTP/gRPC correspondence and are 【推测】.
var httpStatusNames = map[int]string{
	400: "INVALID_ARGUMENT",
	401: "UNAUTHENTICATED",
	403: "PERMISSION_DENIED",
	404: "NOT_FOUND",
	408: "DEADLINE_EXCEEDED",
	409: "ABORTED",
	412: "FAILED_PRECONDITION",
	429: "RESOURCE_EXHAUSTED",
	499: "CANCELLED",
	500: "INTERNAL",
	501: "UNIMPLEMENTED",
	502: "UNAVAILABLE",
	503: "UNAVAILABLE",
	504: "DEADLINE_EXCEEDED",
}

// retryableStatus reports whether the reference considers an HTTP status worth
// retrying. The captured 429 is `retryable: true`; 408 and the 5xx family are
// the transient class the changelog describes ("transient `genai.APIError`
// failures (`502`, `503`, `504`, per-minute `429` rate limits, and mid-stream
// interruptions) automatically retry").
var retryableStatus = map[int]bool{
	408: true,
	429: true,
	500: true,
	502: true,
	503: true,
	504: true,
}

// statusName returns the canonical status name for an HTTP status, defaulting by
// class for codes the table does not list.
func statusName(code int) string {
	if n, ok := httpStatusNames[code]; ok {
		return n
	}
	if code >= 500 {
		return "INTERNAL"
	}
	return "INVALID_ARGUMENT"
}

// classifyAPIError converts a turn error into the structured form the reference
// reports, or returns false when the failure is not a model API failure (a
// usage error, a tool-argument error, …), in which case the caller keeps the
// ordinary exit path.
//
// Cycle auth-expired-message (AGY 1.3.3 line 47): when the backend returned a
// 401 mid-session the verbatim message "Authentication expired. Please log
// back in." replaces the upstream body. The HTTPError.Kind field carries
// that classification (KindAuthExpired); the headless formatter renders the
// canonical string regardless of what the upstream body actually contained.
//
// Cycle 263 (AGY 1.4.x changelog line 271): when the upstream body
// represents the "OAuth token lacks required scopes" case (status 403 +
// scope keyword), the canonical OAuthScopeHintMessage is appended to the
// user-facing Message + ShortError and a `remediation` field is added
// to the AGY_ERROR JSON envelope. The hint is verbatim and ends with a
// single period (matching the AGY 1.4.x string-table form).
func classifyAPIError(err error) (*apiError, bool) {
	var he *backend.HTTPError
	if !errors.As(err, &he) {
		return nil, false
	}
	status := statusName(he.StatusCode)
	msg := he.Message()
	if he.KindError() == backend.KindAuthExpired {
		// 401 surfaces as the verbatim canonical string. The recovered
		// literal ends with a single period — do not append another.
		msg = backend.AuthExpiredMessage
	}
	out := &apiError{
		Message:    msg,
		ShortError: fmt.Sprintf("%s (code %d): %s", status, he.StatusCode, msg),
		Status:     status,
		ErrorCode:  he.StatusCode,
		CodeKind:   "http",
		Retryable:  retryableStatus[he.StatusCode],
		ErrorID:    newErrorID(),
	}
	// Cycle 263: OAuth scope-mismatch case. The detection requires
	// BOTH status 403 AND a scope keyword in the body / message —
	// IsScopeMismatch owns the precision so a 403 from an
	// IP-allowlist denial or a model rate-limit does NOT trip the
	// hint. The remediation field is only set when the predicate
	// trips, so the AGY_ERROR envelope shape for every other failure
	// mode is unchanged (omitempty).
	if he.IsScopeMismatch() {
		hint := backend.OAuthScopeHintMessage
		out.Remediation = hint
		out.Message = msg + " " + hint
		out.ShortError = fmt.Sprintf("%s (code %d): %s %s", status, he.StatusCode, msg, hint)
	}
	return out, true
}

// Lines renders the two stderr lines the reference emits, in order. The
// dispatcher prints the returned message verbatim, so the leading `error: `
// belongs here.
func (e *apiError) Lines() string {
	b, err := json.Marshal(e)
	if err != nil {
		return "error: " + e.Message
	}
	return "error: " + e.Message + "\nAGY_ERROR: " + string(b)
}

// newErrorID returns the `error_id` value: an RFC 4122 v4 UUID with the
// reference's `-1` suffix (the captured value was
// `890cbab5-0640-47b4-8f5e-b3a650115e7a-1`). Falls back to a hex string when
// the system RNG is unavailable.
func newErrorID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "-1"
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	h := hex.EncodeToString(b[:])
	var sb strings.Builder
	sb.WriteString(h[0:8])
	sb.WriteByte('-')
	sb.WriteString(h[8:12])
	sb.WriteByte('-')
	sb.WriteString(h[12:16])
	sb.WriteByte('-')
	sb.WriteString(h[16:20])
	sb.WriteByte('-')
	sb.WriteString(h[20:32])
	sb.WriteString("-1")
	return sb.String()
}
