package failover

// scenarios_test.go — Storybook-style scenario contracts for the failover package.
//
// Philosophy: each scenario is independently constructed (its own fake clients and
// its own isolated HealthTracker), has an explicit name, explicit inputs, and
// asserts observable outputs (final chunk sequence, error type, Event order, and
// absence of duplicated output). No real HTTP is performed.
//
// Every scenario uses NewWithOptions(..., WithHealthTracker(NewHealthTracker())) so
// the process-wide DefaultHealthTracker shared state is never touched. This keeps
// the scenarios hermetic and safe to run in parallel (t.Parallel).

import (
	"context"
	"errors"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// scriptedClient is a fully scripted fake backend.Client. It never performs I/O;
// it returns a pre-baked sequence of chunks (and/or a terminal error) and records
// how many times it was invoked plus the last request it observed.
type scriptedClient struct {
	name  string
	calls int
	// onStream, when set, is invoked on each call and returns the channel/err to
	// surface. When nil, scriptedChunks is replayed.
	onStream      func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error)
	scriptedChunks []backend.StreamChunk
}

func (s *scriptedClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.calls++
	if s.onStream != nil {
		return s.onStream(ctx, req)
	}
	ch := make(chan backend.StreamChunk, len(s.scriptedChunks))
	for _, c := range s.scriptedChunks {
		ch <- c
	}
	close(ch)
	return ch, nil
}

// collector accumulates the observable output of a stream run.
type collector struct {
	chunks []backend.StreamChunk
	text   string
	err    error
}

func collect(ch <-chan backend.StreamChunk) collector {
	var c collector
	for chunk := range ch {
		c.chunks = append(c.chunks, chunk)
		c.text += chunk.Text
		if chunk.Error != nil {
			c.err = chunk.Error
		}
	}
	return c
}

// newIsolatedClient builds a failover Client wired to a fresh, isolated
// HealthTracker so scenarios never share circuit-breaker state.
func newIsolatedClient(targets []Target, notify Notifier) *Client {
	return NewWithOptions(targets, notify, WithHealthTracker(NewHealthTracker()))
}

// recordingNotifier returns a Notifier that appends every Event to the slice.
func recordingNotifier(sink *[]Event) Notifier {
	return func(e Event) { *sink = append(*sink, e) }
}

// Scenario 1: a 429 surfaced on the very first chunk (before any content) must
// immediately fail over to the backup target, which then completes cleanly.
func TestScenario_FirstChunk429FailsOverBeforeContent(t *testing.T) {
	t.Parallel()

	primary := &scriptedClient{
		name: "primary",
		scriptedChunks: []backend.StreamChunk{
			{Error: &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"resource_exhausted"}`)}, Done: true},
		},
	}
	secondary := &scriptedClient{
		name: "secondary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "from-secondary"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, recordingNotifier(&events))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	if got.text != "from-secondary" {
		t.Errorf("final text = %q, want %q", got.text, "from-secondary")
	}
	if got.err != nil {
		t.Errorf("unexpected terminal error: %v", got.err)
	}
	if primary.calls != 1 || secondary.calls != 1 {
		t.Errorf("calls: primary=%d secondary=%d, want 1/1", primary.calls, secondary.calls)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].FromTarget != "primary" || events[0].ToTarget != "secondary" {
		t.Errorf("event route = %s->%s, want primary->secondary", events[0].FromTarget, events[0].ToTarget)
	}
}

// Scenario 2: once partial text has been emitted, a mid-stream eligible error must
// NOT trigger a blind retry on the backup (which would duplicate/stitch output).
// The partial text is preserved and a typed MidStreamFailoverError is surfaced.
func TestScenario_MidStreamErrorAfterContentNoBlindRetry(t *testing.T) {
	t.Parallel()

	primary := &scriptedClient{
		name: "primary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "Hello, "},
			{Text: "world"},
			{Error: &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"mid-stream quota"}`)}, Done: true},
		},
	}
	secondary := &scriptedClient{
		name: "secondary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "SHOULD-NOT-APPEAR"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, recordingNotifier(&events))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	// No duplicate/stitched output: only the primary's partial text survives.
	if got.text != "Hello, world" {
		t.Errorf("final text = %q, want %q (no stitched backup output)", got.text, "Hello, world")
	}
	// Secondary must never be invoked after content was emitted.
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0 (no blind retry)", secondary.calls)
	}
	// No failover Event should be broadcast for a mid-stream error.
	if len(events) != 0 {
		t.Errorf("events = %d, want 0 on mid-stream error", len(events))
	}
	// Error must be the recoverable typed error so the outer loop can continue.
	if !IsMidStreamRecoverable(got.err) {
		t.Fatalf("expected recoverable MidStreamFailoverError, got %T: %v", got.err, got.err)
	}
	var msErr *MidStreamFailoverError
	if !errors.As(got.err, &msErr) || msErr.Target != "primary" || !msErr.Eligible {
		t.Errorf("MidStreamFailoverError = %+v, want {Target:primary Eligible:true}", msErr)
	}
}

