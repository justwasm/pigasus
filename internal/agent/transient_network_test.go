// transient_network_test.go — cover IsTransientNetworkErr + the
// ErrAutoContNetworkFailed wrapper that closed the AGY 1.4.x line in
// changelog_reference.txt:10.
//
// The test cases pin the classification matrix the cycle delivers:
//
//   - covered phrases (verbatim substrings)
//   - wrapped-error recovery (net.OpError chains)
//   - case-insensitivity
//   - EOF handling (substring scan over text)
//   - negative cases (nil, context errors, scope/quota typed errors,
//     non-network generic errors)
//
// The auto-continue budget tests live in auto_continue_test.go
// (TestAutoContNetworkBudget / TestAutoContTypedGiveUp) so the two
// halves of the bug story stay in their right files.
package agent

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"testing"
)

func TestIsTransientNetworkErr_PhrasesCovered(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		// Verbatim phrases from the AGY 1.4.x fix scope.
		{"broken pipe", fmt.Errorf("read tcp: broken pipe"), true},
		{"software caused connection abort", fmt.Errorf("read tcp: software caused connection abort"), true},
		{"network is down", fmt.Errorf("dial: network is down"), true},
		{"network is unreachable", fmt.Errorf("dial: network is unreachable"), true},
		{"http2 client connection lost", fmt.Errorf("http2: client connection lost"), true},
		{"http2 stream closed", fmt.Errorf("http2: stream closed"), true},
		{"connection reset by peer", fmt.Errorf("read: connection reset by peer"), true},
		{"connection refused", fmt.Errorf("dial: connection refused"), true},
		{"connection timed out", fmt.Errorf("dial: connection timed out"), true},
		{"i/o timeout", fmt.Errorf("read: i/o timeout"), true},
		{"no such host", fmt.Errorf("dial: no such host"), true},
		{"tls handshake timeout", fmt.Errorf("tls: handshake timeout"), true},
		{"unexpected eof", fmt.Errorf("read: unexpected EOF"), true},
		{"server closed idle connection", fmt.Errorf("net/http: server closed idle connection"), true},

		// Case-insensitivity: upstream errors are lowercased by
		// the net/http/http2 packages, but defensive testing of the
		// mixed-case path costs nothing.
		{"uppercase BROKEN PIPE", fmt.Errorf("Broken Pipe"), true},
		{"uppercase CONNECTION RESET", fmt.Errorf("Connection Reset By Peer"), true},

		// Negative cases — must NOT trip the rule.
		{"nil", nil, false},
		{"context canceled", context.Canceled, false},
		{"context deadline exceeded", context.DeadlineExceeded, false},
		{"generic runtime error", fmt.Errorf("agent: open stream: bad request"), false},
		{"scope denied (text-only, no 5xx signal)",
			fmt.Errorf("HTTP 403: insufficient_scope"), false},
		{"quota text without wrapping HTTPError",
			fmt.Errorf("HTTP 429: rate limit exceeded"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := IsTransientNetworkErr(tc.err)
			if got != tc.want {
				t.Errorf("IsTransientNetworkErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientNetworkErr_PiG051GatewayPhrases(t *testing.T) {
	// Cycle stream-error-frames: PiG 0.5.0's retryableProviderErrorPattern
	// names the gateway's upstream-stream failures, and its release notes
	// call out "PiG retries on the gateway error `Unable to forward
	// upstream stream`". The TUI's budget selection must agree with the
	// print path (which retries through PiG's own policy), so these get
	// AutoContMaxNetworkDepth, not AutoContMaxDepth.
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"unable to forward upstream", fmt.Errorf("agw: upstream 500: Unable to forward upstream stream"), true},
		{"upstream request failed", fmt.Errorf("gateway: upstream request failed"), true},
		{"upstream_stream_read_error", fmt.Errorf("upstream_stream_read_error"), true},
		{"stream was interrupted", fmt.Errorf("stream was interrupted"), true},
		{"http/2 stream failed", fmt.Errorf("http/2 stream failed"), true},
		{"other side closed", fmt.Errorf("other side closed"), true},
		{"socket connection was closed", fmt.Errorf("socket connection was closed"), true},
		{"reset before headers", fmt.Errorf("reset before headers"), true},
		{"ended without (EOF-without-terminal marker)", fmt.Errorf("EOF: stream ended without [DONE] marker and no frames"), true},

		// A permanent failure must not be pulled into the long-retry class
		// just because the gateway wording is nearby.
		{"scope denied", fmt.Errorf("HTTP 403: insufficient_scope"), false},
		{"quota", fmt.Errorf("HTTP 429: rate limit exceeded"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsTransientNetworkErr(tc.err); got != tc.want {
				t.Errorf("IsTransientNetworkErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestIsTransientNetworkErr_WrappedChain(t *testing.T) {
	// *url.Error wrapping a *net.OpError wrapping a wrapped text
	// error. This is the shape upstream Go net/http produces; the
	// substring walk over errors.Unwrap must find the inner
	// "broken pipe".
	host := &url.Error{
		Op:  "Post",
		URL: "https://example.com/v1/stream",
		Err: &net.OpError{
			Op:  "read",
			Net: "tcp",
			Err: fmt.Errorf("read tcp: broken pipe"),
		},
	}
	if !IsTransientNetworkErr(host) {
		t.Error("expected wrapped url.Error→OpError→broken pipe to classify as transient network")
	}
}

func TestIsTransientNetworkErr_ContextErrorsTakePrecedence(t *testing.T) {
	// A context.Canceled wrapping a network-style error must
	// short-circuit to false — the caller's cancel outranks the
	// network classification, matching the AGY behaviour where a
	// user-Esc mid-stream never retries.
	base := fmt.Errorf("read: connection reset by peer")
	wrapped := fmt.Errorf("%w: %w", context.Canceled, base)
	if errors.Is(wrapped, context.Canceled) && IsTransientNetworkErr(wrapped) {
		t.Error("wrapped context.Canceled must not classify as transient network")
	}
}

func TestErrAutoContNetworkFailed_ErrorString(t *testing.T) {
	inner := fmt.Errorf("dial: connection refused")
	err := &ErrAutoContNetworkFailed{Attempts: 8, Last: inner}
	want := "agent: network error after 8 retries (last error: dial: connection refused)"
	if got := err.Error(); got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrAutoContNetworkFailed_Unwrap(t *testing.T) {
	inner := fmt.Errorf("dial: connection refused")
	err := &ErrAutoContNetworkFailed{Attempts: 8, Last: inner}
	if got := errors.Unwrap(err); got != inner {
		t.Errorf("Unwrap = %v, want %v", got, inner)
	}
	if !errors.Is(err, inner) {
		t.Error("errors.Is should reach inner")
	}
}

func TestErrAutoContNetworkFailed_NestedUnwrap(t *testing.T) {
	// cycle 266 regression: errors.As / errors.Is must still
	// recover the innermost typed error (a *backend.HTTPError, an
	// ErrMaxTurnsReached, a custom quota error, etc.) so callers
	// that classify via the chain keep working.
	innermost := errors.New("connection reset by peer")
	mid := fmt.Errorf("read tcp: %w", innermost)
	outer := &ErrAutoContNetworkFailed{Attempts: 8, Last: mid}

	if !errors.Is(outer, innermost) {
		t.Errorf("errors.Is(ErrAutoContNetworkFailed, %v) = false, want true", innermost)
	}
	// Walking Unwrap down to the chain's tail must still classify
	// as transient — this is the path a future caller might take
	// to figure out whether the failure was a network failure at
	// the wire level. Note: IsTransientNetworkErr(*ErrAutoCont...)
	// itself returns TRUE (the wrapper's Error() string happens
	// to include "connection reset by peer"), and that is the
	// SAFE direction — there is no way for IsTransientNetworkErr
	// to return TRUE on a non-network failure, only to return TRUE
	// on a network failure (true-positive), and that is the
	// behaviour we want for the typed-error classifier.
	if !IsTransientNetworkErr(mid) {
		t.Errorf("IsTransientNetworkErr(mid-wrapped error) = false, want true (mid's text contains 'connection reset by peer')")
	}
}
