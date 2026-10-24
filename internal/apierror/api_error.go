// Package apierror implements structured headless error reporting.
//
// Changelog 1.3.3: "Improved headless (`-p` / `--prompt`) error reporting when a
// turn terminates on an agent or model API failure: the CLI now prints a
// structured `AGY_ERROR: {...}` JSON line on stderr with canonical status, HTTP
// or gRPC error code, retryability, and error ID (including HTTP status mapping
// for `GEMINI_API_KEY` SDK errors) and exits with code `3` instead of `1`."
package apierror

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// HeadlessAPIExitCode is the exit status the reference uses for a turn that
// terminates on an agent or model API failure. Changelog: "exits with code `3`
// instead of `1`".
const HeadlessAPIExitCode = 3

// APIError is the structured form of a headless model/agent API failure. The
// field order is the order the reference emits: `short_error` first, then
// `status`, `error_code`, `code_kind`, `retryable`, `error_id`.
type APIError struct {
	Message     string `json:"-"`
	ShortError  string `json:"short_error"`
	Status      string `json:"status"`
	ErrorCode   int    `json:"error_code"`
	CodeKind    string `json:"code_kind"`
	Retryable   bool   `json:"retryable"`
	ErrorID     string `json:"error_id"`
	Remediation string `json:"remediation,omitempty"`
}

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

var retryableStatus = map[int]bool{
	408: true,
	429: true,
	500: true,
	502: true,
	503: true,
	504: true,
}

func statusName(code int) string {
	if n, ok := httpStatusNames[code]; ok {
		return n
	}
	if code >= 500 {
		return "INTERNAL"
	}
	return "INVALID_ARGUMENT"
}

// ClassifyAPIError converts a turn error into the structured form the reference
// reports, or returns false when the failure is not a model API failure.
func ClassifyAPIError(err error) (*APIError, bool) {
	var he *backend.HTTPError
	if !errors.As(err, &he) {
		return nil, false
	}
	status := statusName(he.StatusCode)
	msg := he.Message()
	if he.KindError() == backend.KindAuthExpired {
		msg = backend.AuthExpiredMessage
	}
	out := &APIError{
		Message:    msg,
		ShortError: fmt.Sprintf("%s (code %d): %s", status, he.StatusCode, msg),
		Status:     status,
		ErrorCode:  he.StatusCode,
		CodeKind:   "http",
		Retryable:  retryableStatus[he.StatusCode],
		ErrorID:    NewErrorID(),
	}
	if he.IsScopeMismatch() {
		hint := backend.OAuthScopeHintMessage
		out.Remediation = hint
		out.Message = msg + " " + hint
		out.ShortError = fmt.Sprintf("%s (code %d): %s %s", status, he.StatusCode, msg, hint)
	}
	return out, true
}

// Lines renders the two stderr lines the reference emits, in order.
func (e *APIError) Lines() string {
	b, err := json.Marshal(e)
	if err != nil {
		return "error: " + e.Message
	}
	return "error: " + e.Message + "\nAGY_ERROR: " + string(b)
}

// NewErrorID generates a random error ID matching reference format.
func NewErrorID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "-1"
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
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

func newErrorID() string {
	return NewErrorID()
}
