package backendselect

// credential_presence_test.go — HasOAuthToken asks the cascade once.
//
// The dispatch asks this question on the composer's turn path, and on macOS
// the answer requires a keychain read — a `security` spawn, ~11-15 ms here.
// The function used to probe the keychain directly (auth.HasKeychainToken) and
// then call auth.LoadTokenUnified, which reads the keychain again on its way
// down the cascade. A machine whose credential lives in the account registry
// rather than the keychain therefore paid for two keychain reads to answer one
// question.
//
// The test counts the reads, because that is the property that regresses
// silently: the answer is identical, and only the number of syscalls changes.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// credentialTestHome points every credential location at a temp directory and
// makes the keychain answer with `keychainToken` (nil means "no entry"),
// returning a pointer to the keychain read count.
func credentialTestHome(t *testing.T, keychainToken *auth.AntigravityToken) *int {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, "accounts.json"))
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")

	reads := new(int)
	orig := auth.LoadKeychainTokenFn
	auth.LoadKeychainTokenFn = func() (*auth.AntigravityToken, error) {
		*reads++
		if keychainToken == nil {
			return nil, auth.ErrNoToken
		}
		return keychainToken, nil
	}
	t.Cleanup(func() { auth.LoadKeychainTokenFn = orig })
	return reads
}

// writeRegistryCredential registers one account in the account registry, the
// cascade's fourth source — the one a machine without a keychain entry uses.
func writeRegistryCredential(t *testing.T, email, access string) {
	t.Helper()
	p := os.Getenv("OPENAGY_ACCOUNTS_CONFIG")
	doc := map[string]any{
		"active": email,
		"accounts": []any{map[string]any{
			"email": email,
			"token": map[string]any{
				"token": map[string]any{
					"access_token":  access,
					"refresh_token": "rt-" + access,
					"token_type":    "Bearer",
				},
				"auth_method": "consumer",
			},
		}},
	}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal registry: %v", err)
	}
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
}

// TestHasOAuthTokenReadsTheKeychainOnceForARegistryCredential is the case the
// old implementation double-read: the keychain has no entry, the credential is
// in the registry, so the probe read the keychain and the load read it again.
func TestHasOAuthTokenReadsTheKeychainOnceForARegistryCredential(t *testing.T) {
	reads := credentialTestHome(t, nil)
	writeRegistryCredential(t, "a@example.com", "registry-token")

	if !HasOAuthToken() {
		t.Fatal("a credential in the account registry was not found")
	}
	if *reads != 1 {
		t.Errorf("the keychain was read %d times, want 1: the presence check and the load share one read", *reads)
	}
}

// TestHasOAuthTokenDoesNotTouchTheKeychainWhenTheTokenIsOnDisk keeps the cheap
// answer cheap: the canonical token file is checked first, so a machine that
// has one never spawns `security` at all.
func TestHasOAuthTokenDoesNotTouchTheKeychainWhenTheTokenIsOnDisk(t *testing.T) {
	reads := credentialTestHome(t, nil)

	tok := &auth.AntigravityToken{
		Token:      auth.AntigravityAccessToken{AccessToken: "disk-token"},
		AuthMethod: "consumer",
	}
	if err := auth.SaveAntigravityTokenTo(auth.TokenFilePath(), tok); err != nil {
		t.Fatalf("SaveAntigravityTokenTo: %v", err)
	}
	if !HasOAuthToken() {
		t.Fatal("a token on disk was not found")
	}
	if *reads != 0 {
		t.Errorf("the keychain was read %d times for a token that is on disk", *reads)
	}
}

// TestHasOAuthTokenFindsTheKeychainEntry keeps the cascade intact: the keychain
// is the reference's own source and is still consulted.
func TestHasOAuthTokenFindsTheKeychainEntry(t *testing.T) {
	reads := credentialTestHome(t, &auth.AntigravityToken{
		Token: auth.AntigravityAccessToken{
			AccessToken:  "keychain-token",
			RefreshToken: "keychain-refresh",
		},
		AuthMethod: "consumer",
	})

	if !HasOAuthToken() {
		t.Fatal("a token in the keychain was not found")
	}
	if *reads != 1 {
		t.Errorf("the keychain was read %d times, want 1", *reads)
	}
}

// TestHasOAuthTokenReportsNoCredential is the other side: nothing anywhere
// means the caller shows the login screen.
func TestHasOAuthTokenReportsNoCredential(t *testing.T) {
	credentialTestHome(t, nil)

	if HasOAuthToken() {
		t.Error("HasOAuthToken reports a credential on an empty machine")
	}
}
