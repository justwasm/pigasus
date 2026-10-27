// RefreshingTokenSource is the recovered wrapper that auto-refreshes
// access tokens on 401. Source: codeassistclient.RefreshingTokenSource
// (see _RefreshingTokenSource.c at 0x798dbc0). Implements the
// TokenSource interface from codeassistclient.go.
package codeassistclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/justwasm/pigasus/internal/backend"
)

// httpClient is the package-level HTTP client used for token refresh.
// Tests can replace it with a mock by setting httpClient.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// TokenRefresher is the recovered token-refresh interface. Source:
// codeassistclient.tokenRefresher.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
}

// TokenResponse is the response from oauth2.googleapis.com/token. Source:
// codeassistclient.TokenResponse (recovered 1:1 with internal/auth).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// IsExpired returns true if the token has expired.
func (t *TokenResponse) IsExpired() bool {
	if t == nil || t.AccessToken == "" {
		return true
	}
	// Assume 1 hour validity if not parsed.
	return false
}

// RefreshingTokenSource auto-refreshes access tokens. The recovered
// implementation caches the access token in memory and refreshes it
// either proactively (5 minutes before expiry) or reactively (on
// 401 from a backend RPC).
type RefreshingTokenSource struct {
	mu           sync.Mutex
	clientID     string
	scopes       []string
	credStore    CredentialStore
	cached       *TokenResponse
	cachedExpiry time.Time
	refreshFn    TokenRefresher
	refreshHook  func(*TokenResponse) // called after a successful refresh

	// refreshGroup is the singleflight group: concurrent Refresh()
	// calls share one oauth2.googleapis.com/token round-trip instead
	// of racing two of them. golang.org/x/sync is the library the
	// reference's own binary carries (errgroup + singleflight); the
	// clone now imports it too.
	refreshGroup singleflight.Group

	// inflight counts the refreshes currently running. With401Retry
	// needs to know whether a refresh is in flight *without joining
	// it*: cycle auth-expired-message (AGY 1.3.3 line 47) — when a
	// 401 arrives while a refresh is already in flight, the new
	// token, when it lands, is for the next request, not this one,
	// so the 401 is propagated instead of retried.
	inflight atomic.Int64
}

// refreshKey is the singleflight key: one token source has one
// refresh at a time.
const refreshKey = "refresh"

// NewRefreshingTokenSource constructs a RefreshingTokenSource backed by
// a refresh token and the standard OAuth2 /token endpoint.
func NewRefreshingTokenSource(refreshToken, clientID string, scopes []string, credStore CredentialStore) *RefreshingTokenSource {
	return &RefreshingTokenSource{
		clientID:  clientID,
		scopes:    scopes,
		credStore: credStore,
		cached:    &TokenResponse{RefreshToken: refreshToken},
	}
}

// SetRefreshHook sets a hook called after every successful refresh.
func (r *RefreshingTokenSource) SetRefreshHook(fn func(*TokenResponse)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshHook = fn
}

// SetTokenRefresher replaces the default refresh function (useful in
// tests).
func (r *RefreshingTokenSource) SetTokenRefresher(fn TokenRefresher) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.refreshFn = fn
}

// Name implements TokenSource.
func (r *RefreshingTokenSource) Name() string { return "RefreshingTokenSource" }

// Token returns the current access token, refreshing it if necessary.
// This is the recovered Token() function (see _Token.c).
func (r *RefreshingTokenSource) Token() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil && r.cached.AccessToken != "" && Now().Before(r.cachedExpiry.Add(-5*time.Minute)) {
		return r.cached.AccessToken, nil
	}
	// Proactive refresh: at most 5 minutes before expiry. This path
	// runs its own round-trip while r.mu is held — it does not join a
	// concurrent Refresh(), because Token() has no context to hand the
	// shared call and the lock is already taken here.
	if err := r.refreshLocked(context.Background()); err != nil {
		// If the cached token is still good, fall back to it.
		if r.cached != nil && r.cached.AccessToken != "" && Now().Before(r.cachedExpiry) {
			return r.cached.AccessToken, nil
		}
		return "", err
	}
	return r.cached.AccessToken, nil
}

// Refresh forces an immediate refresh. Source: codeassistclient.refresh.
//
// Cycle auth-expired-message: if another refresh is already in flight,
// this call joins the same in-flight refresh (singleflight) and waits
// for its result. The first caller's network round-trip is shared;
// concurrent callers never produce two oauth2.googleapis.com/token
// requests for the same refresh token. r.mu is released during the
// network round-trip so concurrent Token() callers and inflight probes
// are not blocked.
//
// The join half is x/sync's singleflight; the "is one running" probe
// With401Retry needs is the inflight counter.
func (r *RefreshingTokenSource) Refresh(ctx context.Context) error {
	ch := r.refreshGroup.DoChan(refreshKey, func() (any, error) {
		// The counter answers With401Retry's question: is a refresh
		// running right now? It is raised for the duration of the
		// round-trip and nothing else.
		r.inflight.Add(1)
		defer r.inflight.Add(-1)
		return r.refreshOnce(ctx)
	})
	select {
	case res := <-ch:
		return res.Err
	case <-ctx.Done():
		// The refresh keeps running for the callers that joined it;
		// this caller walks away with its own cancellation.
		return ctx.Err()
	}
}

