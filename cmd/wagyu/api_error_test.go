package main

// api_error_test.go — cycle 112: the recovered structured headless error report.
//
// The expected JSON below is copied from the 1.3.6 capture on this machine
// (`agy -p 'say hi'` with the plan quota exhausted):
//
//	error: Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s.
//	AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): Individual quota reached. …","status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http","retryable":true,"error_id":"890cbab5-…-1"}
//	rc 3

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

const refQuotaMessage = "Individual quota reached. Please upgrade your subscription to increase your limits. Resets in 89h28m34s."

func TestClassifyAPIError_QuotaCapture(t *testing.T) {
	err := fmt.Errorf("agent: open stream: %w", &backend.HTTPError{
		StatusCode: 429,
		Body:       []byte(`{"error":{"message":"` + refQuotaMessage + `"}}`),
	})
	api, ok := classifyAPIError(err)
	if !ok {
		t.Fatal("quota failure was not classified as an API error")
	}
	if api.Status != "RESOURCE_EXHAUSTED" || api.ErrorCode != 429 ||
		api.CodeKind != "http" || !api.Retryable {
		t.Errorf("classification = %+v", api)
	}
	want := "RESOURCE_EXHAUSTED (code 429): " + refQuotaMessage
	if api.ShortError != want {
		t.Errorf("short_error = %q, want %q", api.ShortError, want)
	}

	// The two stderr lines, with the error id pinned to the captured value.
	api.ErrorID = "890cbab5-0640-47b4-8f5e-b3a650115e7a-1"
	wantLines := "error: " + refQuotaMessage + "\n" +
		`AGY_ERROR: {"short_error":"RESOURCE_EXHAUSTED (code 429): ` + refQuotaMessage + `",` +
		`"status":"RESOURCE_EXHAUSTED","error_code":429,"code_kind":"http",` +
		`"retryable":true,"error_id":"890cbab5-0640-47b4-8f5e-b3a650115e7a-1"}`
	if got := api.Lines(); got != wantLines {
		t.Errorf("Lines() =\n%s\nwant\n%s", got, wantLines)
	}
}

func TestClassifyAPIError_NotAPIError(t *testing.T) {
	for _, err := range []error{
		errors.New("usage: --print requires a prompt"),
		errors.New("agent: exceeded tool-iteration budget (8)"),
		fmt.Errorf("agent: handler error: %w", errors.New("boom")),
	} {
		if _, ok := classifyAPIError(err); ok {
			t.Errorf("non-API error was classified as an API failure: %v", err)
		}
	}
}

func TestStatusName(t *testing.T) {
	for code, want := range map[int]string{
		400: "INVALID_ARGUMENT",
		401: "UNAUTHENTICATED",
		403: "PERMISSION_DENIED",
		404: "NOT_FOUND",
		408: "DEADLINE_EXCEEDED",
		429: "RESOURCE_EXHAUSTED",
		500: "INTERNAL",
		501: "UNIMPLEMENTED",
		502: "UNAVAILABLE",
		503: "UNAVAILABLE",
		504: "DEADLINE_EXCEEDED",
		418: "INVALID_ARGUMENT", // unlisted 4xx
		599: "INTERNAL",         // unlisted 5xx
	} {
		if got := statusName(code); got != want {
			t.Errorf("statusName(%d) = %q, want %q", code, got, want)
		}
	}
}

func TestRetryableStatus(t *testing.T) {
	for code, want := range map[int]bool{
		429: true, 503: true, 502: true, 504: true, 500: true, 408: true,
		400: false, 401: false, 403: false, 404: false,
	} {
		if got := retryableStatus[code]; got != want {
			t.Errorf("retryableStatus[%d] = %v, want %v", code, got, want)
		}
	}
}

// TestAPIErrorJSONShape pins the field order: short_error first, then status,
// error_code, code_kind, retryable, error_id.
func TestAPIErrorJSONShape(t *testing.T) {
	api := &apiError{
		Message:    "m",
		ShortError: "INTERNAL (code 500): m",
		Status:     "INTERNAL",
		ErrorCode:  500,
		CodeKind:   "http",
		Retryable:  true,
		ErrorID:    "id",
	}
	b, err := json.Marshal(api)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"short_error":"INTERNAL (code 500): m","status":"INTERNAL","error_code":500,"code_kind":"http","retryable":true,"error_id":"id"}`
	if string(b) != want {
		t.Errorf("marshal =\n%s\nwant\n%s", b, want)
	}
	// The user-facing message is not a JSON field.
	if strings.Contains(string(b), `"Message"`) {
		t.Errorf("Message leaked into the JSON: %s", b)
	}
}

// TestNewErrorID pins the recovered `<uuid>-1` shape.
func TestNewErrorID(t *testing.T) {
	re := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}-1$`)
	for i := 0; i < 8; i++ {
		id := newErrorID()
		if !re.MatchString(id) {
			t.Fatalf("newErrorID() = %q", id)
		}
	}
}

