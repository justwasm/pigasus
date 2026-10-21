// load_code_assist_test.go — verifies the recovered HTTPS client can talk
// to daily-cloudcode-pa.googleapis.com using the same OAuth token the v1.3.5
// binary uses. Skips cleanly if the on-disk token is missing or sandbox is offline.
package auth

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"testing"
	"time"
)

// proxyFromEnv returns the proxy URL the sandbox uses (ALL_PROXY) or nil.
func proxyFromEnv() *url.URL {
	for _, k := range []string{"ALL_PROXY", "all_proxy"} {
		if v := os.Getenv(k); v != "" {
			// Strip socks5h:// or http:// prefix as Go accepts both
			if u, err := url.Parse(v); err == nil {
				return u
			}
		}
	}
	return nil
}

// newTestHTTPClient returns an HTTP client that respects the sandbox's
// ALL_PROXY env. Go's net/http doesn't auto-detect ALL_PROXY, so we
// wire it up explicitly.
func newTestHTTPClient() *http.Client {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		// Also fall back to ALL_PROXY if not in the standard list
	}
	if proxyFromEnv() != nil && os.Getenv("HTTP_PROXY") == "" &&
		os.Getenv("HTTPS_PROXY") == "" {
		// Manually set the env so http.ProxyFromEnvironment picks it up
		os.Setenv("HTTPS_PROXY", os.Getenv("ALL_PROXY"))
		defer os.Unsetenv("HTTPS_PROXY")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}
}

func TestCloudCodeAPIClient_LoadCodeAssist_Live(t *testing.T) {
	if os.Getenv("SKIP_LIVE_CLOUDCODE") == "1" || os.Getenv("GO_TEST_NO_NET") == "1" {
		t.Skip("SKIP_LIVE_CLOUDCODE=1 or GO_TEST_NO_NET=1")
	}
	tok, err := LoadAntigravityToken()
	if err != nil {
		t.Skipf("no token on disk: %v", err)
	}
	c := NewCloudCodeAPIClient(tok)
	c.HTTP = newTestHTTPClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	resp, err := c.LoadCodeAssist(ctx)
	if err != nil {
		t.Fatalf("LoadCodeAssist: %v", err)
	}
	if len(resp.AllowedTiers) == 0 {
		t.Fatal("allowedTiers is empty")
	}
	for i, tier := range resp.AllowedTiers {
		t.Logf("tier[%d]: id=%s name=%s default=%t gcpTos=%t",
			i, tier.ID, tier.Name, tier.IsDefault, tier.UsesGcpTos)
		if tier.ID == "" {
			t.Errorf("tier[%d].id is empty", i)
		}
	}
	defaultCount := 0
	for _, tier := range resp.AllowedTiers {
		if tier.IsDefault {
			defaultCount++
		}
	}
	if defaultCount == 0 {
		t.Error("no tier marked as default")
	}
}

func TestTokenValidatesViaUserinfo(t *testing.T) {
	if os.Getenv("SKIP_LIVE_OAUTH") == "1" || os.Getenv("GO_TEST_NO_NET") == "1" {
		t.Skip("SKIP_LIVE_OAUTH=1 or GO_TEST_NO_NET=1")
	}
	tok, err := LoadAntigravityToken()
	if err != nil {
		t.Skipf("no token on disk: %v", err)
	}
	cli := newTestHTTPClient()
	req, _ := http.NewRequest("GET", "https://www.googleapis.com/oauth2/v3/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tok.Token.AccessToken)
	resp, err := cli.Do(req)
	if err != nil {
		t.Skipf("sandbox offline: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("userinfo HTTP %d (token may be revoked or scope wrong)", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var info struct {
		Sub   string `json:"sub"`
		Email string `json:"email"`
		Name  string `json:"name"`
	}
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("parse: %v body=%s", err, string(body))
	}
	if info.Sub == "" {
		t.Fatal("userinfo.sub is empty")
	}
	t.Logf("token valid: sub=%s email=%s name=%s", info.Sub, info.Email, info.Name)
}
