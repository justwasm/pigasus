// Package grpc / auth_providers.go — AuthProvider implementations.
//
// Extracted from api_server.go: contains the AuthProvider interface and
// 7 implementations:
//  1. CLIAuthProvider
//  2. AntigravityHubAuthProvider
//  3. StandaloneAuthProvider
//  4. HostAuthProvider
//  5. StubbyAuthProvider
//  6. IDEAuthProvider
//  7. GeminiAPIKeyAuthProvider
package grpc

import (
	"context"
	"sync"
)

// ---------------------------------------------------------------------------
// AuthMode
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

// ---------------------------------------------------------------------------
// AuthProvider Interface
// ---------------------------------------------------------------------------

// AuthProvider is the recovered AuthProvider interface.
type AuthProvider interface {
	GetTokenSource(ctx context.Context) (TokenSource, error)
	Name() string
	GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error)
	GetUserTier(ctx context.Context) (Tier, error)
	GetEntitlement(ctx context.Context) (Entitlement, error)
	IsEval() bool
}

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
