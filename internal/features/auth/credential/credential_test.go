// credential_test.go — the credential cascade's own answers.
//
// The cascade is the clone's: the keychain first, the on-disk copies
// second. What the reference adds is a name for the failure the
// keychain gives when it will not open.
//
// 【文档】 official-docs/cli/troubleshooting.md:75 —
//
//	Error: failed to retrieve token: secret keyring is locked
package credential

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/features/auth"
)

// lockedKeyringError is the failure a locked keychain produces.
//
// go-keyring's macOS backend keeps only the "could not be found" case
// as keyring.ErrNotFound and hands every other `security` failure back
// as it is (go-keyring@v0.2.8/keyring_darwin.go), so this is the shape
// a locked keychain arrives in.
var lockedKeyringError = errors.New("exit status 36: SecKeychainSearchCopyNext: User interaction is not allowed.")

// isolateHome points the cascade at a scratch home.
//
// The cascade reads the user's own token files, so a test that does not
// move the home reads the developer's credential — and answers with it
// instead of with the failure under test.
func isolateHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", filepath.Join(home, ".gemini", "antigravity-cli"))
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	t.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")
	return home
}

// lockTheKeychain makes the keychain read fail the way a locked one does.
func lockTheKeychain(t *testing.T) {
	t.Helper()
	orig := auth.LoadKeychainTokenFn
	t.Cleanup(func() { auth.LoadKeychainTokenFn = orig })
	auth.LoadKeychainTokenFn = func() (*auth.AntigravityToken, error) {
		return nil, lockedKeyringError
	}
}

// TestDefaultTokenSourceNamesTheLockedKeyring pins the reference's own
// sentence at the site the user reads it: the credential read.
//
// The bytes are the whole point. The "Error: " in front of the
// documented line belongs to the transcript's error row
// (`internal/ui/toolsteps/error_row.go`), so the error value itself
// carries no prefix.
func TestDefaultTokenSourceNamesTheLockedKeyring(t *testing.T) {
	isolateHome(t)
	lockTheKeychain(t)

	_, err := DefaultTokenSource()(context.Background())
	if err == nil {
		t.Fatal("the token source answered a locked keyring with no error")
	}
	const want = "failed to retrieve token: secret keyring is locked"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err.Error(), want)
	}
}

// TestDefaultTokenSourcePrefersTheOnDiskCopyOverALockedKeychain pins
// the order of the cascade: the on-disk copy still authenticates the
// turn, so a locked keyring is not reported as a failure.
//
// The locked keyring is the *last* thing the cascade reports, not the
// first: it is a more precise answer than "no token", but it is not a
// reason to refuse a credential the clone already holds.
func TestDefaultTokenSourcePrefersTheOnDiskCopyOverALockedKeychain(t *testing.T) {
	home := isolateHome(t)
	lockTheKeychain(t)

	path := filepath.Join(home, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	const body = `{"token":{"access_token":"ya29.on-disk"},"auth_method":"consumer"}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write token: %v", err)
	}

	tok, err := DefaultTokenSource()(context.Background())
	if err != nil {
		t.Fatalf("the token source failed: %v", err)
	}
	if tok != "ya29.on-disk" {
		t.Errorf("token = %q, want the on-disk copy", tok)
	}
}
