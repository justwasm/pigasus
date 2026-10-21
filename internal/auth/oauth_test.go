// Package auth: oauth_test.go — verify the cycle 44 ship constants + the
// RefreshToken placeholder are intact. The v1.3.5 binary does not run an
// OAuth device-code flow at runtime (cycle 49 close-gap: it loads the
// bearer token from $JETSKI_OAUTH_TOKEN / keyring); the device-code
// tests that used to live here were removed in cycle 50.
package auth

import (
	"context"
	"strings"
	"testing"
)

func TestRefreshToken(t *testing.T) {
	ctx := context.Background()
	tok, err := RefreshToken(ctx, CLIClientID, "test-refresh-token")
	if err != nil {
		t.Fatalf("RefreshToken: %v", err)
	}
	if tok.AccessToken == "" {
		t.Error("AccessToken is empty")
	}
	if tok.ExpiresIn <= 0 {
		t.Errorf("ExpiresIn = %d, want > 0", tok.ExpiresIn)
	}
}

func TestConstants(t *testing.T) {
	// Verify the recovered constants are non-empty.
	if CLIClientID == "" {
		t.Error("CLIClientID is empty")
	}
	if !strings.HasSuffix(CLIClientID, ".apps.googleusercontent.com") {
		t.Errorf("CLIClientID = %q, want .apps.googleusercontent.com suffix", CLIClientID)
	}
	if IDEClientID == "" {
		t.Error("IDEClientID is empty")
	}
	if !strings.HasSuffix(IDEClientID, ".apps.googleusercontent.com") {
		t.Errorf("IDEClientID = %q, want .apps.googleusercontent.com suffix", IDEClientID)
	}
	if CloudCodeAPI == "" {
		t.Error("CloudCodeAPI is empty")
	}
	if !strings.HasPrefix(CloudCodeAPI, "https://") {
		t.Errorf("CloudCodeAPI = %q, want https:// prefix", CloudCodeAPI)
	}
	// Cycle 50 sub-agent C: CloudCodeDaily is the v1.3.5 binary's
	// actual API endpoint.
	if CloudCodeDaily != "https://daily-cloudcode-pa.googleapis.com" {
		t.Errorf("CloudCodeDaily = %q, want daily-cloudcode-pa", CloudCodeDaily)
	}
	// TokenURL must point at the plain oauth2 endpoint (no mTLS).
	if TokenURL != "https://oauth2.googleapis.com/token" {
		t.Errorf("TokenURL = %q, want https://oauth2.googleapis.com/token", TokenURL)
	}
	// Scopes (cycle 44 ship: 6 scopes for refresh).
	if ScopeCloudPlatform == "" {
		t.Error("ScopeCloudPlatform is empty")
	}
}

// TestOpenBrowser_RespectsNoBrowserEnvVar confirms that
// auth.OpenBrowser honours $OPENAGY_NO_BROWSER the same way
// cli.OpenBrowser's BrowserAutoOpen guard does — when the env var
// is set to a recognised truthy value, OpenBrowser returns nil
// without spawning the platform launcher.
//
// The original TestOpenBrowser_NoPanic at cycle 25 literally
// invoked `open https://example.com` on macOS during `go test
// ./...`, which left a stray browser tab open every time the
// suite ran. Setting OPENAGY_NO_BROWSER=1 turns this test into
// a true no-op: it asserts the gate, not the cross-platform
// child-process plumbing (cli's open_browser_test.go covers
// that with a different URL).
func TestOpenBrowser_RespectsNoBrowserEnvVar(t *testing.T) {
	for _, val := range []string{"1", "true", "TRUE", "True", "yes", "YES"} {
		t.Run("val="+val, func(t *testing.T) {
			t.Setenv("OPENAGY_NO_BROWSER", val)
			if err := OpenBrowser("https://example.com"); err != nil {
				t.Errorf("OpenBrowser with OPENAGY_NO_BROWSER=%q returned %v; want nil (gate hit, no spawn)", val, err)
			}
		})
	}
}

// TestOpenBrowser_EmptyURLIsNoop confirms the empty-string short
// circuit — no env var, no spawn, nil error.
func TestOpenBrowser_EmptyURLIsNoop(t *testing.T) {
	t.Setenv("OPENAGY_NO_BROWSER", "")
	if err := OpenBrowser(""); err != nil {
		t.Errorf("OpenBrowser(\"\") returned %v; want nil", err)
	}
}
