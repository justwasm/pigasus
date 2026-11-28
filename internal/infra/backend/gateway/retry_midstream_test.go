// retry_midstream_test.go — the mid-stream half of the 1.5.0 retry rule.
//
// AGY 1.5.0:
//
//	"Fixed certain models failing a turn on rate-limit (429) and server (5xx)
//	 errors instead of retrying them, now honoring the server's Retry-After
//	 header".
//
// The open path (streampipe.ExecuteWithRetry) honoured that from the start, so
// a 500 in the status line was retried. The same failure arriving *inside* an
// already-open stream as an SSE `error` frame was not: streampipe.Pipeline.Run
// only retried connection drops and the 1.5.17 capacity case, so an upstream
// 500 killed the turn on the first attempt.
//
// That is the shape the user hit:
//
//	[tui] turn failed: gateway: upstream 500: Unknown error
//
// with no [streampipe] retry line anywhere in the session log. The tests below
// drive the real GatewayClient — no hand-built chunks — so they fail if either
// the SSE error mapping or the pipeline predicate regresses.

package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
	"github.com/justwasm/pigasus/internal/infra/backend/retrytest"
)

// fastGatewayRetries shortens the exponential schedule so a test spends
// milliseconds instead of the production 1s/2s/4s. The package's TestMain
// already shortens it (retrytest.Short), so a retry costs milliseconds even
// where a test forgets; this widens the *cap* as well, because a test that
// asserts the server's Retry-After was honoured needs a cap above the value
// the server asks for.
func fastGatewayRetries(t *testing.T) {
	t.Helper()
	retrytest.Fast(t)
	retry.MaxConnectionRetryInterval = 50 * time.Millisecond
}

// TestMidStream500ErrorFrameIsRetried is the regression test for the reported
// session kill: the first attempt streams a text delta and then an SSE error
// frame carrying 500, the retry answers cleanly, and the caller sees one
// continuous answer with no error chunk.
func TestMidStream500ErrorFrameIsRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hel\"}}]}\n\n")
		if flusher != nil {
			flusher.Flush()
		}
		if n == 1 {
			// The upstream gateway reports its own failure as an error frame on
			// an already-open 200 stream; http_error.go renders this exact body
			// as "gateway: upstream 500: Unknown error".
			fmt.Fprint(w, "data: {\"error\":{\"code\":500,\"type\":\"server_error\",\"message\":\"Unknown error\"}}\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"lo\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	fastGatewayRetries(t)

	stream, err := c.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotText string
	var gotDone bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk.Error on a retried stream: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("stream did not terminate with Done=true")
	}
	// The failed attempt's "Hel" already reached the caller and must not be
	// repeated; the retry re-streams the whole answer and the tap swallows the
	// replayed prefix.
	if gotText != "Hello" {
		t.Fatalf("delivered text = %q, want %q", gotText, "Hello")
	}
	if n := attempts.Load(); n < 2 {
		t.Fatalf("server saw %d attempt(s); the mid-stream 500 was not retried", n)
	}
}

// TestMidStream429ErrorFrameHonoursRetryAfter pins the other half of the 1.5.0
// sentence: the retry delay comes from the server, not from the client's own
// backoff. The frame's retry_after_ms is deliberately longer than the
// configured cap so the assertion also covers the clamp.
func TestMidStream429ErrorFrameHonoursRetryAfter(t *testing.T) {
	var attempts atomic.Int32
	var firstRetryAt time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, _ := w.(http.Flusher)
		if n == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"a\"}}]}\n\n")
			if flusher != nil {
				flusher.Flush()
			}
			// Google RPC RetryInfo in the error frame, the shape apierror's
			// RetryAfterDuration already parses for a 429 status line.
			fmt.Fprint(w, "data: {\"error\":{\"code\":429,\"type\":\"rate_limit_error\",\"message\":\"slow down\","+
				"\"details\":[{\"@type\":\"type.googleapis.com/google.rpc.RetryInfo\",\"retryDelay\":\"0.030s\"}]}}\n\n")
			return
		}
		firstRetryAt = time.Now()
		fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"b\"}}]}\n\n")
		fmt.Fprint(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	fastGatewayRetries(t)

	start := time.Now()
	stream, err := c.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk.Error on a retried stream: %v", chunk.Error)
		}
		gotText += chunk.Text
	}
	if gotText != "ab" {
		t.Fatalf("delivered text = %q, want %q", gotText, "ab")
	}
	if n := attempts.Load(); n < 2 {
		t.Fatalf("server saw %d attempt(s); the mid-stream 429 was not retried", n)
	}
	// The server asked for 30ms, which is above the 1ms test schedule; either
	// way the wait must be driven by the frame rather than by the backoff.
	if elapsed := firstRetryAt.Sub(start); elapsed < 20*time.Millisecond {
		t.Fatalf("retried after %v; the frame's RetryInfo was ignored", elapsed)
	}
}

// TestMidStream500AlwaysFailingTerminatesQuickly is the guard against the
// class of bug that made three classification subtests take eight minutes
// each: a server that answers the same retryable frame forever, and a caller
// that did not pin the budget. The package's TestMain shortens the schedule
// (retrytest.Short), so the full production budget collapses to milliseconds —
// the assertion is that a *forgotten* pin can no longer cost a coffee break,
// not that retrying is wrong. The error must still surface once the budget is
// spent.
func TestMidStream500AlwaysFailingTerminatesQuickly(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"error\":{\"code\":500,\"type\":\"server_error\",\"message\":\"Unknown error\"}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})

	// Deliberately *no* retry pin and no budget override: this is the shape of
	// a test that predates the mid-stream retry rule.
	start := time.Now()
	stream, err := c.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	for chunk := range stream {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if gotErr == nil {
		t.Fatal("a stream that fails on every attempt must report the upstream error")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("the exhausted budget took %v; the package-level schedule is not shortened", elapsed)
	}
	if n := attempts.Load(); n < 2 {
		t.Fatalf("server saw %d attempt(s); a mid-stream 500 must be retried", n)
	}
}

// TestMidStream400ErrorFrameIsNotRetried keeps the boundary: a malformed
// request describes this answer, so replaying it cannot help. Only one attempt
// may reach the server.
func TestMidStream400ErrorFrameIsNotRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "data: {\"error\":{\"code\":400,\"type\":\"invalid_request_error\",\"message\":\"bad request\"}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	fastGatewayRetries(t)

	stream, err := c.Stream(context.Background(), backend.StreamRequest{
		Messages: []backend.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotErr error
	for chunk := range stream {
		if chunk.Error != nil {
			gotErr = chunk.Error
		}
	}
	if gotErr == nil {
		t.Fatal("want the 400 to surface as a terminal error")
	}
	if n := attempts.Load(); n != 1 {
		t.Fatalf("server saw %d attempts, want 1 (a 400 must not be retried)", n)
	}
}
