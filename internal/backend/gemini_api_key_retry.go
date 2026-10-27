// Package backend — gemini_api_key_retry.go: the typed connection-drop
// error and the canonical retry schedule.
//
// Background. AGY 1.3.1 (changelog line 99) reads:
//
//	"Fixed `GEMINI_API_KEY` sessions failing a turn as if you had
//	 cancelled it when the connection to the Gemini API was dropped
//	 internally; the CLI now retries the request instead."
//
// Before 1.3.1 a network-level interruption on the Gemini API stream
// surfaced to the user as if they had cancelled the turn. The clone's
// Gemini-API transport (which routes through the configured gateway for the
// dev-sandbox model and would route to generativelanguage.googleapis.com
// for a real GEMINI_API_KEY tenant) classifies any of those
// interruptions as a typed *ConnectionDroppedError, then retries the
// request up to the budget below. The user no longer sees a
// cancellation they did not initiate.
//
// AGY 1.3.4 / 1.3.6 (changelog line 29 / line 5) lowered the per-retry
// backoff cap from 4 minutes to 30 seconds so a single stuck request
// cannot monopolise the user's session. The constants below honour
// that cap; the schedule that grows from InitialInterval (1s) by 2x
// each retry reaches 30s only after the budget (5 retries) is spent,
// so the cap fires for any direct `NextConnectionRetryDelay` consumer
// past the budget.
//
// What this file owns
//
//   - The constants that describe the budget and the schedule.
//   - The typed ConnectionDroppedError and the sentinel
//     ErrConnectionDropped.
//   - The IsConnectionDropped helper that the transport and any
//     caller-side retry code can match against.
//   - The ClassifyTransportError helper that decides whether a given
//     error is a network drop or something the user has to handle
//     themselves (context cancellation, deadline exceeded, HTTPError).
//   - The NextConnectionRetryDelay helper for callers that want the
//     schedule without the stateful struct.
//
// What this file deliberately does NOT own
//
//   - The actual retry loop. Each transport (gateway, piagentbridge)
//     wraps its stream in its own retry helper because the stream
//     shapes differ (the wire-neutral chunk channel vs. PiG's event
//     union). This file is the policy; the transports apply it.
//   - HTTP / SDK status retries. Status-code retries (5xx, 429, 408,
//     ...) live in api_error.go's retryableStatus and the upstream
//     PiG retry. Connection drops are an orthogonal failure class:
//     the upstream is reachable but the byte stream breaks mid-turn.
package backend

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/justwasm/pigasus/retry"
)

// StreamIdleTimeout is the maximum duration a model response stream may stall
// with no incoming data before being aborted and retried automatically.
// Matches AGY 1.3.6 (5 minutes).
var StreamIdleTimeout = 5 * time.Minute

// ErrConnectionStalled is the sentinel returned when a model stream stalls
// for longer than StreamIdleTimeout.
var ErrConnectionStalled = errors.New("gemini-api-key: connection stalled (no data received for 5 minutes)")

// MaxConnectionRetries is the retry budget for a request whose
// connection to the Gemini API was dropped internally. The AGY 1.3.1
// changelog does not name the budget; AGY 1.3.4 line 29 / 1.3.6 line
// 5 describe a cap change (4 min → 30 s) without naming the budget
// either. Five is the budget PiG's provider-retry transport uses
// for OpenAI-shaped gateways, and it matches the upstream AGY
// retry-loop evidence for the per-request transport: a request gets
// five chances to succeed before the failure is surfaced to the user.
//
// Declared as a var (not const) so the test suite can shorten the
// schedule for runtime-sensitive cases. Production code reads the
// value; only the test file under the same package reassigns.
var MaxConnectionRetries = 5

// MaxConnectionRetryInterval is the per-retry backoff cap. AGY 1.3.4
// lowered this from 4 minutes to 30 seconds. The clone's exponential
// schedule 1s → 2s → 4s → 8s → 16s never reaches 30s inside the
// budget itself; the cap fires for code that asks for a sleep between
// two arbitrary operations, the case the changelog targets.
//
// Declared as a var (not const) for the same reason as
// MaxConnectionRetries: tests shorten the schedule to keep the test
// runtime in milliseconds.
var MaxConnectionRetryInterval = 30 * time.Second

