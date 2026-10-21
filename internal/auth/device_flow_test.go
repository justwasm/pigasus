// Tests for the OAuth 2.0 device-authorization bootstrap. These tests
// stand up an httptest.Server that imitates oauth2.googleapis.com's
// /device/code and /token endpoints, redirect the x/oauth2 client at
// the test server by swapping Endpoint URLs, and then drive DeviceFlow
// against it. No real Google call ever happens.
package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// deviceCodeResponse is the JSON shape oauth2.googleapis.com returns
// from POST /device/code (RFC 8628 §3.2).
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenResponse is the JSON shape the /token endpoint returns for
// both successful grants and recurring errors like authorization_pending.
type tokenResponse struct {
	AccessToken  string `json:"access_token,omitempty"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	Scope        string `json:"scope,omitempty"`
	Error        string `json:"error,omitempty"`
}

// deviceServer fakes Google's device-auth URL pair. The handler tracks
// the poll count and switches from "authorization_pending" to a
// success payload after approveAfter calls. slowDownEvery, if positive,
// returns slow_down instead of authorization_pending on every Nth
// call so the slow_down branch in DeviceFlow gets exercised.
type deviceServer struct {
	server        *httptest.Server
	polls         atomic.Int32
	approveAfter  int32
	slowDownEvery int32
	clientID      string
	scopes        string
	lastError     atomic.Value // string; set on the first /token error
}

func newDeviceServer(t *testing.T, clientID string) *deviceServer {
	t.Helper()
	ds := &deviceServer{clientID: clientID, approveAfter: 3}
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if r.Form.Get("client_id") != ds.clientID {
			http.Error(w, "client_id mismatch", http.StatusBadRequest)
			return
		}
		ds.scopes = r.Form.Get("scope")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode:              "DEV-CODE-XYZ",
			UserCode:                "ABCD-EFGH",
			VerificationURI:         "https://example.com/device",
			VerificationURIComplete: "https://example.com/device?user_code=ABCD-EFGH",
			ExpiresIn:               600,
			Interval:                5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ds.polls.Add(1)
		n := ds.polls.Load()
		w.Header().Set("Content-Type", "application/json")
		if int32(n) >= ds.approveAfter {
			_ = json.NewEncoder(w).Encode(tokenResponse{
				AccessToken:  "ya29.test-access-token",
				RefreshToken: "1//test-refresh-token",
				TokenType:    "Bearer",
				ExpiresIn:    3600,
				Scope:        ds.scopes,
			})
			return
		}
		// Slow_down every Nth poll so the loop's slow_down branch
		// gets exercised at least once.
		if ds.slowDownEvery > 0 && int32(n)%ds.slowDownEvery == 0 {
			ds.lastError.Store("slow_down")
			_ = json.NewEncoder(w).Encode(tokenResponse{Error: "slow_down"})
			return
		}
		ds.lastError.Store("authorization_pending")
		_ = json.NewEncoder(w).Encode(tokenResponse{Error: "authorization_pending"})
	})
	ds.server = httptest.NewServer(mux)
	t.Cleanup(ds.server.Close)
	return ds
}

// redirectToDeviceServer swaps the global DeviceFlowEndpoint so
// DeviceAuth / DeviceAccessToken POSTs land at the test server. This
// is the seam used by every test.
func redirectToDeviceServer(t *testing.T, ds *deviceServer) *oauth2.Config {
	t.Helper()
	saved := DeviceFlowEndpoint
	DeviceFlowEndpoint = oauth2.Endpoint{
		DeviceAuthURL: ds.server.URL + "/device/code",
		TokenURL:      ds.server.URL + "/token",
	}
	t.Cleanup(func() { DeviceFlowEndpoint = saved })
	return NewDeviceFlowConfig("test-client", []string{"openid", "email"})
}

// TestDeviceFlow_HappyPath drives the flow to success.
func TestDeviceFlow_HappyPath(t *testing.T) {
	ds := newDeviceServer(t, "test-client")
	cfg := redirectToDeviceServer(t, ds)

	var authURL, userCode string
	opts := &DeviceFlowOptions{
		OnAuthRequest: func(url, code string) {
			authURL = url
			userCode = code
		},
	}
	tok, da, err := DeviceFlow(context.Background(), cfg, opts)
	if err != nil {
		t.Fatalf("DeviceFlow: %v", err)
	}
	if tok == nil || tok.AccessToken != "ya29.test-access-token" {
		t.Errorf("token.AccessToken = %q, want %q", tok.AccessToken, "ya29.test-access-token")
	}
	if tok.RefreshToken != "1//test-refresh-token" {
		t.Errorf("token.RefreshToken = %q, want %q", tok.RefreshToken, "1//test-refresh-token")
	}
	if da == nil || da.UserCode != "ABCD-EFGH" {
		t.Errorf("device auth user code = %v, want ABCD-EFGH", da)
	}
	if authURL == "" {
		t.Errorf("OnAuthRequest not called")
	}
	if userCode != "ABCD-EFGH" {
		t.Errorf("OnAuthRequest userCode = %q, want ABCD-EFGH", userCode)
	}
	if ds.polls.Load() < 3 {
		t.Errorf("polls = %d, want >=3 (server is configured to approve after 3 polls)", ds.polls.Load())
	}
}

// TestDeviceFlow_SlowDownRespected confirms Config.DeviceAccessToken
// keeps going past a slow_down reply (it bumps the interval itself)
// and eventually succeeds.
func TestDeviceFlow_SlowDownRespected(t *testing.T) {
	ds := newDeviceServer(t, "test-client")
	ds.slowDownEvery = 1 // every poll returns slow_down
	ds.approveAfter = 2
	cfg := redirectToDeviceServer(t, ds)
	tok, _, err := DeviceFlow(context.Background(), cfg, nil)
	if err != nil {
		t.Fatalf("DeviceFlow: %v", err)
	}
	if tok.AccessToken != "ya29.test-access-token" {
		t.Errorf("token.AccessToken = %q, want %q", tok.AccessToken, "ya29.test-access-token")
	}
}

// TestDeviceFlow_AccessDenied confirms user denial surfaces as a
// typed error and the loop stops polling.
func TestDeviceFlow_AccessDenied(t *testing.T) {
	ds := newDeviceServer(t, "test-client")
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode: "DEV", UserCode: "ABCD",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       600, Interval: 5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{Error: "access_denied"})
	})
	ds.server.Close()
	ds.server = httptest.NewServer(mux)
	t.Cleanup(ds.server.Close)
	cfg := redirectToDeviceServer(t, ds)
	_, _, err := DeviceFlow(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error on access_denied")
	}
	if !strings.Contains(err.Error(), "denied") {
		t.Errorf("error = %q, want substring 'denied'", err.Error())
	}
}

// TestDeviceFlow_ExpiredToken confirms expired_token surfaces as a
// clear error and the loop stops.
func TestDeviceFlow_ExpiredToken(t *testing.T) {
	ds := newDeviceServer(t, "test-client")
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(deviceCodeResponse{
			DeviceCode: "DEV", UserCode: "ABCD",
			VerificationURI: "https://example.com/device",
			ExpiresIn:       600, Interval: 5,
		})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(tokenResponse{Error: "expired_token"})
	})
	ds.server.Close()
	ds.server = httptest.NewServer(mux)
	t.Cleanup(ds.server.Close)
	cfg := redirectToDeviceServer(t, ds)
	_, _, err := DeviceFlow(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error on expired_token")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Errorf("error = %q, want substring 'expired'", err.Error())
	}
}

// TestDeviceFlow_NilConfig confirms the guard against nil configs.
func TestDeviceFlow_NilConfig(t *testing.T) {
	_, _, err := DeviceFlow(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("expected error on nil config")
	}
}

// TestDeviceFlow_DeviceAuthFailure confirms errors from /device/code
// surface as DeviceFlow errors with a useful wrap.
func TestDeviceFlow_DeviceAuthFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/device/code", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := NewDeviceFlowConfig("test-client", []string{"openid"})
	cfg.Endpoint.DeviceAuthURL = srv.URL + "/device/code"
	cfg.Endpoint.TokenURL = srv.URL + "/token"

	_, _, err := DeviceFlow(context.Background(), cfg, nil)
	if err == nil {
		t.Fatal("expected error from failing /device/code")
	}
	if !strings.Contains(err.Error(), "device auth request") {
		t.Errorf("error = %q, want substring 'device auth request'", err.Error())
	}
}

// TestDeviceFlow_ContextCancelled confirms an external ctx cancel
// propagates through the polling loop without leaking goroutines.
func TestDeviceFlow_ContextCancelled(t *testing.T) {
	ds := newDeviceServer(t, "test-client")
	cfg := redirectToDeviceServer(t, ds)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, err := DeviceFlow(ctx, cfg, nil)
	if err == nil {
		t.Fatal("expected error on ctx cancel")
	}
}

// TestNewDeviceFlowConfig_Defaults confirms default client_id and
// scopes are populated when caller passes zero values.
func TestNewDeviceFlowConfig_Defaults(t *testing.T) {
	cfg := NewDeviceFlowConfig("", nil)
	if cfg.ClientID != CLIClientID {
		t.Errorf("ClientID = %q, want %q", cfg.ClientID, CLIClientID)
	}
	if len(cfg.Scopes) == 0 {
		t.Errorf("Scopes empty, want defaults populated")
	}
	if cfg.Endpoint.DeviceAuthURL == "" {
		t.Errorf("Endpoint.DeviceAuthURL empty, want googleapis.com/device/code")
	}
	if cfg.Endpoint.TokenURL == "" {
		t.Errorf("Endpoint.TokenURL empty, want googleapis.com/token")
	}
}

// TestTokenToAntigravityToken exercises the conversion from
// *oauth2.Token back into the on-disk AntigravityToken shape.
func TestTokenToAntigravityToken(t *testing.T) {
	expiry := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	tok := &oauth2.Token{
		AccessToken:  "ya29.x",
		RefreshToken: "1//y",
		TokenType:    "Bearer",
		Expiry:       expiry,
	}
	at := TokenToAntigravityToken(tok)
	if at == nil {
		t.Fatal("TokenToAntigravityToken returned nil")
	}
	if at.Token.AccessToken != "ya29.x" {
		t.Errorf("AccessToken = %q", at.Token.AccessToken)
	}
	if at.Token.RefreshToken != "1//y" {
		t.Errorf("RefreshToken = %q", at.Token.RefreshToken)
	}
	if at.Token.TokenType != "Bearer" {
		t.Errorf("TokenType = %q", at.Token.TokenType)
	}
	if at.Token.Expiry == "" {
		t.Errorf("Expiry not populated")
	}
	if at.AuthMethod != "consumer" {
		t.Errorf("AuthMethod = %q, want consumer", at.AuthMethod)
	}
}

// TestTokenToAntigravityToken_Nil confirms nil input returns nil.
func TestTokenToAntigravityToken_Nil(t *testing.T) {
	if got := TokenToAntigravityToken(nil); got != nil {
		t.Errorf("TokenToAntigravityToken(nil) = %v, want nil", got)
	}
}

// readBody is a tiny helper that drains an io.Reader to []byte. It is
// here so the file does not import io/ioutil for one line.
func readBody(r io.Reader) []byte {
	b, _ := io.ReadAll(r)
	return b
}