// Scenario 3: a context cancellation is not a failover-eligible condition. Even
// with a healthy backup available, the chain must surface the cancellation rather
// than switching targets.
func TestScenario_ContextCancelDoesNotFailover(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	primary := &scriptedClient{
		name: "primary",
		onStream: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			// Simulate the upstream aborting because the caller cancelled.
			cancel()
			return nil, context.Canceled
		},
	}
	secondary := &scriptedClient{
		name: "secondary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "backup-should-not-run"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, recordingNotifier(&events))

	ch, err := client.Stream(ctx, backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	if got.err == nil {
		t.Fatal("expected a terminal error, got nil")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Errorf("terminal error = %v, want context.Canceled", got.err)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0 (cancel must not fail over)", secondary.calls)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0 on cancellation", len(events))
	}
}

// Scenario 4: a non-retryable error (e.g. 400 invalid request) must not trigger
// failover even before any content is emitted; it surfaces directly to the caller.
func TestScenario_NonRetryableErrorDoesNotFailover(t *testing.T) {
	t.Parallel()

	nonRetryable := &apierror.HTTPError{StatusCode: 400, Body: []byte(`{"error":"invalid_request"}`)}
	primary := &scriptedClient{
		name: "primary",
		onStream: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, nonRetryable
		},
	}
	secondary := &scriptedClient{
		name: "secondary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "backup-should-not-run"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, recordingNotifier(&events))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	var he *apierror.HTTPError
	if !errors.As(got.err, &he) {
		t.Fatalf("terminal error = %T (%v), want *apierror.HTTPError", got.err, got.err)
	}
	if he.StatusCode != 400 {
		t.Errorf("status code = %d, want 400", he.StatusCode)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0 (non-retryable must not fail over)", secondary.calls)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0 for non-retryable error", len(events))
	}
}

// Scenario 5: a clean single-target chain streams through without any failover,
// exercising the len(targets)==1 fast path and asserting zero events.
func TestScenario_SingleTargetCleanStream(t *testing.T) {
	t.Parallel()

	only := &scriptedClient{
		name: "only",
		scriptedChunks: []backend.StreamChunk{
			{Text: "solo-"},
			{Text: "response"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "only", Client: only},
	}, recordingNotifier(&events))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	if got.text != "solo-response" {
		t.Errorf("final text = %q, want %q", got.text, "solo-response")
	}
	if got.err != nil {
		t.Errorf("unexpected terminal error: %v", got.err)
	}
	if only.calls != 1 {
		t.Errorf("only calls = %d, want 1", only.calls)
	}
	if len(events) != 0 {
		t.Errorf("events = %d, want 0", len(events))
	}
}

// Scenario 6: a multi-hop chain where the first two targets fail before content
// with eligible errors, and the third succeeds. Asserts ordered Events and that
// only the surviving target's content reaches the caller (no duplicate output).
func TestScenario_MultiHopFailoverEventOrder(t *testing.T) {
	t.Parallel()

	primary := &scriptedClient{
		name: "primary",
		onStream: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 503, Body: []byte(`{"error":"unavailable"}`)}
		},
	}
	secondary := &scriptedClient{
		name: "secondary",
		onStream: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"quota"}`)}
		},
	}
	tertiary := &scriptedClient{
		name: "tertiary",
		scriptedChunks: []backend.StreamChunk{
			{Text: "tertiary-ok"},
			{Done: true},
		},
	}

	var events []Event
	client := newIsolatedClient([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
		{Name: "tertiary", Client: tertiary},
	}, recordingNotifier(&events))

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("Stream returned direct error: %v", err)
	}
	got := collect(ch)

	if got.text != "tertiary-ok" {
		t.Errorf("final text = %q, want %q", got.text, "tertiary-ok")
	}
	if primary.calls != 1 || secondary.calls != 1 || tertiary.calls != 1 {
		t.Errorf("calls: primary=%d secondary=%d tertiary=%d, want 1/1/1",
			primary.calls, secondary.calls, tertiary.calls)
	}
	if len(events) != 2 {
		t.Fatalf("events = %d, want 2", len(events))
	}
	if events[0].FromTarget != "primary" || events[0].ToTarget != "secondary" {
		t.Errorf("event[0] = %s->%s, want primary->secondary", events[0].FromTarget, events[0].ToTarget)
	}
	if events[1].FromTarget != "secondary" || events[1].ToTarget != "tertiary" {
		t.Errorf("event[1] = %s->%s, want secondary->tertiary", events[1].FromTarget, events[1].ToTarget)
	}
}