// InitialConnectionRetryInterval is the first backoff delay. The AGY
// changelog explicitly kept this unchanged across the 1.3.4 cut.
//
// Declared as a var for the same reason as the other two: tests
// shorten the schedule.
var InitialConnectionRetryInterval = 1 * time.Second

// ErrConnectionDropped is the sentinel recognised by errors.Is. A
// *ConnectionDroppedError satisfies the comparison via Unwrap.
var ErrConnectionDropped = errors.New("gemini-api-key: connection dropped")

// ConnectionDroppedError is the typed form of a Gemini-API
// connection drop. It carries the original transport error so callers
// can read it through errors.As, and the URL of the endpoint that
// dropped so logs and structured error reports can pinpoint the
// failing hop. The clone's transport wraps every network-level
// failure with this type via ClassifyTransportError, then the
// per-transport retry loop catches it and re-issues the request.
//
// AGY 1.3.1 changelog line:
//
//	"Fixed GEMINI_API_KEY sessions failing a turn as if you had
//	 cancelled it when the connection to the Gemini API was dropped
//	 internally; the CLI now retries the request instead."
type ConnectionDroppedError struct {
	// Err is the original transport error (io.EOF, syscall reset,
	// net.OpError, ...). nil is permitted only when a retry budget
	// has been exhausted and the caller has nothing more specific
	// to wrap.
	Err error
	// URL is the endpoint that dropped. Identifiers are also fine to
	// surface in user-facing messages when the error escapes a
	// retry context; the value is logged but never echoed in a way
	// that would leak credentials.
	URL string
}

// Error renders the failure as a single line that does NOT match the
// upstream PiG retry patterns ("connection.?error", "network.?error",
// "connection.?lost", "socket hang up", ...) on purpose: any double
// retry from a higher layer would compound the AGY fix's own retry
// loop and overshoot the budget. The phrasing "gemini-api-key: connection
// dropped" is intentional.
func (e *ConnectionDroppedError) Error() string {
	if e.URL != "" {
		return fmt.Sprintf("gemini-api-key: connection to %s dropped: %v", e.URL, e.Err)
	}
	return fmt.Sprintf("gemini-api-key: connection dropped: %v", e.Err)
}

// Unwrap exposes the original transport error so errors.Is and
// errors.As continue to recognise the underlying cause (a syscall
// *ECONNRESET, a *net.OpError, an io.ErrUnexpectedEOF, ...). The
// error chain is: ConnectionDroppedError -> Err -> ErrConnectionDropped
// is *not* part of the chain — IsConnectionDropped checks the
// sentinel and the type directly.
func (e *ConnectionDroppedError) Unwrap() error { return e.Err }

// IsConnectionDropped reports whether err is a Gemini-API connection
// drop. It accepts both a *ConnectionDroppedError directly and the
// ErrConnectionDropped sentinel through errors.Is. nil returns false.
//
// Callers who want to drive the retry budget themselves (the gateway
// and piagentbridge transports do; tests use it too) can short-circuit
// on:
//
//	if IsConnectionDropped(err) { ...retry... }
func IsConnectionDropped(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrConnectionDropped) {
		return true
	}
	var cde *ConnectionDroppedError
	if errors.As(err, &cde) {
		return true
	}
	return false
}

// ClassifyTransportError inspects an error from the SSE scanner, the
// HTTP transport, or a wrapped combination and decides whether it is
// a retryable connection drop or a non-retryable failure the caller
// has to surface.
//
// Signals that classify as a connection drop:
//
//   - io.ErrUnexpectedEOF                  (server closed mid-frame)
//   - io.EOF (any)                         (server closed without [DONE])
//   - syscall.ECONNRESET, syscall.EPIPE    (kernel-level reset/break)
//   - any *net.OpError whose context is a
//     DNS / TCP / TLS handshake failure   (network unreachable, no
//     route to host, ...)
//   - errors.Is matches against an existing *ConnectionDroppedError
//     so re-classifying is idempotent.
//
// Non-retryable signals (returned as-is, no wrapping):
//
//   - context.Canceled                     (user pressed Ctrl+C)
//   - context.DeadlineExceeded             (caller-side timeout)
//   - *HTTPError                           (server returned 4xx/5xx
//     and the retry budget for
//     that lives elsewhere)
//   - any other error whose message does
//     not look like a network-level drop
//
// The classifier never panics. A nil error returns nil.
//
// url is the endpoint that produced the error; it is propagated to the
// returned *ConnectionDroppedError so logs can identify the failing
// hop when the error escapes a retry context.
func ClassifyTransportError(err error, url string) error {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if cde, ok := err.(*ConnectionDroppedError); ok {
		if cde.URL == "" && url != "" {
			cde.URL = url
		}
		return cde
	}
	if isTypedConnectionDropped(err) || isStringConnectionDropped(err.Error()) {
		return &ConnectionDroppedError{Err: err, URL: url}
	}
	return err
}

