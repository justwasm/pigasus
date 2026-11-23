package failover

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func okStream(text string) <-chan backend.StreamChunk {
	ch := make(chan backend.StreamChunk, 2)
	ch <- backend.StreamChunk{Text: text}
	ch <- backend.StreamChunk{Done: true}
	close(ch)
	return ch
}

func drainText(t *testing.T, ch <-chan backend.StreamChunk) string {
	t.Helper()
	var text string
	for c := range ch {
		if c.Error != nil {
			t.Fatalf("unexpected chunk error: %v", c.Error)
		}
		text += c.Text
	}
	return text
}

func TestHealthTracker_ExponentialCooldownAndHalfOpenRecovery(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	ht := NewHealthTracker(HealthConfig{Now: func() time.Time { return now }})
	failErr := &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"rate limited"}`)}

	if st := ht.State("primary"); st != StateClosed {
		t.Fatalf("initial state = %v, want Closed", st)
	}
	if !ht.ShouldAttempt("primary") {
		t.Fatal("expected Closed target to be attemptable")
	}

	// 1st failure -> Open for 15s.
	ht.RecordFailure("primary", failErr)
	if st := ht.State("primary"); st != StateOpen {
		t.Fatalf("state after 1st failure = %v, want Open", st)
	}
	if ht.ShouldAttempt("primary") {
		t.Fatal("expected Open target to be skipped during cooldown")
	}
	if rem := ht.CooldownRemaining("primary"); rem != 15*time.Second {
		t.Fatalf("cooldown remaining = %v, want 15s", rem)
	}

	// Advance 15s -> HalfOpen probe allowed.
	now = now.Add(15 * time.Second)
	if st := ht.State("primary"); st != StateHalfOpen {
		t.Fatalf("state after 15s = %v, want HalfOpen", st)
	}
	if !ht.ShouldAttempt("primary") {
		t.Fatal("expected HalfOpen target to allow probe")
	}

	// Probe fails -> Open for 30s (exponential backoff).
	ht.RecordFailure("primary", failErr)
	if rem := ht.CooldownRemaining("primary"); rem != 30*time.Second {
		t.Fatalf("cooldown after 2nd failure = %v, want 30s", rem)
	}

	// Advance 30s -> HalfOpen, then RecordSuccess -> Closed.
	now = now.Add(30 * time.Second)
	if !ht.ShouldAttempt("primary") {
		t.Fatal("expected HalfOpen probe after 30s")
	}
	ht.RecordSuccess("primary")
	if st := ht.State("primary"); st != StateClosed {
		t.Fatalf("state after RecordSuccess = %v, want Closed", st)
	}
	if rem := ht.CooldownRemaining("primary"); rem != 0 {
		t.Fatalf("cooldown after recovery = %v, want 0", rem)
	}

	// Verify cap at 5m after repeated failures.
	for i := 0; i < 10; i++ {
		ht.RecordFailure("primary", failErr)
	}
	if rem := ht.CooldownRemaining("primary"); rem != 5*time.Minute {
		t.Fatalf("max cooldown = %v, want 5m", rem)
	}
}

func TestFailover_CrossTurnCircuitBreakerAndFastRetryBudget(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	ht := NewHealthTracker(HealthConfig{Now: func() time.Time { return now }})

	var primaryMaxRetries []int
	var primaryHasOverride []bool
	var secondaryMaxRetries []int
	var secondaryHasOverride []bool

	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			n, ok := backend.MaxRetriesFromContext(ctx)
			primaryMaxRetries = append(primaryMaxRetries, n)
			primaryHasOverride = append(primaryHasOverride, ok)
			return nil, &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"rate limited"}`)}
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			n, ok := backend.MaxRetriesFromContext(ctx)
			secondaryMaxRetries = append(secondaryMaxRetries, n)
			secondaryHasOverride = append(secondaryHasOverride, ok)
			return okStream("from-secondary"), nil
		},
	}

	var events []Event
	client := NewWithOptions([]Target{
		{Name: "primary", LogicalModel: "logical-m1", Client: primary, ModelMap: func(string) string { return "m1" }},
		{Name: "secondary", LogicalModel: "logical-m1", Client: secondary, ModelMap: func(string) string { return "m2" }},
	}, func(ev Event) {
		events = append(events, ev)
	}, WithHealthTracker(ht))

	// Turn 1: primary fails -> fast retry budget (0, true) on primary, full budget (0, false) on final secondary.
	ch1, err := client.Stream(context.Background(), backend.StreamRequest{Model: "logical-m1"})
	if err != nil {
		t.Fatalf("turn 1 Stream: %v", err)
	}
	if got := drainText(t, ch1); got != "from-secondary" {
		t.Fatalf("turn 1 text = %q, want from-secondary", got)
	}
	if primary.calls != 1 || secondary.calls != 1 {
		t.Fatalf("turn 1 calls: primary=%d secondary=%d", primary.calls, secondary.calls)
	}
	if !primaryHasOverride[0] || primaryMaxRetries[0] != 0 {
		t.Fatalf("expected primary to receive MaxRetries=(0, true), got (%d, %v)", primaryMaxRetries[0], primaryHasOverride[0])
	}
	if secondaryHasOverride[0] {
		t.Fatalf("expected final target secondary to have no MaxRetries override, got (%d, %v)", secondaryMaxRetries[0], secondaryHasOverride[0])
	}

	// Turn 2 (within 15s cooldown): primary is Open and MUST be skipped completely!
	ch2, err := client.Stream(context.Background(), backend.StreamRequest{Model: "logical-m1"})
	if err != nil {
		t.Fatalf("turn 2 Stream: %v", err)
	}
	if got := drainText(t, ch2); got != "from-secondary" {
		t.Fatalf("turn 2 text = %q, want from-secondary", got)
	}
	if primary.calls != 1 {
		t.Fatalf("expected Open primary to be skipped on turn 2 (calls=1), got %d", primary.calls)
	}
	if secondary.calls != 2 {
		t.Fatalf("expected secondary calls=2, got %d", secondary.calls)
	}
	if len(events) < 2 || events[1].FromTarget != "primary" || events[1].ToTarget != "secondary" {
		t.Fatalf("expected failover event when skipping Open primary, got %+v", events)
	}

	// Advance past 15s cooldown -> HalfOpen probe succeeds on primary and closes circuit.
	now = now.Add(16 * time.Second)
	primary.streamFn = func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
		return okStream("primary-recovered"), nil
	}
	ch3, err := client.Stream(context.Background(), backend.StreamRequest{Model: "logical-m1"})
	if err != nil {
		t.Fatalf("turn 3 Stream: %v", err)
	}
	if got := drainText(t, ch3); got != "primary-recovered" {
		t.Fatalf("turn 3 text = %q, want primary-recovered", got)
	}
	if st := ht.State("primary"); st != StateClosed {
		t.Fatalf("expected primary state Closed after HalfOpen success, got %v", st)
	}
}

