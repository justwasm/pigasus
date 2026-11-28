// prepaid_balance_test.go — AGY 1.5.17: an empty organization prepaid wallet
// stops the turn instead of retrying.
//
//	"Fixed turns on Gemini Enterprise accounts with an empty prepaid wallet
//	 retrying repeatedly and reporting `You have exhausted your quota on this
//	 model.`; the CLI now stops right away and tells you `Your organization's
//	 prepaid balance is insufficient. Contact your administrator to add
//	 funds.` (with an `Organization prepaid wallet is empty` notice in the
//	 Remote Control browser view)."
//
// # Where the wire shape comes from
//
// The distinguishing signal is NOT captured from a live enterprise account.
// It is recovered from the reference's own 1.5.17 web bundle
// (out/extracted-1.5.18/web-assets/main.js; recovered at 1.5.17, unchanged at 1.5.18), which classifies the same failure
// the CLI reports:
//
//	const oza = z.object({error: z.object({message: z.string(),
//	  details: z.array(z.object({"@type": z.string(),
//	    reason: z.string().optional(),
//	    metadata: z.object({validation_url: …, uiMessage: …, appeal_url: …}).optional()}))})});
//	function hq(a, b) { … for (let c of a.data.error.details)
//	  if (c?.["@type"] === "type.googleapis.com/google.rpc.ErrorInfo") { … } }
//	function rza(a) { return hq(a, b => b.reason === "PREPAID_BALANCE_EXHAUSTED") === true }
//	…
//	if (rza(p)) return {kind: "prepaidBalanceExhausted"};      // before errorCode === 429
//	…
//	case "prepaidBalanceExhausted": return {title: "Organization prepaid wallet is empty", …}
//
// So the signal is a structured `google.rpc.ErrorInfo` detail with
// `reason: "PREPAID_BALANCE_EXHAUSTED"`, and the reference evaluates it before
// it branches on the status code. The tests below pin that reading; a live
// enterprise account is still needed to confirm the CLI-side sentence and the
// exact status the endpoint pairs with the detail (see the report).
package apierror

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// prepaidBody builds the response body the reference parses: a Google RPC
// error envelope whose details array carries one ErrorInfo entry.
//
// The message is deliberately the *ordinary* quota sentence — the changelog
// says that is what the CLI reported while it retried, so the body text alone
// cannot distinguish the condition.
func prepaidBody(reason, message string) []byte {
	return []byte(`{"error":{"code":429,"message":"` + message + `","status":"RESOURCE_EXHAUSTED",` +
		`"details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `",` +
		`"domain":"cloudcode-pa.googleapis.com","metadata":{"model":"gemini-3-pro-preview"}}]}}`)
}

const exhaustedQuotaMessage = "You have exhausted your quota on this model."

// TestPrepaidBalance_CanonicalStrings pins the three literals the changelog
// quotes. The message and the notice live in this package so every surface —
// the TUI row, the headless `error:` line and the Remote Control page — reads
// the same bytes.
func TestPrepaidBalance_CanonicalStrings(t *testing.T) {
	if PrepaidBalanceMessage != "Your organization's prepaid balance is insufficient. Contact your administrator to add funds." {
		t.Errorf("PrepaidBalanceMessage = %q", PrepaidBalanceMessage)
	}
	if PrepaidBalanceNotice != "Organization prepaid wallet is empty" {
		t.Errorf("PrepaidBalanceNotice = %q", PrepaidBalanceNotice)
	}
	// The old sentence is the one the changelog says was WRONG. It must not
	// be what the prepaid case reports.
	if strings.Contains(PrepaidBalanceMessage, exhaustedQuotaMessage) {
		t.Error("the prepaid message carries the quota sentence it replaces")
	}
}

