//go:build darwin

package auth_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
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
