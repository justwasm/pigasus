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
	"encoding/base64"
	"encoding/json"
	"flag"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/justwasm/pigasus/endpoints"
)

// OAuth2 endpoints. TokenURL is a var (not a const) so the
// oauth_test.go test seam can stub it.
//
// TokenURL is empty by default: the live endpoint comes from the
// endpoint resolver (endpoints.Current().OAuth2Token), which the
// configuration file may point at a local service. A non-empty value
// wins over the resolver — that is what the test seam is.
var (
	TokenURL     = "" // var for test seam
	IamCredsURL  = "https://iamcredentials.googleapis.com/v1/projects/-/serviceAccounts"
	CloudCodeAPI = endpoints.DefaultCloudCodeProd
	AICodeAPI    = "https://aicode.googleapis.com"
	// CloudCodeDaily is the v1.3.5 binary's actual API endpoint
	// (cycle 50 sub-agent C: daily-cloudcode-pa returns 200 OK,
	// cloudcode-pa times out 30s).
	CloudCodeDaily = endpoints.DefaultCloudCode
	CloudCodeStage = "https://autopush-cloudcode-pa.sandbox.googleapis.com"
	GeminiAPI      = endpoints.DefaultGenerativeLanguage
)

// tokenURL returns the OAuth2 token endpoint the refresh grant posts to.
//
// The package var is the test seam: a test that points it at an
// httptest server wins over the configured endpoint. Otherwise the
// endpoint resolver answers, and the configuration file may point it
// at a local service.
func tokenURL() string {
	if u := strings.TrimSpace(TokenURL); u != "" {
		return u
	}
	return endpoints.Current().OAuth2Token
}

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

// The browser sign-in's fixed OAuth2 parameters.
//
// googleAuthURL is the consent page, oauthRedirectURI the redirect
// Google sends the authorization code back to. Both are read from the
// endpoint resolver on every use, so the configuration file may point
// the browser sign-in at a local service.
func googleAuthURL() string    { return endpoints.Current().OAuth2Auth }
func oauthRedirectURI() string { return endpoints.Current().OAuthRedirect }

// pkceScopes is the scope list the browser sign-in requests, in the
// order the clone has always sent it — the order is part of the URL
// the login test pins.
var pkceScopes = []string{
	ScopeCloudPlatform,
	ScopeUserInfoEmail,
	"https://www.googleapis.com/auth/userinfo.profile",
	ScopeCCLog,
	"https://www.googleapis.com/auth/experimentsandconfigs",
	ScopeAICode,
	ScopeOpenID,
}

// oauthConfig builds the oauth2.Config the browser sign-in uses.
//
// The endpoint pins AuthStyleInParams: the CLI is a public OAuth2
// client and Google wants client_id / client_secret in the form body.
// x/oauth2's default (AuthStyleAutoDetect) would probe with the
// secret in an Authorization header first and only fall back to the
// form afterwards, which changes the bytes on the wire.
//
// The token URL is read from the package var on every call, so the
// test seam (TokenURL) keeps working.
func oauthConfig(clientID, clientSecret string, scopes []string) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  oauthRedirectURI(),
		Scopes:       scopes,
		Endpoint: oauth2.Endpoint{
			AuthURL:   googleAuthURL(),
			TokenURL:  tokenURL(),
			AuthStyle: oauth2.AuthStyleInParams,
		},
	}
}

// GeneratePKCE generates an OAuth2 PKCE code verifier and S256 code challenge,
// along with a random state parameter.
//
// The verifier and the challenge come from golang.org/x/oauth2:
// GenerateVerifier draws the 32 octets RFC 7636 §4.1 asks for and
// S256ChallengeFromVerifier derives the challenge — both base64url
// without padding, i.e. the very bytes the hand-rolled version
// produced. The state stays the clone's own (x/oauth2 does not model
// one): 16 random bytes, base64url without padding.
func GeneratePKCE() (verifier, challenge, state string, err error) {
	verifier = oauth2.GenerateVerifier()
	challenge = oauth2.S256ChallengeFromVerifier(verifier)

	sBytes := make([]byte, 16)
	if _, err := rand.Read(sBytes); err != nil {
		return "", "", "", err
	}
	state = base64.RawURLEncoding.EncodeToString(sBytes)
	return verifier, challenge, state, nil
}

// BuildAuthorizationURL builds the Google OAuth2 authorization URL matching the agy binary.
//
// The URL carries the same nine query parameters as it always did:
// access_type=offline, client_id, code_challenge,
// code_challenge_method=S256, prompt=consent, redirect_uri,
// response_type=code, scope, state.
//
// The challenge is derived from the verifier by the library
// (oauth2.S256ChallengeOption), so the caller passes the verifier —
// the half of the PKCE pair the browser never sees.
func BuildAuthorizationURL(verifier, state string) string {
	cfg := oauthConfig(CLIClientID, publicClientSecret, pkceScopes)
	return cfg.AuthCodeURL(state,
		oauth2.S256ChallengeOption(verifier),
		oauth2.AccessTypeOffline,
		oauth2.ApprovalForce,
	)
}

// ExchangeAuthorizationCode exchanges an OAuth authorization code for tokens,
// returning an *AntigravityToken on success.
//
// The exchange posts the same form the clone has always posted —
// client_id, client_secret, code, code_verifier, grant_type and
// redirect_uri — and a failure comes back as a *oauth2.RetrieveError,
// which carries the endpoint's status and its RFC 6749 `error` field.
func ExchangeAuthorizationCode(ctx context.Context, code, verifier string) (*AntigravityToken, error) {
	cfg := oauthConfig(CLIClientID, publicClientSecret, pkceScopes)
	tok, err := cfg.Exchange(ctx, strings.TrimSpace(code), oauth2.VerifierOption(verifier))
	if err != nil {
		return nil, err
	}

	expiry := time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second).UTC().Format(time.RFC3339)
	if !tok.Expiry.IsZero() {
		expiry = tok.Expiry.UTC().Format(time.RFC3339)
	}
	out := &AntigravityToken{
		Token: AntigravityAccessToken{
			AccessToken:  tok.AccessToken,
			RefreshToken: tok.RefreshToken,
			TokenType:    tok.TokenType,
			Expiry:       expiry,
		},
		AuthMethod: "consumer",
		IDToken:    tokenExtraString(tok, "id_token"),
	}

	if out.UserEmail() == "" && tok.AccessToken != "" && flag.Lookup("test.v") == nil {
		populateUserEmailFromUserInfo(ctx, out, tok.AccessToken)
	}

	_ = SaveTokenUnified(out)
	return out, nil
}

// tokenExtraString reads a string field out of an oauth2.Token's raw
// response — the fields x/oauth2 does not model (id_token, scope).
func tokenExtraString(tok *oauth2.Token, key string) string {
	if tok == nil {
		return ""
	}
	s, _ := tok.Extra(key).(string)
	return s
}

func populateUserEmailFromUserInfo(ctx context.Context, tok *AntigravityToken, accessToken string) {
	uReq, _ := http.NewRequestWithContext(ctx, "GET", endpoints.Current().UserInfo, nil)
	uReq.Header.Set("Authorization", "Bearer "+accessToken)
	uResp, err := http.DefaultClient.Do(uReq)
	if err != nil || uResp.StatusCode != http.StatusOK {
		return
	}
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
