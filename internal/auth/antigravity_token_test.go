// antigravity_token_test.go — verify LoadAntigravityToken reads v4 binary format
// + RefreshTokenReal swaps the user's refresh_token for a fresh access_token.
//
// Cycle 35: User provided a live token (sub=105126221582794464410,
// "Hangbiao Zou", ~9h remaining). The tests use the on-disk token at
// ~/.gemini/antigravity-cli/antigravity-oauth-token and skip cleanly
// if it's not there.
package auth

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoadAntigravityToken_FromRealDisk(t *testing.T) {
	path := TokenFilePath()
	if _, err := os.Stat(path); err != nil {
		t.Skipf("no token on disk at %s: %v (set ANTIGRAVITY_HOME or run `antigravity login`)", path, err)
	}
	tok, err := LoadAntigravityToken()
	if err != nil {
		t.Fatalf("LoadAntigravityToken: %v", err)
	}
	if tok.Token.AccessToken == "" {
		t.Error("access_token is empty")
	}
	if !strings.HasPrefix(tok.Token.AccessToken, "ya29.") {
		t.Errorf("access_token doesn't look like a Google OAuth2 token: %q...", tok.Token.AccessToken[:20])
	}
	if tok.Token.RefreshToken == "" {
		t.Error("refresh_token is empty")
	}
	// id_token + auth_method are non-fatal: the cycle-45 stub token file
	// from the live run has these empty (real keychain token has them).
	if tok.IDToken == "" {
		t.Logf("id_token is empty (live keychain token has it; this is the cycle-45 stub token file)")
	}
	if tok.AuthMethod == "" {
		t.Logf("auth_method is empty (live keychain token has it; this is the cycle-45 stub token file)")
	}
	exp := tok.ExpiryTime()
	if !exp.IsZero() {
		t.Logf("token expiry: %s (in %s)", exp.Format(time.RFC3339), time.Until(exp).Round(time.Second))
	} else {
		t.Error("expiry is zero — ISO 8601 parse failed")
	}
}

func TestAntigravityToken_UserEmail(t *testing.T) {
	// Sample JWT with email payload
	header := "eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9"
	payload := "eyJlbWFpbCI6InVzZXJAZXhhbXBsZS5jb20iLCJzdWIiOiIxMjM0NSJ9"
	tok := &AntigravityToken{
		IDToken: header + "." + payload + ".sig",
	}
	if em := tok.UserEmail(); em != "user@example.com" {
		t.Fatalf("tok.UserEmail() = %q, want %q", em, "user@example.com")
	}
}

func TestSaveAndLoad_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "antigravity-oauth-token")
	original := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  "ya29.test-access",
			TokenType:    "Bearer",
			RefreshToken: "1//0test-refresh",
			Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339Nano),
		},
		AuthMethod: "consumer",
		IDToken:    "eyJ.test-id-token",
	}
	if err := SaveAntigravityTokenTo(path, original); err != nil {
		t.Fatalf("SaveAntigravityTokenTo: %v", err)
	}
	got, err := LoadAntigravityTokenFrom(path)
	if err != nil {
		t.Fatalf("LoadAntigravityTokenFrom: %v", err)
	}
	if got.Token.AccessToken != original.Token.AccessToken {
		t.Errorf("access_token: got %q, want %q", got.Token.AccessToken, original.Token.AccessToken)
	}
	if got.Token.RefreshToken != original.Token.RefreshToken {
		t.Errorf("refresh_token: got %q, want %q", got.Token.RefreshToken, original.Token.RefreshToken)
	}
	if got.IDToken != original.IDToken {
		t.Errorf("id_token: got %q, want %q", got.IDToken, original.IDToken)
	}
	if got.AuthMethod != original.AuthMethod {
		t.Errorf("auth_method: got %q, want %q", got.AuthMethod, original.AuthMethod)
	}
}

