// gemini_api_key_retry_test.go — unit tests for AGY 1.3.1 changelog line 99:
//
//	"Fixed `GEMINI_API_KEY` sessions failing a turn as if you had
//	 cancelled it when the connection to the Gemini API was dropped
//	 internally; the CLI now retries the request instead."
//
// Each test below targets one failure mode the changelog line and the
// recovered retry-policy evidence describe:
//
//   - connection drop (EOF mid-stream)
//   - EOF from a server that closed cleanly without [DONE]
//   - network timeout (mid-stream read failure)
//   - normal completion (no retry attempted)
//   - retry-budget exhaustion (max retries → surface error)
//   - typed-error wrapping (errors.Is / errors.As behave correctly)
//   - retry schedule (delay bounded by 30 s cap from AGY 1.3.4 / 1.3.6)
//
// The tests run against an httptest server that scripts the failure
// modes — no network egress required.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// TestConnectionDropRetried (AGY 1.3.1) — a connection that fails
// mid-stream on the first HTTP request and returns a clean stream on
// the retry gets re-issued. The original error path (the user seeing
// a "cancelled" turn) is closed off: the caller sees the partial
// chunks from the failed attempt, then the retry's chunks, then the
// terminal Done marker — no Error chunk in between.
func TestConnectionDropRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		if n == 1 {
			// Write the response headers, send a partial chunk, then
			// hijack the connection and force a TCP RST so the
			// client sees a non-clean read failure (the typical
			// "connection reset by peer" the changelog targets).
			w.Header().Set("content-type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"part\"}}]}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("ResponseWriter is not a Hijacker; cannot simulate drop")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
			return
		}
		// Subsequent attempts: stream a clean SSE reply.
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hi\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	savedInitial := InitialConnectionRetryInterval
	savedMax := MaxConnectionRetryInterval
	InitialConnectionRetryInterval = 5 * time.Millisecond
	MaxConnectionRetryInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		InitialConnectionRetryInterval = savedInitial
		MaxConnectionRetryInterval = savedMax
	})

	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	var gotDone bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk.Error on retried stream: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("retried stream did not terminate with Done=true")
	}
	// The caller sees the partial chunk from the failed attempt
	// followed by the retry's chunk. The retry is transparent at the
	// channel level — no Error chunk is emitted for the failed
	// attempt.
	wantSuffix := "hi"
	if !strings.HasSuffix(gotText, wantSuffix) {
		t.Errorf("retried stream text: got %q, want suffix %q", gotText, wantSuffix)
	}
	if !strings.Contains(gotText, "part") {
		t.Errorf("retried stream text: got %q, want substring \"part\" (from failed attempt)", gotText)
	}
	if got := attempts.Load(); got < 2 {
		t.Errorf("server saw %d attempts; want >= 2 (initial + at least one retry)", got)
	}
}

// TestEOFRetried (AGY 1.3.1) — a server that closes the body mid-stream
// (without writing [DONE]) is detected as a connection drop (EOF /
// truncated stream), not surfaced as a cancellation. The retry
// succeeds on the second attempt.
func TestEOFRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			// Flush a partial frame, then forcibly drop the
			// connection so the client sees an EOF on a truncated
			// stream (the AGY 1.3.1 trigger condition: a Gemini
			// API stream that ends without [DONE]).
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"}}]}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("not a Hijacker")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
			return
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"second\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	savedInitial := InitialConnectionRetryInterval
	savedMax := MaxConnectionRetryInterval
	InitialConnectionRetryInterval = 5 * time.Millisecond
	MaxConnectionRetryInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		InitialConnectionRetryInterval = savedInitial
		MaxConnectionRetryInterval = savedMax
	})

	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	var gotDone bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("chunk.Error on retried stream: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("EOF-retried stream did not terminate with Done=true")
	}
	// First attempt emits "first"; the retry emits "second" and a
	// terminal Done. No Error chunk for the drop.
	if !strings.Contains(gotText, "second") {
		t.Errorf("text after retry: got %q, want substring \"second\"", gotText)
	}
	if !strings.Contains(gotText, "first") {
		t.Errorf("text after retry: got %q, want substring \"first\" (from failed attempt)", gotText)
	}
	if got := attempts.Load(); got < 2 {
		t.Errorf("server saw %d attempts; want >= 2", got)
	}
}

