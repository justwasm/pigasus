package backend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/store"
)

func TestCLIAuthProviderGetTokenSource_NoCachedToken(t *testing.T) {
	mgr := store.NewManager()
	prov := NewCLIAuthProvider(mgr)
	if prov.Name() != "CLIAuthProvider" {
		t.Errorf("Name = %q, want CLIAuthProvider", prov.Name())
	}
	// No cached token -> ErrNoCachedToken.
	ctx := context.Background()
	_, err := prov.GetTokenSource(ctx)
	if err == nil {
		t.Error("expected error when no cached token, got nil")
	}
}

func TestCLIAuthProviderStartOAuthProcess(t *testing.T) {
	// Cycle 50: StartOAuthProcess is now a no-op that returns an
	// error pointing callers to $JETSKI_OAUTH_TOKEN / auth.LoadTokenUnified.
	// The v1.3.5 binary does NOT run a device-code flow at runtime.
	mgr := store.NewManager()
	prov := NewCLIAuthProvider(mgr)
	dev, err := prov.StartOAuthProcess(context.Background())
	if err == nil {
		t.Fatal("StartOAuthProcess: expected error (device-code flow removed in cycle 50)")
	}
	if dev != nil {
		t.Errorf("StartOAuthProcess: dev = %+v, want nil", dev)
	}
	if !strings.Contains(err.Error(), "JETSKI_OAUTH_TOKEN") &&
		!strings.Contains(err.Error(), "LoadTokenUnified") &&
		!strings.Contains(err.Error(), "removed") {
		t.Errorf("StartOAuthProcess: error %q should mention $JETSKI_OAUTH_TOKEN or LoadTokenUnified", err)
	}
}

func TestFetchLicenses(t *testing.T) {
	// Without a token source, FetchLicenses should return an error.
	ctx := context.Background()
	_, err := FetchLicenses(ctx, nil)
	if err == nil {
		t.Error("expected error when token source is nil")
	}
}

func TestSelfAssignLicense_NoToken(t *testing.T) {
	ctx := context.Background()
	err := SelfAssignLicense(ctx, nil, "free-tier")
	if err == nil {
		t.Error("expected error when token source is nil")
	}
}

func TestAICodeClient_DefaultEndpoint(t *testing.T) {
	c := NewAICodeClient("")
	if c == nil {
		t.Fatal("NewAICodeClient returned nil")
	}
}

// TestAICodeClient_GenerateContent_BearerScope is the cycle-50
// "test real values" replacement for the cycle-38
// TestAICodeClient_GenerateContent_Placeholder. Instead of asserting
// that the recovered client returns an error in "placeholder build"
// mode, we now assert the real request shape: Bearer auth + the
// cloud-platform OAuth scope, sent to the v1internal:streamGenerateContent
// endpoint. The test stands up an httptest server that captures
// the headers and verifies them.
func TestAICodeClient_GenerateContent_BearerScope(t *testing.T) {
	var sawAuth, sawScope, sawPath, sawMethod, sawUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		sawScope = r.Header.Get("X-Goog-Auth-Scope")
		sawPath = r.URL.Path
		sawMethod = r.Method
		sawUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"hi from test","done":true}`))
	}))
	defer srv.Close()

	c := NewAICodeClient(srv.URL)
	out, err := c.GenerateContent(context.Background(), "gemini-1.5-pro", "hello")
	if err != nil {
		t.Fatalf("GenerateContent: %v (transport captured auth=%q scope=%q)", err, sawAuth, sawScope)
	}
	// Real-value assertions: Bearer + cloud-platform + /v1internal: path.
	if sawMethod != http.MethodPost {
		t.Errorf("method: got %q, want POST", sawMethod)
	}
	if !strings.HasPrefix(sawPath, "/v1internal:") {
		t.Errorf("path: got %q, want /v1internal:* prefix", sawPath)
	}
	if !strings.HasPrefix(sawAuth, "Bearer ") {
		t.Errorf("Authorization: got %q, want Bearer <token>", sawAuth)
	}
	if sawScope != "https://www.googleapis.com/auth/cloud-platform" {
		t.Errorf("X-Goog-Auth-Scope: got %q, want cloud-platform", sawScope)
	}
	if sawUA == "" {
		t.Errorf("User-Agent is empty (recovered client should set it)")
	}
	if !strings.Contains(out, "hi from test") {
		t.Errorf("output: got %q, want chunk containing 'hi from test'", out)
	}
}