// TestPrepaidBalance_ClassifiesTheErrorInfoDetail is the core classification
// test: the reason token on an otherwise ordinary 429 marks the condition.
func TestPrepaidBalance_ClassifiesTheErrorInfoDetail(t *testing.T) {
	e := NewHTTPError(429, prepaidBody("PREPAID_BALANCE_EXHAUSTED", exhaustedQuotaMessage), "https://example/v1internal:streamGenerateContent")

	if !e.IsPrepaidBalanceExhausted() {
		t.Fatal("IsPrepaidBalanceExhausted() = false, want true")
	}
	if got := e.KindError(); got != KindPrepaidBalanceExhausted {
		t.Errorf("KindError() = %d, want KindPrepaidBalanceExhausted", got)
	}
	if got := e.Message(); got != PrepaidBalanceMessage {
		t.Errorf("Message() = %q, want %q", got, PrepaidBalanceMessage)
	}

	// The chain walk sees it through wrapping — the transports wrap the
	// provider error more than once.
	wrapped := fmt.Errorf("agent: open stream: %w", e)
	if !IsPrepaidBalanceExhaustedErr(wrapped) {
		t.Error("IsPrepaidBalanceExhaustedErr did not see the wrapped error")
	}
	if got := PrepaidBalanceMessageErr(wrapped); got != PrepaidBalanceMessage {
		t.Errorf("PrepaidBalanceMessageErr = %q, want %q", got, PrepaidBalanceMessage)
	}
}

// TestPrepaidBalance_IsNotRetryable is the point of the bullet: the condition
// must not be retried, whatever status it arrives with.
//
// Note on the status: the changelog's old symptom was a turn that "retried
// repeatedly" while reporting "You have exhausted your quota on this model."
// Ordinary quota exhaustion is already non-retryable in this clone (AGY 1.3.3:
// "reaching your individual quota is reported right away instead of being
// retried"), and that rule is status-gated to 429/402/403. So the retrying
// could only happen on a status the gate does not cover, or through the
// message-substring rule the failover chain applies. The classification below
// is therefore deliberately not status-gated: it stops the turn on any status.
func TestPrepaidBalance_IsNotRetryable(t *testing.T) {
	for _, status := range []int{400, 402, 403, 429, 500, 503} {
		e := NewHTTPError(status, prepaidBody("PREPAID_BALANCE_EXHAUSTED", exhaustedQuotaMessage), "https://example/x")
		if e.IsRetryableStatus() {
			t.Errorf("status %d: IsRetryableStatus() = true, want false (the turn must stop)", status)
		}
		if IsRetryableStatusErr(e) {
			t.Errorf("status %d: IsRetryableStatusErr = true, want false", status)
		}
	}
}

// TestPrepaidBalance_MessageReplacesTheQuotaSentence pins the visible effect:
// the body the endpoint sends still carries the ordinary quota sentence (that
// is what the CLI used to print), so Message() must replace it rather than
// echo it.
func TestPrepaidBalance_MessageReplacesTheQuotaSentence(t *testing.T) {
	e := NewHTTPError(429, prepaidBody("PREPAID_BALANCE_EXHAUSTED", exhaustedQuotaMessage), "https://example/x")
	if got := e.Message(); got != PrepaidBalanceMessage {
		t.Errorf("Message() = %q, want %q", got, PrepaidBalanceMessage)
	}
	// The same body without the detail keeps the upstream sentence.
	plain := NewHTTPError(429, []byte(`{"error":{"message":"`+exhaustedQuotaMessage+`"}}`), "")
	if got := plain.Message(); got != exhaustedQuotaMessage {
		t.Errorf("ordinary quota Message() = %q, want the upstream sentence", got)
	}
}

// TestPrepaidBalance_StructuredReportIsNotRetryable pins the headless
// `AGY_ERROR` line: a 429 makes retryableStatus true, so the prepaid branch
// has to override it explicitly or the JSON would contradict the behaviour.
func TestPrepaidBalance_StructuredReportIsNotRetryable(t *testing.T) {
	e := NewHTTPError(429, prepaidBody("PREPAID_BALANCE_EXHAUSTED", exhaustedQuotaMessage), "https://example/x")
	api, ok := ClassifyAPIError(fmt.Errorf("agent: open stream: %w", e))
	if !ok {
		t.Fatal("the prepaid failure was not classified as an API error")
	}
	if api.Retryable {
		t.Error("AGY_ERROR retryable = true, want false")
	}
	if api.Message != PrepaidBalanceMessage {
		t.Errorf("AGY_ERROR message = %q, want %q", api.Message, PrepaidBalanceMessage)
	}
	wantShort := "RESOURCE_EXHAUSTED (code 429): " + PrepaidBalanceMessage
	if api.ShortError != wantShort {
		t.Errorf("short_error = %q, want %q", api.ShortError, wantShort)
	}
	if api.Status != "RESOURCE_EXHAUSTED" || api.ErrorCode != 429 {
		t.Errorf("status/code = %q/%d, want RESOURCE_EXHAUSTED/429", api.Status, api.ErrorCode)
	}
}

