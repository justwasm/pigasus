// prepaid_balance_test.go — AGY 1.5.17: a failover chain must not retry an
// empty organization prepaid wallet onto another target.
//
//	"Fixed turns on Gemini Enterprise accounts with an empty prepaid wallet
//	 retrying repeatedly and reporting `You have exhausted your quota on this
//	 model.`; the CLI now stops right away …"
//
// The chain is where "retrying repeatedly" actually happened: IsFailoverEligible
// is what decides whether the next target is tried, and every rule in it —
// the quota predicate, the retryable-status predicate, and the message-substring
// scan at the bottom — answers "yes" for a prepaid-wallet failure that arrives
// as an ordinary 429. The tests below pin that the prepaid check runs first and
// that the ordinary rules keep their answers.
package failover

import (
	"context"
	"errors"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// prepaidBody is the response body the reference parses: a Google RPC error
// envelope with one ErrorInfo detail whose reason is the prepaid-wallet token.
func prepaidBody() []byte {
	return []byte(`{"error":{"code":429,"message":"You have exhausted your quota on this model.",` +
		`"status":"RESOURCE_EXHAUSTED","details":[{"@type":"type.googleapis.com/google.rpc.ErrorInfo",` +
		`"reason":"PREPAID_BALANCE_EXHAUSTED","metadata":{"model":"gemini-3-pro-preview"}}]}}`)
}

// TestIsFailoverEligible_PrepaidBalanceIsTerminal pins the classification
// rule itself: the condition is not eligible, on every status the endpoint
// might pair with it.
func TestIsFailoverEligible_PrepaidBalanceIsTerminal(t *testing.T) {
	for _, status := range []int{400, 402, 403, 429, 500, 503} {
		err := apierror.NewHTTPError(status, prepaidBody(), "https://example/x")
		if IsFailoverEligible(err) {
			t.Errorf("status %d: IsFailoverEligible() = true, want false", status)
		}
		// The same error wrapped the way the transports wrap it.
		if IsFailoverEligible(errors.Join(errors.New("agent: open stream"), err)) {
			t.Errorf("status %d: a wrapped prepaid failure was eligible", status)
		}
	}
}

// TestFailover_PrepaidBalanceStopsWithoutTryingTheBackup is the behaviour the
// changelog describes: the primary answers with the prepaid-wallet failure and
// the chain does not go on to the secondary. The error the user sees is the
// verbatim sentence, not the quota sentence.
func TestFailover_PrepaidBalanceStopsWithoutTryingTheBackup(t *testing.T) {
	DefaultHealthTracker().Reset()
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, apierror.NewHTTPError(429, prepaidBody(), "https://example/x")
		},
	}
	secondary := &mockClient{}

	var events []Event
	client := New([]Target{
		{Name: "enterprise", Client: primary},
		{Name: "backup", Client: secondary},
	}, func(e Event) { events = append(events, e) })

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gemini-3-pro-preview"})
	if err != nil {
		t.Fatalf("Stream returned a synchronous error: %v", err)
	}
	var last *backend.StreamChunk
	for c := range ch {
		c := c
		last = &c
	}
	if last == nil || last.Error == nil {
		t.Fatal("the stream ended without the prepaid failure")
	}
	if !apierror.IsPrepaidBalanceExhaustedErr(last.Error) {
		t.Errorf("the surfaced error is not the prepaid failure: %v", last.Error)
	}
	if got := apierror.PrepaidBalanceMessageErr(last.Error); got != apierror.PrepaidBalanceMessage {
		t.Errorf("surfaced message = %q, want %q", got, apierror.PrepaidBalanceMessage)
	}
	if secondary.calls != 0 {
		t.Errorf("backup calls = %d, want 0 (the turn must stop on this account)", secondary.calls)
	}
	if len(events) != 0 {
		t.Errorf("failover events = %d, want 0: %+v", len(events), events)
	}
}

// TestFailover_OrdinaryQuotaStillFailsOver pins the negative half: a 429 with
// a quota message and no ErrorInfo detail keeps the pre-existing behaviour and
// fails over to the backup. Ordinary quota exhaustion is account-scoped, so
// another account can serve the turn — unlike the organization's wallet.
func TestFailover_OrdinaryQuotaStillFailsOver(t *testing.T) {
	DefaultHealthTracker().Reset()
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, apierror.NewHTTPError(429,
				[]byte(`{"error":{"message":"Individual quota reached. Please upgrade your subscription."}}`), "")
		},
	}
	secondary := &mockClient{}

	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, nil)

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var text string
	for c := range ch {
		text += c.Text
	}
	if text != "mock-response" {
		t.Errorf("text = %q, want the backup's answer", text)
	}
	if secondary.calls != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls)
	}
}

// TestFailover_PrepaidBalanceMidStreamIsNotRecoverable pins the mid-stream
// branch: a prepaid failure after partial content is surfaced as-is, not as a
// MidStreamFailoverError, so the outer turn loop does not auto-continue onto
// another target.
func TestFailover_PrepaidBalanceMidStreamIsNotRecoverable(t *testing.T) {
	DefaultHealthTracker().Reset()
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "partial answer"}
			ch <- backend.StreamChunk{Error: apierror.NewHTTPError(429, prepaidBody(), ""), Done: true}
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{}

	client := New([]Target{
		{Name: "enterprise", Client: primary},
		{Name: "backup", Client: secondary},
	}, nil)

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "m"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sawText bool
	var last *backend.StreamChunk
	for c := range ch {
		c := c
		if c.Text != "" {
			sawText = true
		}
		last = &c
	}
	if !sawText {
		t.Error("the partial answer was dropped")
	}
	if last == nil || last.Error == nil {
		t.Fatal("the stream did not end on the prepaid failure")
	}
	if IsMidStreamRecoverable(last.Error) {
		t.Errorf("the prepaid failure was marked recoverable: %v", last.Error)
	}
	if secondary.calls != 0 {
		t.Errorf("backup calls = %d, want 0", secondary.calls)
	}
}
