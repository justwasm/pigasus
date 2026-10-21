//go:build darwin

package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"os"
	"os/exec"
	"strings"
	"time"
)

// LoadKeychainToken retrieves the Antigravity OAuth token from macOS Keychain.
// Real agy binary uses github.com/zalando/go-keyring with service "gemini" and
// account "antigravity", storing a base64-encoded JSON blob prefixed with "go-keyring-base64:".
func LoadKeychainToken() (*AntigravityToken, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/security", "find-generic-password", "-s", "gemini", "-a", "antigravity", "-w")
	out, err := cmd.Output()
	if err != nil {
		return nil, ErrNoToken
	}

	val := strings.TrimSpace(string(out))
	if !strings.HasPrefix(val, "go-keyring-base64:") {
		// Try raw JSON if not base64-prefixed
		var rawTok AntigravityToken
		if err := json.Unmarshal([]byte(val), &rawTok); err == nil && rawTok.Token.AccessToken != "" {
			return &rawTok, nil
		}
		return nil, ErrNoToken
	}

	payload := strings.TrimPrefix(val, "go-keyring-base64:")
	rawBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, err
	}

	var tok AntigravityToken
	if err := json.Unmarshal(rawBytes, &tok); err != nil {
		return nil, err
	}
	if tok.Token.AccessToken == "" {
		return nil, ErrNoToken
	}
	return &tok, nil
}

// HasKeychainToken checks if the macOS Keychain contains the antigravity token.
func HasKeychainToken() bool {
	tok, err := LoadKeychainToken()
	return err == nil && tok != nil && tok.Token.AccessToken != ""
}

// ClearKeychainToken deletes the antigravity token from the macOS
// Keychain. Returns nil when no entry exists (the "fresh machine"
// case), or any error from /usr/bin/security that is not "item not
// found". Called by ClearTokenUnified when a stale credential must be
// wiped out (AGY 1.2.5).
func ClearKeychainToken() error {
	if ClearKeychainTokenFn != nil {
		return ClearKeychainTokenFn()
	}
	if os.Getenv("OPENAGY_DISABLE_KEYCHAIN") != "" || flag.Lookup("test.v") != nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	// `security delete-generic-password` returns exit code 44 when the
	// item is missing, which is the desired steady state for a fresh
	// machine and not an error from the caller's perspective.
	cmd := exec.CommandContext(ctx, "/usr/bin/security",
		"delete-generic-password", "-s", "gemini", "-a", "antigravity")
	if err := cmd.Run(); err != nil {
		// exit 44 == "The specified item could not be found in the keychain."
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 44 {
			return nil
		}
		return err
	}
	return nil
}

// ClearKeychainTokenFn is a test seam allowing tests to mock keychain clearance.
var ClearKeychainTokenFn func() error

// SaveKeychainTokenFn is a test seam allowing tests to mock keychain writes.
var SaveKeychainTokenFn func(tok *AntigravityToken) error

// SaveKeychainToken persists the Antigravity OAuth token to the macOS Keychain
// using the same service ("gemini") and account ("antigravity") as the reference binary.
func SaveKeychainToken(tok *AntigravityToken) error {
	if SaveKeychainTokenFn != nil {
		return SaveKeychainTokenFn(tok)
	}
	if tok == nil {
		return nil
	}
	if os.Getenv("OPENAGY_DISABLE_KEYCHAIN") != "" || flag.Lookup("test.v") != nil {
		return nil
	}
	rawBytes, err := json.Marshal(tok)
	if err != nil {
		return err
	}
	payload := "go-keyring-base64:" + base64.StdEncoding.EncodeToString(rawBytes)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "/usr/bin/security",
		"add-generic-password", "-U", "-s", "gemini", "-a", "antigravity", "-w", payload)
	return cmd.Run()
}
