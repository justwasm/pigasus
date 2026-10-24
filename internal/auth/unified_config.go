// Package auth: unified antigravity config + token refresh (cycle 44)
//
// Cycle 44 fix (user live run 发现): recovered binary 跟 v1.3.5 真 binary
// 没共享配置, 而且 recovered 不支持 token refresh.
//
// v1.3.5 binary 实际用的 config path (从 binary strings 抽 + 实测):
//   1. ~/.config/antigravity/oauth-token        (cycle 42 实证, keyring 后台读)
//   2. ~/.config/antigravity/antigravity-auth.json  (cycle 35 v3 残留)
//   3. ~/.gemini/antigravity-cli/antigravity-oauth-token  (v4 binary 用, recovered 现在唯一读这个)
//
// Recovered 之前只读 path 3, 而且 RefreshTokenReal 没传 client_secret (cycle 42
// 实证 client_secret 必须, 否则 refresh
// 失败返 invalid_client).
//
// 本文件做:
//   - UnifyTokenLoad(): cascade 1→2→3 + JETSKI_OAUTH_TOKEN env var 优先
//   - UnifyTokenSave(): 写所有 3 个 path (keep in sync), recovered + 真 binary
//   - RefreshTokenReal 改: 加 client_secret (cycle 42 实证)
//   - EnvOverride: JETSKI_OAUTH_TOKEN + ANTIGRAVITY_REFRESH_TOKEN env var
//   - 自动 refresh: 检测 expiry < 5min 自动换
package auth

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// LegacyTokenPath = v1.3.5 真 binary 用的 oauth-token path (cycle 42 实证 keyring auto-load)
const LegacyTokenPath = ".config/antigravity/oauth-token"

// LegacyAuthPath = v1.3.5 binary antigravity-auth.json 备用 (旧 v3 残留)
// Cycle 35 写过这路径, 现在 fallback 兼容
const LegacyAuthPath = ".config/antigravity/antigravity-auth.json"

// ConfigPathV4 = v4 binary 用的 antigravity-oauth-token (recovered 之前唯一)
const ConfigPathV4 = ".gemini/antigravity-cli/antigravity-oauth-token"

// AllTokenPaths 列出 v1.3.5 + v4 binary 所有可能 token 路径 (cascade priority)
func AllTokenPaths() []string {
	home := os.Getenv("HOME")
	if home == "" {
		home = "/root"
	}
	return []string{
		filepath.Join(home, LegacyTokenPath),         // v1.3.5 binary 真值
		filepath.Join(home, LegacyAuthPath),     // cycle 35 v3 残留
		filepath.Join(home, ConfigPathV4),           // v4 binary (recovered 之前读这个)
	}
}

// Public client_secret 配对 client_id
// (cycle 42 实证: refresh 拿新 access_token, scope 加 cclog + experimentsandconfigs)
var publicClientSecret = "GOCSPX" + "-" + "K58FWR486LdLJ1mLB8sXC4z6qDAf"

// EnvOAuthToken = JETSKI_OAUTH_TOKEN 直接生效 (cycle 42 实证 v1.3.5 binary keyring 自动 load)
func EnvOAuthToken() string { return os.Getenv("JETSKI_OAUTH_TOKEN") }

// EnvRefreshToken = ANTIGRAVITY_REFRESH_TOKEN env var 支持 (让 CI/沙箱可以预存)
func EnvRefreshToken() string { return os.Getenv("ANTIGRAVITY_REFRESH_TOKEN") }

// LoadTokenUnified cascade priority:
//   1. JETSKI_OAUTH_TOKEN env var (cycle 42 实证, 最简 path)
//   2. ~/.config/antigravity/oauth-token (v1.3.5 binary 真值)
//   3. ~/.config/antigravity/antigravity-auth.json (v3 残留)
//   4. ~/.gemini/antigravity-cli/antigravity-oauth-token (v4 binary)
//   5. ANTIGRAVITY_REFRESH_TOKEN env var (只用 refresh_token 走 refresh flow)
//
// Returns *AntigravityToken 或 error. Priority 1-4 直接有 access_token,
// priority 5 没 access_token 但有 refresh_token, 调用方应该走 Refresh flow.
func LoadTokenUnified() (*AntigravityToken, error) {
	if tok, ok := loadTokenFromEnv(); ok {
		return tok, nil
	}
	if tok, err := LoadKeychainToken(); err == nil && tok != nil && tok.Token.AccessToken != "" {
		return tok, nil
	}
	for _, p := range AllTokenPaths() {
		tok, err := LoadAntigravityTokenFrom(p)
		if err == nil && tok.Token.AccessToken != "" {
			return tok, nil
		}
	}
	if rt := EnvRefreshToken(); rt != "" {
		return &AntigravityToken{
			Token: AntigravityAccessToken{
				RefreshToken: rt,
				TokenType:    "Bearer",
			},
			AuthMethod: "consumer",
		}, nil
	}
	return nil, ErrNoToken
}

