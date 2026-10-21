// CodeAssistClient implementation. Source-of-truth artifacts:
// /decompiled/c/google3_third_party_jetski_language_server_code_assist_client_codeassistclient./
//   _FetchLicensesFromAICode.c      (FetchLicensesFromAICode)
//   _SelfAssignLicense.c           (SelfAssignLicense) [via _LanguageServerService_SelfAssignLicense_Handler]
//   _StreamGenerateContentFromAICode.c
//   _GenerateContentFromAICode.c
//   _FetchConfigFromAICode.c
//   _GetHTTPClient.c
//   _SetEndpointURL.c / _GetEndpointURL.c
//   _GetCAICProject.c / _SetCAICProject
//   _GetGCPLocation.c
//   _IsBusinessPayGo.c / _IsBusinessEnterprise.c
//   _ResolveAuthMode.c
//
// In the recovered 1.3.3 build, this client preserves the recovered
// structure but does not actually dial cloudcode-pa.googleapis.com —
// that endpoint requires Google's internal mTLS trust, which is not
// available in the sandbox. Calls return ErrNotImplemented (or a
// structured placeholder) so the public surface can be exercised.
package codeassistclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"
)

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

// Name returns the user agent name. Source: _GetUserAgentName.c.
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
// Source: _SetHTTPHeaders.c.
func (c *codeAssistClient) SetHTTPHeaders(h map[string]string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.headers = h
}

// HTTPClient returns the underlying *http.Client. Source: _GetHTTPClient.c.
func (c *codeAssistClient) HTTPClient() *http.Client { return c.httpClient }

// IsBusinessPayGo reports whether the user is on pay-go billing.
// Source: _IsBusinessPayGo.c.
func (c *codeAssistClient) IsBusinessPayGo(ctx context.Context) (bool, error) {
	t, err := c.provider.GetUserTier(ctx)
	if err != nil {
		return false, err
	}
	return t == TierPayGo, nil
}

// IsBusinessEnterprise reports whether the user is on enterprise billing.
// Source: _IsBusinessEnterprise.c.
func (c *codeAssistClient) IsBusinessEnterprise(ctx context.Context) (bool, error) {
	t, err := c.provider.GetUserTier(ctx)
	if err != nil {
		return false, err
	}
	return t == TierEnterprise, nil
}

// ResolveAuthMode returns the resolved auth mode for this client.
// Source: _ResolveAuthMode.c.
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
// Source: _FetchLicensesFromAICode.c. Calls the LanguageServerService's
// ListLicenses RPC; in the recovered placeholder, returns the
// hard-coded set.
func (c *codeAssistClient) FetchLicensesFromAICode(ctx context.Context) ([]string, error) {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, ErrNoTokenSource
	}
	// Recovered set: 1.3.3 ships three license tiers.
	return []string{"free-tier", "standard-tier", "enterprise-tier"}, nil
}

// SelfAssignLicense assigns a license tier to the current user. Source:
// codeassistclient.(*CodeAssistClient).SelfAssignLicense — implemented
// via LanguageServerService.SelfAssignLicense RPC.
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
	// Recovered validation: tier must be non-empty.
	if tier == "" {
		return errors.New("codeassistclient: tier is empty")
	}
	// In the recovered build, the call is a placeholder (real call
	// would invoke LanguageServerService_SelfAssignLicense via gRPC).
	return nil
}

// FetchConfigFromAICode returns the per-user server-side config.
// Source: _FetchConfigFromAICode.c.
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
// session. Source: _StreamGenerateContentFromAICode.c. The recovered
// 1.3.3 implementation translates a single prompt into a unary
// GenerateContent and demuxes the response into a stream of
// StreamChunk values.
func (c *codeAssistClient) StreamGenerateContentFromAICode(ctx context.Context, model, prompt string) (<-chan StreamChunk, error) {
	ts, err := c.provider.GetTokenSource(ctx)
	if err != nil {
		return nil, err
	}
	if ts == nil {
		return nil, ErrNoTokenSource
	}
	if model == "" {
		return nil, errors.New("codeassistclient: model is empty")
	}
	if prompt == "" {
		return nil, errors.New("codeassistclient: prompt is empty")
	}

	// Recovered call path: build a GenerateContentRequest, POST to
	// {endpoint}/v1internal:generateContent, stream the response.
	// The sandbox cannot reach cloudcode-pa.googleapis.com, so the
	// recovered implementation emits a single placeholder chunk and
	// closes the channel.
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

// Close releases resources. Source: gRPC connection close.
func (c *codeAssistClient) Close() error { return nil }

// GenerateContentFromAICode is the recovered unary variant. Source:
// _GenerateContentFromAICode.c.
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
