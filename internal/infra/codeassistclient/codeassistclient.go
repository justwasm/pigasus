// Package codeassistclient is a recovered (re-implemented) Go binding for
// the 1.3.3 antigravity CodeAssistClient. Source-of-truth artifacts are in
// /decompiled/c/google3_third_party_jetski_language_server_code_assist_client_codeassistclient./
// (104 Ghidra-decompiled .c files).
//
// 1.3.3 defines seven concrete AuthProvider implementations, all of which
// satisfy the same interface and produce a TokenSource that the
// LanguageServerService gRPC stub can consume. The recovered providers:
//
//   - CLIAuthProvider            (default for `agy` CLI: device-code + refresh)
//   - AntigravityHubAuthProvider (browser hub session cookie)
//   - StandaloneAuthProvider     (Gemini API key, server-side billing)
//   - HostAuthProvider           (IDE / VS Code / JetBrains host pass-through)
//   - StubbyAuthProvider         (internal Stubs server, eval mode)
//   - IDEAuthProvider            (IDE-managed OAuth; non-CLI client_id)
//   - GeminiAPIKeyAuthProvider   (raw Gemini API key, no project)
//
// This file is the public surface (interfaces, enums, types, factories).
// The provider implementations live in providers.go. The
// RefreshingTokenSource wrapper lives in refreshing_token_source.go.
// The CodeAssistClient gRPC wrapper lives in client.go.
package codeassistclient

import (
	"context"
	"errors"
	"time"
)

// AuthMode is the recovered auth mode enum. Source:
// codeassistclient.AuthMode / resolveAuthMode (see _ResolveAuthMode.c).
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

// Tier is the recovered user tier enum. Source:
// codeassistclient.getUserTier / setUserTier (see _GetUserTier.c /
// _SetUserTier.c).
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

// Entitlement is the recovered user entitlement enum. Source:
// codeassistclient.getEntitlement (see _GetEntitlement.c).
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

// TokenSource is the recovered TokenSource interface from
// codeassistclient.TokenSource (see _Token.c). All seven AuthProvider
// implementations return a TokenSource.
type TokenSource interface {
	// Token returns the current bearer token. Implementations may
	// transparently refresh on 401. Source: _Token.c.
	Token() (string, error)
	// Name returns a human-readable name for diagnostics.
	Name() string
}

// OAuthTokenInfo carries the parsed ID-token claims. Source:
// codeassistclient.getOAuthTokenInfo (see _GetOAuthTokenInfo.c).
type OAuthTokenInfo struct {
	Email         string
	Domain        string
	HD            string // hosted domain (Workspace)
	AccessToken   string
	RefreshToken  string
	IDToken       string
	TokenType     string
	ExpiresAt     time.Time
	Scopes        []string
	ProjectNumber int64
	ProjectID     string
}

// AuthProvider is the recovered AuthProvider interface from
// codeassistclient.AuthProvider (see _GetTokenSource.c).
//
// Seven concrete implementations exist in 1.3.3; all live in providers.go.
type AuthProvider interface {
	// GetTokenSource returns a TokenSource for the configured credentials.
	// Source: codeassistclient.(*AuthProvider).GetTokenSource.
	GetTokenSource(ctx context.Context) (TokenSource, error)
	// Name returns the auth provider name (e.g. "CLIAuthProvider").
	Name() string
	// GetOAuthTokenInfo returns the parsed OAuth token info, if available.
	// Source: codeassistclient.(*AuthProvider).GetOAuthTokenInfo.
	GetOAuthTokenInfo(ctx context.Context) (*OAuthTokenInfo, error)
	// GetUserTier returns the resolved user tier (free / standard / ...).
	// Source: codeassistclient.(*AuthProvider).GetUserTier.
	GetUserTier(ctx context.Context) (Tier, error)
	// GetEntitlement returns the resolved entitlement.
	// Source: codeassistclient.(*AuthProvider).GetEntitlement.
	GetEntitlement(ctx context.Context) (Entitlement, error)
	// IsEval returns true if this is a stubby / eval session.
	// Source: codeassistclient.(*AuthProvider).IsEval.
	IsEval() bool
}

// CodeAssistClient is the recovered top-level client interface from
// codeassistclient.CodeAssistClient (see _StreamGenerateContentFromAICode.c).
type CodeAssistClient interface {
	// FetchLicensesFromAICode returns the list of available license tiers.
	// Source: codeassistclient.(*CodeAssistClient).FetchLicensesFromAICode.
	FetchLicensesFromAICode(ctx context.Context) ([]string, error)
	// SelfAssignLicense assigns a license tier to the current user.
	// Source: codeassistclient.(*CodeAssistClient).SelfAssignLicense.
	SelfAssignLicense(ctx context.Context, tier string) error
	// StreamGenerateContentFromAICode opens a streaming GenerateContent
	// session against cloudcode-pa.googleapis.com. Source:
	// codeassistclient.(*CodeAssistClient).StreamGenerateContentFromAICode.
	StreamGenerateContentFromAICode(ctx context.Context, model, prompt string) (<-chan StreamChunk, error)
	// FetchConfigFromAICode returns the per-user server-side config.
	// Source: codeassistclient.(*CodeAssistClient).FetchConfigFromAICode.
	FetchConfigFromAICode(ctx context.Context) (*UserConfig, error)
	// Close releases the underlying gRPC connection.
	Close() error
}

// UserConfig is the recovered per-user server config. Source:
// codeassistclient.UserConfig (see _FetchConfigFromAICode.c).
type UserConfig struct {
	CAICProject    string // overridden project for Cloud AI Companion
	GCPLocation    string // "us-central1" / "europe-west1" / ...
	EndpointURL    string // cloudcode-pa endpoint (prod / daily / stage)
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
var ErrNotImplemented = errors.New("codeassistclient: not implemented in recovered build")

// ErrNoTokenSource is returned when an AuthProvider has no usable
// credentials configured.
var ErrNoTokenSource = errors.New("codeassistclient: no token source available")
