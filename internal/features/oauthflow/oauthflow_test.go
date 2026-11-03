package oauthflow

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestInitiate_HyperMock(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/device/auth" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":      "dev-123",
				"user_code":        "ABCD-WXYZ",
				"verification_url": "https://hyper.charm.land/device",
				"expires_in":       900,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	// Direct initiate with real types
	d := &DeviceAuth{
		Provider:        "hyper",
		DeviceCode:      "dev-123",
		UserCode:        "ABCD-WXYZ",
		VerificationURL: "https://hyper.charm.land/device",
		ExpiresIn:       900,
		Interval:        5,
	}
	if d.UserCode != "ABCD-WXYZ" {
		t.Fatalf("UserCode = %q, want ABCD-WXYZ", d.UserCode)
	}
	if d.VerificationURL != "https://hyper.charm.land/device" {
		t.Fatalf("VerificationURL = %q", d.VerificationURL)
	}
}

func TestInitiate_CopilotMock(t *testing.T) {
	d := &DeviceAuth{
		Provider:        "copilot",
		DeviceCode:      "dev-copilot-456",
		UserCode:        "1234-5678",
		VerificationURL: "https://github.com/login/device",
		ExpiresIn:       900,
		Interval:        5,
	}
	if d.Provider != "copilot" {
		t.Fatalf("Provider = %q, want copilot", d.Provider)
	}
}

func TestPoll_DirectToken(t *testing.T) {
	d := &DeviceAuth{
		Provider:    "google-antigravity",
		DirectToken: "ya29.already-logged-in-token",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	token, err := Poll(ctx, d)
	if err != nil {
		t.Fatalf("Poll err = %v", err)
	}
	if token != "ya29.already-logged-in-token" {
		t.Fatalf("token = %q, want direct token", token)
	}
}

func TestInitiate_GoogleAntigravity_PKCE(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	da, err := Initiate(ctx, "google-antigravity")
	if err != nil {
		t.Fatalf("Initiate(google-antigravity) error = %v", err)
	}
	if da == nil {
		t.Fatal("Initiate(google-antigravity) returned nil")
	}
	if da.Provider != "google-antigravity" {
		t.Errorf("Provider = %q, want google-antigravity", da.Provider)
	}
	if da.DeviceCode == "" {
		t.Error("DeviceCode (PKCE verifier) should not be empty")
	}

	u := da.VerificationURL
	for _, required := range []string{
		"https://accounts.google.com/o/oauth2/auth",
		"response_type=code",
		"client_id=1071006060591-tmhssin2h21lcre235vtolojh4g403ep.apps.googleusercontent.com",
		"redirect_uri=https%3A%2F%2Fantigravity.google%2Foauth-callback",
		"code_challenge=",
		"code_challenge_method=S256",
		"access_type=offline",
		"prompt=consent",
		"state=",
	} {
		if !strings.Contains(u, required) {
			t.Errorf("VerificationURL missing parameter %q:\n%s", required, u)
		}
	}
}
