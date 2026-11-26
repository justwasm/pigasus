//go:build darwin

package auth

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestDarwinSSHKeychain_UnlockedReusesKeychain(t *testing.T) {
	t.Setenv("SSH_CONNECTION", "10.0.0.1 5000 10.0.0.2 22")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")

	origProbe := probeDarwinKeychainUnlockedFn
	origGet := keyringGetFn
	origLoad := LoadKeychainTokenFn
	t.Cleanup(func() {
		probeDarwinKeychainUnlockedFn = origProbe
		keyringGetFn = origGet
		LoadKeychainTokenFn = origLoad
	})

	LoadKeychainTokenFn = nil
	probeDarwinKeychainUnlockedFn = func() bool { return true }
	keyringGetFn = func(service, account string) (string, error) {
		return `{"token":{"access_token":"ssh-unlocked-keychain-token"}}`, nil
	}

	tok, err := LoadKeychainToken()
	if err != nil {
		t.Fatalf("LoadKeychainToken failed when SSH keychain unlocked: %v", err)
	}
	if tok.Token.AccessToken != "ssh-unlocked-keychain-token" {
		t.Fatalf("AccessToken = %q, want ssh-unlocked-keychain-token", tok.Token.AccessToken)
	}
}

func TestDarwinSSHKeychain_LockedFallsBackToFileStorage(t *testing.T) {
	t.Setenv("SSH_TTY", "/dev/ttys001")
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("OPENAGY_CONFIG_HOME", filepath.Join(home, ".openagy"))
	t.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(home, ".openagy", "accounts.json"))

	origProbe := probeDarwinKeychainUnlockedFn
	origGet := keyringGetFn
	origLoad := LoadKeychainTokenFn
	t.Cleanup(func() {
		probeDarwinKeychainUnlockedFn = origProbe
		keyringGetFn = origGet
		LoadKeychainTokenFn = origLoad
	})

	LoadKeychainTokenFn = nil
	probeDarwinKeychainUnlockedFn = func() bool { return false }
	keyringGetFn = func(service, account string) (string, error) {
		t.Fatal("keyring.Get should not be called when SSH keychain probe reports locked")
		return "", nil
	}

	_, err := LoadKeychainToken()
	if !errors.Is(err, errKeyringLocked) {
		t.Fatalf("expected errKeyringLocked when SSH keychain locked, got %v", err)
	}

	// Save a file-based token and verify LoadTokenUnified falls back cleanly.
	fileTok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken: "ssh-file-fallback-token",
			TokenType:   "Bearer",
		},
		AuthMethod: "consumer",
	}
	if err := SaveAntigravityTokenTo(AllTokenPaths()[0], fileTok); err != nil {
		t.Fatalf("SaveAntigravityTokenTo: %v", err)
	}

	loaded, err := LoadTokenUnified()
	if err != nil {
		t.Fatalf("LoadTokenUnified failed to fall back to file storage: %v", err)
	}
	if loaded.Token.AccessToken != "ssh-file-fallback-token" {
		t.Fatalf("AccessToken = %q, want ssh-file-fallback-token", loaded.Token.AccessToken)
	}
}
