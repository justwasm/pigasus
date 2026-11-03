//go:build !darwin

package auth

import (
	"github.com/zalando/go-keyring"
)

// readKeychainSecret is a stub on non-darwin systems: there is no
// keychain to read.
//
// It answers "not found" rather than "locked" — the shared layer in
// keychain.go turns that into ErrNoToken, which is what the clone's
// credential cascade expects on a platform without a keychain.
func readKeychainSecret() (string, error) {
	return "", keyring.ErrNotFound
}

// ClearKeychainTokenFn is a test seam allowing tests to mock keychain clearance.
var ClearKeychainTokenFn func() error

// ClearKeychainToken is a no-op on non-darwin systems unless ClearKeychainTokenFn is set.
func ClearKeychainToken() error {
	if ClearKeychainTokenFn != nil {
		return ClearKeychainTokenFn()
	}
	return nil
}

// SaveKeychainToken is a no-op on non-darwin systems.
func SaveKeychainToken(tok *AntigravityToken) error {
	return nil
}