// TestClassifyAPIError_AuthExpired_RendersVerbatim pins the cycle
// auth-expired-message (AGY 1.3.3 line 47) contract: a 401 carrying
// KindAuthExpired surfaces the canonical
// "Authentication expired. Please log back in." string instead of
// the upstream body. The HTTPError is built via backend.NewHTTPError
// (the constructor used by the agw transport in cycle
// auth-expired-message) so the Kind field is populated automatically.
func TestClassifyAPIError_AuthExpired_RendersVerbatim(t *testing.T) {
	const upstreamBody = `{"error":{"message":"token rejected, please reauth"}}`
	he := backend.NewHTTPError(401, []byte(upstreamBody), "https://example.test/v1/x")
	err := fmt.Errorf("agent: open stream: %w", he)

	api, ok := classifyAPIError(err)
	if !ok {
		t.Fatal("auth-expired failure was not classified as an API error")
	}
	if api.Status != "UNAUTHENTICATED" || api.ErrorCode != 401 || api.CodeKind != "http" {
		t.Errorf("classification = %+v", api)
	}
	if api.Message != "Authentication expired. Please log back in." {
		t.Errorf("Message = %q, want verbatim canonical literal", api.Message)
	}
	want := "UNAUTHENTICATED (code 401): Authentication expired. Please log back in."
	if api.ShortError != want {
		t.Errorf("ShortError = %q, want %q", api.ShortError, want)
	}

	// Pin the rendered two-line stderr shape. The user-facing
	// `error:` line carries the canonical message; the JSON envelope
	// reports Status=UNAUTHENTICATED, ErrorCode=401. The body of
	// the HTTPError does NOT leak into the canonical message.
	if strings.Contains(api.Lines(), "token rejected") {
		t.Errorf("upstream body leaked into user-facing lines: %s", api.Lines())
	}
	if !strings.Contains(api.Lines(), "Authentication expired. Please log back in.") {
		t.Errorf("canonical literal missing from rendered lines: %s", api.Lines())
	}
}

// TestClassifyAPIError_AuthExpired_KindDrivenClassification pins the
// precedence rule: KindAuthExpired wins over the body-shape rules in
// HTTPError.Message(). A non-empty upstream body that would normally
// produce a "quota reached" or "token rejected" message is overridden
// by the KindAuthExpired branch.
func TestClassifyAPIError_AuthExpired_KindDrivenClassification(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_body", ""},
		{"json_error_message", `{"error":{"message":"some upstream text"}}`},
		{"json_top_message", `{"message":"some upstream text"}`},
		{"html_title", `<html><head><title>401</title></head></html>`},
		{"truncatable_text", strings.Repeat("x", 900)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			he := backend.NewHTTPError(401, []byte(tc.body), "x")
			api, ok := classifyAPIError(fmt.Errorf("wrap: %w", he))
			if !ok {
				t.Fatalf("not classified")
			}
			if api.Message != backend.AuthExpiredMessage {
				t.Errorf("Message = %q, want %q", api.Message, backend.AuthExpiredMessage)
			}
		})
	}
}

// TestClassifyAPIError_AuthExpired_LazyKind pins the lazy-classify
// path: existing call sites that still build &HTTPError{StatusCode:
// 401, ...} literally (no Kind field) get KindAuthExpired on read via
// the KindError() helper, so the headless formatter still renders the
// canonical literal.
func TestClassifyAPIError_AuthExpired_LazyKind(t *testing.T) {
	he := &backend.HTTPError{StatusCode: 401, Body: []byte(`{"error":{"message":"old-style"}}`)}
	api, ok := classifyAPIError(fmt.Errorf("wrap: %w", he))
	if !ok {
		t.Fatalf("not classified")
	}
	if api.Message != backend.AuthExpiredMessage {
		t.Errorf("Message = %q, want %q (lazy kind)", api.Message, backend.AuthExpiredMessage)
	}
}

