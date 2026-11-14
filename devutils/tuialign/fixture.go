package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
)

//go:embed fixtures/models.json
var embeddedModelsJSON []byte

// MockPlanTier is the plan tier the mock's loadCodeAssist answers with.
//
// Both binaries put it in the banner's account row — "user@example.com
// (Antigravity Pro)" — but only after their own tier call returns, so a
// snapshot taken at boot can catch either state. The harness waits for this
// string before comparing a boot screen (see waitForBannerSettled).
const MockPlanTier = "Antigravity Pro"

// DefaultSynthesizedCodeAssist returns a valid Pro tier loadCodeAssist response
// that allows both agy and pigasus to pass eligibility checks without real auth.
func DefaultSynthesizedCodeAssist() []byte {
	resp := map[string]any{
		"currentTier": map[string]any{
			"id":          "standard-tier",
			"name":        MockPlanTier,
			"description": "Google AI Pro (Mock Harness)",
		},
		"paidTier": map[string]any{
			"id":          "standard-tier",
			"name":        MockPlanTier,
			"description": "Google AI Pro (Mock Harness)",
		},
		"allowedTiers": []map[string]any{
			{
				"id":        "free-tier",
				"name":      "Antigravity",
				"isDefault": false,
			},
			{
				"id":        "standard-tier",
				"name":      "Antigravity Pro",
				"isDefault": true,
			},
		},
		"cloudaicompanionProject": "aicode-consumers",
		"gcpManaged":              false,
	}
	b, _ := json.Marshal(resp)
	return b
}

// DefaultOnboardUser returns a successful OnboardUser response.
func DefaultOnboardUser() []byte {
	resp := map[string]any{
		"done": true,
		"response": map[string]any{
			"@type":                   "type.googleapis.com/google.internal.cloud.code.v1internal.OnboardUserResponse",
			"cloudaicompanionProject": "aicode-consumers",
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// DefaultQuotaSummary returns plenty of mock quota for 5h and weekly windows.
func DefaultQuotaSummary() []byte {
	resp := map[string]any{
		"groups": []map[string]any{
			{
				"displayName": "Gemini",
				"buckets": []map[string]any{
					{
						"bucketId":          "gemini-flash-high",
						"displayName":       "Gemini 3.8 Flash (High)",
						"window":            "5h",
						"remainingFraction": 1.0,
					},
					{
						"bucketId":          "gemini-flash-medium",
						"displayName":       "Gemini 3.8 Flash (Medium)",
						"window":            "5h",
						"remainingFraction": 1.0,
					},
				},
			},
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// DefaultModelsJSON returns the captured real models catalogue.
func DefaultModelsJSON() []byte {
	if len(embeddedModelsJSON) > 0 {
		return embeddedModelsJSON
	}
	return []byte(`{"models":{"gemini-3.8-flash-high":{"name":"models/gemini-3.8-flash-high","displayName":"Gemini 3.8 Flash (High)"}},"defaultAgentModelId":"gemini-3.8-flash-high"}`)
}

// DefaultUserInfo returns basic user info.
func DefaultUserInfo(email string) []byte {
	if email == "" {
		email = "tuialign@antigravity.test"
	}
	resp := map[string]any{
		"email": email,
		"name":  "TUI Align Tester",
	}
	b, _ := json.Marshal(resp)
	return b
}

// FormatSSEChunk formats a Gemini CloudCode SSE event.
func FormatSSEChunk(data []byte) []byte {
	return []byte(fmt.Sprintf("data: %s\n\n", string(data)))
}
