// Package oauthflow implements device and browser OAuth authentication flows
// for providers such as Charm Hyper, GitHub Copilot, and Google Antigravity.
package oauthflow

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
)

// DeviceAuth carries the information presented to the user during device code flow.
type DeviceAuth struct {
	Provider        string `json:"provider"`
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURL string `json:"verification_url"`
	ExpiresIn       int    `json:"expires_in"`
	Interval        int    `json:"interval"`
	DirectToken     string `json:"direct_token,omitempty"` // populated if already authenticated (e.g. keychain)
}

const (
	defaultHTTPTimeout = 30 * time.Second
	githubClientID     = "Iv1.b507a08c87ecfe98"
	userAgent          = "pigasus"
)

// Initiate starts the device authentication flow for the given provider.
func Initiate(ctx context.Context, providerID string) (*DeviceAuth, error) {
	switch strings.ToLower(strings.TrimSpace(providerID)) {
	case "hyper":
		return initiateHyper(ctx)
	case "copilot":
		return initiateCopilot(ctx)
	case "google-antigravity", "antigravity":
		return initiateAntigravity(ctx)
	default:
		return nil, fmt.Errorf("unsupported browser auth provider: %s", providerID)
	}
}

// Poll waits for the user to complete authorization in the browser and returns the token.
func Poll(ctx context.Context, d *DeviceAuth) (string, error) {
	if d.DirectToken != "" {
		return d.DirectToken, nil
	}
	switch strings.ToLower(strings.TrimSpace(d.Provider)) {
	case "hyper":
		return pollHyper(ctx, d.DeviceCode, d.ExpiresIn)
	case "copilot":
		return pollCopilot(ctx, d.DeviceCode, d.Interval, d.ExpiresIn)
	case "google-antigravity", "antigravity":
		return pollAntigravity(ctx)
	default:
		return "", fmt.Errorf("unsupported poll provider: %s", d.Provider)
	}
}

// ---- Hyper Implementation ----------------------------------------------

func initiateHyper(ctx context.Context) (*DeviceAuth, error) {
	apiURL := "https://hyper.charm.land/device/auth"
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "terminal"
	}
	payload := fmt.Sprintf(`{"device_name":"Pigasus (%s)"}`, hostname)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL, strings.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("hyper auth initiate: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("hyper auth returned status %d: %s", resp.StatusCode, string(body))
	}

	var data struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURL string `json:"verification_url"`
		ExpiresIn       int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("unmarshal hyper device response: %w", err)
	}
	if data.ExpiresIn <= 0 {
		data.ExpiresIn = 900
	}
	return &DeviceAuth{
		Provider:        "hyper",
		DeviceCode:      data.DeviceCode,
		UserCode:        data.UserCode,
		VerificationURL: data.VerificationURL,
		ExpiresIn:       data.ExpiresIn,
		Interval:        5,
	}, nil
}

func pollHyper(ctx context.Context, deviceCode string, expiresIn int) (string, error) {
	if expiresIn <= 0 {
		expiresIn = 900
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(expiresIn)*time.Second)
	defer cancel()

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	pollURL := fmt.Sprintf("https://hyper.charm.land/device/auth/%s", deviceCode)
	client := &http.Client{Timeout: defaultHTTPTimeout}

	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, pollURL, nil)
			if err != nil {
				return "", err
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("User-Agent", userAgent)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				continue
			}

			var tokenResp struct {
				RefreshToken     string `json:"refresh_token"`
				Error            string `json:"error"`
				ErrorDescription string `json:"error_description"`
			}
			if err := json.Unmarshal(body, &tokenResp); err != nil {
				continue
			}

			if tokenResp.RefreshToken != "" {
				// Exchange refresh token for an access token
				return exchangeHyperToken(ctx, tokenResp.RefreshToken)
			}
			if tokenResp.Error != "" && tokenResp.Error != "authorization_pending" {
				return "", errors.New(tokenResp.ErrorDescription)
			}
		}
	}
}

func exchangeHyperToken(ctx context.Context, refreshToken string) (string, error) {
	reqBody, _ := json.Marshal(map[string]string{
		"refresh_token": refreshToken,
	})
	url := "https://hyper.charm.land/token/exchange"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return refreshToken, nil // fallback to refresh token
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return refreshToken, nil
	}
	defer resp.Body.Close()

	var data struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && data.AccessToken != "" {
		return data.AccessToken, nil
	}
	return refreshToken, nil
}

// ---- Copilot Implementation --------------------------------------------

