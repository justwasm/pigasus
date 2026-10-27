//go:build darwin

package auth_test

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/zalando/go-keyring"
)

func TestKeychainToken(t *testing.T) {
	tok, err := auth.LoadKeychainToken()
	if err != nil {
		t.Logf("No keychain token (expected in CI): %v", err)
		return
	}
	if tok.Token.AccessToken == "" {
		t.Fatal("token has empty access_token")
	}
	if !auth.HasKeychainToken() {
		t.Error("HasKeychainToken returned false despite successful load")
	}
	t.Logf("Got token with access_token prefix: %s...", tok.Token.AccessToken[:10])
}

// TestGoKeyringStoresTheSameBytes pins the storage format. The clone
// used to encode the payload itself ("go-keyring-base64:" + base64 of
// the JSON) and hand it to `security add-generic-password`; go-keyring
// does that same encoding itself. If the two ever diverge, the clone
// and the reference stop reading each other's keychain entry — so the
// bytes are compared against what `security` actually stores here.
//
// The probe entry is deleted again; it never carries a real token.
func TestGoKeyringStoresTheSameBytes(t *testing.T) {
	const service = "pigasus-format-probe"
	const account = "probe"
	const payload = `{"token":{"access_token":"probe-not-a-real-token"}}`

	if err := keyring.Set(service, account, payload); err != nil {
		t.Skipf("no keychain available here: %v", err)
	}
	defer func() { _ = keyring.Delete(service, account) }()

	// What the keychain really holds, read the way the reference's own
	// tooling reads it.
	out, err := exec.Command("/usr/bin/security",
		"find-generic-password", "-s", service, "-a", account, "-w").Output()
	if err != nil {
		t.Skipf("cannot read the probe entry back: %v", err)
	}
	got := strings.TrimSpace(string(out))
	want := "go-keyring-base64:" + base64.StdEncoding.EncodeToString([]byte(payload))
	if got != want {
		t.Fatalf("stored value = %q, want %q", got, want)
	}

	// And the library reads its own entry back as the plain payload.
	back, err := keyring.Get(service, account)
	if err != nil {
		t.Fatalf("keyring.Get: %v", err)
	}
	if back != payload {
		t.Fatalf("keyring.Get = %q, want %q", back, payload)
	}
}
