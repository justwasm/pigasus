package core_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/features/plugins/core"
)

func TestReadPluginAuth_Missing(t *testing.T) {
	tmp := t.TempDir()
	doc, err := core.ReadPluginAuth(tmp)
	if err != nil {
		t.Fatalf("unexpected error for missing auth.json: %v", err)
	}
	if doc != nil {
		t.Fatalf("expected nil doc for missing auth.json, got %+v", doc)
	}
}

func TestReadPluginAuth_EmptyProviders(t *testing.T) {
	tmp := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(`{"providers": {}}`), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := core.ReadPluginAuth(tmp)
	if err == nil || !strings.Contains(err.Error(), "auth.json declares no providers") {
		t.Fatalf("expected 'auth.json declares no providers' error, got %v", err)
	}
}

func TestReadPluginAuth_InvalidType(t *testing.T) {
	tmp := t.TempDir()
	raw := `{"providers": {"foo": {"type": "basic"}}}`
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := core.ReadPluginAuth(tmp)
	if err == nil || !strings.Contains(err.Error(), "unsupported provider type") {
		t.Fatalf("expected unsupported provider type error, got %v", err)
	}
}

func TestReadPluginAuth_OAuth2Validation(t *testing.T) {
	tmp := t.TempDir()
	// Missing oauth2 section
	raw1 := `{"providers": {"oauth": {"type": "oauth2"}}}`
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(raw1), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := core.ReadPluginAuth(tmp)
	if err == nil || !strings.Contains(err.Error(), "requires an \"oauth2\" section") {
		t.Fatalf("expected 'requires an \"oauth2\" section', got %v", err)
	}

	// Missing clientId
	raw2 := `{"providers": {"oauth": {"type": "oauth2", "oauth2": {"authUrl": "https://example.com"}}}}`
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(raw2), 0644); err != nil {
		t.Fatal(err)
	}
	_, err = core.ReadPluginAuth(tmp)
	if err == nil || !strings.Contains(err.Error(), "oauth2.clientId is required") {
		t.Fatalf("expected 'oauth2.clientId is required', got %v", err)
	}
}

func TestReadPluginAuth_ApiKeyOptional(t *testing.T) {
	tmp := t.TempDir()
	raw := `{
		"providers": {
			"gemini_api": {
				"type": "api_key",
				"optional": true,
				"env": {
					"GEMINI_API_KEY": "API_KEY"
				},
				"domains": ["generativelanguage.googleapis.com"]
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	doc, err := core.ReadPluginAuth(tmp)
	if err != nil {
		t.Fatalf("failed to read valid auth.json: %v", err)
	}
	provider, ok := doc.Providers["gemini_api"]
	if !ok {
		t.Fatalf("expected provider gemini_api")
	}
	if !provider.Optional {
		t.Fatalf("expected optional=true")
	}
	if provider.Env["GEMINI_API_KEY"] != "API_KEY" {
		t.Fatalf("expected env mapping GEMINI_API_KEY->API_KEY, got %+v", provider.Env)
	}
}

func TestResolvePluginAuthEnv_OptionalBehavior(t *testing.T) {
	providerRequired := &core.PluginAuthProvider{
		Type:     "api_key",
		Optional: false,
		Env: map[string]string{
			"SERVICE_KEY": "STORED_SECRET",
		},
	}
	providerOptional := &core.PluginAuthProvider{
		Type:     "api_key",
		Optional: true,
		Env: map[string]string{
			"SERVICE_KEY": "STORED_SECRET",
		},
	}

	// Case 1: Secret missing, provider is required -> should return auth error.
	_, err := core.ResolvePluginAuthEnv("my-plugin", "fetch-data", "my-provider", providerRequired, func(string) (string, bool) {
		return "", false
	})
	if err == nil {
		t.Fatal("expected error for required missing secret")
	}
	wantMsg := "fetch-data needs authentication (my-provider). Ask the user to sign in to my-plugin in the Plugins Manager, then try again"
	if err.Error() != wantMsg {
		t.Fatalf("error message = %q, want %q", err.Error(), wantMsg)
	}

	// Case 2: Secret missing, provider has "optional": true (agy 1.4.12) -> should NOT fail, variable left unset.
	env, err := core.ResolvePluginAuthEnv("my-plugin", "fetch-data", "my-provider", providerOptional, func(string) (string, bool) {
		return "", false
	})
	if err != nil {
		t.Fatalf("unexpected error for optional missing secret: %v", err)
	}
	if _, ok := env["SERVICE_KEY"]; ok {
		t.Fatalf("expected SERVICE_KEY to be unset, got %+v", env)
	}

	// Case 3: Secret present -> should resolve into env map for both.
	envPresent, err := core.ResolvePluginAuthEnv("my-plugin", "fetch-data", "my-provider", providerOptional, func(k string) (string, bool) {
		if k == "STORED_SECRET" {
			return "secret-token-123", true
		}
		return "", false
	})
	if err != nil {
		t.Fatalf("unexpected error when secret present: %v", err)
	}
	if envPresent["SERVICE_KEY"] != "secret-token-123" {
		t.Fatalf("expected secret-token-123, got %q", envPresent["SERVICE_KEY"])
	}
}

func TestListPluginAuthProviders(t *testing.T) {
	tmp := t.TempDir()
	raw := `{
		"providers": {
			"p2": {
				"type": "api_key",
				"optional": true
			},
			"p1": {
				"type": "api_key",
				"optional": false
			}
		}
	}`
	if err := os.WriteFile(filepath.Join(tmp, "auth.json"), []byte(raw), 0644); err != nil {
		t.Fatal(err)
	}
	list, err := core.ListPluginAuthProviders(tmp, "test-plugin")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(list))
	}
	// Sorted by name
	if list[0].Provider != "p1" || list[0].Optional != false {
		t.Errorf("expected p1 optional=false, got %+v", list[0])
	}
	if list[1].Provider != "p2" || list[1].Optional != true {
		t.Errorf("expected p2 optional=true, got %+v", list[1])
	}
}
