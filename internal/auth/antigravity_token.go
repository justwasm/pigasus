// Package auth: AntigravityFileStore 跟 v4 binary 共享 ~/.gemini/antigravity-cli/
//
// Cycle 35: 用户给真 OAuth token (cycle 11 拿的 ya29.a0AX07CmueHK5... 已过期,
// 新的 ya29.a0AX07CmueHK5-4niUGxWojxvguWMVMdsmzo8uDk0XDG7BtwtDE0CT06GijDpq0d5RzfUdkiW
// 还 ~9h 有效, 验证 userinfo 返 "Hangbiao Zou" sub=105126221582794464410)。
//
// v4 binary 实际存的 token:
//   ~/.gemini/antigravity-cli/antigravity-oauth-token
// 格式:
//   {"token": {"access_token": "ya29...", "refresh_token": "1//...", "expiry": "..."},
//    "auth_method": "consumer", "id_token": "eyJ..."}
//
// 之前 recovered code 用 keyring.FileStore 默认路径:
//   ~/.config/antigravity/antigravity-auth.json
// 格式 (Auth struct) 跟 v4 binary 不兼容。
//
// 这个文件加 AntigravityTokenFile 跟 v4 binary 1:1 兼容:
//   1. LoadAntigravityToken() 读 ~/.gemini/antigravity-cli/antigravity-oauth-token
//   2. SaveAntigravityToken() 写同一个路径
//   3. 跟 ANTIGRAVITY_HOME env 兼容 (默认 ~/.gemini/antigravity-cli)
//   4. RefreshToken() 走真 oauth2.googleapis.com/token 拿新 access_token
//   5. WriteAntigravityToken 原子 (temp+rename) + 0600 perms
package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// AntigravityToken is the on-disk format used by the v4 binary.
// Source: 1.3.3 binary decompilation — google3/third_party/jetski/cli/store/store
// (see /decompiled/c/google3_third_party_jetski_cli_store_store/google3_third_party_jetski_cli_store_store_AuthState.c).
type AntigravityToken struct {
	Token       AntigravityAccessToken `json:"token"`
	AuthMethod  string                 `json:"auth_method"` // "consumer" / "antigravity-hub" / etc
	IDToken     string                 `json:"id_token"`
}

// UserEmail extracts the email claim from an ID token (JWT) if present.
func (t *AntigravityToken) UserEmail() string {
	if t == nil || t.IDToken == "" {
		return ""
	}
	parts := strings.Split(t.IDToken, ".")
	if len(parts) < 2 {
		return ""
	}
	data, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		data, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return ""
		}
	}
	var claims struct {
		Email string `json:"email"`
	}
	_ = json.Unmarshal(data, &claims)
	return claims.Email
}

// EffectiveUserEmail returns the email associated with the active credentials,
// or empty string if unauthenticated.
func EffectiveUserEmail() string {
	if em := strings.TrimSpace(os.Getenv("AGY_USER_EMAIL")); em != "" {
		return em
	}
	if em := strings.TrimSpace(os.Getenv("OPENAGY_USER_EMAIL")); em != "" {
		return em
	}
	tok, err := LoadTokenUnified()
	if err == nil && tok != nil {
		if em := tok.UserEmail(); em != "" {
			return em
		}
		// If IDToken was missing or didn't contain email, try userinfo endpoint
		// if access token is present (skipped in tests).
		if tok.Token.AccessToken != "" && flag.Lookup("test.v") == nil {
			req, err := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
			if err == nil {
				req.Header.Set("Authorization", "Bearer "+tok.Token.AccessToken)
				client := &http.Client{Timeout: 2 * time.Second}
				resp, err := client.Do(req)
				if err == nil && resp.StatusCode == http.StatusOK {
					defer resp.Body.Close()
					var info struct {
						Email string `json:"email"`
					}
					if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.Email != "" {
						return info.Email
					}
				}
			}
		}
	}
	return ""
}