func initiateCopilot(ctx context.Context) (*DeviceAuth, error) {
	endpoint := "https://github.com/login/device/code"
	data := url.Values{}
	data.Set("client_id", githubClientID)
	data.Set("scope", "read:user")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("github device auth failed: status %d %s", resp.StatusCode, string(b))
	}

	var res struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, err
	}
	interval := res.Interval
	if interval < 5 {
		interval = 5
	}
	return &DeviceAuth{
		Provider:        "copilot",
		DeviceCode:      res.DeviceCode,
		UserCode:        res.UserCode,
		VerificationURL: res.VerificationURI,
		ExpiresIn:       res.ExpiresIn,
		Interval:        interval,
	}, nil
}

func pollCopilot(ctx context.Context, deviceCode string, interval, expiresIn int) (string, error) {
	if interval < 5 {
		interval = 5
	}
	if expiresIn <= 0 {
		expiresIn = 900
	}
	deadline := time.Now().Add(time.Duration(expiresIn) * time.Second)
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()

	client := &http.Client{Timeout: defaultHTTPTimeout}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			data := url.Values{}
			data.Set("client_id", githubClientID)
			data.Set("device_code", deviceCode)
			data.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

			req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(data.Encode()))
			if err != nil {
				continue
			}
			req.Header.Set("Accept", "application/json")
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			req.Header.Set("User-Agent", userAgent)

			resp, err := client.Do(req)
			if err != nil {
				continue
			}
			var result struct {
				AccessToken string `json:"access_token"`
				Error       string `json:"error"`
			}
			_ = json.NewDecoder(resp.Body).Decode(&result)
			resp.Body.Close()

			if result.AccessToken != "" {
				// Exchange for Copilot internal token
				copilotToken, err := getCopilotInternalToken(ctx, result.AccessToken)
				if err == nil && copilotToken != "" {
					return copilotToken, nil
				}
				return result.AccessToken, nil
			}
			if result.Error == "authorization_pending" {
				continue
			}
			if result.Error == "slow_down" {
				interval += 5
				ticker.Reset(time.Duration(interval) * time.Second)
				continue
			}
			if result.Error != "" {
				return "", fmt.Errorf("copilot authorization failed: %s", result.Error)
			}
		}
	}
	return "", fmt.Errorf("copilot authorization timed out")
}

func getCopilotInternalToken(ctx context.Context, ghToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/copilot_internal/v2/token", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "token "+ghToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	client := &http.Client{Timeout: defaultHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var data struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&data); err == nil && data.Token != "" {
		return data.Token, nil
	}
	return ghToken, nil
}

// ---- Google Antigravity Implementation ---------------------------------

func initiateAntigravity(ctx context.Context) (*DeviceAuth, error) {
	v, _, s, err := auth.GeneratePKCE()
	if err != nil {
		return nil, fmt.Errorf("generate pkce: %w", err)
	}
	authURL := auth.BuildAuthorizationURL(v, s)

	return &DeviceAuth{
		Provider:        "google-antigravity",
		DeviceCode:      v, // PKCE verifier stored in DeviceCode for ExchangeCode
		UserCode:        "(paste authorization code with Cmd+V / Ctrl+V)",
		VerificationURL: authURL,
		ExpiresIn:       600,
		Interval:        1,
	}, nil
}

func pollAntigravity(ctx context.Context) (string, error) {
	<-ctx.Done()
	return "", ctx.Err()
}

// ExchangeCode exchanges an authorization code for a token (e.g. for Google PKCE code flow).
func ExchangeCode(ctx context.Context, d *DeviceAuth, code string) (string, error) {
	if d == nil {
		return "", errors.New("nil device auth")
	}
	switch strings.ToLower(strings.TrimSpace(d.Provider)) {
	case "google-antigravity", "antigravity":
		code = strings.TrimSpace(code)
		if code == "" {
			return "", errors.New("empty authorization code")
		}
		tok, err := auth.ExchangeAuthorizationCode(ctx, code, d.DeviceCode)
		if err != nil {
			return "", fmt.Errorf("exchange authorization code: %w", err)
		}
		if tok != nil && tok.UserEmail() != "" {
			_, _, _ = accounts.Register(tok)
		}
		if tok == nil || tok.Token.AccessToken == "" {
			return "", errors.New("token exchange returned empty access token")
		}
		return tok.Token.AccessToken, nil
	default:
		return "", fmt.Errorf("code exchange not supported for provider: %s", d.Provider)
	}
}