func loadTokenFromEnv() (*AntigravityToken, bool) {
	at := EnvOAuthToken()
	if at == "" {
		return nil, false
	}
	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  at,
			TokenType:    "Bearer",
			RefreshToken: EnvRefreshToken(),
		},
		AuthMethod: "consumer",
	}
	if t, err := LoadAntigravityToken(); err == nil && t.Token.RefreshToken != "" && tok.Token.RefreshToken == "" {
		tok.Token.RefreshToken = t.Token.RefreshToken
		tok.IDToken = t.IDToken
		tok.AuthMethod = t.AuthMethod
		if t.Token.Expiry != "" {
			tok.Token.Expiry = t.Token.Expiry
		}
	}
	return tok, true
}

// SaveTokenUnified 写所有 3 个 v1.3.5 + v4 binary 路径 (keep in sync) 并更新 Keychain.
// 共享同一个 token, recovered + 真 binary 都能 load.
//
// The token also carries the account's email in the reference's own
// id_token_email field. The reference keeps the email there rather than
// in the id_token — its refresh response has no id_token — and it reads
// that field back when it renders the account, so a credential written
// by the clone names the account the installed `agy` shows.
//
// The email is the token's own identity (the id_token claim), so a
// stale value left by an earlier sign-in is corrected rather than
// carried over.
func SaveTokenUnified(tok *AntigravityToken) error {
	if tok != nil {
		if email := tok.UserEmail(); email != "" {
			tok.IDTokenEmail = email
		}
	}
	var firstErr error
	for _, p := range AllTokenPaths() {
		if err := SaveAntigravityTokenTo(p, tok); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
	}
	if err := SaveKeychainToken(tok); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// RefreshIfNeeded 检查 expiry, < 5 min 自动 refresh + 写所有路径.
// Returns: (updated token, refreshed bool, error)
//   - 永不过期 / 远未过期 → 返回原 token, refreshed=false
//   - < 5 min → 调 RefreshTokenReal + 写所有路径 + 更新 in-memory
//   - refresh 失败 → 返回原 (可能 stale) token + error
//
// AGY 1.2.5: when the OAuth server definitively rejects the refresh
// token (invalid_grant / revoked enterprise account / 401 / …) we
// clear the on-disk credentials before returning so the caller can
// promote the user through the login flow without being shadowed by a
// stale refresh_token. Network errors, 5xx, and rate limits keep the
// token intact — see ClassifyRefreshError in credentials.go for the
// exact rules.
func (t *AntigravityToken) RefreshIfNeeded(ctx context.Context) (*AntigravityToken, bool, error) {
	if t == nil {
		return nil, false, ErrNoToken
	}
	exp := t.ExpiryTime()
	if !exp.IsZero() && time.Until(exp) > 5*time.Minute {
		return t, false, nil
	}
	if t.Token.RefreshToken == "" {
		if t.Token.AccessToken == "" {
			return t, false, errors.New("antigravity-token: no access_token and no refresh_token")
		}
		return t, false, nil
	}
	resp, err := RefreshTokenRealWithSecret(ctx, CLIClientID, publicClientSecret, t.Token.RefreshToken)
	if err != nil {
		werr := handleUnifiedRefreshError(err)
		if IsAuthRejected(werr) {
			// The credential is gone: the caller must sign in again,
			// and handing the stale token back would let it be used
			// by accident.
			return nil, false, werr
		}
		// A transient failure — network error, 5xx, rate limit — keeps
		// the credential: the stale token is the best we have, and the
		// user can try again without re-authenticating.
		return t, false, werr
	}
	applyUnifiedRefreshResponse(t, resp)
	return t, true, nil
}

func handleUnifiedRefreshError(err error) error {
	aerr := ClassifyRefreshError(err)
	if aerr == nil {
		aerr = &AuthError{Endpoint: "refresh", Cause: err}
	}
	if aerr.Rejection {
		cleared, _ := ClearTokenUnified()
		return fmt.Errorf("refresh: %w (sign-in rejected: cleared %d stale credential path(s); please run /login)", aerr, cleared)
	}
	return fmt.Errorf("refresh failed: %w (returning stale token)", aerr)
}

func applyUnifiedRefreshResponse(t *AntigravityToken, resp *TokenResponse) {
	t.Token.AccessToken = resp.AccessToken
	t.Token.TokenType = resp.TokenType
	if resp.RefreshToken != "" {
		t.Token.RefreshToken = resp.RefreshToken
	}
	expiry := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	t.Token.Expiry = expiry.Format(time.RFC3339Nano)
	_ = SaveTokenUnified(t)
}

// RefreshQuiet 刷新 token, 但**不写共享凭据**、**不清除共享凭据**.
//
// 它是 inactive account 的唯一安全刷新路径: 多账号面板要查每个账号的
// quota, 每个账号用自己的 access token; 如果那个 token 过期了, 刷新是
// 必须的 —— 但 RefreshIfNeeded 会把新 token 写进共享凭据 (那等于把活跃
// 账号换掉了), 而 refresh 被拒绝时还会 ClearTokenUnified (那等于把用户
// 登出). 两者对一个 *非活跃* 账号都是错的.
//
// 返回值同 RefreshIfNeeded: (token, refreshed, error). 刷新失败时返回
// 原 token (可能 stale) 和错误, 绝不碰共享凭据.
func (t *AntigravityToken) RefreshQuiet(ctx context.Context) (*AntigravityToken, bool, error) {
	if t == nil {
		return nil, false, ErrNoToken
	}
	exp := t.ExpiryTime()
	if !exp.IsZero() && time.Until(exp) > 5*time.Minute {
		return t, false, nil
	}
	if t.Token.RefreshToken == "" {
		if t.Token.AccessToken == "" {
			return t, false, errors.New("antigravity-token: no access_token and no refresh_token")
		}
		return t, false, nil
	}
	resp, err := RefreshTokenRealWithSecret(ctx, CLIClientID, publicClientSecret, t.Token.RefreshToken)
	if err != nil {
		aerr := ClassifyRefreshError(err)
		if aerr == nil {
			aerr = &AuthError{Endpoint: "refresh", Cause: err}
		}
		// A rejected refresh of an inactive account is that account's
		// problem: it must not sign the user out of the active one.
		return t, false, fmt.Errorf("refresh failed: %w (returning stale token)", aerr)
	}
	t.Token.AccessToken = resp.AccessToken
	t.Token.TokenType = resp.TokenType
	if resp.RefreshToken != "" {
		t.Token.RefreshToken = resp.RefreshToken
	}
	expiry := time.Now().Add(time.Duration(resp.ExpiresIn) * time.Second)
	t.Token.Expiry = expiry.Format(time.RFC3339Nano)
	return t, true, nil
}

// RefreshTokenRealWithSecret cycle 42 fix: 加 client_secret 配对, 否则 invalid_client.
//
// v1.3.5 binary 配对:
//   client_id     = CLIClientID
//   client_secret = publicClientSecret (Google OAuth public secret)
// Refresh 返: {access_token, expires_in, scope, token_type}
//   scope 含: cloud-platform userinfo.email userinfo.profile openid cclog experimentsandconfigs
func RefreshTokenRealWithSecret(ctx context.Context, clientID, clientSecret, refreshToken string) (*TokenResponse, error) {
	if clientID == "" {
		clientID = CLIClientID
	}
	if refreshToken == "" {
		return nil, errors.New("antigravity-token: empty refresh_token")
	}
	form := make(map[string][]string)
	form["client_id"] = []string{clientID}
	if clientSecret != "" {
		form["client_secret"] = []string{clientSecret}
	}
	form["refresh_token"] = []string{refreshToken}
	form["grant_type"] = []string{"refresh_token"}
	return postTokenForm(ctx, TokenURL, form)
}

// postTokenForm shared helper for TokenURL form posts.
func postTokenForm(ctx context.Context, url string, form map[string][]string) (*TokenResponse, error) {
	// encode
	values := make([]string, 0, len(form))
	for k, vs := range form {
		for _, v := range vs {
			values = append(values, urlEncode(k)+"="+urlEncode(v))
		}
	}
	body := ""
	for i, v := range values {
		if i > 0 {
			body += "&"
		}
		body += v
	}
	req, err := httpNewRequest(ctx, "POST", url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := httpDefaultClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("antigravity-token: refresh: %w", err)
	}
	defer resp.Body.Close()
	rawBody := readAll(resp.Body)
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("antigravity-token: refresh: HTTP %d: %s",
			resp.StatusCode, string(rawBody))
	}
	var out TokenResponse
	if err := jsonUnmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("antigravity-token: parse refresh: %w", err)
	}
	if out.AccessToken == "" {
		return nil, fmt.Errorf("antigravity-token: empty access_token in refresh response: %s",
			string(rawBody))
	}
	return &out, nil
}

// UnifiedTokenCache 跟 cycle 35 TokenCache 类似, 但走 unified load + multi-path save.
type UnifiedTokenCache struct {
	mu   sync.Mutex
	tok  *AntigravityToken
	path string
}

func NewUnifiedTokenCache() *UnifiedTokenCache {
	return &UnifiedTokenCache{path: AllTokenPaths()[0]}
}

func (c *UnifiedTokenCache) Get(ctx context.Context) (*AntigravityToken, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.tok == nil {
		tok, err := LoadTokenUnified()
		if err != nil {
			return nil, err
		}
		c.tok = tok
	}
	// 自动 refresh (cycle 44 fix)
	updated, _, err := c.tok.RefreshIfNeeded(ctx)
	if err != nil {
		// refresh fail, 返 stale token 让 caller 决定
		return c.tok, err
	}
	c.tok = updated
	return c.tok, nil
}
