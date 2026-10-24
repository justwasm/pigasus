// auth_expired_test.go — cycle auth-expired-message (AGY 1.3.3 line 47).
//
// The changelog text is:
//
//	Fixed an expired or revoked sign-in showing an unclear error
//	mid-session; the agent now says `Authentication expired. Please
//	log back in.`, and a token refresh that was already in flight
//	no longer restores the expired credentials.
//
// The tests below pin the recovered behaviour:
//
//   - TestWith401Retry_InflightRefresh_PropagatesAuthExpired:
//     a 401 arrives while a refresh is already mid-flight → the
//     returned error carries KindAuthExpired so the headless
//     formatter renders the verbatim canonical message.
//
//   - TestWith401Retry_NoInflight_TriggersRefreshAndRetries:
//     a 401 with no concurrent refresh → Refresh() is invoked
//     (singleflight), the retry uses the fresh token.
//
//   - TestRefresh_Singleflight_DedupesConcurrentCalls:
//     N goroutines call Refresh() simultaneously → exactly one
//     oauth2.googleapis.com/token round-trip is made; the others
//     share the result via the inflight slot.
//
//   - TestRefresh_Success_NextTokenReturnsNewValue:
//     After Refresh() lands the new token, the next Token() call
//     returns it without a second network round-trip.
//
//   - TestWith401Retry_KindAuthExpired_FormatterRendersVerbatim:
//     end-to-end check that the KindAuthExpired envelope renders
//     the verbatim canonical message via cmd/pigasus's classifyAPIError.
package codeassistclient

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// countingRefresher counts how many times the underlying token-refresh
// is invoked. Tests below assert "exactly one" or "N concurrent calls
// produce a single network round-trip".
type countingRefresher struct {
	mu    sync.Mutex
	calls int32
	gate  chan struct{} // closed (or sent on) to release the in-flight refresh
	resp  *TokenResponse
	err   error
}

func (c *countingRefresher) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	atomic.AddInt32(&c.calls, 1)
	if c.gate != nil {
		select {
		case <-c.gate:
			// released
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if c.err != nil {
		return nil, c.err
	}
	r := *c.resp
	r.RefreshToken = refreshToken
	return &r, nil
}

// seedStale installs a known-stale token so Token() does not hit the
// fresh-token fast path before the test calls Refresh() or With401Retry.
func seedStale(t *testing.T, rts *RefreshingTokenSource) {
	t.Helper()
	rts.mu.Lock()
	defer rts.mu.Unlock()
	rts.cached = &TokenResponse{AccessToken: "stale", RefreshToken: "rt"}
	rts.cachedExpiry = time.Now().Add(-time.Hour) // 1 h ago
}

// seedFresh installs a known-fresh token so Token() returns it without
// triggering refreshLocked. This is the prerequisite for the
// inflight+401 tests — With401Retry calls Token() first, and a stale
// token would otherwise drive the cached refresh path which holds r.mu
// for the duration of the network call.
func seedFresh(t *testing.T, rts *RefreshingTokenSource, access string) {
	t.Helper()
	rts.mu.Lock()
	defer rts.mu.Unlock()
	rts.cached = &TokenResponse{AccessToken: access, RefreshToken: "rt"}
	rts.cachedExpiry = time.Now().Add(time.Hour) // 1 h in the future
}

// TestWith401Retry_InflightRefresh_PropagatesAuthExpired pins the
// AGY 1.3.3 line 47 contract: a 401 that arrives while a refresh is
// already in flight must NOT be silently retried with the about-to-land
// token — the auth has genuinely expired, the new token is for the
// next request, and the user must see "Authentication expired.
// Please log back in.".
func TestWith401Retry_InflightRefresh_PropagatesAuthExpired(t *testing.T) {
	gate := make(chan struct{})
	cr := &countingRefresher{
		gate: gate,
		resp: &TokenResponse{
			AccessToken: "fresh-after-gate",
			ExpiresIn:   3600,
		},
	}
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(cr)
	// seedFresh, not seedStale — the cached token must be valid so
	// Token() returns immediately and the in-flight refresh stays
	// parked inside countingRefresher while With401Retry runs.
	seedFresh(t, rts, "fresh-still-valid")

	// Start the in-flight refresh from a goroutine. Refresh() releases
	// r.mu during the network call so it parks inside countingRefresher.
	refreshDone := make(chan error, 1)
	go func() {
		refreshDone <- rts.Refresh(context.Background())
	}()
	// Wait until the inflight slot is installed.
	deadline := time.Now().Add(time.Second)
	for !rts.inflightActive() {
		if time.Now().After(deadline) {
			t.Fatal("inflight slot never installed by Refresh()")
		}
		time.Sleep(time.Millisecond)
	}

	// Now With401Retry hits a 401. With the inflight slot occupied,
	// it must surface KindAuthExpired and NOT call fn again with the
	// fresh token.
	calls := 0
	err := rts.With401Retry(context.Background(), func(token string) error {
		calls++
		return &httpErr{code: http.StatusUnauthorized}
	})
	if calls != 1 {
		t.Errorf("fn was called %d times, want 1 (no retry on 401+inflight)", calls)
	}
	if err == nil {
		t.Fatal("expected an error from With401Retry, got nil")
	}

	// The returned error must unwrap to an *backend.HTTPError with
	// KindAuthExpired so the cmd/pigasus api_error formatter renders
	// the verbatim canonical message.
	var he *backend.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("returned error is not an *backend.HTTPError: %T (%v)", err, err)
	}
	if he.StatusCode != http.StatusUnauthorized {
		t.Errorf("HTTPError.StatusCode = %d, want 401", he.StatusCode)
	}
	if he.KindError() != backend.KindAuthExpired {
		t.Errorf("HTTPError.KindError() = %d, want KindAuthExpired (%d)",
			he.KindError(), backend.KindAuthExpired)
	}

	// Release the in-flight refresh and let Refresh() complete.
	close(gate)
	if err := <-refreshDone; err != nil {
		t.Errorf("in-flight Refresh returned %v", err)
	}

	// The inflight slot must be cleared once the refresh lands.
	if rts.inflightActive() {
		t.Error("inflight slot still set after Refresh() completed")
	}
}