// AntigravityAccessToken is the OAuth2 access token envelope.
type AntigravityAccessToken struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	Expiry       string `json:"expiry"` // ISO 8601 (RFC 3339)
}

// AntigravityHome returns the directory where the v4 binary stores
// its OAuth token + related state. Respects ANTIGRAVITY_HOME env.
func AntigravityHome() string {
	if h := os.Getenv("ANTIGRAVITY_HOME"); h != "" {
		return h
	}
	// v4 binary default: $HOME/.gemini/antigravity-cli
	if h := os.Getenv("HOME"); h != "" {
		return filepath.Join(h, ".gemini", "antigravity-cli")
	}
	// fallback
	return filepath.Join(os.TempDir(), ".gemini", "antigravity-cli")
}

// DefaultAntigravityTokenPath is the canonical v4-binary token path.
const DefaultAntigravityTokenPath = "antigravity-oauth-token"

// TokenFilePath returns ~/.gemini/antigravity-cli/antigravity-oauth-token
// (or $ANTIGRAVITY_HOME/antigravity-oauth-token).
func TokenFilePath() string {
	return filepath.Join(AntigravityHome(), DefaultAntigravityTokenPath)
}

// LoadAntigravityToken reads the v4 binary's OAuth token file. Returns
// ErrNoToken if the file does not exist.
func LoadAntigravityToken() (*AntigravityToken, error) {
	return LoadAntigravityTokenFrom(TokenFilePath())
}

// LoadAntigravityTokenFrom reads from a custom path (testability).
func LoadAntigravityTokenFrom(path string) (*AntigravityToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoToken
		}
		return nil, err
	}
	var tok AntigravityToken
	if err := json.Unmarshal(data, &tok); err != nil {
		return nil, fmt.Errorf("antigravity-token: parse %s: %w", path, err)
	}
	if tok.Token.AccessToken == "" {
		return nil, ErrNoToken
	}
	return &tok, nil
}

// SaveAntigravityToken atomically writes the v4 binary's OAuth token file
// to the canonical path. 0600 perms.
func SaveAntigravityToken(tok *AntigravityToken) error {
	return SaveAntigravityTokenTo(TokenFilePath(), tok)
}

// SaveAntigravityTokenTo is the test-friendly variant.
func SaveAntigravityTokenTo(path string, tok *AntigravityToken) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	// Atomic: write to temp, fsync, rename
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ExpiryTime parses the ISO 8601 expiry string. Returns zero time on
// parse failure (caller should treat as "needs refresh").
func (t *AntigravityToken) ExpiryTime() time.Time {
	if t == nil || t.Token.Expiry == "" {
		return time.Time{}
	}
	// Try RFC 3339 first
	if tm, err := time.Parse(time.RFC3339Nano, t.Token.Expiry); err == nil {
		return tm
	}
	if tm, err := time.Parse(time.RFC3339, t.Token.Expiry); err == nil {
		return tm
	}
	return time.Time{}
}

// IsExpired returns true if the access token is expired or within 60s
// of expiry.
func (t *AntigravityToken) IsExpired() bool {
	exp := t.ExpiryTime()
	if exp.IsZero() {
		return true
	}
	return time.Until(exp) < 60*time.Second
}

// ErrNoToken is returned when no v4-binary OAuth token is on disk.
var ErrNoToken = errors.New("antigravity-token: not found")

// ---------------------------------------------------------------------------
// Real OAuth refresh (cycle 35, replaces cycle-25 placeholder)
// ---------------------------------------------------------------------------

