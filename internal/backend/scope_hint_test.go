package backend

// scope_hint_test.go — cycle 263: the OAuth scope-mismatch predicate and
// the canonical hint constant.
//
// AGY 1.4.x changelog (`internal/cli/testdata/changelog_reference.txt`
// line 271): "Improved authentication error messages when OAuth tokens
// lack required scopes by including explicit instructions to run
// `/logout` and `/login`." This file pins the predicate
// (`HTTPError.IsScopeMismatch` / `IsScopeMismatchErr`) and the
// canonical hint string (`OAuthScopeHintMessage`) the headless
// formatter and the TUI error surface read.
//
// The status-403 + scope-keyword pairing is the precise detection
// rule — the task explicitly forbids emitting the hint on any 403
// (an IP-allowlist denial, a model rate-limit, etc. are also 403 and
// must NOT get the refresh-grant instruction). Each test exercises
// one axis of the rule.
//
// 7 tests here; the 7th in cmd/wagyu/api_error_test.go
// (TestScopeHint_PrintModeRemediation) covers the AGY_ERROR JSON
// envelope. TestScopeHint_PreservesOriginalError pins the contract
// that the upstream body is kept verbatim and the hint is only an
// addendum downstream call sites append.

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// TestScopeHint_403InsufficientScope is the canonical OAuth case:
// the upstream body carries the RFC 6750 §3.1 error code
// "insufficient_scope" with a colon-separated scope name. The hint
// must be available (callers downstream append it).
func TestScopeHint_403InsufficientScope(t *testing.T) {
	body := `403 insufficient_scope: missing models.read`
	e := &HTTPError{StatusCode: 403, Body: []byte(body), URL: "https://example.test/v1/x"}
	if !e.IsScopeMismatch() {
		t.Errorf("IsScopeMismatch() = false for body %q, want true", body)
	}
	// The canonical hint is the verbatim string downstream surfaces.
	want := "Run `/logout` then `/login` to refresh your OAuth grant with the required scopes."
	if OAuthScopeHintMessage != want {
		t.Errorf("OAuthScopeHintMessage = %q, want %q", OAuthScopeHintMessage, want)
	}
	// The upstream body is preserved — the hint is appended, not
	// substituted.
	if e.Message() != "403 insufficient_scope: missing models.read" {
		t.Errorf("Message() = %q, want preserved upstream body", e.Message())
	}
}

// TestScopeHint_403MissingScopeKeyword exercises the
// "missing scope" keyword variant — Anthropic-style bodies use this
// shape and must trigger the same hint.
func TestScopeHint_403MissingScopeKeyword(t *testing.T) {
	body := `missing scope models.read`
	e := &HTTPError{StatusCode: 403, Body: []byte(body), URL: "https://example.test/v1/x"}
	if !e.IsScopeMismatch() {
		t.Errorf("IsScopeMismatch() = false for body %q, want true", body)
	}
	// The hint is available and unchanged.
	if OAuthScopeHintMessage == "" {
		t.Errorf("OAuthScopeHintMessage is empty, want canonical non-empty hint")
	}
}

// TestScopeHint_403Generic is the negative case at status 403: a
// non-scope 403 (e.g. an IP-allowlist denial or a quota-blocked
// model call) MUST NOT trigger the hint. The keyword guard is the
// precision the cycle relies on.
func TestScopeHint_403Generic(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"plain_forbidden", "403 forbidden"},
		{"ip_allowlist", "access denied: 10.0.0.0/8 not in allowlist"},
		{"region_block", "model claude-opus not available in region EU"},
		{"rate_limit_403", "rate limit exceeded"}, // edge: rare 403-as-rate-limit shape
		{"empty_body", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &HTTPError{StatusCode: 403, Body: []byte(tc.body), URL: "x"}
			if e.IsScopeMismatch() {
				t.Errorf("IsScopeMismatch() = true for body %q, want false", tc.body)
			}
		})
	}
}