// TestScopeHint_PrintModeRemediation is the cycle 263 contract pin:
// a 403 with the OAuth scope keyword surfaces in the headless
// AGY_ERROR JSON envelope as a `remediation` field whose value is the
// verbatim canonical hint, AND the user-facing `error:` line carries
// the upstream message with the hint appended. The hint is omitted
// (omitempty) for every other failure mode, so the canonical
// envelope shape for quota / expired / unknown errors is unchanged.
//
// AGY 1.4.x changelog (`internal/cli/testdata/changelog_reference.txt`
// line 271): "Improved authentication error messages when OAuth
// tokens lack required scopes by including explicit instructions to
// run `/logout` and `/login`."
func TestScopeHint_PrintModeRemediation(t *testing.T) {
	const upstream = `{"error":{"message":"insufficient_scope","details":"missing models.read"}}`
	he := &backend.HTTPError{StatusCode: 403, Body: []byte(upstream), URL: "https://example.test/v1/x"}
	err := fmt.Errorf("agent: open stream: %w", he)

	api, ok := classifyAPIError(err)
	if !ok {
		t.Fatal("scope-mismatch failure was not classified as an API error")
	}

	// Remediation field carries the canonical hint verbatim.
	if api.Remediation != backend.OAuthScopeHintMessage {
		t.Errorf("Remediation = %q, want %q", api.Remediation, backend.OAuthScopeHintMessage)
	}

	// The user-facing message keeps the upstream body AND appends the
	// hint — a single space separates them.
	wantSubstr := "insufficient_scope Run `/logout` then `/login`"
	if !strings.Contains(api.Message, wantSubstr) {
		t.Errorf("Message = %q, want it to contain %q", api.Message, wantSubstr)
	}

	// The AGY_ERROR envelope carries a non-empty remediation field.
	if !strings.Contains(api.Lines(), "`/logout`") || !strings.Contains(api.Lines(), "`/login`") || !strings.Contains(api.Lines(), `"remediation":"`) {
		t.Errorf("AGY_ERROR envelope missing remediation field; got:\n%s", api.Lines())
	}

	// The classification fields match the rest of the contract: 403
	// → PERMISSION_DENIED, code_kind=http, not retryable.
	if api.Status != "PERMISSION_DENIED" {
		t.Errorf("Status = %q, want PERMISSION_DENIED", api.Status)
	}
	if api.ErrorCode != 403 {
		t.Errorf("ErrorCode = %d, want 403", api.ErrorCode)
	}
	if api.CodeKind != "http" {
		t.Errorf("CodeKind = %q, want http", api.CodeKind)
	}
	if api.Retryable {
		t.Errorf("Retryable = true, want false (scope mismatch is not retryable)")
	}
}

// TestScopeHint_NonScopeErrorHasNoRemediation verifies the omitempty
// contract: a 403 that does NOT carry a scope keyword (an IP-block
// 403, a quota 403, anything else) renders the canonical envelope
// without a remediation field. This is what keeps the cycle 263 fix
// precise — only true scope mismatches get the hint.
func TestScopeHint_NonScopeErrorHasNoRemediation(t *testing.T) {
	cases := []struct {
		name string
		he   *backend.HTTPError
	}{
		{"plain_403_forbidden", &backend.HTTPError{StatusCode: 403, Body: []byte(`{"error":{"message":"forbidden"}}`)}},
		{"quota_message_403", &backend.HTTPError{StatusCode: 403, Body: []byte(`{"error":{"message":"quota reached"}}`)}},
		{"retryable_5xx", &backend.HTTPError{StatusCode: 503, Body: []byte("anything")}},
		{"auth_expired_401", &backend.HTTPError{StatusCode: 401, Body: []byte(`{"error":{"message":"reauth"}}`)}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			api, ok := classifyAPIError(fmt.Errorf("wrap: %w", tc.he))
			if !ok {
				t.Fatalf("not classified")
			}
			if api.Remediation != "" {
				t.Errorf("Remediation = %q, want empty (not a scope mismatch)", api.Remediation)
			}
			if strings.Contains(api.Lines(), `"remediation"`) {
				t.Errorf("envelope leaked a remediation field for non-scope 403:\n%s", api.Lines())
			}
		})
	}
}
