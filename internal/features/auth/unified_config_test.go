// unified_config_test.go — verify cascade load + multi-path save + refresh.
//
// Cycle 44 (user live run 反馈): recovered binary 跟 v1.3.5 binary 没共享
// config, 而且不自动 refresh. 本 test 覆盖新 unified config logic.
package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fakeTokenServer mocks oauth2.googleapis.com/token
func fakeTokenServer(t *testing.T, calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		_ = r.ParseForm()
		if r.FormValue("grant_type") != "refresh_token" {
			http.Error(w, "expected refresh_token grant", 400)
			return
		}
		if r.FormValue("client_id") == "" {
			http.Error(w, "missing client_id", 400)
			return
		}
		if r.FormValue("client_secret") == "" {
			http.Error(w, "missing client_secret (cycle 42 fix)", 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "ya29.fake-refreshed",
			"refresh_token": "1//fake-refresh-rotated",
			"expires_in":    3600,
			"scope":         "cloud-platform cclog experimentsandconfigs",
			"token_type":    "Bearer",
		})
	}))
}

func TestLoadTokenUnified_EnvVar(t *testing.T) {
	// 1. JETSKI_OAUTH_TOKEN env var 优先
	t.Setenv("JETSKI_OAUTH_TOKEN", "ya29.env-token-direct")
	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified: %v", err)
	}
	if tok.Token.AccessToken != "ya29.env-token-direct" {
		t.Errorf("expected env token, got %q", tok.Token.AccessToken)
	}
}

func TestLoadTokenUnified_CascadeFromFile(t *testing.T) {
	// Modern official binary path: ~/.gemini/antigravity-cli/antigravity-oauth-token
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	modernDir := filepath.Join(home, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(modernDir, 0700); err != nil {
		t.Fatal(err)
	}
	modernTok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.from-modern-path",
			TokenType:    "Bearer",
			RefreshToken: "1//fake-refresh",
			Expiry:       time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano),
		},
		AuthMethod: "consumer",
		IDToken:    "eyJ.fake.idtoken",
	}
	if err := SaveAntigravityTokenTo(filepath.Join(modernDir, "antigravity-oauth-token"), modernTok); err != nil {
		t.Fatal(err)
	}

	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified: %v", err)
	}
	if tok.Token.AccessToken != "ya29.from-modern-path" {
		t.Errorf("expected modern token, got %q", tok.Token.AccessToken)
	}
}

func TestLoadTokenUnified_PriorityOrder(t *testing.T) {
	// Priority order: Env > Modern CLI path > accounts.json
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	LoadKeychainTokenFn = func() (*AntigravityToken, error) {
		return nil, ErrNoToken
	}
	defer func() { LoadKeychainTokenFn = nil }()

	// 1. Setup accounts.json (lowest priority)
	openagyDir := filepath.Join(home, ".openagy")
	os.MkdirAll(openagyDir, 0700)
	os.WriteFile(filepath.Join(openagyDir, "accounts.json"), []byte(`{
		"version": 1,
		"active": "acc@example.com",
		"accounts": [
			{"email": "acc@example.com", "token": {"token": {"access_token": "ya29.from-accounts-json"}}}
		]
	}`), 0600)

	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token.AccessToken != "ya29.from-accounts-json" {
		t.Errorf("expected accounts.json token, got %q", tok.Token.AccessToken)
	}

	// 2. Setup modern CLI on-disk token (beats accounts.json)
	modernDir := filepath.Join(home, ".gemini", "antigravity-cli")
	os.MkdirAll(modernDir, 0700)
	SaveAntigravityTokenTo(filepath.Join(modernDir, "antigravity-oauth-token"), &AntigravityToken{
		Token: AntigravityAccessToken{AccessToken: "ya29.from-modern-cli"},
	})

	tok, err = LoadTokenUnified()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token.AccessToken != "ya29.from-modern-cli" {
		t.Errorf("expected modern CLI token, got %q", tok.Token.AccessToken)
	}

	// 3. Set JETSKI_OAUTH_TOKEN (beats on-disk token)
	t.Setenv("JETSKI_OAUTH_TOKEN", "ya29.from-env")
	tok, err = LoadTokenUnified()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token.AccessToken != "ya29.from-env" {
		t.Errorf("expected env token, got %q", tok.Token.AccessToken)
	}
}

func TestSaveTokenUnified_WritesAllPaths(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken: "ya29.unified",
			TokenType:   "Bearer",
		},
		AuthMethod: "consumer",
	}
	if err := SaveTokenUnified(tok); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}

	// 3 个 path 都应该写
	for _, p := range AllTokenPaths() {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("expected token at %s: %v", p, err)
		}
	}
}