// TestPrepaidBalance_OrdinaryQuotaUnchanged is the negative half: the ordinary
// quota cases the clone already handled must be untouched by this change —
// including the case whose message is the sentence the prepaid failure used to
// report.
//
// "As before" is pinned to the *pre-existing* behaviour, which is not
// "retries": AGY 1.3.3 already made individual quota non-retryable
// ("reaching your individual quota is reported right away instead of being
// retried"), and AGY 1.5.10 extended that to spend caps, prepaid balances and
// weekly quotas. So the invariant to hold is that none of these becomes
// *more* or *less* retryable than it was, and none of them reports the
// prepaid sentence.
func TestPrepaidBalance_OrdinaryQuotaUnchanged(t *testing.T) {
	cases := []struct {
		name      string
		he        *HTTPError
		retryable bool
	}{
		{
			name:      "429 quota sentence, no details",
			he:        NewHTTPError(429, []byte(`{"error":{"message":"`+exhaustedQuotaMessage+`"}}`), ""),
			retryable: false,
		},
		{
			name:      "429 individual quota reached",
			he:        NewHTTPError(429, []byte(`{"error":{"message":"Individual quota reached. Please upgrade your subscription."}}`), ""),
			retryable: false,
		},
		{
			// The RATE_LIMIT_EXCEEDED reason is the reference's own
			// per-minute limit ("Too many requests, please try again in
			// a bit!"), which AGY 1.5.10 explicitly keeps retrying —
			// its message carries no quota keyword, so the pre-existing
			// quota predicate does not fire.
			name:      "429 ErrorInfo RATE_LIMIT_EXCEEDED still retries",
			he:        NewHTTPError(429, prepaidBody("RATE_LIMIT_EXCEEDED", "Too many requests, please try again in a bit!"), ""),
			retryable: true,
		},
		{
			name:      "429 ErrorInfo QUOTA_EXHAUSTED",
			he:        NewHTTPError(429, prepaidBody("QUOTA_EXHAUSTED", exhaustedQuotaMessage), ""),
			retryable: false,
		},
		{
			// The per-minute rate limit is the one upstream 429 that IS
			// retried (AGY 1.5.10: "short-lived per-minute rate limits
			// are still retried"). A change here would break the
			// changelog's own promise.
			name:      "429 plain rate limit still retries",
			he:        NewHTTPError(429, []byte(`{"error":{"message":"rate limit exceeded, retry in 3s"}}`), ""),
			retryable: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.he.IsPrepaidBalanceExhausted() {
				t.Error("classified as prepaid-wallet exhausted, want false")
			}
			if got := tc.he.IsRetryableStatus(); got != tc.retryable {
				t.Errorf("IsRetryableStatus() = %v, want %v (err=%v)", got, tc.retryable, tc.he)
			}
			if tc.he.Message() == PrepaidBalanceMessage {
				t.Error("an ordinary failure reported the prepaid sentence")
			}
		})
	}
}