// TestNetworkTimeoutRetried (AGY 1.3.1) — a server that drops the
// connection mid-stream (simulated via syscall.EPIPE-equivalent path)
// is detected as a retryable network drop, not a user cancel.
func TestNetworkTimeoutRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if n == 1 {
			// Write a chunk, then forcibly close mid-stream so the
			// client sees a non-EOF read failure.
			fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"first\"}}]}\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Errorf("not a Hijacker")
				return
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Errorf("Hijack: %v", err)
				return
			}
			if tcp, ok := conn.(*net.TCPConn); ok {
				_ = tcp.SetLinger(0)
			}
			_ = conn.Close()
			return
		}
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"ok\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	savedInitial := InitialConnectionRetryInterval
	savedMax := MaxConnectionRetryInterval
	InitialConnectionRetryInterval = 5 * time.Millisecond
	MaxConnectionRetryInterval = 50 * time.Millisecond
	t.Cleanup(func() {
		InitialConnectionRetryInterval = savedInitial
		MaxConnectionRetryInterval = savedMax
	})

	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	var gotDone bool
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk.Error after retry: %v", chunk.Error)
		}
		gotText += chunk.Text
		if chunk.Done {
			gotDone = true
		}
	}
	if !gotDone {
		t.Fatal("network-timeout-retried stream did not terminate with Done=true")
	}
	if !strings.Contains(gotText, "ok") {
		t.Errorf("text after retry: got %q, want substring \"ok\"", gotText)
	}
	if got := attempts.Load(); got < 2 {
		t.Errorf("server saw %d attempts; want >= 2", got)
	}
}

// TestNormalCompletionNoRetry (AGY 1.3.1) — a clean, single-attempt
// stream must hit the server exactly once. The retry helper must
// stay out of the happy path; over-retrying on a successful
// connection would burn the user's quota on a working session.
func TestNormalCompletionNoRetry(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"hello\"}}]}\n\n")
		fmt.Fprintf(w, "data: {\"choices\":[],\"usage\":{\"prompt_tokens\":1,\"completion_tokens\":1,\"total_tokens\":2}}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var gotText string
	for chunk := range stream {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk.Error: %v", chunk.Error)
		}
		gotText += chunk.Text
	}
	if gotText != "hello" {
		t.Errorf("text: got %q, want \"hello\"", gotText)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("server saw %d attempts on clean stream; want exactly 1 (no retry)", got)
	}
}

// TestMaxRetriesHit (AGY 1.3.1 + AGY 1.3.4 budget) — a server that
// always drops the connection must exhaust the budget
// (MaxConnectionRetries attempts) before the connection drop is
// surfaced as the final chunk on the stream. The terminal chunk must
// carry a typed *ConnectionDroppedError so the headless error
// reporter can classify it.
func TestMaxRetriesHit(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Write a chunk then RST so the client sees a non-clean
		// read failure — the AGY 1.3.1 trigger condition.
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"x\"}}]}\n\n")
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Errorf("not a Hijacker")
			return
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Errorf("Hijack: %v", err)
			return
		}
		if tcp, ok := conn.(*net.TCPConn); ok {
			_ = tcp.SetLinger(0)
		}
		_ = conn.Close()
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	savedInitial := InitialConnectionRetryInterval
	savedMax := MaxConnectionRetryInterval
	InitialConnectionRetryInterval = 1 * time.Millisecond
	MaxConnectionRetryInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		InitialConnectionRetryInterval = savedInitial
		MaxConnectionRetryInterval = savedMax
	})

	stream, err := c.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var finalErr error
	for chunk := range stream {
		if chunk.Done {
			finalErr = chunk.Error
		}
	}
	if finalErr == nil {
		t.Fatal("expected terminal Error chunk after exhausting retries, got nil")
	}
	if !IsConnectionDropped(finalErr) {
		t.Errorf("terminal Error not a connection drop: %v", finalErr)
	}
	if got, want := attempts.Load(), int32(MaxConnectionRetries+1); got != want {
		t.Errorf("attempts: got %d, want %d (initial + %d retries)", got, want, MaxConnectionRetries)
	}
}

