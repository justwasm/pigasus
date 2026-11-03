// keychain_test.go — the keyring read's own answers.
//
// The clone reads the reference's own entry through the same library
// (`github.com/zalando/go-keyring`), so the only thing the clone has to
// decide for itself is what a *failed* read means.
//
// 【文档】 official-docs/cli/troubleshooting.md:75 —
//
//	Error: failed to retrieve token: secret keyring is locked
package auth

import (
	"errors"
	"testing"

	"github.com/zalando/go-keyring"
)

// lockedKeyringError is the failure a locked keychain produces.
//
// go-keyring's macOS backend keeps only the "could not be found" case
// as keyring.ErrNotFound and hands every other `security` failure back
// as it is (go-keyring@v0.2.8/keyring_darwin.go), so this is the shape
// a locked keychain arrives in.
var lockedKeyringError = errors.New("exit status 36: SecKeychainSearchCopyNext: User interaction is not allowed.")

// TestLoadKeychainTokenNamesTheLockedKeyring pins the reference's own
// sentence for a keyring that will not open.
//
// The bytes are the whole point: the sentence is what the user reads,
// and the "Error: " in front of it belongs to the transcript's error
// row, not to the error value.
func TestLoadKeychainTokenNamesTheLockedKeyring(t *testing.T) {
	orig := LoadKeychainTokenFn
	t.Cleanup(func() { LoadKeychainTokenFn = orig })
	LoadKeychainTokenFn = func() (*AntigravityToken, error) {
		return nil, lockedKeyringError
	}

	_, err := LoadKeychainToken()
	if err == nil {
		t.Fatal("LoadKeychainToken on a locked keyring = nil error")
	}
	const want = "failed to retrieve token: secret keyring is locked"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestLoadKeychainTokenKeepsAMissingEntryAsNoToken pins the other half:
// a keyring with no entry is not a broken keyring, it is a machine that
// has never been signed in.
func TestLoadKeychainTokenKeepsAMissingEntryAsNoToken(t *testing.T) {
	orig := LoadKeychainTokenFn
	t.Cleanup(func() { LoadKeychainTokenFn = orig })
	LoadKeychainTokenFn = func() (*AntigravityToken, error) {
		return nil, keyring.ErrNotFound
	}

	_, err := LoadKeychainToken()
	if !errors.Is(err, ErrNoToken) {
		t.Errorf("error = %v, want ErrNoToken", err)
	}
}

// TestKeyringReadErrorClassifiesTheTwoAnswers pins the classification
// itself, so the seam above is not the only thing that is tested.
func TestKeyringReadErrorClassifiesTheTwoAnswers(t *testing.T) {
	if got := keyringReadError(nil); got != nil {
		t.Errorf("keyringReadError(nil) = %v, want nil", got)
	}
	if got := keyringReadError(keyring.ErrNotFound); !errors.Is(got, ErrNoToken) {
		t.Errorf("keyringReadError(ErrNotFound) = %v, want ErrNoToken", got)
	}
	if got := keyringReadError(ErrNoToken); !errors.Is(got, ErrNoToken) {
		t.Errorf("keyringReadError(ErrNoToken) = %v, want ErrNoToken", got)
	}
	got := keyringReadError(lockedKeyringError)
	if got == nil {
		t.Fatal("keyringReadError(locked) = nil, want the reference's sentence")
	}
	const want = "failed to retrieve token: secret keyring is locked"
	if got.Error() != want {
		t.Errorf("error = %q, want %q", got.Error(), want)
	}
	if !errors.Is(got, errKeyringLocked) {
		t.Errorf("error %v does not carry errKeyringLocked", got)
	}
}