// refreshOnce is the body of the shared refresh: it snapshots the
// refresh token under the lock, runs the round-trip without the lock,
// and writes the new token back under the lock.
//
// Only the first caller of a singleflight key runs this; the others
// share its result.
func (r *RefreshingTokenSource) refreshOnce(ctx context.Context) (*TokenResponse, error) {
	r.mu.Lock()
	// Snapshot the refresh token (and the hooks) under the lock so
	// the locked window does not extend over the entire oauth2
	// round-trip.
	rt := r.cachedRefreshTokenLocked()
	refreshFn := r.refreshFn
	refreshHook := r.refreshHook
	r.mu.Unlock()

	resp, err := r.callRefresh(ctx, rt, refreshFn)
	if err != nil {
		return nil, err
	}
	if refreshHook != nil {
		go refreshHook(resp)
	}
	r.mu.Lock()
	r.applyRefreshLocked(resp)
	r.mu.Unlock()
	return resp, nil
}

// cachedRefreshTokenLocked returns the current refresh_token while the
// caller holds r.mu. Returns "" if there is no token to refresh.
func (r *RefreshingTokenSource) cachedRefreshTokenLocked() string {
	if r.cached == nil {
		return ""
	}
	return r.cached.RefreshToken
}

// callRefresh runs the underlying refresh function WITHOUT holding
// r.mu. The refresh token and the refresher are snapshotted by the
// caller. The returned *TokenResponse is owned by the caller;
// applyRefreshLocked takes it under the lock when the call succeeds.
func (r *RefreshingTokenSource) callRefresh(ctx context.Context, refreshToken string, refreshFn TokenRefresher) (*TokenResponse, error) {
	if refreshToken == "" {
		return nil, ErrNoTokenSource
	}
	var (
		resp *TokenResponse
		err  error
	)
	if refreshFn != nil {
		resp, err = refreshFn.RefreshToken(ctx, refreshToken)
	} else {
		resp, err = defaultRefresh(ctx, r.clientID, refreshToken, r.scopes)
	}
	if err != nil {
		return nil, err
	}
	if resp.AccessToken == "" {
		return nil, fmt.Errorf("codeassistclient: refresh returned empty access token")
	}
	// Keep existing refresh token if the response omits one.
	if resp.RefreshToken == "" {
		resp.RefreshToken = refreshToken
	}
	return resp, nil
}

// applyRefreshLocked writes the response into r.cached and bumps
// r.cachedExpiry. Caller must hold r.mu.
func (r *RefreshingTokenSource) applyRefreshLocked(resp *TokenResponse) {
	r.cached = resp
	if resp.ExpiresIn > 0 {
		r.cachedExpiry = Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	} else {
		r.cachedExpiry = Now().Add(1 * time.Hour)
	}
}

// inflightActive reports whether a refresh is currently in flight. It
// answers the "is one running" question without joining it. Used by
// With401Retry to detect the "401 while another refresh is in flight"
// race the AGY 1.3.3 line 47 changelog calls out: when a 401 arrives
// and another refresh is already mid-flight, the auth has genuinely
// expired — propagate the 401 as KindAuthExpired instead of waiting
// on the in-flight refresh and retrying.
func (r *RefreshingTokenSource) inflightActive() bool {
	return r.inflight.Load() > 0
}

// OAuthTokenInfo returns the parsed OAuth token info for diagnostics.
func (r *RefreshingTokenSource) OAuthTokenInfo() *OAuthTokenInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached == nil {
		return nil
	}
	return &OAuthTokenInfo{
		AccessToken:  r.cached.AccessToken,
		RefreshToken: r.cached.RefreshToken,
		IDToken:      r.cached.IDToken,
		TokenType:    r.cached.TokenType,
		ExpiresAt:    r.cachedExpiry,
		Scopes:       strings.Fields(r.cached.Scope),
	}
}

// refreshLocked performs the actual refresh; caller holds r.mu.
func (r *RefreshingTokenSource) refreshLocked(ctx context.Context) error {
	if r.cached == nil || r.cached.RefreshToken == "" {
		return ErrNoTokenSource
	}
	resp, err := r.callRefresh(ctx, r.cached.RefreshToken, r.refreshFn)
	if err != nil {
		return err
	}
	r.cached = resp
	if resp.ExpiresIn > 0 {
		r.cachedExpiry = Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	} else {
		r.cachedExpiry = Now().Add(1 * time.Hour)
	}
	if r.credStore != nil {
		// Best-effort write-through.
		_ = r.credStore
	}
	if r.refreshHook != nil {
		go r.refreshHook(resp)
	}
	return nil
}

