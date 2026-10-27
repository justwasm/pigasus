// Tests for the merged grpc package.
//
// These tests merge the cycle-25 test coverage of:
//   - internal/codeassistclient/codeassistclient_test.go (7 providers)
//   - internal/language_server_pb/language_server_test.go (4 RPCs)
//
// into a single file under the new grpc package.
package grpc

import (
	"context"
	"errors"
	"testing"
)

// ---------------------------------------------------------------------------
// AllServices / ListServiceNames — v4 alignment
// ---------------------------------------------------------------------------

func TestAllServices_Count(t *testing.T) {
	if len(AllServices) != 6 {
		t.Fatalf("AllServices has %d entries, want 6", len(AllServices))
	}
}

func TestAllServices_Names(t *testing.T) {
	want := []string{
		"exa.api_server_pb.ApiServerService",
		"exa.extension_server_pb.ExtensionServerService",
		"google.gca.aicode.v1main.JetskiRemoteAgentProxyService",
		"exa.knowledge_base_pb.KnowledgeBaseService",
		"exa.language_server_pb.LanguageServerService",
		"exa.seat_management_pb.SeatManagementService",
	}
	for i, s := range AllServices {
		if got := s.Name(); got != want[i] {
			t.Errorf("AllServices[%d].Name = %q, want %q", i, got, want[i])
		}
	}
}

func TestAllServices_Description(t *testing.T) {
	for i, s := range AllServices {
		if s.Description() == "" {
			t.Errorf("AllServices[%d].Description is empty", i)
		}
	}
}

func TestListServiceNames(t *testing.T) {
	names := ListServiceNames()
	if len(names) != 6 {
		t.Fatalf("ListServiceNames has %d entries, want 6", len(names))
	}
	// Spot-check the first name.
	if names[0] != "exa.api_server_pb.ApiServerService" {
		t.Errorf("names[0] = %q, want exa.api_server_pb.ApiServerService", names[0])
	}
}

// ---------------------------------------------------------------------------
// api_server tests (from cycle-25 codeassistclient_test.go)
// ---------------------------------------------------------------------------

// fakeTokenRefresher is a TokenRefresher that returns a canned
// response.
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

// stubProviders returns the 7 providers.
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
	cred := &InMemoryCredentialStore{}
	prov := NewCLIAuthProvider(cred, CLIAuthOptions{})
	_, err := prov.GetTokenSource(context.Background())
	if !errors.Is(err, ErrNoTokenSource) {
		t.Errorf("expected ErrNoTokenSource, got %v", err)
	}
}

func TestRefreshingTokenSource_ProactiveRefresh(t *testing.T) {
	rts := NewRefreshingTokenSource("rt-1", CLIClientID, defaultCLIScopes, &InMemoryCredentialStore{Refresh: "rt-1"})
	rts.SetTokenRefresher(&fakeTokenRefresher{resp: &TokenResponse{
		AccessToken: "fake-refreshed",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}})
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

// ---------------------------------------------------------------------------
// language_server tests (from cycle-25 language_server_test.go)
// ---------------------------------------------------------------------------

func TestDialLanguageServerService_MissingToken(t *testing.T) {
	_, err := DialLanguageServerService(DialOptions{Endpoint: "https://cloudcode-pa.googleapis.com"})
	if err == nil {
		t.Error("expected error when BearerToken func is missing")
	}
}

func TestSelectLicense(t *testing.T) {
	client, err := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()
	resp, err := client.SelectLicense(context.Background(), &SelectLicenseRequest{
		LicenseTier: LicenseFree,
	})
	if err != nil {
		t.Fatalf("SelectLicense: %v", err)
	}
	if resp.LicenseTier != LicenseFree {
		t.Errorf("LicenseTier = %q, want %q", resp.LicenseTier, LicenseFree)
	}
	if resp.LicenseID == "" {
		t.Error("LicenseID is empty")
	}
}

func TestSelectLicense_EmptyTier(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	_, err := client.SelectLicense(context.Background(), &SelectLicenseRequest{})
	if err == nil {
		t.Error("expected error for empty tier")
	}
}

func TestSelfAssignLicense(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.SelfAssignLicense(context.Background(), &SelfAssignLicenseRequest{
		LicenseTier: LicenseStandard,
	})
	if err != nil {
		t.Fatalf("SelfAssignLicense: %v", err)
	}
	if resp.LicenseID == "" {
		t.Error("LicenseID is empty")
	}
}

func TestGetStatus(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	resp, err := client.GetStatus(context.Background(), &GetStatusRequest{})
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if !resp.Eligible {
		t.Error("Eligible = false, want true (placeholder)")
	}
	if resp.EndpointURL == "" {
		t.Error("EndpointURL is empty")
	}
}

func TestStreamGenerateContent(t *testing.T) {
	client, _ := DialLanguageServerService(DialOptions{
		Endpoint:    "https://cloudcode-pa.googleapis.com",
		BearerToken: func() (string, error) { return "test-token", nil },
	})
	defer client.Close()
	stream, err := client.StreamGenerateContent(context.Background(), &GenerateContentRequest{
		Model: "gemini-1.5-pro",
		Contents: []Content{{
			Role:  "user",
			Parts: []Part{{Text: "hello"}},
		}},
	})
	if err != nil {
		t.Fatalf("StreamGenerateContent: %v", err)
	}
	count := 0
	for resp := range stream {
		count++
		if resp.ModelVersion != "gemini-1.5-pro" {
			t.Errorf("ModelVersion = %q, want gemini-1.5-pro", resp.ModelVersion)
		}
	}
	if count == 0 {
		t.Error("expected at least 1 response")
	}
}
