// Package auth contains the recovered OAuth bearer-token endpoints derived
// from decompiling the 1.3.5 antigravity release binary.
//
// Source-of-truth: the v1.3.5 binary does NOT perform an OAuth device-code
// flow at runtime. Cycle 42 close-gap confirmed: it reads the bearer
// token from $JETSKI_OAUTH_TOKEN (or ~/.config/antigravity/oauth-token
// via keyring auto-load) and sends it as a plain Authorization: Bearer
// header. There is no PKCE, no device-code, no mTLS in the v1.3.5 binary.
//
// The constants/var block below keeps the OAuth2 endpoint URLs + the
// public OAuth2 client IDs + the OAuth2 scopes (cycle 44 ship) used by
// RefreshTokenReal / RefreshTokenRealWithSecret for the refresh_token
// grant against oauth2.googleapis.com/token. Refresh-token exchange
// requires the public client_secret (see unified_config.go
// publicClientSecret).
//
// See docs/auth-flow.md for the full state machine reconstructed from
// Ghidra-decompiled pseudocode (TrySilentAuth, etc.) of the 1.3.3 binary
// that we no longer implement.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// OAuth2 endpoints. TokenURL is a var (not a const) so the
// oauth_test.go test seam can stub it.
var (
	TokenURL     = "https://oauth2.googleapis.com/token" // var for test seam
	IamCredsURL  = "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts"
	CloudCodeAPI = "https://cloudcode-pa.googleapis.com"
	AICodeAPI    = "https://aicode.googleapis.com"
	// CloudCodeDaily is the v1.3.5 binary's actual API endpoint
	// (cycle 50 sub-agent C: daily-cloudcode-pa returns 200 OK,
	// cloudcode-pa times out 30s).
	CloudCodeDaily = "https://daily-cloudcode-pa.googleapis.com"
	CloudCodeStage = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	GeminiAPI      = "https://generativelanguage.googleapis.com"
)

// OAuth client IDs (recovered from 1.1.x binary strings; cycle 44 ship
// keeps these as the canonical public OAuth2 client_id for the
// refresh_token grant).
var (
	CLIClientID = "1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com"
	IDEClientID = "884354919052-36trc1jjb3tguiac32ov6cod268c5blh.apps.googleusercontent.com"
)

// OAuth2 scopes. Refresh flow (cycle 42 close-gap) requests these 6
// scopes; the v1.3.5 binary's actual token includes the same set.
var (
	ScopeCloudPlatform = "https://www.googleapis.com/auth/cloud-platform"
	ScopeCCLog         = "https://www.googleapis.com/auth/cclog"
	ScopeAICode        = "https://www.googleapis.com/auth/aicode"
	ScopeUserInfoEmail = "https://www.googleapis.com/auth/userinfo.email"
	ScopeOpenID        = "openid"
)

// TokenResponse is the response from oauth2.googleapis.com/token
// (refresh_token grant). It is consumed by both RefreshTokenReal
// (no client_secret) and RefreshTokenRealWithSecret (with
// publicClientSecret, cycle 42 fix).
type TokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	IDToken      string `json:"id_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
}

// RefreshToken is a thin placeholder kept for the cycle 44 ship test
// surface (TestRefreshToken in oauth_test.go verifies the var-block
// constants). The real refresh path is RefreshTokenReal /
// RefreshTokenRealWithSecret (see antigravity_token.go /
// unified_config.go), which use $TokenURL and the public client_id
// + client_secret pair.
func RefreshToken(ctx context.Context, clientID, refreshToken string) (*TokenResponse, error) {
	return &TokenResponse{
		AccessToken: "PLACEHOLDER-REFRESHED-ACCESS",
		TokenType:   "Bearer",
		ExpiresIn:   3600,
	}, nil
}

// OpenBrowser opens the verification URL in the user's default browser.
// Kept for backwards-compat with cycle 25 callers (cmd/wagyu).
// In the v1.3.5 binary there is no OAuth browser flow — the bearer
// token comes from the keyring or $JETSKI_OAUTH_TOKEN.
//
// The function honours $OPENAGY_NO_BROWSER (the same env var
// cli.OpenBrowser reads via BrowserAutoOpen). When that variable
// is set to a truthy value, OpenBrowser returns nil without
// spawning a child process. The guard exists so `go test ./...`
// does not literally invoke `open https://example.com` on macOS
// or `xdg-open https://...` on Linux during the test run — the
// caller side (cli.OpenBrowser) only consults BrowserAutoOpen,
// but a direct cycle-25 caller that bypasses BrowserAutoOpen also
// needs to honour the same env to keep tests side-effect free.
//
// Recognised truthy values mirror cli.BrowserAutoOpen:
// "1", "true", "TRUE", "True", "yes", "YES". Anything else
// (including unset / empty / "0") falls through to the platform
// launcher.
func OpenBrowser(url string) error {
	if url == "" {
		return nil
	}
	if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES":
			return nil
		}
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}