// TestSaveTokenUnified_WritesTheReferenceEmailField covers the field the
// reference reads the account from. A credential the clone writes has to
// name the account, or the installed `agy` shows the previous one.
func TestSaveTokenUnified_WritesTheReferenceEmailField(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	t.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	payload := "eyJlbWFpbCI6InVzZXJAZXhhbXBsZS5jb20iLCJzdWIiOiIxMjM0NSJ9"
	tok := &AntigravityToken{
		Token:      AntigravityAccessToken{AccessToken: "ya29.unified", TokenType: "Bearer"},
		AuthMethod: "consumer",
		IDToken:    header + "." + payload + ".sig",
	}
	if err := SaveTokenUnified(tok); err != nil {
		t.Fatalf("SaveTokenUnified: %v", err)
	}
	if tok.IDTokenEmail != "user@example.com" {
		t.Fatalf("id_token_email = %q, want the token's own email", tok.IDTokenEmail)
	}
	raw, err := os.ReadFile(AllTokenPaths()[0])
	if err != nil {
		t.Fatalf("read the shared token: %v", err)
	}
	if !strings.Contains(string(raw), `"id_token_email": "user@example.com"`) {
		t.Fatalf("the shared credential does not name the account: %s", raw)
	}
	// And it reads back as the same account.
	back, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified: %v", err)
	}
	if em := back.UserEmail(); em != "user@example.com" {
		t.Fatalf("the shared credential reads back %q, want user@example.com", em)
	}
}

func TestRefreshIfNeeded_ClientSecret(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))

	// cycle 42 fix: RefreshTokenRealWithSecret 必须传 client_secret
	var calls int32
	srv := fakeTokenServer(t, &calls)
	defer srv.Close()

	// Override TokenURL via test seam (replace TokenURL constant for this test)
	origTokenURL := TokenURL
	TokenURL = srv.URL
	defer func() { TokenURL = origTokenURL }()

	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.stale",
			RefreshToken: "1//fake-refresh",
			Expiry:       time.Now().Add(1 * time.Minute).Format(time.RFC3339Nano), // < 5min → refresh
		},
	}
	updated, refreshed, err := tok.RefreshIfNeeded(context.Background())
	if err != nil {
		t.Fatalf("RefreshIfNeeded: %v", err)
	}
	if !refreshed {
		t.Error("expected refreshed=true")
	}
	if updated.Token.AccessToken != "ya29.fake-refreshed" {
		t.Errorf("expected refreshed token, got %q", updated.Token.AccessToken)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 refresh call, got %d", calls)
	}
}

func TestRefreshIfNeeded_NotExpired(t *testing.T) {
	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken: "ya29.still-valid",
			Expiry:      time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano),
		},
	}
	_, refreshed, err := tok.RefreshIfNeeded(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if refreshed {
		t.Error("expected refreshed=false (token not expired)")
	}
}

func TestUnifiedTokenCache_AutoRefresh(t *testing.T) {
	var calls int32
	srv := fakeTokenServer(t, &calls)
	defer srv.Close()

	origTokenURL := TokenURL
	TokenURL = srv.URL
	defer func() { TokenURL = origTokenURL }()

	// setup 过期 token
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("JETSKI_OAUTH_TOKEN")
	modernDir := filepath.Join(home, ".gemini", "antigravity-cli")
	os.MkdirAll(modernDir, 0700)
	SaveAntigravityTokenTo(filepath.Join(modernDir, "antigravity-oauth-token"), &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.expired",
			RefreshToken: "1//fake",
			Expiry:       time.Now().Add(1 * time.Minute).Format(time.RFC3339Nano),
		},
	})

	cache := NewUnifiedTokenCache()
	tok, err := cache.Get(context.Background())
	if err != nil {
		t.Fatalf("cache.Get: %v", err)
	}
	if tok.Token.AccessToken != "ya29.fake-refreshed" {
		t.Errorf("expected auto-refreshed token, got %q", tok.Token.AccessToken)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 refresh call, got %d", calls)
	}
}

func TestLoadTokenUnified_AccountsJSONFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	// No keychain, no agy config files
	LoadKeychainTokenFn = func() (*AntigravityToken, error) {
		return nil, ErrNoToken
	}
	defer func() { LoadKeychainTokenFn = nil }()

	// Write accounts.json
	openagyDir := filepath.Join(home, ".openagy")
	os.MkdirAll(openagyDir, 0700)
	accountsPath := filepath.Join(openagyDir, "accounts.json")
	accountsData := `{
		"version": 1,
		"active": "standalone@example.com",
		"accounts": [
			{
				"email": "standalone@example.com",
				"token": {
					"token": {
						"access_token": "ya29.standalone-account",
						"refresh_token": "1//refresh-standalone",
						"token_type": "Bearer"
					},
					"auth_method": "consumer",
					"id_token_email": "standalone@example.com"
				}
			}
		]
	}`
	if err := os.WriteFile(accountsPath, []byte(accountsData), 0600); err != nil {
		t.Fatal(err)
	}

	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("expected token from accounts.json, got error: %v", err)
	}
	if tok.Token.AccessToken != "ya29.standalone-account" {
		t.Errorf("expected ya29.standalone-account, got %q", tok.Token.AccessToken)
	}
	if !TokenFileExists() {
		t.Errorf("expected TokenFileExists=true with accounts.json present")
	}
}