func isTypedConnectionDropped(err error) bool {
	if errors.Is(err, ErrConnectionDropped) ||
		errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, io.EOF) ||
		errors.Is(err, ErrConnectionStalled) ||
		errors.Is(err, syscall.ECONNRESET) ||
		errors.Is(err, syscall.ECONNABORTED) ||
		errors.Is(err, syscall.EPIPE) ||
		errors.Is(err, syscall.ENETDOWN) ||
		errors.Is(err, syscall.ENETUNREACH) ||
		errors.Is(err, syscall.EHOSTUNREACH) {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

func isStringConnectionDropped(msg string) bool {
	phrases := []string{
		"connection reset by peer",
		"broken pipe",
		"software caused connection abort",
		"network is down",
		"http2: client connection lost",
		"client connection lost",
		"connection refused",
		"no such host",
		"network is unreachable",
		"no route to host",
		"connection reset",
		"connection stalled",
		"EOF",
	}
	for _, p := range phrases {
		if strings.Contains(msg, p) {
			return true
		}
	}
	return false
}

// IdleTimeoutReader wraps an io.ReadCloser and triggers ErrConnectionStalled
// if no bytes are received within timeout. Matches AGY 1.3.6 auto-abort on
// connection stalls.
type IdleTimeoutReader struct {
	rc      io.ReadCloser
	timeout time.Duration
	timer   *time.Timer
	errMu   sync.Mutex
	err     error
}

// NewIdleTimeoutReader wraps rc with an activity-based idle timeout.
func NewIdleTimeoutReader(rc io.ReadCloser, timeout time.Duration) *IdleTimeoutReader {
	if timeout <= 0 {
		timeout = StreamIdleTimeout
	}
	r := &IdleTimeoutReader{
		rc:      rc,
		timeout: timeout,
	}
	r.timer = time.AfterFunc(timeout, func() {
		r.errMu.Lock()
		if r.err == nil {
			r.err = ErrConnectionStalled
		}
		r.errMu.Unlock()
		_ = rc.Close()
	})
	return r
}

func (r *IdleTimeoutReader) Read(p []byte) (int, error) {
	r.errMu.Lock()
	if r.err != nil {
		err := r.err
		r.errMu.Unlock()
		return 0, err
	}
	r.errMu.Unlock()

	n, err := r.rc.Read(p)
	if n > 0 && r.timer != nil {
		r.timer.Reset(r.timeout)
	}
	if err != nil {
		if r.timer != nil {
			r.timer.Stop()
		}
		r.errMu.Lock()
		if r.err != nil {
			err = r.err
		}
		r.errMu.Unlock()
	}
	return n, err
}

func (r *IdleTimeoutReader) Close() error {
	if r.timer != nil {
		r.timer.Stop()
	}
	return r.rc.Close()
}

// NextConnectionRetryDelay returns the delay that precedes retry
// attempt `attempt` (zero-indexed: 0 is the first retry, after the
// initial failure). The schedule is exponential with a factor of 2,
// clamped to MaxConnectionRetryInterval:
//
//	attempt 0 -> InitialConnectionRetryInterval (1s)
//	attempt 1 -> 2s
//	attempt 2 -> 4s
//	attempt 3 -> 8s
//	attempt 4 -> 16s   (budget exhausted past this)
//
// The schedule itself is retry's Step — the clone's one
// exponential backoff, shared with the canonical schedule and the
// stall recovery's.
//
// attempt must be in [0, MaxConnectionRetries). An attempt index past
// the budget returns MaxConnectionRetryInterval — that is the cap, by
// design — so a caller that has miscounted still backs off instead of
// hot-looping.
//
// attempt < 0 is clamped to 0; the schedule is symmetric there.
func NextConnectionRetryDelay(attempt int) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	if attempt >= MaxConnectionRetries {
		return MaxConnectionRetryInterval
	}
	return retry.Step(attempt, InitialConnectionRetryInterval, MaxConnectionRetryInterval)
}
