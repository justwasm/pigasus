package codeassistclient

import (
	"context"
	"errors"
	"testing"
)

// fakeTokenRefresher is a TokenRefresher that returns a canned
// response. Used to avoid real network calls in tests.
type fakeTokenRefresher struct {
	resp *TokenResponse
	err  error
}

func (f *fakeTokenRefresher) RefreshToken(ctx context.Context, refreshToken string) (*TokenResponse, error) {
	if f.err != nil {
		return nil, f.err
	}
	r := *f.resp
	r.RefreshToken = refreshToken
	return &r, nil
}

// stubProviders returns the 7 providers, each with their RefreshingTokenSource
// (where applicable) configured to use a fake refresher that does not hit the
// network. The other providers (Hub / Host / Stubby / API key) already return
// non-refreshing static TokenSources, so they don't need a refresher.
func stubProviders() []AuthProvider {
	cred := &InMemoryCredentialStore{Refresh: "test-rt", Host: "test-host-tok", Acct: "test@example.com"}
	cli := NewCLIAuthProvider(cred, CLIAuthOptions{})
	if rts, err := cli.GetTokenSource(context.Background()); err == nil {
		if r, ok := rts.(*RefreshingTokenSource); ok {
			r.SetTokenRefresher(&fakeTokenRefresher{resp: &TokenResponse{
				AccessToken: "fake-access",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
				Scope:       "openid",
			}})
		}
	}
	ide := NewIDEAuthProvider(cred, IDEAuthOptions{})
	if rts, err := ide.GetTokenSource(context.Background()); err == nil {
		if r, ok := rts.(*RefreshingTokenSource); ok {
			r.SetTokenRefresher(&fakeTokenRefresher{resp: &TokenResponse{
				AccessToken: "fake-access",
				TokenType:   "Bearer",
				ExpiresIn:   3600,
			}})
		}
	}
	return []AuthProvider{
		cli,
		NewAntigravityHubAuthProvider(cred, "hub-cookie"),
		NewStandaloneAuthProvider("api-key", "test-project"),
		NewHostAuthProvider(cred, "/tmp/host.sock"),
		NewStubbyAuthProvider(cred, "http://stubby:8080"),
		ide,
		NewGeminiAPIKeyAuthProvider("gemini-key"),
	}
}

func TestAllSevenAuthProviders(t *testing.T) {
	providers := stubProviders()
	names := []string{
		"CLIAuthProvider",
		"AntigravityHubAuthProvider",
		"StandaloneAuthProvider",
		"HostAuthProvider",
		"StubbyAuthProvider",
		"IDEAuthProvider",
		"GeminiAPIKeyAuthProvider",
	}
	if len(providers) != 7 {
		t.Fatalf("got %d providers, want 7", len(providers))
	}
	for i, p := range providers {
		if p.Name() != names[i] {
			t.Errorf("provider[%d].Name = %q, want %q", i, p.Name(), names[i])
		}
		ts, err := p.GetTokenSource(context.Background())
		if err != nil {
			t.Errorf("provider[%d].GetTokenSource: %v", i, err)
			continue
		}
		if ts == nil {
			t.Errorf("provider[%d] returned nil TokenSource", i)
			continue
		}
		tok, err := ts.Token()
		if err != nil {
			t.Errorf("provider[%d].Token: %v", i, err)
		}
		if tok == "" {
			t.Errorf("provider[%d] returned empty token", i)
		}
	}
}

func TestAuthProvider_NoToken(t *testing.T) {
	// Empty cred store -> CLIAuthProvider should fail.
	cred := &InMemoryCredentialStore{}
	prov := NewCLIAuthProvider(cred, CLIAuthOptions{})
	_, err := prov.GetTokenSource(context.Background())
	if !errors.Is(err, ErrNoTokenSource) {
		t.Errorf("expected ErrNoTokenSource, got %v", err)
	}
}

func TestRefreshingTokenSource_ProactiveRefresh(t *testing.T) {
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	// Use a fake refresher that does not hit the network.
	rts.SetTokenRefresher(&fakeTokenRefresher{resp: &TokenResponse{
		AccessToken: "fake-refreshed",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}})
	// Manually set cached token to a stale value.
	rts.mu.Lock()
	rts.cached = &TokenResponse{AccessToken: "stale", RefreshToken: "rt-1"}
	rts.cachedExpiry = Now()
	rts.mu.Unlock()
	tok, err := rts.Token()
	if err != nil {
		t.Fatalf("Token: %v", err)
	}
	if tok != "fake-refreshed" {
		t.Errorf("Token = %q, want %q", tok, "fake-refreshed")
	}
}

func TestCodeAssistClient_FetchLicenses(t *testing.T) {
	providers := stubProviders()
	client := NewCodeAssistClient(providers[0], EndpointProd)
	licenses, err := client.FetchLicensesFromAICode(context.Background())
	if err != nil {
		t.Fatalf("FetchLicensesFromAICode: %v", err)
	}
	if len(licenses) < 1 {
		t.Errorf("expected at least 1 license, got %d", len(licenses))
	}
}

func TestCodeAssistClient_SelfAssignLicense(t *testing.T) {
	providers := stubProviders()
	client := NewCodeAssistClient(providers[0], EndpointProd)
	if err := client.SelfAssignLicense(context.Background(), "free-tier"); err != nil {
		t.Fatalf("SelfAssignLicense: %v", err)
	}
	// Empty tier should error.
	if err := client.SelfAssignLicense(context.Background(), ""); err == nil {
		t.Error("expected error for empty tier")
	}
}

func TestCodeAssistClient_StreamGenerateContent(t *testing.T) {
	providers := stubProviders()
	client := NewCodeAssistClient(providers[0], EndpointProd)
	stream, err := client.StreamGenerateContentFromAICode(context.Background(), "gemini-1.5-pro", "hello")
	if err != nil {
		t.Fatalf("StreamGenerateContentFromAICode: %v", err)
	}
	count := 0
	for chunk := range stream {
		count++
		if chunk.Error != nil {
			t.Errorf("chunk.Error: %v", chunk.Error)
		}
		if chunk.Text == "" {
			t.Error("chunk.Text is empty")
		}
	}
	if count == 0 {
		t.Error("expected at least 1 chunk")
	}
}

func TestCodeAssistClient_ResolveAuthMode(t *testing.T) {
	providers := stubProviders()
	want := []AuthMode{
		AuthModeCLI,
		AuthModeAntigravityHub,
		AuthModeStandalone,
		AuthModeHost,
		AuthModeStubby,
		AuthModeIDE,
		AuthModeGeminiAPIKey,
	}
	if len(providers) != len(want) {
		t.Fatalf("got %d providers, want %d", len(providers), len(want))
	}
	for i, p := range providers {
		client := NewCodeAssistClient(p, EndpointProd)
		if cac, ok := client.(*codeAssistClient); ok {
			got := cac.ResolveAuthMode()
			if got != want[i] {
				t.Errorf("%s: got %v, want %v", p.Name(), got, want[i])
			}
		} else {
			t.Errorf("client is not *codeAssistClient: %T", client)
		}
	}
}
