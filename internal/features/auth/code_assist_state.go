// code_assist_state.go — the account's code-assist state, verbatim.
//
// The Web UI's credits panel asks the language server for the
// account's state:
//
//	/exa.language_server_pb.LanguageServerService/GetLoadCodeAssist
//
// The answer it wants is the cloud's own `v1internal:loadCodeAssist`
// answer: the tier the account is on, the tiers it may move to, the
// project it is bound to and the credits its plan carries.
//
// plan_tier.go reads the same endpoint but throws everything but the
// banner label away. This one keeps the answer as it arrived, so the
// clone can hand the cloud's own state to the Web UI instead of
// inventing one.
//
// 【一手】 2026-10-27, the operator's own account, the live answer:
//
//	{
//	  "currentTier": {"id": "free-tier", "name": "Antigravity",
//	                  "description": "Gemini-powered code suggestions …"},
//	  "allowedTiers": [{"id": "free-tier", "isDefault": true},
//	                   {"id": "standard-tier", …}],
//	  "cloudaicompanionProject": "aicode-consumers",
//	  "gcpManaged": false,
//	  "paidTier": {"id": "g1-pro-tier", "name": "Google AI Pro",
//	               "availableCredits": [{"creditType": "GOOGLE_ONE_AI",
//	                                      "minimumCreditAmountForUsage": "50"}]}
//	}
//
// The tier of the *current* account is the free one even on a paid
// plan: `currentTier` is the tier the session runs on, `paidTier` is
// the plan the account bought.
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

	"github.com/justwasm/pigasus/endpoints"
)

// codeAssistStateTimeout bounds the live read.
const codeAssistStateTimeout = 8 * time.Second

// FetchCodeAssistState asks daily-cloudcode-pa for the account's
// code-assist state and returns the answer verbatim.
//
// The keychain token wins, exactly as in plan_tier.go: the on-disk
// token can be stale and resolve to a different account.
func FetchCodeAssistState(ctx context.Context) (json.RawMessage, error) {
	tok, err := cloudCodeAccessToken()
	if err != nil {
		return nil, err
	}
	return FetchCodeAssistStateWithToken(ctx, tok)
}

// FetchCodeAssistStateWithToken asks daily-cloudcode-pa for the
// code-assist state of the account behind an explicit bearer token.
//
// It is the state of *that* account: the multi-account registry
// queries one account at a time, each with its own access token.
func FetchCodeAssistStateWithToken(ctx context.Context, accessToken string) (json.RawMessage, error) {
	if strings.TrimSpace(accessToken) == "" {
		return nil, fmt.Errorf("no access token")
	}
	var reqBody PlanTierRequest
	reqBody.Metadata.IDEType = "ANTIGRAVITY"
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		endpoints.Current().CloudCode+"/v1internal:loadCodeAssist", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent",
		"antigravity/cli/1.4.4 (aidev_client; os_type=darwin; arch=arm64; auth_method=consumer)")
	client := &http.Client{Timeout: codeAssistStateTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("daily-cloudcode-pa: loadCodeAssist: HTTP %d", resp.StatusCode)
	}
	if !json.Valid(raw) {
		return nil, fmt.Errorf("daily-cloudcode-pa: loadCodeAssist: not JSON")
	}
	return json.RawMessage(raw), nil
}