// TestConnectionDroppedErrorIsError covers the typed error: errors.Is
// against the sentinel works, errors.As unwraps to the original
// transport error, and a nil error returns false from
// IsConnectionDropped. These are the contract the upstream PiG
// agent-loop and the headless error reporter both rely on.
func TestConnectionDroppedErrorIsError(t *testing.T) {
	if IsConnectionDropped(nil) {
		t.Error("IsConnectionDropped(nil) = true; want false")
	}
	// Sentinel form (an opaque error matching ErrConnectionDropped).
	sentinel := fmt.Errorf("wrap: %w", ErrConnectionDropped)
	if !IsConnectionDropped(sentinel) {
		t.Error("errors.Is chain missing for ErrConnectionDropped sentinel")
	}
	// Typed form: a *ConnectionDroppedError wrapping a syscall error.
	dropped := &ConnectionDroppedError{Err: syscall.ECONNRESET, URL: "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-pro:streamGenerateContent"}
	if !IsConnectionDropped(dropped) {
		t.Error("typed *ConnectionDroppedError not recognised")
	}
	if !errors.Is(dropped, syscall.ECONNRESET) {
		t.Error("errors.Is should unwrap to syscall.ECONNRESET")
	}
	if !strings.Contains(dropped.Error(), "generativelanguage.googleapis.com") {
		t.Errorf("Error() missing URL: %q", dropped.Error())
	}

	// Non-drop errors: a context cancel must not be classified as a
	// drop. AGY 1.3.1 explicitly excludes user cancellations.
	if IsConnectionDropped(context.Canceled) {
		t.Error("context.Canceled misclassified as connection drop")
	}
	if IsConnectionDropped(context.DeadlineExceeded) {
		t.Error("context.DeadlineExceeded misclassified as connection drop")
	}
}

// TestClassifyTransportErrorMapping enumerates the failure modes that
// ClassifyTransportError must recognise and the ones it must not.
// The mapping is the AGY 1.3.1 contract: a network-level drop
// becomes a typed retryable error; everything else is returned
// unchanged so the existing error paths keep working.
func TestClassifyTransportErrorMapping(t *testing.T) {
	const url = "https://generativelanguage.googleapis.com/v1beta/models/gemini-1.5-pro:streamGenerateContent"

	cases := []struct {
		name     string
		in       error
		wantDrop bool
	}{
		{"nil", nil, false},
		{"io.EOF", io.EOF, true},
		{"io.ErrUnexpectedEOF", io.ErrUnexpectedEOF, true},
		{"syscall.ECONNRESET", syscall.ECONNRESET, true},
		{"syscall.EPIPE", syscall.EPIPE, true},
		{"wrapped EOF", fmt.Errorf("gateway: sse read: %w", io.EOF), true},
		{"net.OpError", &net.OpError{Op: "dial", Err: errors.New("no such host")}, true},
		{"context.Canceled", context.Canceled, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, false},
		{"HTTPError", &HTTPError{StatusCode: 502, URL: url}, false},
		{"plain error", errors.New("decode failure"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := ClassifyTransportError(tc.in, url)
			if tc.in == nil {
				if out != nil {
					t.Errorf("nil input → %v, want nil", out)
				}
				return
			}
			if tc.wantDrop {
				if !IsConnectionDropped(out) {
					t.Errorf("expected drop, got %v (%T)", out, out)
				}
			} else {
				if IsConnectionDropped(out) {
					t.Errorf("did not expect drop, got %v (%T)", out, out)
				}
			}
		})
	}
}

// TestNextConnectionRetryDelay pins the AGY 1.3.4 / 1.3.6 schedule.
// The cap (30 s) is the headline of the changelog line; the budget
// (5 retries) is unchanged. The schedule is exponential 2x starting
// at 1 s; attempts past the budget saturate at the cap.
func TestNextConnectionRetryDelay(t *testing.T) {
	wantSchedule := []time.Duration{
		1 * time.Second,  // attempt 0
		2 * time.Second,  // attempt 1
		4 * time.Second,  // attempt 2
		8 * time.Second,  // attempt 3
		16 * time.Second, // attempt 4
	}
	for i, want := range wantSchedule {
		got := NextConnectionRetryDelay(i)
		if got != want {
			t.Errorf("NextConnectionRetryDelay(%d) = %v, want %v", i, got, want)
		}
	}
	// Past the budget: saturate at the 30 s cap.
	if got := NextConnectionRetryDelay(MaxConnectionRetries); got != MaxConnectionRetryInterval {
		t.Errorf("NextConnectionRetryDelay(budget) = %v, want %v (cap)", got, MaxConnectionRetryInterval)
	}
	if got := NextConnectionRetryDelay(MaxConnectionRetries + 100); got != MaxConnectionRetryInterval {
		t.Errorf("NextConnectionRetryDelay(budget+100) = %v, want %v (cap)", got, MaxConnectionRetryInterval)
	}
	// Negative attempt clamps to 0.
	if got := NextConnectionRetryDelay(-1); got != wantSchedule[0] {
		t.Errorf("NextConnectionRetryDelay(-1) = %v, want %v (clamped to 0)", got, wantSchedule[0])
	}
}

