package failover

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
)

type mockClient struct {
	streamFn func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error)
	calls    int
}

func (m *mockClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	m.calls++
	if m.streamFn != nil {
		return m.streamFn(ctx, req)
	}
	ch := make(chan backend.StreamChunk, 2)
	ch <- backend.StreamChunk{Text: "mock-response", Done: false}
	ch <- backend.StreamChunk{Done: true}
	close(ch)
	return ch, nil
}

func TestFailover_PrimarySuccess(t *testing.T) {
	primary := &mockClient{}
	secondary := &mockClient{}

	var events []Event
	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, func(e Event) {
		events = append(events, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt-primary"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}

	if text != "mock-response" {
		t.Errorf("got text %q, want 'mock-response'", text)
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
	if secondary.calls != 0 {
		t.Errorf("secondary calls = %d, want 0", secondary.calls)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 events, got %d", len(events))
	}
}

func TestFailover_ImmediateErrorSwitchesToSecondary(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"quota"}`)}
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			if req.Model != "mapped-model" {
				t.Errorf("secondary received model %q, want 'mapped-model'", req.Model)
			}
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "secondary-response", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	var events []Event
	client := New([]Target{
		{
			Name:   "primary",
			Client: primary,
		},
		{
			Name:   "secondary",
			Client: secondary,
			ModelMap: func(m string) string {
				return "mapped-model"
			},
		},
	}, func(e Event) {
		events = append(events, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "original-model"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}

	if text != "secondary-response" {
		t.Errorf("got text %q, want 'secondary-response'", text)
	}
	if primary.calls != 1 {
		t.Errorf("primary calls = %d, want 1", primary.calls)
	}
	if secondary.calls != 1 {
		t.Errorf("secondary calls = %d, want 1", secondary.calls)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].FromTarget != "primary" || events[0].ToTarget != "secondary" {
		t.Errorf("unexpected event: %+v", events[0])
	}
}

func TestFailover_FirstChunk429SwitchesToSecondary(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 1)
			ch <- backend.StreamChunk{
				Error: &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"resource_exhausted"}`)},
				Done:  true,
			}
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "failover-success", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	var events []Event
	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, func(e Event) {
		events = append(events, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}

	if text != "failover-success" {
		t.Errorf("got text %q, want 'failover-success'", text)
	}
	if len(events) != 1 {
		t.Errorf("expected 1 failover event, got %d", len(events))
	}
}

func TestFailover_MidStreamErrorDoesNotBlindRetry(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "partial text already sent", Done: false}
			ch <- backend.StreamChunk{Error: errors.New("connection reset mid-turn"), Done: true}
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{}

	var events []Event
	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, func(e Event) {
		events = append(events, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var sawText string
	var sawErr error
	for c := range ch {
		if c.Text != "" {
			sawText += c.Text
		}
		if c.Error != nil {
			sawErr = c.Error
		}
	}

	if sawText != "partial text already sent" {
		t.Errorf("sawText = %q, want 'partial text already sent'", sawText)
	}
	if sawErr == nil {
		t.Fatal("expected error to be preserved, got nil")
	}
	// Secondary must NOT be called when text has already been emitted!
	if secondary.calls != 0 {
		t.Errorf("secondary should NOT be called on mid-stream failure, got calls = %d", secondary.calls)
	}
	if len(events) != 0 {
		t.Errorf("expected 0 failover events on mid-stream error, got %d", len(events))
	}
}

func TestFailover_AllTargetsFailPreservesHTTPError(t *testing.T) {
	expectedErr := &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"all exhausted"}`)}
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 503, Body: []byte(`{"error":"unavailable"}`)}
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, expectedErr
		},
	}

	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, nil)

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected direct error: %v", err)
	}

	var gotErr error
	for c := range ch {
		if c.Error != nil {
			gotErr = c.Error
		}
	}

	var he *apierror.HTTPError
	if !errors.As(gotErr, &he) {
		t.Fatalf("expected *apierror.HTTPError, got %T: %v", gotErr, gotErr)
	}
	if he.StatusCode != 429 {
		t.Errorf("expected StatusCode 429, got %d", he.StatusCode)
	}
}

func TestFailover_MultiHopAndGlobalNotifier(t *testing.T) {
	ResetGlobalNotifiers()
	defer ResetGlobalNotifiers()

	var globalEvents []Event
	RegisterGlobalNotifier(func(e Event) {
		globalEvents = append(globalEvents, e)
	})

	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 503, Body: []byte(`{"error":"outage"}`)}
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"quota"}`)}
		},
	}
	tertiary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "tertiary-success", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	var localEvents []Event
	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
		{Name: "tertiary", Client: tertiary},
	}, func(e Event) {
		localEvents = append(localEvents, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}

	if text != "tertiary-success" {
		t.Errorf("got %q, want 'tertiary-success'", text)
	}
	if len(localEvents) != 2 {
		t.Fatalf("expected 2 local events, got %d", len(localEvents))
	}
	if localEvents[0].FromTarget != "primary" || localEvents[0].ToTarget != "secondary" {
		t.Errorf("event 0 mismatch: %+v", localEvents[0])
	}
	if localEvents[1].FromTarget != "secondary" || localEvents[1].ToTarget != "tertiary" {
		t.Errorf("event 1 mismatch: %+v", localEvents[1])
	}

	// Verify global notifier also caught both events
	if len(globalEvents) != 2 {
		t.Errorf("expected 2 global events, got %d", len(globalEvents))
	}
}

func TestFailover_NetworkErrorTriggersFailover(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			return nil, errors.New("dial tcp: connection refused")
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "backup-ok", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

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
	if text != "backup-ok" {
		t.Errorf("got text %q, want 'backup-ok'", text)
	}
}

func TestFailover_NonContentChunksDiscardedOnFailover(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			// Non-content ping/metadata chunk before error
			ch <- backend.StreamChunk{Text: "", Done: false}
			ch <- backend.StreamChunk{Error: &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":"quota"}`)}, Done: false}
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "clean-secondary", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, nil)

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var chunks []backend.StreamChunk
	for c := range ch {
		chunks = append(chunks, c)
	}

	if len(chunks) != 2 {
		t.Fatalf("expected exactly 2 chunks from secondary, got %d", len(chunks))
	}
	if chunks[0].Text != "clean-secondary" {
		t.Errorf("chunk 0 text = %q, want 'clean-secondary'", chunks[0].Text)
	}
}

func TestFailover_StreamClosedWithoutContentTriggersFailover(t *testing.T) {
	primary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk)
			// Dropped immediately (0 chunks, premature EOF)
			close(ch)
			return ch, nil
		},
	}
	secondary := &mockClient{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			ch := make(chan backend.StreamChunk, 2)
			ch <- backend.StreamChunk{Text: "recovered-content", Done: false}
			ch <- backend.StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	var events []Event
	client := New([]Target{
		{Name: "primary", Client: primary},
		{Name: "secondary", Client: secondary},
	}, func(e Event) {
		events = append(events, e)
	})

	ch, err := client.Stream(context.Background(), backend.StreamRequest{Model: "gpt"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var text string
	for c := range ch {
		text += c.Text
	}
	if text != "recovered-content" {
		t.Errorf("got text %q, want 'recovered-content'", text)
	}
	if len(events) != 1 {
		t.Fatalf("expected 1 failover event, got %d", len(events))
	}
	if !strings.Contains(events[0].Err.Error(), "stream closed without content") {
		t.Errorf("expected 'stream closed without content', got %v", events[0].Err)
	}
}
