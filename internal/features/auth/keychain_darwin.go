//go:build darwin

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"os"
	"os/exec"
	"time"

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

var (
	probeDarwinKeychainUnlockedFn func() bool
	keyringGetFn                  = keyring.Get
)

func isSSHSession() bool {
	return os.Getenv("SSH_CONNECTION") != "" || os.Getenv("SSH_TTY") != "" || os.Getenv("SSH_CLIENT") != ""
}

// probeDarwinKeychainUnlocked checks whether the macOS login keychain is
// currently unlocked without triggering an interactive GUI prompt.
func probeDarwinKeychainUnlocked() bool {
	if probeDarwinKeychainUnlockedFn != nil {
		return probeDarwinKeychainUnlockedFn()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/usr/bin/security", "show-keychain-info")
	cmd.Stdin = nil
	return cmd.Run() == nil
}

// readKeychainSecret reads the raw secret out of the macOS Keychain.
//
// Over SSH on macOS (reference behaviour since 1.5.16, unchanged in 1.5.17),
// probe whether the login keychain is unlocked
// first: if unlocked, reuse the Keychain entry; if locked, fail immediately
// with errKeyringLocked so callers fall back to file-based token storage
// without triggering a GUI prompt.
func readKeychainSecret() (string, error) {
	if isSSHSession() && !probeDarwinKeychainUnlocked() {
		return "", errKeyringLocked
	}
	return keyringGetFn(keychainService, keychainAccount)
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
	if isSSHSession() && !probeDarwinKeychainUnlocked() {
		return errKeyringLocked
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
