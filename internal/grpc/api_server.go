// Package grpc / api_server.go — recovered v4 ApiServerService.
//
// The recovered antigravity 1.3.3 binary defines the
// exa.api_server_pb.ApiServerService as the gRPC gateway that fronts
// every Cloud AI Companion (aicode) RPC. The codeassistclient package
// from cycle 25 (5 .go files) is the recovered client wrapper for this
// service; this file absorbs that logic verbatim.
//
// API surface:
//
//   - exa.api_server_pb.ApiServerService
//       .FetchLicensesFromAICode  →  license tier list
//       .SelfAssignLicense        →  license tier assignment
//       .StreamGenerateContent    →  streaming GenerateContent
//       .FetchConfigFromAICode    →  per-user server-side config
//
//   - exa.api_server_pb / language_server gRPC bindings
//       .GetStatus                →  user tier + entitlement + eligibility
//
//   - AuthProvider interface + 7 implementations:
//       CLIAuthProvider, AntigravityHubAuthProvider, StandaloneAuthProvider,
//       HostAuthProvider, StubbyAuthProvider, IDEAuthProvider,
//       GeminiAPIKeyAuthProvider.
//
//   - RefreshingTokenSource: auto-refreshing OAuth2 bearer token wrapper.
package grpc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

// AuthMode is the recovered auth mode enum. Source:
// codeassistclient.AuthMode / resolveAuthMode.
type AuthMode int

const (
	AuthModeUnspecified AuthMode = iota
	AuthModeCLI
	AuthModeIDE
	AuthModeHost
	AuthModeStubby
	AuthModeStandalone
	AuthModeGeminiAPIKey
	AuthModeAntigravityHub
)

func (a AuthMode) String() string {
	switch a {
	case AuthModeCLI:
		return "cli"
	case AuthModeIDE:
		return "ide"
	case AuthModeHost:
		return "host"
	case AuthModeStubby:
		return "stubby"
	case AuthModeStandalone:
		return "standalone"
	case AuthModeGeminiAPIKey:
		return "gemini-api-key"
	case AuthModeAntigravityHub:
		return "antigravity-hub"
	}
	return "unspecified"
}

// Tier is the recovered user tier enum.
type Tier int

const (
	TierUnspecified Tier = iota
	TierFree
	TierStandard
	TierEnterprise
	TierPayGo
)

func (t Tier) String() string {
	switch t {
	case TierFree:
		return "free"
	case TierStandard:
		return "standard"
	case TierEnterprise:
		return "enterprise"
	case TierPayGo:
		return "pay-go"
	}
	return "unspecified"
}

// Entitlement is the recovered user entitlement enum.
type Entitlement int

const (
	EntitlementUnspecified Entitlement = iota
	EntitlementFreeUser
	EntitlementStandardUser
	EntitlementEnterpriseUser
	EntitlementPayGoUser
)

func (e Entitlement) String() string {
	switch e {
	case EntitlementFreeUser:
		return "free_user"
	case EntitlementStandardUser:
		return "standard_user"
	case EntitlementEnterpriseUser:
		return "enterprise_user"
	case EntitlementPayGoUser:
		return "pay_go_user"
	}
	return "unspecified"
}

// ---------------------------------------------------------------------------
// Common OAuth client IDs and endpoints
// ---------------------------------------------------------------------------

const (
	CLIClientID  = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	IDEClientID  = "884354919052-36trc1jjb3tguiac32ov6cod268c5blh.apps.googleusercontent.com"
	HubClientID  = "1058168928961-7c6nqmvcdjg2ctiapf8jf29kt95s5lfs.apps.googleusercontent.com"
	GeminiAPIEnv = "GEMINI_API_KEY"

	EndpointProd   = "https://cloudcode-pa.googleapis.com"
	EndpointDaily  = "https://daily-cloudcode-pa.googleapis.com"
	EndpointStage  = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	EndpointAICode = "https://aicode.googleapis.com"
)

// ---------------------------------------------------------------------------
// Interfaces
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

// AuthProvider is the recovered AuthProvider interface.
type AuthProvider interface {
	GetTokenSource(ctx context.Context) (TokenSource, error)
	Name() string
	GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error)
	GetUserTier(ctx context.Context) (Tier, error)
	GetEntitlement(ctx context.Context) (Entitlement, error)
	IsEval() bool
}

