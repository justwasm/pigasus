package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// HasOAuthToken returns true if an OAuth token exists in env, on disk, or in Keychain.
func HasOAuthToken() bool {
	if v := strings.TrimSpace(os.Getenv("JETSKI_OAUTH_TOKEN")); v != "" {
		return true
	}
	if v := strings.TrimSpace(os.Getenv("ANTIGRAVITY_REFRESH_TOKEN")); v != "" {
		return true
	}
	candidates := []string{
		filepath.Join(AntigravityHome(), "oauth-token"),
		filepath.Join(AntigravityHome(), "antigravity-auth.json"),
		TokenFilePath(),
	}
	for _, p := range candidates {
		if p == "" {
			continue
		}
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return true
		}
	}
	if runtime.GOOS == "darwin" {
		if tok, err := LoadKeychainToken(); err == nil && tok != nil && tok.Token.AccessToken != "" {
			return true
		}
	}
	return false
}
