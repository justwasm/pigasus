// Package grpc / token_source.go — token source and credential store primitives.
//
// Extracted from api_server.go: contains TokenSource, OAuthTokenInfo,
// CredentialStore, InMemoryCredentialStore, staticTokenSource,
// hubCookieTokenSource, and RefreshingTokenSource.
package grpc

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// TokenSource & OAuthTokenInfo
// ---------------------------------------------------------------------------

// TokenSource is the recovered TokenSource interface. All seven
// AuthProvider implementations return one.
type TokenSource interface {
	Token() (string, error)
	Name() string
}

// OAuthTokenInfo carries the parsed ID-token claims.
type OAuthTokenInfo struct {
	Email         string
	Domain        string
	HD            string
	AccessToken   string
	RefreshToken  string
	IDToken       string
	TokenType     string
	ExpiresAt     time.Time
	Scopes        []string
	ProjectNumber int64
	ProjectID     string
}

// ---------------------------------------------------------------------------
// Helpers & CredentialStore
// ---------------------------------------------------------------------------

// staticTokenSource is a TokenSource that always returns a static token.
type staticTokenSource struct {
	token string
	name  string
}

func (s *staticTokenSource) Token() (string, error) { return s.token, nil }
func (s *staticTokenSource) Name() string           { return s.name }

// hubCookieTokenSource is a TokenSource backed by a hub session cookie.
type hubCookieTokenSource struct{ cookie string }

func (h *hubCookieTokenSource) Token() (string, error) { return h.cookie, nil }
func (h *hubCookieTokenSource) Name() string           { return "HubCookieTokenSource" }

// CredentialStore is the abstract credential store that AuthProviders
// consult.
type CredentialStore interface {
	RefreshToken() string
	AccessToken() string
	IDToken() string
	HubCookie() string
	HostToken() string
	Account() string
}

// InMemoryCredentialStore is a simple in-memory CredentialStore.
type InMemoryCredentialStore struct {
	Refresh string
	Access  string
	ID      string
	Hub     string
	Host    string
	Acct    string
}

func (m *InMemoryCredentialStore) RefreshToken() string { return m.Refresh }
func (m *InMemoryCredentialStore) AccessToken() string  { return m.Access }
func (m *InMemoryCredentialStore) IDToken() string      { return m.ID }
func (m *InMemoryCredentialStore) HubCookie() string    { return m.Hub }
func (m *InMemoryCredentialStore) HostToken() string    { return m.Host }
func (m *InMemoryCredentialStore) Account() string      { return m.Acct }

// Now is a package-level clock override.
var Now = func() time.Time { return time.Now() }

// ---------------------------------------------------------------------------
// RefreshingTokenSource
// ---------------------------------------------------------------------------

// httpClient is the package-level HTTP client used for token refresh.
// Tests can replace it with a mock by setting httpClient.
var httpClient = &http.Client{Timeout: 30 * time.Second}

// TokenRefresher is the recovered token-refresh interface.
type TokenRefresher interface {
	RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error)
}

// TokenResponse is the response from oauth2.googleapis.com/token.
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
	return false
}

// RefreshingTokenSource auto-refreshes access tokens.
type RefreshingTokenSource struct {
	mu           sync.Mutex
	clientID     string
	scopes       []string
	credStore    CredentialStore
	cached       *TokenResponse
	cachedExpiry time.Time
	refreshFn    TokenRefresher
	refreshHook  func(*TokenResponse)
}

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
func (r *RefreshingTokenSource) Token() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cached != nil && r.cached.AccessToken != "" && Now().Before(r.cachedExpiry.Add(-5*time.Minute)) {
		return r.cached.AccessToken, nil
	}
	if err := r.refreshLocked(context.Background()); err != nil {
		if r.cached != nil && r.cached.AccessToken != "" && Now().Before(r.cachedExpiry) {
			return r.cached.AccessToken, nil
		}
		return "", err
	}
	return r.cached.AccessToken, nil
}

// Refresh forces an immediate refresh.
func (r *RefreshingTokenSource) Refresh(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refreshLocked(ctx)
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

func (r *RefreshingTokenSource) refreshLocked(ctx context.Context) error {
	if r.cached == nil || r.cached.RefreshToken == "" {
		return ErrNoTokenSource
	}
	var (
		resp *TokenResponse
		err  error
	)
	if r.refreshFn != nil {
		resp, err = r.refreshFn.RefreshToken(ctx, r.cached.RefreshToken)
	} else {
		resp, err = defaultRefresh(ctx, r.clientID, r.cached.RefreshToken, r.scopes)
	}
	if err != nil {
		return err
	}
	if resp.AccessToken == "" {
		return fmt.Errorf("grpc: refresh returned empty access token")
	}
	if resp.RefreshToken == "" {
		resp.RefreshToken = r.cached.RefreshToken
	}
	r.cached = resp
	if resp.ExpiresIn > 0 {
		r.cachedExpiry = Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	} else {
		r.cachedExpiry = Now().Add(1 * time.Hour)
	}
	if r.refreshHook != nil {
		go r.refreshHook(resp)
	}
	return nil
}

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
		return nil, fmt.Errorf("grpc: refresh http %d", resp.StatusCode)
	}
	var tr TokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return nil, err
	}
	return &tr, nil
}

// With401Retry runs fn with auto-refresh on 401.
func (r *RefreshingTokenSource) With401Retry(ctx context.Context, fn func(token string) error) error {
	tok, err := r.Token()
	if err != nil {
		return err
	}
	if err := fn(tok); err != nil {
		if !is401(err) {
			return err
		}
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

// is401 reports whether err looks like a 401 Unauthorized.
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
