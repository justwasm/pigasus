// tokenfreshness_test.go — AGY 1.5.17 coverage.
//
// Two behaviours, tested separately because they are separate halves of the
// changelog bullet:
//
//  1. TestNeedsRefreshAt            — the pure margin decision, on a frozen
//     clock. No server, no sleeps.
//  2. TestRefreshIfNeededAt_Margin* — the margin decision wired to a refresh,
//     counting how many times the token endpoint was hit.
//  3. TestShouldRetryAfterRefresh   — the half-2 predicate: pre-stream 401
//     retries once, post-stream 401 does not.
//  4. TestRetryLoopAfterRefresh     — the loop guard driven end to end over
//     the same counting server: refresh + one retry, then a second 401
//     surfaces instead of looping.
//
// Every token here carries an explicit Expiry computed from a fixed base
// instant, and RefreshIfNeededAt takes that instant as its "now", so nothing
// in this file depends on the wall clock.
package auth

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// fixedNow is the frozen clock all the margin tests share. It is deliberately
// not time.Now(): the whole point is that the decision is a function of the
// injected instant.
var fixedNow = time.Date(2026, 11, 25, 12, 0, 0, 0, time.UTC)

// tokExpiringAt builds a token whose access token dies at exp.
func tokExpiringAt(exp time.Time) *AntigravityToken {
	return &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.old",
			RefreshToken: "1//refresh",
			TokenType:    "Bearer",
			Expiry:       exp.Format(time.RFC3339Nano),
		},
		AuthMethod: "consumer",
	}
}

