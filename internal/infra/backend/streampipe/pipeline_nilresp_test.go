package streampipe

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
)

// fastRetries shrinks the retry backoff so the exhaustion path runs in
// milliseconds instead of seconds.
func fastRetries(t *testing.T) {
	t.Helper()
	origInitial := retry.InitialConnectionRetryInterval
	origMax := retry.MaxConnectionRetryInterval
	retry.InitialConnectionRetryInterval = time.Millisecond
	retry.MaxConnectionRetryInterval = 2 * time.Millisecond
	t.Cleanup(func() {
		retry.InitialConnectionRetryInterval = origInitial
		retry.MaxConnectionRetryInterval = origMax
	})
}

// TestPipelineNilResponseFromReopenIsRetriedNotPanicked pins the gateway
// crash: a failed reopen leaves resp nil, and the next loop iteration used to
// hand that nil to ReadBody, where every reader faults on resp.Body at offset
// 0x40.
//
// ReadBody now never sees a nil response. The missing response is reported as
// a connection drop, the retry budget is consumed, and the stream ends with a
// Done+Error chunk instead of panicking.
func TestPipelineNilResponseFromReopenIsRetriedNotPanicked(t *testing.T) {
	fastRetries(t)

	openCalls := 0
	readCalls := 0
	var sawNilResp bool

	p := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			openCalls++
			// Reopen fails with a connection-class error and no response.
			return nil, &retry.ConnectionDroppedError{URL: "http://test", Err: io.ErrUnexpectedEOF}
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			readCalls++
			if resp == nil {
				sawNilResp = true
				return errors.New("ReadBody must never receive a nil response")
			}
			// The first attempt drops mid-stream; that is what schedules a reopen.
			return &retry.ConnectionDroppedError{URL: "http://test", Err: io.ErrUnexpectedEOF}
		},
	}

	initialResp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("data: hello\n")),
	}
	out := make(chan backend.StreamChunk, 8)
	ctx := backend.ContextWithMaxRetries(context.Background(), 2)
	p.Run(ctx, initialResp, "req", out)
	close(out)

	var chunks []backend.StreamChunk
	for c := range out {
		chunks = append(chunks, c)
	}

	if sawNilResp {
		t.Fatal("ReadBody was called with a nil *http.Response")
	}
	// Only the initial attempt reaches ReadBody: every reopen returns no
	// response, so the remaining attempts are absorbed by the nil guard.
	if readCalls != 1 {
		t.Fatalf("readCalls = %d, want 1 (only the initial response is real)", readCalls)
	}
	if openCalls != 2 {
		t.Fatalf("openCalls = %d, want 2 (one reopen per retry)", openCalls)
	}
	if len(chunks) == 0 {
		t.Fatal("no chunks emitted; the stream must terminate with a Done or Error chunk")
	}
	last := chunks[len(chunks)-1]
	if !last.Done {
		t.Fatalf("last chunk = %+v, want Done=true", last)
	}
	if last.Error == nil {
		t.Fatal("an exhausted retry budget must surface the connection error")
	}
	if !retry.IsConnectionDropped(last.Error) {
		t.Fatalf("last error = %v, want a connection-dropped error", last.Error)
	}
}

// TestPipelineNilInitialResponseIsHandled covers a caller that passes no
// initial response at all: the pipeline must retry rather than dereference nil.
func TestPipelineNilInitialResponseIsHandled(t *testing.T) {
	fastRetries(t)

	readCalls := 0
	p := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader("data: ok\n")),
			}, nil
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			if resp == nil {
				t.Error("ReadBody was called with a nil *http.Response")
				return nil
			}
			readCalls++
			out <- backend.StreamChunk{Text: "recovered"}
			out <- backend.StreamChunk{Done: true}
			return nil
		},
	}

	out := make(chan backend.StreamChunk, 8)
	ctx := backend.ContextWithMaxRetries(context.Background(), 1)
	p.Run(ctx, nil, "req", out)
	close(out)

	var got string
	for c := range out {
		if c.Text != "" {
			got = c.Text
		}
	}
	if got != "recovered" {
		t.Fatalf("text = %q, want %q", got, "recovered")
	}
	if readCalls != 1 {
		t.Fatalf("readCalls = %d, want 1 (the nil initial attempt must not reach ReadBody)", readCalls)
	}
}

// TestPipelineNilInitialResponseWithNoRetryBudgetFails proves a nil response
// cannot spin forever: with no retries left the stream ends with an error.
func TestPipelineNilInitialResponseWithNoRetryBudgetFails(t *testing.T) {
	fastRetries(t)

	p := Pipeline[string]{
		EndpointURL: "http://test",
		Open: func(ctx context.Context, req string) (*http.Response, error) {
			return nil, nil // reopen yields neither response nor error
		},
		ReadBody: func(ctx context.Context, resp *http.Response, out chan<- backend.StreamChunk) error {
			t.Error("ReadBody was called with a nil *http.Response")
			return nil
		},
	}

	out := make(chan backend.StreamChunk, 8)
	ctx := backend.ContextWithMaxRetries(context.Background(), 0)
	p.Run(ctx, nil, "req", out)
	close(out)

	var last *backend.StreamChunk
	for c := range out {
		chunk := c
		last = &chunk
	}
	if last == nil || !last.Done {
		t.Fatalf("last chunk = %+v, want Done=true", last)
	}
	if last.Error == nil {
		t.Fatal("want an error chunk, got none")
	}
	if !retry.IsConnectionDropped(last.Error) {
		t.Fatalf("error = %v, want connection-dropped", last.Error)
	}
}