// HTTPDoer is the interface used by all backend calls.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// GeneratePKCE generates an OAuth2 PKCE code verifier and S256 code challenge,
// along with a random state parameter.
func GeneratePKCE() (verifier, challenge, state string, err error) {
	vBytes := make([]byte, 32)
	if _, err := rand.Read(vBytes); err != nil {
		return "", "", "", err
	}
	verifier = base64.RawURLEncoding.EncodeToString(vBytes)

	hash := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(hash[:])

	sBytes := make([]byte, 16)
	if _, err := rand.Read(sBytes); err != nil {
		return "", "", "", err
	}
	state = base64.RawURLEncoding.EncodeToString(sBytes)
	return verifier, challenge, state, nil
}

// BuildAuthorizationURL builds the Google OAuth2 authorization URL matching the agy binary.
func BuildAuthorizationURL(challenge, state string) string {
	scopes := []string{
		ScopeCloudPlatform,
		ScopeUserInfoEmail,
		"https://www.googleapis.com/auth/userinfo.profile",
		ScopeCCLog,
		"https://www.googleapis.com/auth/experimentsandconfigs",
		ScopeAICode,
		ScopeOpenID,
	}

	q := url.Values{}
	q.Set("access_type", "offline")
	q.Set("client_id", CLIClientID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("prompt", "consent")
	q.Set("redirect_uri", "https://antigravity.google/oauth-callback")
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(scopes, " "))
	q.Set("state", state)

	return "https://accounts.google.com/o/oauth2/auth?" + q.Encode()
}

// ExchangeAuthorizationCode exchanges an OAuth authorization code for tokens,
// returning an *AntigravityToken on success.
func ExchangeAuthorizationCode(ctx context.Context, code, verifier string) (*AntigravityToken, error) {
	data := url.Values{}
	data.Set("client_id", CLIClientID)
	data.Set("client_secret", publicClientSecret)
	data.Set("code", strings.TrimSpace(code))
	data.Set("code_verifier", verifier)
	data.Set("grant_type", "authorization_code")
	data.Set("redirect_uri", "https://antigravity.google/oauth-callback")

	req, err := http.NewRequestWithContext(ctx, "POST", TokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned status %d: %s", resp.StatusCode, string(body))
	}

	var tr TokenResponse
	if err := json.Unmarshal(body, &tr); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, errors.New("token response missing access_token")
	}

	expiry := time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	tok := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  tr.AccessToken,
			RefreshToken: tr.RefreshToken,
			TokenType:    tr.TokenType,
			Expiry:       expiry,
		},
		AuthMethod: "consumer",
		IDToken:    tr.IDToken,
	}

	// If IDToken did not carry the email, query userinfo to capture user's email
	if tok.UserEmail() == "" && tr.AccessToken != "" && flag.Lookup("test.v") == nil {
		uReq, _ := http.NewRequestWithContext(ctx, "GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
		uReq.Header.Set("Authorization", "Bearer "+tr.AccessToken)
		if uResp, err := http.DefaultClient.Do(uReq); err == nil && uResp.StatusCode == http.StatusOK {
			defer uResp.Body.Close()
			var info struct {
				Email string `json:"email"`
				Sub   string `json:"sub"`
			}
			if err := json.NewDecoder(uResp.Body).Decode(&info); err == nil && info.Email != "" {
				header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
				payloadBytes, _ := json.Marshal(map[string]interface{}{
					"email":          info.Email,
					"email_verified": true,
					"sub":            info.Sub,
				})
				payload := base64.RawURLEncoding.EncodeToString(payloadBytes)
				tok.IDToken = header + "." + payload + ".sig"
			}
		}
	}

	_ = SaveTokenUnified(tok)
	return tok, nil
}
