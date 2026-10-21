// plan_tier.go — the account plan tier the TUI banner shows.
//
// agy 1.4.3 renders the email row as
//
//	navigaid@gmail.com (Google AI Pro)
//
// On a paid account. The string is the loadCodeAssist response's
// `paidTier.name` (the free account's paidTier is "Antigravity Starter
// Quota"). The reference prefers the login keychain token; the on-disk
// token can be stale and resolves to a different account/tier, so this
// client tries the keychain first and only then the unified cascade.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// PlanTierRequest is the metadata block agy 1.4.3 sends to
// v1internal:loadCodeAssist (captured by cmd/agy:
// {"metadata":{"ideType":"ANTIGRAVITY"}}).
type PlanTierRequest struct {
	Metadata struct {
		IDEType string `json:"ideType"`
	} `json:"metadata"`
}

// planTierResponse is the subset of loadCodeAssist needed for the banner.
type planTierResponse struct {
	PaidTier *struct {
		Name string `json:"name"`
	} `json:"paidTier"`
	CurrentTier *struct {
		Name string `json:"name"`
	} `json:"currentTier"`
}

// parsePlanTier extracts the banner tier from a loadCodeAssist body.
// paidTier is authoritative: currentTier is "Antigravity" for both free
// and paid accounts, while paidTier carries the display label.
func parsePlanTier(body []byte) string {
	var resp planTierResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		return ""
	}
	if resp.PaidTier != nil && strings.TrimSpace(resp.PaidTier.Name) != "" {
		return strings.TrimSpace(resp.PaidTier.Name)
	}
	if resp.CurrentTier != nil && strings.TrimSpace(resp.CurrentTier.Name) != "" {
		return strings.TrimSpace(resp.CurrentTier.Name)
	}
	return ""
}

// cloudCodeAccessToken returns the bearer token the reference uses,
// preferring the login keychain. The disk cascade is the fallback.
func cloudCodeAccessToken() (string, error) {
	tok, err := LoadKeychainToken()
	if err != nil || tok == nil || tok.Token.AccessToken == "" {
		tok, err = LoadTokenUnified()
	}
	if err != nil {
		return "", err
	}
	if tok == nil || tok.Token.AccessToken == "" {
		return "", fmt.Errorf("no access token")
	}
	return tok.Token.AccessToken, nil
}

// FetchPlanTier asks daily-cloudcode-pa for the account's paid tier. It
// returns "" when no token is available, the request fails, or the
// response has no tier; callers keep their existing default in that case.
func FetchPlanTier(ctx context.Context) string {
	tok, err := cloudCodeAccessToken()
	if err != nil {
		return ""
	}
	var reqBody PlanTierRequest
	reqBody.Metadata.IDEType = "ANTIGRAVITY"
	body, err := json.Marshal(reqBody)
	if err != nil {
		return ""
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		CloudCodeEndpoint+"/v1internal:loadCodeAssist", bytes.NewReader(body))
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent",
		"antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; auth_method=consumer)")
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return ""
	}
	return parsePlanTier(raw)
}
