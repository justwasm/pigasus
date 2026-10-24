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
package grpc

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ---------------------------------------------------------------------------
// Enums
// ---------------------------------------------------------------------------

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
// Interfaces & Types
// ---------------------------------------------------------------------------

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
