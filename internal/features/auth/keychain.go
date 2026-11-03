// Package auth keychain.go — the keyring read, and the failure the
// reference names.
//
// The read itself is platform-specific (`readKeychainSecret`), but the
// *answer* it gives is not: a keyring that holds no entry and a keyring
// that will not open are two different failures, and the reference tells
// them apart.
//
// 【文档】 official-docs/cli/troubleshooting.md:75 —
//
//	Error: failed to retrieve token: secret keyring is locked
//
// The sentence is the reference's own, and the clone emits it at the
// same site: the credential read. The "Error: " prefix belongs to
// whoever prints the failure — the transcript's error row is
// `"Error: " + message` (`internal/ui/toolsteps/error_row.go`) — so the
// error value itself carries no prefix and the printed line is the
// documented one.
//
// There is no separate "locked" detection to be had: go-keyring's macOS
// backend hands back `security`'s own failure and keeps only the
// "could not be found" case as `keyring.ErrNotFound`
// (`go-keyring@v0.2.8/keyring_darwin.go`). Every other failure — a
// locked keychain, a denied ACL, a `security` that is not there — is
// "the keyring did not answer", and the reference names that case.
package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/zalando/go-keyring"
)

// errKeyringLocked is the reference's own wording for a keyring that
// will not open.
//
// It is unexported: the sentence the user reads is the whole error,
// and a caller that wants to tell "locked" from "absent" asks with
// errors.Is on the error the read returned.
var errKeyringLocked = errors.New("secret keyring is locked")

// LoadKeychainTokenFn is a test seam allowing tests to mock the
// keychain read.
//
// It stands for the whole read — the lookup *and* the parse — so a test
// can answer with a locked keyring without a keychain behind it. The
// failure it returns is classified exactly like the real one.
var LoadKeychainTokenFn func() (*AntigravityToken, error)

// LoadKeychainToken retrieves the Antigravity OAuth token from the
// platform's secret store.
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
//
// A missing entry is ErrNoToken; a keyring that will not answer is the
// reference's "failed to retrieve token: secret keyring is locked".
func LoadKeychainToken() (*AntigravityToken, error) {
	tok, err := loadKeychainToken()
	if err != nil {
		return nil, err
	}
	if tok == nil || tok.Token.AccessToken == "" {
		return nil, ErrNoToken
	}
	return tok, nil
}

// loadKeychainToken performs the read and the parse.
//
// Only a *read* failure is classified: an entry that holds bytes the
// clone cannot parse is a corrupt credential, not a locked keyring,
// and the reference does not call it one.
func loadKeychainToken() (*AntigravityToken, error) {
	if LoadKeychainTokenFn != nil {
		tok, err := LoadKeychainTokenFn()
		if err != nil {
			return nil, keyringReadError(err)
		}
		return tok, nil
	}
	raw, err := readKeychainSecret()
	if err != nil {
		return nil, keyringReadError(err)
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, ErrNoToken
	}
	var tok AntigravityToken
	if err := json.Unmarshal([]byte(raw), &tok); err != nil {
		return nil, err
	}
	return &tok, nil
}

// keyringReadError classifies a keyring read failure.
//
// A missing entry is ErrNoToken — the credential is simply not there,
// which is the normal state of a fresh machine. Every other failure is
// the keyring refusing to answer, and that is the case the reference
// names.
func keyringReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, keyring.ErrNotFound) || errors.Is(err, ErrNoToken) {
		return ErrNoToken
	}
	return fmt.Errorf("failed to retrieve token: %w", errKeyringLocked)
}

// HasKeychainToken checks if the secret store contains the antigravity
// token.
func HasKeychainToken() bool {
	tok, err := LoadKeychainToken()
	return err == nil && tok != nil && tok.Token.AccessToken != ""
}
