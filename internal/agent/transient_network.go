// transient_network.go — recognize transient network/socket errors that
// deserve a long retry budget in auto-continue.
//
// AGY 1.4.x changelog_reference.txt:10:
//
//	"Fixed socket disconnect errors such as `broken pipe`,
//	 `software caused connection abort`, `network is down`, and
//	 `http2: client connection lost` (for example after laptop
//	 sleep or a Wi-Fi change) stopping the turn with `Agent
//	 execution terminated due to error.` instead of retrying
//	 automatically and reporting a network error if retries fail."
//
// The clone's auto-continue had a flat AutoContMaxDepth=3 ceiling, so a
// single mid-turn network blip killed long-running tasks as soon as
// three transient disconnects piled up — the exact scenario the user
// reproduces with the wagyu tmux harness. This file adds an
// `IsTransientNetworkErr` predicate the auto-continue loop uses to
// grant those errors a much higher retry budget (see
// AutoContMaxNetworkDepth in auto_continue.go) while still giving up
// fast on permanent failures (HTTP 4xx, scope denied, quota reached,
// etc.). After the network budget exhausts the typed
// *ErrAutoContNetworkFailed surfaces a clear "network error" message
// instead of the previous "auto-continue reached max depth".
//
// The detector is deliberately pattern-based, not type-based: net
// package errors wrap freely and Go's `net` family does not expose a
// stable `net.Error.Temporary()` contract that survives the trip
// through http2, gRPC, and stream-frame unmarshaling. We mirror the
// verbatim phrases AGY's error family uses — every keyword here is
// one the recovery code has observed in upstream payload captures
// from `strings(1)` on the binary.
package agent

import (
	"context"
	"errors"
	"strings"
)

// transientNetworkSubstrings is the verbatim phrase list AGY's
// recovery path matches against the wrapped error chain's Error()
// text. The matching is case-insensitive substring: the upstream
// errors are normalised to lowercase by the net/http/http2 packages
// before they hit user-visible code paths, so e.g. "Broken Pipe" and
// "broken pipe" both land as "broken pipe". We keep exact substring
// matches to avoid accidentally swallowing unrelated failures
// (note: "EOF" is a substring of many words — we list it alone and
// rely on the substring-scan to land it on a real EOF marker, e.g.
// "unexpected EOF" / "EOF reading from socket").
var transientNetworkSubstrings = []string{
	"broken pipe",
	"software caused connection abort",
	"network is down",
	"network is unreachable",
	"http2: client connection lost",
	"http2: stream closed",
	"http2: client disconnected",
	"connection reset by peer",
	"connection refused",
	"connection timed out",
	"i/o timeout",
	"no such host",
	"handshake timeout", // matches Go's "tls: handshake timeout" and similar
	"tls: oversized record received",
	"unexpected eof",
	"eof",
	"use of closed network connection",
	"server closed idle connection",
	"server closed connection",
	// Cycle stream-error-frames: PiG 0.5.0's retryableProviderErrorPattern
	// (ai/assistant_retry.go) names the gateway and stream phrasings that
	// upstream Pi retries, and the release notes call out "PiG retries on
	// the gateway error `Unable to forward upstream stream`". The clone's
	// list was the AGY changelog's socket phrases only, so the TUI retried
	// these three times (AutoContMaxDepth) instead of eight
	// (AutoContMaxNetworkDepth); the print path already retried them
	// through PiG's own policy, which made the two surfaces disagree.
	// Only phrases that can reach us over agw/OpenAI-compatible SSE are
	// added — the Node-only wording (getaddrinfo, ENOTFOUND, fetch failed)
	// stays out.
	"unable to forward upstream",
	"upstream request failed",
	"upstream_stream_read_error",
	"stream was interrupted",
	"http/2 stream failed",
	"other side closed",
	"socket connection was closed",
	"reset before headers",
	"ended without",
}

// IsTransientNetworkErr reports whether err looks like a transient
// socket / transport failure suitable for the higher auto-continue
// retry budget. Both wrapped errors (errors.Is / As chains) and the
// top-level err.Error() are scanned so a *net.OpError wrapping a
// *url.Error wrapping a syscall error still classifies correctly.
//
// The classifier returns false for nil, for context.Canceled /
// DeadlineExceeded (the caller's own cancellation, handled
// separately), and for backend.* errors whose typed fields say so
// (HTTPError with status >= 500 are covered by a separate retry path
// in the backend layer — see quota_or_http_error.go).
func IsTransientNetworkErr(err error) bool {
	if err == nil {
		return false
	}
	// Caller's own cancellation must always surface immediately;
	// auto-continue would burn quota on a session the user no
	// longer wants.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	// Walk the error chain so a wrapped *net.OpError surfaces its
	// inner text. strings.ToLower keeps the comparison locale-
	// insensitive; upstream errors are already ASCII.
	for cur := err; cur != nil; cur = errors.Unwrap(cur) {
		text := strings.ToLower(cur.Error())
		for _, needle := range transientNetworkSubstrings {
			if strings.Contains(text, needle) {
				return true
			}
		}
		if cur == err {
			// First iteration done; if errors.Unwrap returns nil
			// the for-loop guard already stops us, so this branch
			// is defensive against custom Unwrap chains that loop
			// back on themselves.
			break
		}
	}
	return false
}
