//go:build darwin

package auth

import (
	"encoding/json"
	"errors"
	"flag"
	"os"

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

// readKeychainSecret reads the raw secret out of the macOS Keychain.
//
// The read is the platform's half; the answer it gives — a missing
// entry, a keyring that will not open — is classified by the shared
// layer in keychain.go, so both platforms answer the same way.
func readKeychainSecret() (string, error) {
	return keyring.Get(keychainService, keychainAccount)
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
	err = keyring.Set(keychainService, keychainAccount, string(rawBytes))
	if err != nil && len(rawBytes) > 1024 {
		// AGY 1.5.4: Gemini Enterprise tokens often contain multi-KB id_token claims
		// that exceed the OS keychain attribute limit. Strip auxiliary IDToken
		// while preserving core AccessToken, RefreshToken, and identity.
		compact := *tok
		compact.IDToken = ""
		if compactBytes, marshalErr := json.Marshal(&compact); marshalErr == nil {
			err = keyring.Set(keychainService, keychainAccount, string(compactBytes))
		}
	}
	return err
}