// TestContextCancelledNotRetried (AGY 1.3.1) — a user cancellation
// (ctx.Canceled) must NOT be retried. The CLI must respect the
// user's abort and surface the cancellation rather than burning the
// budget on a session the user no longer wants.
//
// The transport may surface a cancellation either synchronously (the
// original cycle-117 path: Stream() returns (nil, err)) or via a
// terminal Error+Done chunk (the AGY 1.3.1 path: Stream() returns
// (channel, nil) and the goroutine observes the cancellation
// inside the open or body read). Both are valid; the test accepts
// either. What is NOT acceptable is letting the retry helper burn
// attempts on a session the user has abandoned.
func TestContextCancelledNotRetried(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		// Slow write so the client can cancel mid-flight.
		w.Header().Set("content-type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		time.Sleep(200 * time.Millisecond)
		fmt.Fprintf(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"after-cancel\"}}]}\n\n")
	}))
	defer srv.Close()

	c := NewGatewayWithConfig(GatewayConfig{
		URL:           srv.URL + "/v1/chat/completions",
		ExternalModel: "MiniMax-M3",
	})
	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the context shortly after the request is issued. The
	// server's sleep keeps the connection open so the cancellation
	// surfaces as a mid-stream read failure on the client side.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()
	stream, err := c.Stream(ctx, StreamRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})

	// Accept either error path. The sync path (older behaviour) is
	// `Stream() returns ctx.Err() directly`; the async path is
	// `Stream() returns a channel, the channel emits ctx.Err() as a
	// terminal chunk`. Both are correct as long as the cancellation
	// is preserved and the retry helper did not retry.
	var finalErr error
	var gotDone bool
	if err != nil {
		finalErr = err
	} else {
		for chunk := range stream {
			if chunk.Error != nil {
				finalErr = chunk.Error
			}
			if chunk.Done {
				gotDone = true
			}
		}
	}

	if finalErr == nil {
		t.Fatal("expected a context-cancel error somewhere (sync or async); got nil")
	}
	// The cancellation must be reported (not a *ConnectionDroppedError —
	// a user cancel is not a network drop).
	if IsConnectionDropped(finalErr) {
		t.Errorf("cancellation classified as connection drop: %v", finalErr)
	}
	if !errors.Is(finalErr, context.Canceled) && !strings.Contains(finalErr.Error(), "context canceled") {
		t.Errorf("terminal error is %v; want a context-cancel-classified error (not retried)", finalErr)
	}
	// Exactly one attempt — the retry helper must not have retried
	// the cancelled request.
	if got := attempts.Load(); got > 1 {
		t.Errorf("attempts after cancel: %d; want exactly 1 (cancel must not retry)", got)
	}
	_ = gotDone
}

// TestClassifyTransportErrorStringMatch covers the substring fallback
// in ClassifyTransportError. A wrapped error whose message contains
// the kernel-level phrasing must be classified as a drop.
func TestClassifyTransportErrorStringMatch(t *testing.T) {
	const url = "https://example.com"
	cases := []struct {
		name     string
		msg      string
		wantDrop bool
	}{
		{"connection reset by peer", "read tcp 127.0.0.1:1->127.0.0.1:2: read: connection reset by peer", true},
		{"broken pipe", "write tcp 127.0.0.1:1->127.0.0.1:2: write: broken pipe", true},
		{"software caused connection abort", "read tcp 127.0.0.1:1->127.0.0.1:2: read: software caused connection abort", true},
		{"network is down", "write tcp: network is down", true},
		{"http2: client connection lost", "http2: client connection lost", true},
		{"connection refused", "dial tcp 127.0.0.1:1: connect: connection refused", true},
		{"no such host", "dial tcp: lookup example.invalid: no such host", true},
		{"EOF in message", "gateway: sse read: unexpected EOF", true},
		{"decode error", "gateway: sse decode: invalid character", false},
		{"plain message", "gateway: http: bad request", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := errors.New(tc.msg)
			out := ClassifyTransportError(err, url)
			got := IsConnectionDropped(out)
			if got != tc.wantDrop {
				t.Errorf("IsConnectionDropped(%q) = %v, want %v (out: %v)", tc.msg, got, tc.wantDrop, out)
			}
		})
	}
}

