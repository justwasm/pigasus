// Package auth: real HTTPS client for the daily-cloudcode-pa endpoint.
//
// Cycle 50: User verified the v1.3.5 binary reaches the daily endpoint
// (NOT the production endpoint). Cycle 42 close-gap empirically showed
// daily returns 200 OK in the user's environment while production
// times out (30s). The recovered version uses plain HTTPS (not mTLS)
// and reads the same OAuth file as the v1.3.5 binary.
//
// Reference 1.3.5 binary strings:
//   "google3/third_party/jetski/language_server/code_assist_client/codeassistclient.(*CodeAssistClient).streamGenerateContentFromCCPA"
//   "https://daily-cloudcode-pa.googleapis.com"
//   "v1internal:loadCodeAssist"
//   "v1internal:streamGenerateContent"
//   "v1internal:fetchAvailableModels"
//
// Endpoints verified working (2026-10-14):
//   - v1internal:loadCodeAssist        POST → allowedTiers JSON
//   - v1internal:onboardUser           POST → 400 (needs schema)
//   - v1internal:streamGenerateContent POST → 400 (needs schema)
//   - v1internal:fetchAvailableModels  POST → models catalog JSON
//
// Endpoints returning 403 (sandbox IP / scopes):
//   - generativelanguage.googleapis.com (cloud-platform scope insufficient)
//
// The production endpoint (Cycle 50 CloudCodeEndpointFallback) is
// reserved for ops-driven fallback when the daily endpoint is down.
// It is NOT in the v1.3.5 binary code path.
package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/justwasm/pigasus/endpoints"
)

// CloudCodeEndpoint is the v1.3.5 binary's default API endpoint.
// Source: 1.3.5 binary strings: "https://daily-cloudcode-pa.googleapis.com"
// Cycle 42 close-gap: daily returns 200 OK, production times out (30s)
// in the user's sandbox. The v1.3.5 binary is on the daily endpoint.
//
// The live endpoint is endpoints.Current().CloudCode — the configuration
// file may point it at a local service, and this constant stays the name
// of what it falls back to.
const CloudCodeEndpoint = endpoints.DefaultCloudCode

// CloudCodeEndpointFallback is the production endpoint. Use this when
// the daily endpoint is unreachable and we need to fall back to
// production. Cycle 50 reserved constant for ops-driven fallback
// (NOT in the v1.3.5 binary code path).
//
// The live endpoint is endpoints.Current().CloudCodeProd.
const CloudCodeEndpointFallback = endpoints.DefaultCloudCodeProd

// CloudCodeAPIClient is a thin HTTPS client for the daily endpoint.
// It uses the same OAuth Bearer token the v1.3.5 binary reads from
// ~/.gemini/antigravity-cli/antigravity-oauth-token.
type CloudCodeAPIClient struct {
	Endpoint string
	Token    *AntigravityToken
	HTTP     *http.Client
}

// NewCloudCodeAPIClient constructs a client using the on-disk token.
func NewCloudCodeAPIClient(tok *AntigravityToken) *CloudCodeAPIClient {
	return &CloudCodeAPIClient{
		Endpoint: endpoints.Current().CloudCode,
		Token:    tok,
		HTTP:     &http.Client{Timeout: 30 * time.Second},
	}
}

// IdeMetadata is the metadata block v4 binary attaches to every
// daily-cloudcode-pa call. Source: codeassistclient.(*CodeAssistClient).
type IdeMetadata struct {
	IDEType     string `json:"ideType"`     // "IDE_UNSPECIFIED" / "VSCODE" / "CLOUD_SHELL" / ...
	Platform    string `json:"platform"`    // "PLATFORM_UNSPECIFIED" / "MACOS" / "WINDOWS" / "LINUX" / ...
	PluginType  string `json:"pluginType"`  // "GEMINI" / "ANTIGRAVITY"
	PluginVersion string `json:"pluginVersion,omitempty"`
}

// DefaultIdeMetadata returns the metadata v4 binary uses for CLI invocations.
func DefaultIdeMetadata() IdeMetadata {
	return IdeMetadata{
		IDEType:    "IDE_UNSPECIFIED",
		Platform:   "PLATFORM_UNSPECIFIED",
		PluginType: "GEMINI",
	}
}

// LoadCodeAssistRequest is the v1internal:loadCodeAssist request body.
type LoadCodeAssistRequest struct {
	Metadata IdeMetadata `json:"metadata"`
}

// AllowedTier describes one tier the user is eligible for.
// Source: LoadCodeAssistResponse protobuf.
type AllowedTier struct {
	ID                              string `json:"id"`
	Name                            string `json:"name"`
	Description                     string `json:"description"`
	UserDefinedCloudaicompanionProject bool `json:"userDefinedCloudaicompanionProject"`
	PrivacyNotice                   map[string]any `json:"privacyNotice"`
	IsDefault                       bool   `json:"isDefault"`
	UsesGcpTos                      bool   `json:"usesGcpTos"`
}

// LoadCodeAssistResponse is the parsed v1internal:loadCodeAssist response.
// Source: 1.3.3 binary protobuf reflection.
type LoadCodeAssistResponse struct {
	AllowedTiers []AllowedTier `json:"allowedTiers"`
}

// LoadCodeAssist calls v1internal:loadCodeAssist and returns the user's
// eligible tiers. Verified working 2026-10-14 with user's live token.
func (c *CloudCodeAPIClient) LoadCodeAssist(ctx context.Context) (*LoadCodeAssistResponse, error) {
	body, err := json.Marshal(LoadCodeAssistRequest{
		Metadata: DefaultIdeMetadata(),
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.Endpoint+"/v1internal:loadCodeAssist", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token.Token.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	// v4 binary also sends these (visible in REA capture):
	req.Header.Set("User-Agent", "antigravity/1.3.5 (linux; x86_64) openagy-recovered")
	req.Header.Set("X-Goog-Api-Client", "gl-go/1.27.2 gccl/1.3.5")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("daily-cloudcode-pa: loadCodeAssist: %w", err)
	}
	defer resp.Body.Close()
	rawBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daily-cloudcode-pa: loadCodeAssist: HTTP %d: %s",
			resp.StatusCode, string(rawBody))
	}
	var out LoadCodeAssistResponse
	if err := json.Unmarshal(rawBody, &out); err != nil {
		return nil, fmt.Errorf("daily-cloudcode-pa: loadCodeAssist: parse: %w (body=%s)", err, string(rawBody))
	}
	return &out, nil
}
