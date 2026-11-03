// AuthProvider implementations recovered from 1.3.3 antigravity binary.
// Source-of-truth artifacts are in
// /decompiled/c/google3_third_party_jetski_language_server_code_assist_client_codeassistclient./
//
// All seven providers satisfy the AuthProvider interface (see
// codeassistclient.go). They differ in:
//   - where the OAuth credentials come from (CLI device-code, IDE host,
//     browser hub, Gemini API key, internal stubby, ...);
//   - which OAuth client_id is used (CLIClientID / IDEClientID);
//   - which scopes are requested.
//
// In the recovered 1.3.3 build, all providers return non-functional
// placeholder TokenSources (the real Antigravity backend is not
// reachable from the sandbox); the structure is preserved for
// round-trip testing and as a starting point for a real client.
package codeassistclient

import (
	"context"
	"sync"
	"time"
)

// Common OAuth client IDs and endpoints. Source: cli/backend/auth/auth.
const (
	CLIClientID  = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	IDEClientID  = "884354919052-36trc1jjb3tguiac32ov6cod268c5blh.apps.googleusercontent.com"
	HubClientID  = "1058168928961-7c6nqmvcdjg2ctiapf8jf29kt95s5lfs.apps.googleusercontent.com"
	GeminiAPIEnv = "GEMINI_API_KEY"

	EndpointProd    = "https://cloudcode-pa.googleapis.com"
	EndpointDaily   = "https://daily-cloudcode-pa.googleapis.com"
	EndpointStage   = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	EndpointAICode  = "https://aicode.googleapis.com"
)

// ---------------------------------------------------------------------------
// 1. CLIAuthProvider — default for the `agy` CLI
// ---------------------------------------------------------------------------

// CLIAuthProvider is the recovered default AuthProvider for the CLI.
// Source: codeassistclient.(*CLIAuthProvider).GetTokenSource (see
// _GetTokenSource.c at 0x798ddc0).
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

// GetTokenSource returns a RefreshingTokenSource. Source:
// codeassistclient.(*CLIAuthProvider).GetTokenSource.
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

// AntigravityHubAuthProvider authenticates via a browser session
// established at https://hub.antigravity.dev. Source:
// codeassistclient.AntigravityHubAuthProvider (see _GetTokenSource.c at
// 0x798ea00).
type AntigravityHubAuthProvider struct {
	mu     sync.Mutex
	cached TokenSource
	cookie string
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

// StandaloneAuthProvider authenticates with a Gemini API key, where
// billing happens on Google's side via the API key's linked billing
// account. Source: codeassistclient.NewStandaloneAuthProvider (see
// _NewStandaloneAuthProvider.c at 0x798f8c0).
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

func (s *StandaloneAuthProvider) GetUserTier(ctx context.Context) (Tier, error) { return s.tier, nil }
func (s *StandaloneAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementPayGoUser, nil
}
func (s *StandaloneAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 4. HostAuthProvider — IDE / VS Code / JetBrains host pass-through
// ---------------------------------------------------------------------------

// HostAuthProvider delegates auth to the host IDE process. Source:
// codeassistclient.HostAuthProvider (see _GetTokenSource.c at 0x798fac0).
type HostAuthProvider struct {
	mu     sync.Mutex
	cached TokenSource
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

func (h *HostAuthProvider) GetUserTier(ctx context.Context) (Tier, error) { return TierStandard, nil }
func (h *HostAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (h *HostAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 5. StubbyAuthProvider — internal Stubby server, eval mode
// ---------------------------------------------------------------------------

// StubbyAuthProvider is the recovered eval/stub auth provider that
// points at the internal Stubby (gRPC mocking) server. Source:
// codeassistclient.StubbyAuthProvider (see _GetTokenSource.c at
// 0x79915c0).
type StubbyAuthProvider struct {
	mu      sync.Mutex
	cached  TokenSource
	stubURL string
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

func (s *StubbyAuthProvider) GetUserTier(ctx context.Context) (Tier, error)     { return TierStandard, nil }
func (s *StubbyAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (s *StubbyAuthProvider) IsEval() bool { return true }

// ---------------------------------------------------------------------------
// 6. IDEAuthProvider — IDE-managed OAuth; non-CLI client_id
// ---------------------------------------------------------------------------

// IDEAuthProvider is the recovered AuthProvider used when Antigravity
// runs as a side-car of an IDE process. Uses the IDE OAuth client_id
// (different from CLIClientID). Source: codeassistclient.IDEAuthProvider
// (see _GetTokenSource.c at 0x7993e60).
type IDEAuthProvider struct {
	mu     sync.Mutex
	cached TokenSource
	credStore CredentialStore
	opts   IDEAuthOptions
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

func (i *IDEAuthProvider) GetUserTier(ctx context.Context) (Tier, error)     { return TierStandard, nil }
func (i *IDEAuthProvider) GetEntitlement(ctx context.Context) (Entitlement, error) {
	return EntitlementStandardUser, nil
}
func (i *IDEAuthProvider) IsEval() bool { return false }

// ---------------------------------------------------------------------------
// 7. GeminiAPIKeyAuthProvider — raw Gemini API key, no project
// ---------------------------------------------------------------------------

// GeminiAPIKeyAuthProvider authenticates with a raw Gemini API key
// without project affiliation. Source: codeassistclient.GeminiAPIKeyAuthProvider.
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

func (g *GeminiAPIKeyAuthProvider) GetUserTier(ctx context.Context) (Tier, error) { return TierFree, nil }
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
// consult. The real keyring is in internal/keyring; the codeassistclient
// package defines only the interface to keep this package
// self-contained.
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
