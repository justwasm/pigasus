package core_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	core "github.com/justwasm/pigasus/internal/features/accounts/core"
)

func TestAccessToken_NoStoredCredentials(t *testing.T) {
	acc := core.Account{
		Email: "empty@example.com",
	}
	_, err := core.AccessToken(context.Background(), "empty@example.com", acc)
	if err == nil {
		t.Fatalf("expected error for account without stored credentials")
	}
}

func TestRegisterCurrent_EmptyEmail(t *testing.T) {
	// Should not panic or error on empty email
	core.RegisterCurrent("")
	core.RegisterCurrent("   ")
}

func TestFetchQuota_UnknownAccount(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "core.json"))

	_, err := core.FetchQuota(context.Background(), "missing@example.com")
	if err == nil {
		t.Fatalf("expected error for unknown account")
	}
}

func TestAccessToken_QuietRefresh(t *testing.T) {
	acc := core.Account{
		Email: "inactive@example.com",
		Token: &auth.AntigravityToken{
			Token: auth.AntigravityAccessToken{
				AccessToken:  "valid-token",
				RefreshToken: "refresh-token",
				Expiry:       time.Now().Add(1 * time.Hour).Format(time.RFC3339),
			},
		},
	}
	tok, err := core.AccessToken(context.Background(), "inactive@example.com", acc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if tok == nil || tok.Token.AccessToken != "valid-token" {
		t.Fatalf("expected valid token, got %v", tok)
	}
}