// defaultRefresh performs an HTTP POST to oauth2.googleapis.com/token.
func defaultRefresh(ctx context.Context, clientID, refreshToken string, scopes []string) (*TokenResponse, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	form.Set("grant_type", "refresh_token")
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/token", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("codeassistclient: refresh http %d", resp.StatusCode)
	}
	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// With401Retry runs fn with auto-refresh on 401. The recovered helper
// used by every backend RPC.
//
// Cycle auth-expired-message (AGY 1.3.3 line 47): when a 401 arrives
// while a refresh is already in flight, the cached token was known-bad
// when we sent the request and the about-to-land new token is for the
// *next* caller, not us. The auth has genuinely expired — we propagate
// the 401 as a KindAuthExpired HTTPError so the headless formatter
// renders the verbatim "Authentication expired. Please log back in."
// string instead of silently retrying with the fresh token.
func (r *RefreshingTokenSource) With401Retry(ctx context.Context, fn func(token string) error) error {
	tok, err := r.Token()
	if err != nil {
		return err
	}
	if err := fn(tok); err != nil {
		if !is401(err) {
			return err
		}
		// 401 mid-session. If a refresh is already in flight, the
		// about-to-land token is for the next request — propagate
		// the 401 as KindAuthExpired so the user sees the canonical
		// re-auth message rather than a silent retry.
		if r.inflightActive() {
			return newAuthExpiredHTTPError(err)
		}
		// No refresh in flight: trigger one (singleflight), wait,
		// retry once.
		if err := r.Refresh(ctx); err != nil {
			return err
		}
		tok2, err2 := r.Token()
		if err2 != nil {
			return err2
		}
		return fn(tok2)
	}
	return nil
}

// newAuthExpiredHTTPError wraps the original 401 error as a
// *backend.HTTPError with KindAuthExpired so the cmd/pigasus api_error
// formatter renders the verbatim "Authentication expired. Please log
// back in." string. The Body is the upstream message (kept for the
// AGY_ERROR JSON envelope's status code); the formatter overrides the
// user-facing message.
//
// Cycle auth-expired-message (AGY 1.3.3 line 47).
func newAuthExpiredHTTPError(orig error) error {
	body := []byte("")
	if orig != nil {
		body = []byte(orig.Error())
	}
	return &httpErrAuthExpired{
		HTTPError: &backend.HTTPError{
			StatusCode: http.StatusUnauthorized,
			Body:       body,
			Kind:       backend.KindAuthExpired,
		},
		original: orig,
	}
}

// httpErrAuthExpired is a typed wrapper. It embeds *backend.HTTPError
// (pointer) so the wrapped error carries the verbatim
// AuthExpiredMessage contract and an As(any) bool shim so the
// cmd/pigasus classifyAPIError path can extract the *backend.HTTPError
// via errors.As (Go 1.20+ As-target support). The original error is
// preserved via Unwrap so callers that want the raw 401 can use
// errors.Is/As on the inner chain.
//
// Cycle auth-expired-message (AGY 1.3.3 line 47).
type httpErrAuthExpired struct {
	*backend.HTTPError
	original error
}

func (e *httpErrAuthExpired) Error() string { return e.original.Error() }
func (e *httpErrAuthExpired) Unwrap() error { return e.original }

// As supports errors.As target extraction. The wrapped *HTTPError is
// the canonical KindAuthExpired envelope; assigning it to *target
// gives the cmd/pigasus classifier direct access to the StatusCode and
// Kind fields. Go 1.20+ honours the As(any) bool method on error
// chains — earlier Go versions fall back to Unwrap-only matching,
// which does NOT walk into embedded pointer fields, so this is the
// load-bearing line that makes `errors.As(err, &he)` work.
func (e *httpErrAuthExpired) As(target any) bool {
	if he, ok := target.(**backend.HTTPError); ok && e.HTTPError != nil {
		*he = e.HTTPError
		return true
	}
	return false
}

// is401 reports whether err looks like a 401 Unauthorized. The
// recovered 1.3.3 code uses errors.Is + a marker; we accept any error
// whose message contains "401" or "unauthorized".
type httpErr struct{ code int }

func (e *httpErr) Error() string { return fmt.Sprintf("http %d", e.code) }

func is401(err error) bool {
	if err == nil {
		return false
	}
	he, ok := err.(*httpErr)
	if ok {
		return he.code == http.StatusUnauthorized
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "401") || strings.Contains(s, "unauthorized")
}