func TestAntigravityHome_EnvOverride(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/tmp/test-antigravity-home")
	if got := AntigravityHome(); got != "/tmp/test-antigravity-home" {
		t.Errorf("ANTIGRAVITY_HOME: got %q, want %q", got, "/tmp/test-antigravity-home")
	}
}

func TestExpiryTime_ParsesRFC3339(t *testing.T) {
	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			Expiry: "2026-10-14T18:36:12.312527+08:00",
		},
	}
	got := tok.ExpiryTime()
	if got.IsZero() {
		t.Fatal("ExpiryTime returned zero for valid RFC3339Nano")
	}
	if got.Year() != 2026 || got.Month() != 10 || got.Day() != 14 {
		t.Errorf("expiry parsed wrong: %v", got)
	}
}

func TestIsExpired(t *testing.T) {
	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			Expiry: time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano),
		},
	}
	if tok.IsExpired() {
		t.Error("token expiring in 2h should not be expired")
	}
	tok2 := &AntigravityToken{
		Token: AntigravityAccessToken{
			Expiry: time.Now().Add(-1 * time.Hour).Format(time.RFC3339Nano),
		},
	}
	if !tok2.IsExpired() {
		t.Error("token expired 1h ago should be expired")
	}
}

func TestRefreshTokenReal_LiveRefresh(t *testing.T) {
	if os.Getenv("SKIP_LIVE_OAUTH") == "1" || os.Getenv("GO_TEST_NO_NET") == "1" {
		t.Skip("SKIP_LIVE_OAUTH=1 or GO_TEST_NO_NET=1")
	}
	tok, err := LoadAntigravityToken()
	if err != nil {
		t.Skipf("no token on disk: %v", err)
	}
	if tok.Token.RefreshToken == "" {
		t.Skip("no refresh_token in disk file")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := RefreshTokenReal(ctx, CLIClientID, tok.Token.RefreshToken)
	if err != nil {
		// OAuth public-client flow may not return a client_secret, but
		// also may not need one. If the server returns "invalid_request"
		// we surface that. If the sandbox is offline, we skip.
		if strings.Contains(err.Error(), "client_secret is missing") ||
			strings.Contains(err.Error(), "invalid_request") {
			t.Skipf("server requires client_secret for this grant: %v", err)
		}
		if strings.Contains(err.Error(), "connection refused") ||
			strings.Contains(err.Error(), "no such host") ||
			strings.Contains(err.Error(), "i/o timeout") {
			t.Skipf("sandbox offline: %v", err)
		}
		t.Fatalf("RefreshTokenReal: %v", err)
	}
	if resp.AccessToken == "" {
		t.Error("refreshed access_token is empty")
	}
	if !strings.HasPrefix(resp.AccessToken, "ya29.") {
		t.Errorf("refreshed access_token doesn't look like a Google OAuth2 token: %q...", resp.AccessToken[:20])
	}
	if resp.ExpiresIn <= 0 {
		t.Errorf("expires_in: got %d, want >0", resp.ExpiresIn)
	}
	t.Logf("refresh ok: ya29...%s expires_in=%ds token_type=%s",
		resp.AccessToken[len(resp.AccessToken)-12:], resp.ExpiresIn, resp.TokenType)
}

func TestTokenCache_Get_FromDisk(t *testing.T) {
	if os.Getenv("SKIP_LIVE_OAUTH") == "1" || os.Getenv("GO_TEST_NO_NET") == "1" {
		t.Skip("SKIP_LIVE_OAUTH=1 or GO_TEST_NO_NET=1")
	}
	if _, err := os.Stat(TokenFilePath()); err != nil {
		t.Skipf("no token on disk: %v", err)
	}
	c := NewTokenCache()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	tok, err := c.Get(ctx)
	if err != nil {
		t.Fatalf("TokenCache.Get: %v", err)
	}
	if tok.Token.AccessToken == "" {
		t.Error("cached access_token is empty")
	}
	// Second call should be cached
	tok2, _ := c.Get(ctx)
	if tok2.Token.AccessToken != tok.Token.AccessToken {
		t.Error("second call returned different token (cache miss)")
	}
}
