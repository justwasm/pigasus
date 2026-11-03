// oauth_pkce_test.go — the browser sign-in's wire bytes.
//
// The PKCE verifier, the S256 challenge, the authorization URL and the
// token exchange are provided by golang.org/x/oauth2 now. The bytes
// the clone puts on the wire must not move: these tests pin them.
package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/oauth2"
)

// TestBuildAuthorizationURL_MatchesTheHandRolledURL pins the
// authorization URL byte for byte. The library builds it now; the
// query it carries has to stay the one the clone has always sent.
func TestBuildAuthorizationURL_MatchesTheHandRolledURL(t *testing.T) {
	verifier, challenge, state, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}

	// The URL the hand-rolled builder produced.
	q := url.Values{}
	q.Set("access_type", "offline")
	q.Set("client_id", CLIClientID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("prompt", "consent")
	q.Set("redirect_uri", oauthRedirectURI())
	q.Set("response_type", "code")
	q.Set("scope", strings.Join(pkceScopes, " "))
	q.Set("state", state)
	want := googleAuthURL() + "?" + q.Encode()

	got := BuildAuthorizationURL(verifier, state)
	if got != want {
		t.Errorf("BuildAuthorizationURL =\n%s\nwant\n%s", got, want)
	}
}

// TestGeneratePKCE_MatchesTheHandRolledPKCE pins the PKCE parameters:
// a 43-character base64url verifier, the S256 challenge derived from
// it, and a 22-character state.
func TestGeneratePKCE_MatchesTheHandRolledPKCE(t *testing.T) {
	verifier, challenge, state, err := GeneratePKCE()
	if err != nil {
		t.Fatalf("GeneratePKCE: %v", err)
	}
	if len(verifier) != 43 {
		t.Errorf("verifier length = %d, want 43 (%q)", len(verifier), verifier)
	}
	if len(state) != 22 {
		t.Errorf("state length = %d, want 22 (%q)", len(state), state)
	}
	if want := oauth2.S256ChallengeFromVerifier(verifier); challenge != want {
		t.Errorf("challenge = %q, want %q", challenge, want)
	}
}

// TestExchangeAuthorizationCode_PostsTheSameForm pins the exchange's
// request body: the six parameters the clone has always posted, with
// the code trimmed.
func TestExchangeAuthorizationCode_PostsTheSameForm(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.x","refresh_token":"1//y","expires_in":3600,"token_type":"Bearer","id_token":"a.b.c"}`))
	}))
	defer srv.Close()

	orig := TokenURL
	TokenURL = srv.URL
	defer func() { TokenURL = orig }()

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ANTIGRAVITY_HOME", home)

	tok, err := ExchangeAuthorizationCode(t.Context(), "  the-code  ", "the-verifier")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode: %v", err)
	}

	want := url.Values{
		"client_id":     {CLIClientID},
		"client_secret": {publicClientSecret},
		"code":          {"the-code"},
		"code_verifier": {"the-verifier"},
		"grant_type":    {"authorization_code"},
		"redirect_uri":  {oauthRedirectURI()},
	}
	if got.Encode() != want.Encode() {
		t.Errorf("exchange form = %q\nwant %q", got.Encode(), want.Encode())
	}

	if tok.Token.AccessToken != "ya29.x" {
		t.Errorf("access token = %q, want ya29.x", tok.Token.AccessToken)
	}
	if tok.Token.RefreshToken != "1//y" {
		t.Errorf("refresh token = %q, want 1//y", tok.Token.RefreshToken)
	}
	if tok.IDToken != "a.b.c" {
		t.Errorf("id token = %q, want a.b.c", tok.IDToken)
	}
}

// TestRefreshTokenGrant_PostsTheSameForm pins the refresh's request
// body: the four parameters the clone has always posted, client_secret
// included (cycle 42: without it the endpoint answers invalid_client).
func TestRefreshTokenGrant_PostsTheSameForm(t *testing.T) {
	var got url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		got = r.Form
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.new","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	orig := TokenURL
	TokenURL = srv.URL
	defer func() { TokenURL = orig }()

	resp, err := RefreshTokenRealWithSecret(t.Context(), CLIClientID, publicClientSecret, "1//refresh")
	if err != nil {
		t.Fatalf("RefreshTokenRealWithSecret: %v", err)
	}

	want := url.Values{
		"client_id":     {CLIClientID},
		"client_secret": {publicClientSecret},
		"refresh_token": {"1//refresh"},
		"grant_type":    {"refresh_token"},
	}
	if got.Encode() != want.Encode() {
		t.Errorf("refresh form = %q\nwant %q", got.Encode(), want.Encode())
	}
	if resp.AccessToken != "ya29.new" {
		t.Errorf("access token = %q, want ya29.new", resp.AccessToken)
	}
	if resp.ExpiresIn != 3600 {
		t.Errorf("expires_in = %d, want 3600", resp.ExpiresIn)
	}
}

// TestRefreshTokenGrant_FailureCarriesTheStatus pins the structural
// half of the swap: a rejected refresh arrives as a
// *oauth2.RetrieveError, so the status and the OAuth `error` field
// travel with the error instead of being scraped out of a message.
func TestRefreshTokenGrant_FailureCarriesTheStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
	}))
	defer srv.Close()

	orig := TokenURL
	TokenURL = srv.URL
	defer func() { TokenURL = orig }()

	_, err := RefreshTokenRealWithSecret(t.Context(), CLIClientID, publicClientSecret, "1//refresh")
	if err == nil {
		t.Fatal("expected the refresh to fail")
	}

	var retrieve *oauth2.RetrieveError
	if !errors.As(err, &retrieve) {
		t.Fatalf("error %v is not a *oauth2.RetrieveError", err)
	}
	if retrieve.Response == nil || retrieve.Response.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %v, want 400", retrieve.Response)
	}
	if retrieve.ErrorCode != "invalid_grant" {
		t.Errorf("error code = %q, want invalid_grant", retrieve.ErrorCode)
	}

	status, oauth, body := parseRefreshError(err)
	if status != http.StatusBadRequest {
		t.Errorf("parseRefreshError status = %d, want 400", status)
	}
	if oauth != "invalid_grant" {
		t.Errorf("parseRefreshError oauth = %q, want invalid_grant", oauth)
	}
	if len(body) == 0 {
		t.Error("parseRefreshError body is empty")
	}
}