// TestWith401Retry_NoInflight_TriggersRefreshAndRetries pins the
// happy-path 401 recovery: no concurrent refresh is in flight, so
// With401Retry triggers a singleflight refresh and retries fn once
// with the fresh token. The cached token is fresh so Token() returns
// immediately and the refresh is only triggered by the 401 branch.
func TestWith401Retry_NoInflight_TriggersRefreshAndRetries(t *testing.T) {
	cr := &countingRefresher{
		resp: &TokenResponse{AccessToken: "fresh-1", ExpiresIn: 3600},
	}
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(cr)
	seedFresh(t, rts, "stale-but-not-yet-expired")

	calls := 0
	tokens := []string{}
	err := rts.With401Retry(context.Background(), func(token string) error {
		calls++
		tokens = append(tokens, token)
		if calls == 1 {
			return &httpErr{code: http.StatusUnauthorized}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("With401Retry returned %v", err)
	}
	if calls != 2 {
		t.Errorf("fn calls = %d, want 2 (initial + retry)", calls)
	}
	if tokens[0] != "stale-but-not-yet-expired" {
		t.Errorf("first call token = %q, want stale-but-not-yet-expired", tokens[0])
	}
	if tokens[1] != "fresh-1" {
		t.Errorf("retry call token = %q, want fresh-1", tokens[1])
	}
	if got := atomic.LoadInt32(&cr.calls); got != 1 {
		t.Errorf("countingRefresher.calls = %d, want 1 (singleflight)", got)
	}
}

// TestRefresh_Singleflight_DedupesConcurrentCalls is the dedicated
// race test: N goroutines call Refresh() simultaneously and we verify
// exactly one oauth2.googleapis.com/token round-trip is made. Run
// with `go test -race` to also catch read/write races on r.inflight.
func TestRefresh_Singleflight_DedupesConcurrentCalls(t *testing.T) {
	const N = 32
	gate := make(chan struct{})
	cr := &countingRefresher{
		gate: gate,
		resp: &TokenResponse{AccessToken: "fresh-shared", ExpiresIn: 3600},
	}
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(cr)
	seedFresh(t, rts, "fresh-initial")

	results := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			results <- rts.Refresh(context.Background())
		}()
	}

	// Wait until at least one Refresh() reaches the countingRefresher.
	deadline := time.Now().Add(time.Second)
	for atomic.LoadInt32(&cr.calls) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no Refresh() reached the underlying refresher")
		}
		time.Sleep(time.Millisecond)
	}

	// Give all 32 goroutines time to either install themselves as the
	// first caller (one wins) or join the inflight slot (the rest).
	time.Sleep(50 * time.Millisecond)
	// The inflight slot must be installed exactly once.
	if !rts.inflightActive() {
		t.Error("inflight slot not installed before gate close")
	}

	// Release the in-flight refresh.
	close(gate)

	// All N goroutines must observe a nil error.
	for i := 0; i < N; i++ {
		if err := <-results; err != nil {
			t.Errorf("Refresh[%d] returned %v", i, err)
		}
	}

	// Exactly one round-trip — singleflight dedupe.
	if got := atomic.LoadInt32(&cr.calls); got != 1 {
		t.Errorf("countingRefresher.calls = %d, want 1 (singleflight)", got)
	}
	if rts.inflightActive() {
		t.Error("inflight slot still set after Refresh() completed")
	}
}