// RefreshTokenReal calls oauth2.googleapis.com/token to swap a
// refresh_token for a fresh access_token. The v1.3.5 binary does not
// use mTLS (cycle 49 close-gap: it never had a device-bound cert);
// the plain HTTPS oauth2 endpoint is the same one it hits. Returns
// the raw TokenResponse (no id_token in this flow).
func RefreshTokenReal(ctx context.Context, clientID, refreshToken string) (*TokenResponse, error) {
	if clientID == "" {
		clientID = CLIClientID
	}
	if refreshToken == "" {
		return nil, errors.New("antigravity-token: empty refresh_token")
	}
	form := url.Values{
		"client_id":     {clientID},
		"refresh_token": {refreshToken},
		"grant_type":    {"refresh_token"},
	}
	// The CLI client is a public OAuth2 client. The v1.3.5 binary
	// does NOT use a device-code flow at runtime — it loads the
	// bearer token from $JETSKI_OAUTH_TOKEN or the keyring. This
	// refresh path is therefore server-side: the response will say
	// so if the server requires a client_secret for this client_id
	// and we surface the error verbatim. Callers that need the
	// paired client_secret should switch to
	// RefreshTokenRealWithSecret in unified_config.go (cycle 42 fix).
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		TokenURL, bytes.NewBufferString(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("antigravity-token: refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity-token: refresh: HTTP %d: %s",
			resp.StatusCode, string(body))
	}
	var out TokenResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("antigravity-token: parse refresh: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("antigravity-token: empty access_token in refresh response: %s",
			string(body))
	}
	return &out, nil
}

// TokenCache is a small thread-safe in-memory cache for the
// refreshed access token. Decouples callers from disk + network.
type TokenCache struct {
	mu   sync.Mutex
	tok  *AntigravityToken
	path string
}

// NewTokenCache returns a cache rooted at the v4 binary's token path.
func NewTokenCache() *TokenCache {
	return &TokenCache{path: TokenFilePath()}
}

// Get returns a non-expired AntigravityToken, refreshing if needed.
// On refresh success the new token is written back to disk so the v4
// binary sees the same data on next launch.
//
// AGY 1.2.5: when the refresh fails with a definitive rejection (revoked
// enterprise account, invalid_grant, 401), the on-disk token is cleared
// so the next /login can sign in cleanly. Transient failures (network,
// 5xx, rate limit) return the stale token unchanged so the retry can
// take another pass without prompting the user.
func (c *TokenCache) Get(ctx context.Context) (*AntigravityToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok == nil {
		tok, err := LoadAntigravityTokenFrom(c.path)
		if err != nil {
			return nil, err
		}
		c.tok = tok
	}
	if !c.tok.IsExpired() {
		return c.tok, nil
	}
	// Refresh
	resp, err := RefreshTokenReal(ctx, CLIClientID, c.tok.Token.RefreshToken)
	if err != nil {
		// Same inspection as the unified config path: classify first,
		// then act on rejection. ClassifyRefreshError returns a typed
		// *AuthError on structured failures; for unstructured ones we
		// synthesise one so callers can always errors.As to it.
		aerr := ClassifyRefreshError(err)
		if aerr == nil {
			aerr = &AuthError{Endpoint: "refresh", Cause: err}
		}
		if aerr.Rejection {
			// Cycle 225: drop the stale credentials. ClearTokenUnified
			// walks AllTokenPaths() so the legacy ~/.config/antigravity
			// fallbacks get wiped too; the keychain call best-effort
			// purges the macOS credential.
			cleared, _ := ClearTokenUnified()
			// Also reset the in-memory copy so this cache instance
			// does not hand back the stale token after the clear.
			c.tok = nil
			return nil, fmt.Errorf("antigravity-token: refresh: %w (sign-in rejected: cleared %d stale credential path(s); please run /login)", aerr, cleared)
		}
		// Refresh failed; return the (possibly-stale) token anyway
		// so callers can decide. Callers may use the access_token
		// directly if it's still valid.
		return c.tok, fmt.Errorf("antigravity-token: refresh failed: %w (returning stale)", aerr)
	}
	// Update in-memory + on-disk
	c.tok.Token.AccessToken = resp.AccessToken
	c.tok.Token.TokenType = resp.TokenType
	if resp.RefreshToken != "" {
		c.tok.Token.RefreshToken = resp.RefreshToken
	}
	expiry := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	c.tok.Token.Expiry = expiry.Format(time.RFC3339Nano)
	_ = SaveAntigravityTokenTo(c.path, c.tok)
	return c.tok, nil
}