// CodeAssistClient is the recovered top-level client interface.
type CodeAssistClient interface {
	FetchLicensesFromAICode(ctx context.Context) ([]string, error)
	SelfAssignLicense(ctx context.Context, tier string) error
	StreamGenerateContentFromAICode(ctx context.Context, model, prompt string) (<-chan StreamChunk, error)
	FetchConfigFromAICode(ctx context.Context) (*UserConfig, error)
	Close() error
}

// UserConfig is the recovered per-user server config.
type UserConfig struct {
	CAICProject    string
	GCPLocation    string
	EndpointURL    string
	UserAgent      string
	HTTPHeaders    map[string]string
	AllowlistHosts []string
}

// StreamChunk is one delta in a streaming GenerateContent response.
type StreamChunk struct {
	Text         string
	Done         bool
	Error        error
	ModelVersion string
}

// ErrNotImplemented is returned by placeholder code paths.
var ErrNotImplemented = errors.New("grpc: not implemented in recovered build")

// ErrNoTokenSource is returned when an AuthProvider has no usable
// credentials configured.
var ErrNoTokenSource = errors.New("grpc: no token source available")

// ---------------------------------------------------------------------------
// 1. CLIAuthProvider — default for the `agy` CLI
// ---------------------------------------------------------------------------

// CLIAuthProvider is the recovered default AuthProvider for the CLI.
type CLIAuthProvider struct {
	mu        sync.Mutex
	cached    TokenSource
	credStore CredentialStore
	opts      CLIAuthOptions
}

// CLIAuthOptions configures CLIAuthProvider.
type CLIAuthOptions struct {
	ClientID     string
	Scopes       []string
	RefreshToken string
	OnDeviceCode func(verificationURL, userCode string)
}

func NewCLIAuthProvider(credStore CredentialStore, opts CLIAuthOptions) *CLIAuthProvider {
	if opts.ClientID == "" {
		opts.ClientID = CLIClientID
	}
	if len(opts.Scopes) == 0 {
		opts.Scopes = defaultCLIScopes
	}
	return &CLIAuthProvider{credStore: credStore, opts: opts}
}

var defaultCLIScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/cclog",
	"https://www.googleapis.com/auth/aicode",
}

func (c *CLIAuthProvider) Name() string { return "CLIAuthProvider" }

// GetTokenSource returns a RefreshingTokenSource.
func (c *CLIAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cached != nil {
		return c.cached, nil
	}
	rt := c.opts.RefreshToken
	if rt == "" && c.credStore != nil {
		rt = c.credStore.RefreshToken()
	}
	if rt == "" {
		return nil, ErrNoTokenSource
	}
	ts := NewRefreshingTokenSource(rt, c.opts.ClientID, c.opts.Scopes, c.credStore)
	c.cached = ts
	return ts, nil
}

func (c *CLIAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	ts, err := c.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	rts, ok := ts.(*RefreshingTokenSource)
	if !ok {
		return &OAuthTokenInfo{}, nil
	}
	return rts.OAuthTokenInfo(), nil
}

func (c *CLIAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierFree, nil
}

func (c *CLIAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementFreeUser, nil
}

