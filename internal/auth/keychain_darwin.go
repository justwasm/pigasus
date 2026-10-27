//go:build darwin

package auth

import (
	"encoding/json"
	"errors"
	"flag"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// The reference binary keeps the Antigravity OAuth token in the macOS
// Keychain through github.com/zalando/go-keyring, under the service
// "gemini" and the account "antigravity". The clone uses the same
// library with the same service and account, so the two binaries read
// and write each other's entry.
const (
	keychainService = "gemini"
	keychainAccount = "antigravity"
)

// LoadKeychainToken retrieves the Antigravity OAuth token from macOS Keychain.
//
// go-keyring owns the storage format: its macOS backend encodes the
// secret as "go-keyring-base64:" + base64(payload) and decodes that
// prefix on the way back, so what crosses this function's boundary is
// the plain JSON blob. The reference stores the same bytes — it uses
// the same library — so an entry written by either binary is readable
// by the other.
//
// An entry written by something else (a raw JSON blob, no prefix) is
// still understood: go-keyring hands such a value back unchanged.
func LoadKeychainToken() (*AntigravityToken, error) {
	val, err := keyring.Get(keychainService, keychainAccount)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil, ErrNoToken
		}
		return nil, err
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return nil, ErrNoToken
	}
	var tok AntigravityToken
	if err := json.Unmarshal([]byte(val), &tok); err != nil {
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

// ClearKeychainToken deletes the Antigravity OAuth token from the macOS
// Keychain. Returns nil when no entry exists (the "fresh machine"
// case), or any error from the keychain that is not "item not found".
// Called by ClearTokenUnified when a stale credential must be wiped out
// (AGY 1.2.5).
func ClearKeychainToken() error {
	if ClearKeychainTokenFn != nil {
		return ClearKeychainTokenFn()
	}
	if os.Getenv("OPENAGY_DISABLE_KEYCHAIN") != "" || flag.Lookup("test.v") != nil {
		return nil
	}
	// A missing entry is the desired steady state for a fresh machine
	// and not an error from the caller's perspective.
	err := keyring.Delete(keychainService, keychainAccount)
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
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
//
// go-keyring encodes the secret itself ("go-keyring-base64:" + base64 of the
// payload), which is exactly the encoding the clone used to write by hand,
// and it hands the secret to `security` on stdin — the clone's own call put
// the token on the command line, where every process on the machine could
// read it out of the process table.
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
	return keyring.Set(keychainService, keychainAccount, string(rawBytes))
}
