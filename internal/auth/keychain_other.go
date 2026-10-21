//go:build !darwin

package auth

// LoadKeychainToken is a stub on non-darwin systems.
func LoadKeychainToken() (*AntigravityToken, error) {
	return nil, ErrNoToken
}

// HasKeychainToken returns false on non-darwin systems.
func HasKeychainToken() bool {
	return false
}

// ClearKeychainToken is a no-op on non-darwin systems. The mirror
// of ClearTokenUnified deletes the same keychain entry on macOS; every
// other platform has nothing to remove by definition.
func ClearKeychainToken() error {
	return nil
}

// SaveKeychainToken is a no-op on non-darwin systems.
func SaveKeychainToken(tok *AntigravityToken) error {
	return nil
}