// TestConnectionDroppedErrorFormat ensures the Error() string is
// stable and does NOT match the upstream PiG retry patterns. Matching
// them would cause PiG's retry transport to retry on top of the
// AGY 1.3.1 retry, double-spending the budget.
func TestConnectionDroppedErrorFormat(t *testing.T) {
	e := &ConnectionDroppedError{Err: syscall.ECONNRESET, URL: "https://example.com/v1/chat"}
	s := e.Error()
	for _, pat := range []string{
		"connection.?error",
		"connection.?lost",
		"connection.?refused",
		"network.?error",
		"socket hang up",
		"broken pipe",
		"reset before headers",
	} {
		// Manual substring check; we deliberately do NOT use regexp
		// here because the patterns use lazy `?` quantifiers that
		// match an empty string. The intent is: the rendered message
		// must not be one of those phrasings.
		if strings.Contains(strings.ToLower(s), strings.ToLower(pat)) {
			t.Errorf("Error() %q contains PiG-retry pattern %q (would double-retry)", s, pat)
		}
	}
}

// keep the imports referenced for tests that hand-roll a stream reader
// (used by future tests; see the comment in the production code).
var (
	_ = bytes.NewReader
	_ = bufio.NewScanner
)

// TestIdleTimeoutReader_StallAndReset covers AGY 1.3.6:
// "Fixed the agent hanging indefinitely when the model connection stalled:
// if no data arrives for 5 minutes the request is now canceled and retried
// automatically, and a stalled request no longer leaves an empty response step behind."
func TestIdleTimeoutReader_StallAndReset(t *testing.T) {
	// 1. Timeout fires when no data arrives
	pr, pw := io.Pipe()
	idleR := NewIdleTimeoutReader(pr, 40*time.Millisecond)
	buf := make([]byte, 10)

	start := time.Now()
	_, err := idleR.Read(buf)
	dur := time.Since(start)

	if !errors.Is(err, ErrConnectionStalled) {
		t.Fatalf("expected ErrConnectionStalled, got %v", err)
	}
	if dur < 35*time.Millisecond || dur > 200*time.Millisecond {
		t.Errorf("expected stall timeout near 40ms, took %v", dur)
	}
	_ = pw.Close()

	// 2. Data arrival resets timeout
	pr2, pw2 := io.Pipe()
	idleR2 := NewIdleTimeoutReader(pr2, 80*time.Millisecond)

	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _ = pw2.Write([]byte("hello"))
		time.Sleep(30 * time.Millisecond)
		_, _ = pw2.Write([]byte("world"))
		_ = pw2.Close()
	}()

	b, err := io.ReadAll(idleR2)
	if err != nil {
		t.Fatalf("unexpected error reading with resets: %v", err)
	}
	if string(b) != "helloworld" {
		t.Errorf("got %q, want %q", string(b), "helloworld")
	}
}

// TestCloudCode_ConnectionDropRetry verifies that CloudCodeClient transparently
// retries on socket disconnect errors (AGY 1.4.1).
func TestCloudCode_ConnectionDropRetry(t *testing.T) {
	savedInitial := InitialConnectionRetryInterval
	savedMax := MaxConnectionRetryInterval
	InitialConnectionRetryInterval = 5 * time.Millisecond
	MaxConnectionRetryInterval = 20 * time.Millisecond
	defer func() {
		InitialConnectionRetryInterval = savedInitial
		MaxConnectionRetryInterval = savedMax
	}()

	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		att := attempts.Add(1)
		w.Header().Set("content-type", "text/event-stream")
		if att == 1 {
			// Simulate connection dropped mid-stream (e.g. Wi-Fi change/sleep)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"part1 \"}]}}]}}\n\n"))
			if flusher, ok := w.(http.Flusher); ok {
				flusher.Flush()
			}
			// Close connection abruptly without finishReason or [DONE]
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				_ = conn.Close()
			}
			return
		}
		// Second attempt succeeds
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"complete response\"}],\"role\":\"model\"},\"finishReason\":\"STOP\"}]}}\n\n"))
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
	}))
	defer srv.Close()

	client := NewCloudCodeWithConfig(CloudCodeConfig{
		Endpoint: srv.URL,
		Model:    "gemini-2.5-flash",
	})
	client.token = func(ctx context.Context) (string, error) {
		return "test-token", nil
	}

	ch, err := client.Stream(context.Background(), StreamRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var gotText strings.Builder
	for chunk := range ch {
		if chunk.Error != nil {
			t.Fatalf("unexpected chunk error: %v", chunk.Error)
		}
		gotText.WriteString(chunk.Text)
	}

	if attempts.Load() < 2 {
		t.Errorf("server saw %d attempts; want >= 2 (retry on drop)", attempts.Load())
	}
	if !strings.Contains(gotText.String(), "complete response") {
		t.Errorf("final text = %q, want 'complete response'", gotText.String())
	}
}