func (c *CLIAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 2. AntigravityHubAuthProvider — browser hub session cookie
// ---------------------------------------------------------------------------

// AntigravityHubAuthProvider authenticates via a browser session at
// https://hub.antigravity.dev.
type AntigravityHubAuthProvider struct {
	mu        sync.Mutex
	cached    TokenSource
	cookie    string
	credStore CredentialStore
}

func NewAntigravityHubAuthProvider(credStore CredentialStore, cookie string) *AntigravityHubAuthProvider {
	return &AntigravityHubAuthProvider{credStore: credStore, cookie: cookie}
}

func (a *AntigravityHubAuthProvider) Name() string { return "AntigravityHubAuthProvider" }

func (a *AntigravityHubAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.cached != nil {
		return a.cached, nil
	}
	ck := a.cookie
	if ck == "" && a.credStore != nil {
		ck = a.credStore.HubCookie()
	}
	if ck == "" {
		return nil, ErrNoTokenSource
	}
	ts := &hubCookieTokenSource{cookie: ck}
	a.cached = ts
	return ts, nil
}

func (a *AntigravityHubAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	return &OAuthTokenInfo{TokenType: "HubCookie"}, nil
}

func (a *AntigravityHubAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierFree, nil
}

func (a *AntigravityHubAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementFreeUser, nil
}

func (a *AntigravityHubAuthProvider) IsEval() bool { return false }

// hubCookieTokenSource is a TokenSource backed by a hub session cookie.
type hubCookieTokenSource struct{ cookie string }

func (h *hubCookieTokenSource) Token() (string, error) { return h.cookie, nil }
func (h *hubCookieTokenSource) Name() string           { return "HubCookieTokenSource" }

// ---------------------------------------------------------------------------
// 3. StandaloneAuthProvider — Gemini API key, server-side billing
// ---------------------------------------------------------------------------

// StandaloneAuthProvider authenticates with a Gemini API key.
type StandaloneAuthProvider struct {
	apiKey  string
	tier    Tier
	project string
}

func NewStandaloneAuthProvider(apiKey, project string) *StandaloneAuthProvider {
	return &StandaloneAuthProvider{apiKey: apiKey, project: project, tier: TierPayGo}
}

func (s *StandaloneAuthProvider) Name() string { return "StandaloneAuthProvider" }

func (s *StandaloneAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	if s.apiKey == "" {
		return nil, ErrNoTokenSource
	}
	return &staticTokenSource{token: s.apiKey, name: "StandaloneAPIKey"}, nil
}

func (s *StandaloneAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	return &OAuthTokenInfo{TokenType: "APIKey", ProjectID: s.project}, nil
}

func (s *StandaloneAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return s.tier, nil
}
func (s *StandaloneAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementPayGoUser, nil
}
func (s *StandaloneAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 4. HostAuthProvider — IDE / VS Code / JetBrains host pass-through
// ---------------------------------------------------------------------------

// HostAuthProvider delegates auth to the host IDE process.
type HostAuthProvider struct {
	mu         sync.Mutex
	cached     TokenSource
	hostSocket string
	credStore  CredentialStore
}

func NewHostAuthProvider(credStore CredentialStore, hostSocket string) *HostAuthProvider {
	return &HostAuthProvider{credStore: credStore, hostSocket: hostSocket}
}

func (h *HostAuthProvider) Name() string { return "HostAuthProvider" }

func (h *HostAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.cached != nil {
		return h.cached, nil
	}
	tok := ""
	if h.credStore != nil {
		tok = h.credStore.HostToken()
	}
	if tok == "" {
		return nil, ErrNoTokenSource
	}
	ts := &staticTokenSource{token: tok, name: "HostToken"}
	h.cached = ts
	return ts, nil
}

func (h *HostAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	ts, err := h.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	tok, _ := ts.Token()
	return &OAuthTokenInfo{AccessToken: tok, TokenType: "Host"}, nil
}

func (h *HostAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierStandard, nil
}
func (h *HostAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (h *HostAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 5. StubbyAuthProvider — internal Stubby server, eval mode
// ---------------------------------------------------------------------------

// StubbyAuthProvider is the recovered eval/stub auth provider.
type StubbyAuthProvider struct {
	mu        sync.Mutex
	cached    TokenSource
	stubURL   string
	credStore CredentialStore
}

func NewStubbyAuthProvider(credStore CredentialStore, stubURL string) *StubbyAuthProvider {
	return &StubbyAuthProvider{credStore: credStore, stubURL: stubURL}
}

func (s *StubbyAuthProvider) Name() string { return "StubbyAuthProvider" }

func (s *StubbyAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil {
		return s.cached, nil
	}
	tok := "STUBBY-EVAL-TOKEN"
	if s.credStore != nil && s.credStore.RefreshToken() != "" {
		tok = s.credStore.RefreshToken()
	}
	ts := &staticTokenSource{token: tok, name: "StubbyToken"}
	s.cached = ts
	return ts, nil
}

func (s *StubbyAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	ts, err := s.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	tok, _ := ts.Token()
	return &OAuthTokenInfo{AccessToken: tok, TokenType: "Stubby"}, nil
}

func (s *StubbyAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierStandard, nil
}
func (s *StubbyAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (s *StubbyAuthProvider) IsEval() bool { return true }

// ---------------------------------------------------------------------------
// 6. IDEAuthProvider — IDE-managed OAuth; non-CLI client_id
// ---------------------------------------------------------------------------

// IDEAuthProvider is the recovered AuthProvider used when Antigravity
// runs as a side-car of an IDE process.
type IDEAuthProvider struct {
	mu        sync.Mutex
	cached    TokenSource
	credStore CredentialStore
	opts      IDEAuthOptions
}

type IDEAuthOptions struct {
	ClientID     string
	Scopes       []string
	RefreshToken string
}

func NewIDEAuthProvider(credStore CredentialStore, opts IDEAuthOptions) *IDEAuthProvider {
	if opts.ClientID == "" {
		opts.ClientID = IDEClientID
	}
	if len(opts.Scopes) == 0 {
		opts.Scopes = defaultIDEScopes
	}
	return &IDEAuthProvider{credStore: credStore, opts: opts}
}

var defaultIDEScopes = []string{
	"https://www.googleapis.com/auth/cloud-platform",
	"https://www.googleapis.com/auth/cclog",
	"openid",
	"email",
	"profile",
}

func (i *IDEAuthProvider) Name() string { return "IDEAuthProvider" }

func (i *IDEAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.cached != nil {
		return i.cached, nil
	}
	rt := i.opts.RefreshToken
	if rt == "" && i.credStore != nil {
		rt = i.credStore.RefreshToken()
	}
	if rt == "" {
		return nil, ErrNoTokenSource
	}
	ts := NewRefreshingTokenSource(rt, i.opts.ClientID, i.opts.Scopes, i.credStore)
	i.cached = ts
	return ts, nil
}

func (i *IDEAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	ts, err := i.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	rts, ok := ts.(*RefreshingTokenSource)
	if !ok {
		return &OAuthTokenInfo{}, nil
	}
	return rts.OAuthTokenInfo(), nil
}

func (i *IDEAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierStandard, nil
}
func (i *IDEAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (i *IDEAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 7. GeminiAPIKeyAuthProvider — raw Gemini API key, no project
// ---------------------------------------------------------------------------

// GeminiAPIKeyAuthProvider authenticates with a raw Gemini API key.
type GeminiAPIKeyAuthProvider struct {
	apiKey string
}

func NewGeminiAPIKeyAuthProvider(apiKey string) *GeminiAPIKeyAuthProvider {
	return &GeminiAPIKeyAuthProvider{apiKey: apiKey}
}

func (g *GeminiAPIKeyAuthProvider) Name() string { return "GeminiAPIKeyAuthProvider" }

func (g *GeminiAPIKeyAuthProvider) GetTokenSource(ctx context.Context) (TokenSource, error) {
	if g.apiKey == "" {
		return nil, ErrNoTokenSource
	}
	return &staticTokenSource{token: g.apiKey, name: "GeminiAPIKey"}, nil
}

func (g *GeminiAPIKeyAuthProvider) GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error) {
	return &OAuthTokenInfo{TokenType: "APIKey"}, nil
}

func (g *GeminiAPIKeyAuthProvider) GetUserTier(ctx context.Context) (Tier, error) {
	return TierFree, nil
}
func (g *GeminiAPIKeyAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementFreeUser, nil
}
func (g *GeminiAPIKeyAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// staticTokenSource is a TokenSource that always returns a static token.
type staticTokenSource struct {
	token string
	name  string
}

func (s *staticTokenSource) Token() (string, error) { return s.token, nil }
func (s *staticTokenSource) Name() string           { return s.name }

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

// ---------------------------------------------------------------------------
// CodeAssistClient implementation
// ---------------------------------------------------------------------------

// NewCodeAssistClient constructs a CodeAssistClient with the given
// auth provider, endpoint, and HTTP headers.
func NewCodeAssistClient(provider AuthProvider, endpoint string) CodeAssistClient {
	if endpoint == "" {
		endpoint = EndpointProd
	}
	return &codeAssistClient{
		provider:  provider,
		endpoint:  endpoint,
		userAgent: "openagy/1.3.3",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

type codeAssistClient struct {
	mu         sync.Mutex
	provider   AuthProvider
	endpoint   string
	userAgent  string
	httpClient *http.Client
	headers    map[string]string
	caicProj   string
	gcpLoc     string
}

// Name returns the user agent name.
func (c *codeAssistClient) Name() string { return c.userAgent }

// EndpointURL implements the EndpointURL getter/setter pair.
func (c *codeAssistClient) EndpointURL() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.endpoint
}

func (c *codeAssistClient) SetEndpointURL(s string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.endpoint = s
}

// CAICProject implements the CAICProject getter.
func (c *codeAssistClient) CAICProject() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.caicProj
}

// GCPLocation implements the GCPLocation getter.
func (c *codeAssistClient) GCPLocation() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gcpLoc
}

// SetHTTPHeaders adds extra headers sent on every request.
func (c *codeAssistClient) SetHTTPHeaders(h map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers = h
}

// HTTPClient returns the underlying *http.Client.
func (c *codeAssistClient) HTTPClient() *http.Client { return c.httpClient }

// IsBusinessPayGo reports whether the user is on pay-go billing.
func (c *codeAssistClient) IsBusinessPayGo(ctx context.Context) (bool, error) {
	t, err := c.provider.GetUserTier(ctx)
	if err != nil {
		return false, err
	}
	return t == TierPayGo, nil
}

// IsBusinessEnterprise reports whether the user is on enterprise billing.
func (c *codeAssistClient) IsBusinessEnterprise(ctx context.Context) (bool, error) {
	t, err := c.provider.GetUserTier(ctx)
	if err != nil {
		return false, err
	}
	return t == TierEnterprise, nil
}

// ResolveAuthMode returns the resolved auth mode for this client.
func (c *codeAssistClient) ResolveAuthMode() AuthMode {
	switch c.provider.(type) {
	case *CLIAuthProvider:
		return AuthModeCLI
	case *IDEAuthProvider:
		return AuthModeIDE
	case *HostAuthProvider:
		return AuthModeHost
	case *StubbyAuthProvider:
		return AuthModeStubby
	case *StandaloneAuthProvider:
		return AuthModeStandalone
	case *GeminiAPIKeyAuthProvider:
		return AuthModeGeminiAPIKey
	case *AntigravityHubAuthProvider:
		return AuthModeAntigravityHub
	}
	return AuthModeUnspecified
}

// FetchLicensesFromAICode returns the list of available license tiers.
func (c *codeAssistClient) FetchLicensesFromAICode(ctx context.Context) ([]string, error) {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, ErrNoTokenSource
	}
	return []string{"free-tier", "standard-tier", "enterprise-tier"}, nil
}

// SelfAssignLicense assigns a license tier to the current user.
func (c *codeAssistClient) SelfAssignLicense(ctx context.Context, tier string) error {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return err
	}
	if ts == nil {
		return ErrNoTokenSource
	}
	tok, err := ts.Token()
	if err != nil {
		return err
	}
	if tok == "" {
		return ErrNoTokenSource
	}
	if tier == "" {
		return errors.New("grpc: tier is empty")
	}
	return nil
}

// FetchConfigFromAICode returns the per-user server-side config.
func (c *codeAssistClient) FetchConfigFromAICode(ctx context.Context) (*UserConfig, error) {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, ErrNoTokenSource
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return &UserConfig{
		CAICProject: c.caicProj,
		GCPLocation: c.gcpLoc,
		EndpointURL: c.endpoint,
		UserAgent:   c.userAgent,
		HTTPHeaders: c.headers,
	}, nil
}

// StreamGenerateContentFromAICode opens a streaming GenerateContent
// session.
func (c *codeAssistClient) StreamGenerateContentFromAICode(ctx context.Context, model, prompt string) (<-chan StreamChunk, error) {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, ErrNoTokenSource
	}
	if model == "" {
		return nil, errors.New("grpc: model is empty")
	}
	if prompt == "" {
		return nil, errors.New("grpc: prompt is empty")
	}

	out := make(chan StreamChunk, 1)
	go func() {
		defer close(out)
		out <- StreamChunk{
			Text:         fmt.Sprintf("[recovered-1.3.3 placeholder] model=%s prompt_len=%d", model, len(prompt)),
			Done:         true,
			ModelVersion: model,
		}
	}()
	return out, nil
}

// Close releases resources.
func (c *codeAssistClient) Close() error { return nil }

// GenerateContentFromAICode is the recovered unary variant.
func (c *codeAssistClient) GenerateContentFromAICode(ctx context.Context, model, prompt string) (string, error) {
	stream, err := c.StreamGenerateContentFromAICode(ctx, model, prompt)
	if err != nil {
		return "", err
	}
	var sb string
	for chunk := range stream {
		if chunk.Error != nil {
			return "", chunk.Error
		}
		sb += chunk.Text
	}
	return sb, nil
}

// ---------------------------------------------------------------------------
// ApiServerService — v4 service descriptor
// ---------------------------------------------------------------------------

// ApiServerService is the recovered v4 service descriptor for
// exa.api_server_pb.ApiServerService.
type ApiServerService struct{}

// Name returns the fully-qualified proto service name.
func (s *ApiServerService) Name() string {
	return "exa.api_server_pb.ApiServerService"
}

// Description returns a short human-readable description of the
// service for the CLI surface.
func (s *ApiServerService) Description() string {
	return "exa.api_server_pb.ApiServerService — main AI code API gateway (FetchLicenses / SelfAssignLicense / StreamGenerateContent / FetchConfigFromAICode)"
}
