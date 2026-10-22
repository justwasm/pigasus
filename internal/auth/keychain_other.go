//go:build !darwin

package auth

// ClearKeychainTokenFn is a test seam allowing tests to mock keychain clearance.
var ClearKeychainTokenFn func() error

// LoadKeychainToken is a stub on non-darwin systems.
func LoadKeychainToken() (*AntigravityToken, error) {
	return nil, ErrNoToken
}

// HasKeychainToken returns false on non-darwin systems.
func HasKeychainToken() bool {
	return false
}

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