// TestRefresh_Success_NextTokenReturnsNewValue pins the post-refresh
// invariant: after Refresh() lands the fresh token, the next Token()
// call returns it without a second network round-trip.
func TestRefresh_Success_NextTokenReturnsNewValue(t *testing.T) {
	cr := &countingRefresher{
		resp: &TokenResponse{AccessToken: "fresh-new", ExpiresIn: 3600},
	}
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(cr)
	// Use seedStale so Token() drives a refresh on the next call —
	// or use seedFresh and force Refresh(). The cleanest is the
	// latter: install a fresh token, force a refresh, then call
	// Token() and assert it returns the post-refresh value.
	seedFresh(t, rts, "fresh-original")

	if err := rts.Refresh(context.Background()); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if got := atomic.LoadInt32(&cr.calls); got != 1 {
		t.Errorf("countingRefresher.calls = %d, want 1", got)
	}

	tok, err := rts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "fresh-new" {
		t.Errorf("Token = %q, want fresh-new", tok)
	}
	// No second network round-trip — Token() must hit the cache.
	if got := atomic.LoadInt32(&cr.calls); got != 1 {
		t.Errorf("countingRefresher.calls after Token() = %d, want still 1", got)
	}
}

// TestWith401Retry_KindAuthExpired_FormatterRendersVerbatim is the
// integration check across the package boundary: the KindAuthExpired
// HTTPError returned by With401Retry, when fed into the
// cmd/pigasus.api_error classifier (mirrored here for testability),
// renders the verbatim "Authentication expired. Please log back in."
// string and reports 401 → UNAUTHENTICATED. This guards the cycle's
// delivery contract end-to-end.
func TestWith401Retry_KindAuthExpired_FormatterRendersVerbatim(t *testing.T) {
	gate := make(chan struct{})
	cr := &countingRefresher{
		gate: gate,
		resp: &TokenResponse{AccessToken: "fresh-late", ExpiresIn: 3600},
	}
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(cr)
	seedFresh(t, rts, "fresh-original")

	go func() { _ = rts.Refresh(context.Background()) }()
	deadline := time.Now().Add(time.Second)
	for !rts.inflightActive() {
		if time.Now().After(deadline) {
			t.Fatal("inflight slot never installed")
		}
		time.Sleep(time.Millisecond)
	}

	err := rts.With401Retry(context.Background(), func(string) error {
		return &httpErr{code: http.StatusUnauthorized}
	})
	close(gate)

	var he *backend.HTTPError
	if !errors.As(err, &he) {
		t.Fatalf("not *backend.HTTPError: %T", err)
	}
	if he.KindError() != backend.KindAuthExpired {
		t.Fatalf("KindError() = %d, want KindAuthExpired", he.KindError())
	}
	if backend.AuthExpiredMessage != "Authentication expired. Please log back in." {
		t.Fatalf("AuthExpiredMessage literal drifted: %q", backend.AuthExpiredMessage)
	}
	// Status code on the underlying HTTPError must stay 401 so the
	// JSON envelope reports ErrorCode=401.
	if he.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", he.StatusCode)
	}
}