func TestFailover_AllOpenProbesSoonestCooldown(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	ht := NewHealthTracker(HealthConfig{Now: func() time.Time { return now }})
	failErr := &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"rate limited"}`)}

	ht.RecordFailure("primary", failErr)
	ht.RecordFailure("primary", failErr)   // 30s cooldown
	ht.RecordFailure("secondary", failErr) // 15s cooldown

	primary := &mockClient{streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
		return okStream("p"), nil
	}}
	secondary := &mockClient{streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
		return okStream("s"), nil
	}}
	client := NewWithOptions([]Target{
		{Name: "primary", Client: primary, ModelMap: func(string) string { return "m1" }},
		{Name: "secondary", Client: secondary, ModelMap: func(string) string { return "m2" }},
	}, nil, WithHealthTracker(ht))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "m1"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if got := drainText(t, ch); got != "s" {
		t.Fatalf("expected soonest-expiring target secondary to be probed first, got %q", got)
	}
	if secondary.calls != 1 || primary.calls != 0 {
		t.Fatalf("expected secondary=1 primary=0, got secondary=%d primary=%d", secondary.calls, primary.calls)
	}
}

func TestFailover_MidStreamErrorMarksUnhealthyAndReturnsRecoverable(t *testing.T) {
	now := time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC)
	ht := NewHealthTracker(HealthConfig{Now: func() time.Time { return now }})

	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "partial "}
			ch <- backend.StreamChunk{Error: &apierror.HTTPError{StatusCode: http.StatusTooManyRequests, Body: []byte(`{"error":"mid-stream 429"}`)}, Done: true}
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return okStream("continued"), nil
		},
	}

	client := NewWithOptions([]Target{
		{Name: "primary", Client: primary, ModelMap: func(string) string { return "m1" }},
		{Name: "secondary", Client: secondary, ModelMap: func(string) string { return "m2" }},
	}, nil, WithHealthTracker(ht))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "m1"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var text string
	var streamErr error
	for c := range ch {
		text += c.Text
		if c.Error != nil {
			streamErr = c.Error
		}
	}
	if text != "partial " {
		t.Fatalf("expected partial text, got %q", text)
	}
	if !IsMidStreamRecoverable(streamErr) {
		t.Fatalf("expected MidStreamFailoverError, got %T: %v", streamErr, streamErr)
	}
	var msErr *MidStreamFailoverError
	if !errors.As(streamErr, &msErr) || msErr.Target != "primary" {
		t.Fatalf("expected MidStreamFailoverError for primary, got %+v", streamErr)
	}
	if st := ht.State("primary"); st != StateOpen {
		t.Fatalf("expected primary marked Open after mid-stream failure, got %v", st)
	}
}