// TestScopeHint_401NotEnough pins the status guard: a 401 is
// NEVER a scope mismatch — it is the AuthExpiredMessage case (cycle
// 100). The hint must not appear on a 401 even if the body
// mentions "scope" (some old OAuth flows put scope info in the
// initial 401 challenge).
func TestScopeHint_401NotEnough(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"plain_401", "token rejected, please reauth"},
		{"scope_mention_401", "scope not yet granted; please reauthenticate"}, // tricky edge
		{"json_401", `{"error":{"message":"missing scope read:users"}}`},      // 401 with scope keyword
		{"empty_401", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &HTTPError{StatusCode: 401, Body: []byte(tc.body), URL: "x"}
			if e.IsScopeMismatch() {
				t.Errorf("IsScopeMismatch() = true for 401 body %q, want false", tc.body)
			}
			// Belt and braces: the chain-walking helper must agree.
			if IsScopeMismatchErr(e) {
				t.Errorf("IsScopeMismatchErr(<401>) = true, want false")
			}
		})
	}
}

// TestScopeHint_CaseInsensitive verifies the keyword scan is
// case-insensitive — both in the body and in the rendered message.
// Real upstreams emit "INSUFFICIENT_SCOPE", "Missing Scope", "Missing
// Scope", and the predicate must handle every casing.
func TestScopeHint_CaseInsensitive(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"upper_underscore", "INSUFFICIENT_SCOPE: missing models.read"},
		{"title_case", "Missing Scope: models.read"},
		{"lower_insensitive", "403 INSUFFICIENT_SCOPE — try /logout"},
		{"mixed_message_only", "Your token is rejected: Insufficient_Scope"}, // truncated body, Message() carries it
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := &HTTPError{StatusCode: 403, Body: []byte(tc.body), URL: "x"}
			if !e.IsScopeMismatch() {
				t.Errorf("IsScopeMismatch() = false for body %q, want true", tc.body)
			}
			// Belt and braces: chain helper agrees.
			if !IsScopeMismatchErr(e) {
				t.Errorf("IsScopeMismatchErr(<body=%q>) = false, want true", tc.body)
			}
		})
	}
}

// TestScopeHint_PreservesOriginalError is the contract test for the
// upstream-body guarantee: when scope mismatch is detected, the
// upstream error message is preserved verbatim — downstream code
// appends the hint. Pinning this here keeps any future cycle that
// tries to substitute the upstream body with a generic "OAuth failed"
// message from regressing the cycle 263 fix.
func TestScopeHint_PreservesOriginalError(t *testing.T) {
	const upstream = `{"error":{"message":"insufficient_scope","details":"missing models.read"}}`
	e := &HTTPError{StatusCode: 403, Body: []byte(upstream), URL: "x"}
	if !e.IsScopeMismatch() {
		t.Fatal("IsScopeMismatch() = false, want true for upstream body")
	}
	if got := e.Message(); got != "insufficient_scope" {
		t.Errorf("Message() = %q, want %q (the upstream error.message)", got, "insufficient_scope")
	}
	// The hint itself does not collide with the upstream body — a
	// future regression that baked the hint into the body would
	// surface here.
	if strings.Contains(e.Message(), OAuthScopeHintMessage) {
		t.Errorf("hint must not be embedded in Message(); got %q", e.Message())
	}
	// Wrapping semantics: the helper walks errors.As through
	// arbitrary layers and still reports scope mismatch.
	wrapped := fmt.Errorf("agent: open stream: %w", e)
	if !IsScopeMismatchErr(wrapped) {
		t.Errorf("IsScopeMismatchErr(<wrapped>) = false, want true")
	}
	// A plain non-HTTPError error MUST stay negative (tools, quota
	// exhaustion, generic network errors — none of them are scope
	// mismatches even if their text happens to mention "scope").
	plain := errors.New("tool execution failed: insufficient_scope keyword in a diagnostic message")
	if IsScopeMismatchErr(plain) {
		t.Errorf("IsScopeMismatchErr(<plain>) = true, want false (no *HTTPError in chain)")
	}
	plainWrapped := fmt.Errorf("agent: %w", plain)
	if IsScopeMismatchErr(plainWrapped) {
		t.Errorf("IsScopeMismatchErr(<wrapped plain>) = true, want false")
	}
}