// TestPrepaidBalance_NeedsTheRightDetailShape pins the guards: the reason has
// to sit on an ErrorInfo detail of an `error.details` array. A reason string
// somewhere else in the body — a metadata echo, a log line, a nested object —
// is not the condition.
func TestPrepaidBalance_NeedsTheRightDetailShape(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: "reason on a non-ErrorInfo detail",
			body: `{"error":{"message":"x","details":[{"@type":"type.googleapis.com/google.rpc.RetryInfo","reason":"PREPAID_BALANCE_EXHAUSTED"}]}}`,
		},
		{
			name: "reason only in the message text",
			body: `{"error":{"message":"PREPAID_BALANCE_EXHAUSTED"}}`,
		},
		{
			name: "reason in metadata",
			body: `{"error":{"message":"x","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","metadata":{"reason":"PREPAID_BALANCE_EXHAUSTED"}}]}}`,
		},
		{
			name: "reason on a nested object that is not a detail",
			body: `{"error":{"message":"x","reason":"PREPAID_BALANCE_EXHAUSTED"}}`,
		},
		{
			name: "no body",
			body: "",
		},
		{
			name: "non-JSON body",
			body: "PREPAID_BALANCE_EXHAUSTED",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := NewHTTPError(429, []byte(tc.body), "")
			if e.IsPrepaidBalanceExhausted() {
				t.Errorf("IsPrepaidBalanceExhausted() = true for %q, want false", tc.body)
			}
		})
	}
}

// TestPrepaidBalance_DetailOnEitherEnvelope covers both shapes a Google RPC
// error is delivered in: the JSON gateway envelope (`error.details`) and a
// bare gRPC response whose details sit at the top level.
func TestPrepaidBalance_DetailOnEitherEnvelope(t *testing.T) {
	detail := `{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"PREPAID_BALANCE_EXHAUSTED"}`
	for name, body := range map[string]string{
		"json gateway envelope": `{"error":{"message":"m","details":[` + detail + `]}}`,
		"top-level details":     `{"message":"m","code":429,"details":[` + detail + `]}`,
	} {
		t.Run(name, func(t *testing.T) {
			if !NewHTTPError(429, []byte(body), "").IsPrepaidBalanceExhausted() {
				t.Errorf("IsPrepaidBalanceExhausted() = false for %s", name)
			}
		})
	}
}

// TestPrepaidBalance_CaseAndPadding pins the tolerance the comparison gives
// the token: the reference compares with `===`, so the upstream emits exactly
// this spelling; the clone trims and folds case so a proxy that rewrote the
// body cannot defeat the classification.
func TestPrepaidBalance_CaseAndPadding(t *testing.T) {
	for _, reason := range []string{"PREPAID_BALANCE_EXHAUSTED", "prepaid_balance_exhausted", " Prepaid_Balance_Exhausted "} {
		body := `{"error":{"message":"m","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo","reason":"` + reason + `"}]}}`
		if !NewHTTPError(429, []byte(body), "").IsPrepaidBalanceExhausted() {
			t.Errorf("reason %q was not recognised", reason)
		}
	}
}

// TestPrepaidBalance_NilSafety pins the nil receivers: a typed nil and a nil
// error are both "not this condition", not a panic — the same contract as
// IsQuotaReached.
func TestPrepaidBalance_NilSafety(t *testing.T) {
	var e *HTTPError
	if e.IsPrepaidBalanceExhausted() {
		t.Error("nil *HTTPError reported the condition")
	}
	if IsPrepaidBalanceExhaustedErr(nil) {
		t.Error("nil error reported the condition")
	}
	if got := PrepaidBalanceMessageErr(nil); got != "" {
		t.Errorf("PrepaidBalanceMessageErr(nil) = %q, want \"\"", got)
	}
	if IsPrepaidBalanceExhaustedErr(errors.New("quota")) {
		t.Error("a plain error reported the condition")
	}
}

// TestPrepaidBalance_StructuralKindsDoNotOverride pins the lazy-kind
// precedence: when the caller set Kind explicitly the body does not rewrite
// it, and a body-carried kind does not leak into an unrelated status.
func TestPrepaidBalance_StructuralKindsDoNotOverride(t *testing.T) {
	// Explicit kind wins over the body.
	e := NewHTTPError(429, prepaidBody("PREPAID_BALANCE_EXHAUSTED", "m"), "")
	e.Kind = KindAuthExpired
	if got := e.KindError(); got != KindAuthExpired {
		t.Errorf("KindError() = %d, want the explicitly set KindAuthExpired", got)
	}

	// A 4xx that is not the condition stays KindUnknown (only 401 has a
	// status-derived kind today).
	if got := NewHTTPError(429, []byte(`{"error":{"message":"m"}}`), "").KindError(); got != KindUnknown {
		t.Errorf("ordinary 429 KindError() = %d, want KindUnknown", got)
	}
}
