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
	// Mock 1.3.5 真 binary path 写 token
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	legacyDir := filepath.Join(home, ".config", "antigravity")
	if err := os.MkdirAll(legacyDir, 0700); err != nil {
		t.Fatal(err)
	}
	legacyTok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.from-legacy-path",
			TokenType:    "Bearer",
			RefreshToken: "1//fake-refresh",
			Expiry:       time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano),
		},
		AuthMethod: "consumer",
		IDToken:    "eyJ.fake.idtoken",
	}
	if err := SaveAntigravityTokenTo(filepath.Join(legacyDir, "oauth-token"), legacyTok); err != nil {
		t.Fatal(err)
	}

	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified: %v", err)
	}
	if tok.Token.AccessToken != "ya29.from-legacy-path" {
		t.Errorf("expected legacy token, got %q", tok.Token.AccessToken)
	}
}

func TestLoadTokenUnified_PriorityOrder(t *testing.T) {
	// 多个 path 都有 token, 应该走 priority 1 (v1.3.5 真 binary path)
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.Unsetenv("JETSKI_OAUTH_TOKEN")

	// priority 2: v3 残留
	v3Dir := filepath.Join(home, ".config", "antigravity")
	os.MkdirAll(v3Dir, 0700)
	SaveAntigravityTokenTo(filepath.Join(v3Dir, "antigravity-auth.json"), &AntigravityToken{
		Token: AntigravityAccessToken{AccessToken: "ya29.v3"},
	})

	// priority 1: v1.3.5 真 binary
	SaveAntigravityTokenTo(filepath.Join(v3Dir, "oauth-token"), &AntigravityToken{
		Token: AntigravityAccessToken{AccessToken: "ya29.legacy-priority"},
	})

	// priority 3: v4 binary
	v4Dir := filepath.Join(home, ".gemini", "antigravity-cli")
	os.MkdirAll(v4Dir, 0700)
	SaveAntigravityTokenTo(filepath.Join(v4Dir, "antigravity-oauth-token"), &AntigravityToken{
		Token: AntigravityAccessToken{AccessToken: "ya29.v4"},
	})

	tok, err := LoadTokenUnified()
	if err != nil {
		t.Fatal(err)
	}
	if tok.Token.AccessToken != "ya29.legacy-priority" {
		t.Errorf("expected priority 1 (legacy), got %q", tok.Token.AccessToken)
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
	legacyDir := filepath.Join(home, ".config", "antigravity")
	os.MkdirAll(legacyDir, 0700)
	SaveAntigravityTokenTo(filepath.Join(legacyDir, "oauth-token"), &AntigravityToken{
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