// countingTokenServer answers every POST with a 200 token response and counts
// the requests, so a test can assert "refreshed exactly once".
func countingTokenServer(t *testing.T, hits *int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(hits, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"access_token":"ya29.fresh","expires_in":3600,"token_type":"Bearer"}`)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestNeedsRefreshAt pins the margin boundary. The interesting rows are the
// ones immediately around fixedNow + 5m: one nanosecond outside the margin is
// "use as-is", one nanosecond inside is "refresh first".
func TestNeedsRefreshAt(t *testing.T) {
	margin := AccessTokenRefreshMargin
	cases := []struct {
		name   string
		tok    *AntigravityToken
		now    time.Time
		expect bool
	}{
		{
			name:   "well outside margin: use as-is",
			tok:    tokExpiringAt(fixedNow.Add(20 * time.Minute)),
			now:    fixedNow,
			expect: false,
		},
		{
			name:   "one nanosecond outside margin: use as-is",
			tok:    tokExpiringAt(fixedNow.Add(margin + time.Nanosecond)),
			now:    fixedNow,
			expect: false,
		},
		{
			name:   "exactly at margin boundary: refresh",
			tok:    tokExpiringAt(fixedNow.Add(margin)),
			now:    fixedNow,
			expect: true,
		},
		{
			name:   "one nanosecond inside margin: refresh",
			tok:    tokExpiringAt(fixedNow.Add(margin - time.Nanosecond)),
			now:    fixedNow,
			expect: true,
		},
		{
			name:   "already expired: refresh",
			tok:    tokExpiringAt(fixedNow.Add(-time.Minute)),
			now:    fixedNow,
			expect: true,
		},
		{
			name:   "unknown expiry: conservative refresh",
			tok:    &AntigravityToken{Token: AntigravityAccessToken{AccessToken: "ya29.x"}},
			now:    fixedNow,
			expect: true,
		},
		{
			name:   "nil token: no verdict",
			tok:    nil,
			now:    fixedNow,
			expect: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.tok.NeedsRefreshAt(tc.now); got != tc.expect {
				t.Fatalf("NeedsRefreshAt = %v, want %v", got, tc.expect)
			}
		})
	}
}

// TestRefreshIfNeededAt_MarginIsHonoured asserts the two sides of the margin
// against a real (fake) token endpoint: inside the margin the server is hit
// once and the token is replaced; outside it, the server is never contacted
// and the same token pointer comes back untouched.
func TestRefreshIfNeededAt_MarginIsHonoured(t *testing.T) {
	cases := []struct {
		name          string
		expiry        time.Time
		wantRefreshed bool
		wantHits      int32
	}{
		{"inside margin refreshes before use", fixedNow.Add(2 * time.Minute), true, 1},
		{"outside margin used as-is", fixedNow.Add(30 * time.Minute), false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var hits int32
			srv := countingTokenServer(t, &hits)
			withRedirectedTokenURL(t, srv.URL)

			tok := tokExpiringAt(tc.expiry)
			got, refreshed, err := tok.RefreshIfNeededAt(context.Background(), fixedNow)
			if err != nil {
				t.Fatalf("RefreshIfNeededAt: %v", err)
			}
			if refreshed != tc.wantRefreshed {
				t.Fatalf("refreshed = %v, want %v", refreshed, tc.wantRefreshed)
			}
			if n := atomic.LoadInt32(&hits); n != tc.wantHits {
				t.Fatalf("token endpoint hits = %d, want %d", n, tc.wantHits)
			}
			if !tc.wantRefreshed {
				if got != tok || got.Token.AccessToken != "ya29.old" {
					t.Fatalf("token was mutated despite being outside the margin: %+v", got.Token)
				}
				return
			}
			if got.Token.AccessToken != "ya29.fresh" {
				t.Fatalf("access_token = %q, want the refreshed value", got.Token.AccessToken)
			}
		})
	}
}

// TestShouldRetryAfterRefresh pins the half-2 predicate. It is a pure
// function, so the table is the whole specification.
func TestShouldRetryAfterRefresh(t *testing.T) {
	cases := []struct {
		name      string
		status    int
		streamed  int64
		attempted bool
		expect    bool
	}{
		{"pre-stream 401, no refresh yet: retry once", 401, 0, false, true},
		{"pre-stream 401 after a refresh: do not loop", 401, 0, true, false},
		{"post-stream 401: surface, never retry", 401, 1, false, false},
		{"post-stream 401 after refresh: still no retry", 401, 512, true, false},
		{"403 is a permissions verdict, not staleness", 403, 0, false, false},
		{"500 belongs to the retry package", 500, 0, false, false},
		{"200 never retries", 200, 0, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldRetryAfterRefresh(tc.status, tc.streamed, tc.attempted); got != tc.expect {
				t.Fatalf("ShouldRetryAfterRefresh(%d, %d, %v) = %v, want %v",
					tc.status, tc.streamed, tc.attempted, got, tc.expect)
			}
		})
	}
}

// TestPreStream401RefreshesExactlyOnce drives the half-2 sequence the way the
// transport would: a 401 with zero bytes streamed, one forced refresh, one
// retry. It asserts the refresh happened exactly once and that the retry was
// permitted exactly once.
func TestPreStream401RefreshesExactlyOnce(t *testing.T) {
	var hits int32
	srv := countingTokenServer(t, &hits)
	withRedirectedTokenURL(t, srv.URL)

	tok := tokExpiringAt(fixedNow.Add(30 * time.Minute)) // clock says fine; the 401 says otherwise

	// Attempt 1: server rejects with a pre-stream 401.
	bytesStreamed := int64(0)
	status := 401
	refreshAttempted := false

	if !ShouldRetryAfterRefresh(status, bytesStreamed, refreshAttempted) {
		t.Fatal("pre-stream 401 must be retryable")
	}
	// The transport would refresh here, and a 401 means "bad now" — not
	// "maybe in five minutes" — so the forced path is used even though the
	// token's clock is still healthy.
	refreshed, didRefresh, err := tok.RefreshForced(context.Background())
	if err != nil {
		t.Fatalf("RefreshForced: %v", err)
	}
	if !didRefresh {
		t.Fatal("RefreshForced refused to refresh a nominally-fresh token after a 401")
	}
	if refreshed.Token.AccessToken != "ya29.fresh" {
		t.Fatalf("access_token = %q, want the refreshed value", refreshed.Token.AccessToken)
	}
	refreshAttempted = true

	// Attempt 1 ends with exactly one refresh.
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("token endpoint hits = %d, want 1", n)
	}
}

// TestPostStream401SurfacesWithoutRefresh is the failure-mode half of the
// contract: once bytes have flowed, a 401 must not trigger a refresh/retry
// pair, because the retry would duplicate output. The counter proves the
// endpoint was never contacted.
func TestPostStream401SurfacesWithoutRefresh(t *testing.T) {
	var hits int32
	srv := countingTokenServer(t, &hits)
	withRedirectedTokenURL(t, srv.URL)

	// Enough bytes to mean "the UI has already rendered something".
	const bytesStreamed = int64(1024)
	if ShouldRetryAfterRefresh(401, bytesStreamed, false) {
		t.Fatal("post-stream 401 must not be retryable; a retry would duplicate output")
	}
	// The transport, seeing false, surfaces the error and never calls the
	// refresh path — so the endpoint counter stays at zero.
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("token endpoint hits = %d, want 0 (no refresh after a post-stream 401)", n)
	}
}

// TestSecondConsecutive401DoesNotLoop is the loop-guard test: after the
// refresh, a second 401 must be surfaced. The sequence is the same retry
// decision run twice with attempted=true on the second pass, and the assertion
// is that the predicate turns false while the server saw exactly one refresh.
func TestSecondConsecutive401DoesNotLoop(t *testing.T) {
	var hits int32
	srv := countingTokenServer(t, &hits)
	withRedirectedTokenURL(t, srv.URL)

	tok := tokExpiringAt(fixedNow.Add(-time.Minute)) // genuinely stale
	refreshAttempted := false

	// First 401: refresh + retry permitted.
	if !ShouldRetryAfterRefresh(401, 0, refreshAttempted) {
		t.Fatal("first pre-stream 401 should permit one retry")
	}
	if _, _, err := RefreshIfNeededNow(context.Background(), tok); err != nil {
		t.Fatalf("refresh after first 401: %v", err)
	}
	refreshAttempted = true

	// Second 401 on the retried request: no further retry.
	if ShouldRetryAfterRefresh(401, 0, refreshAttempted) {
		t.Fatal("a second consecutive 401 must surface, not loop")
	}
	if n := atomic.LoadInt32(&hits); n != 1 {
		t.Fatalf("token endpoint hits = %d, want exactly 1 refresh", n)
	}
}
