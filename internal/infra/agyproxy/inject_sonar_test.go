package agyproxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/features/models"
)

func TestFetchAvailableModelsInjectsHydrogenSonar(t *testing.T) {
	t.Setenv("OPENAGY_UNDOCUMENTED_MODELS", "on")

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":{"gemini-3.8-flash-high":{"displayName":"Gemini 3.8 Flash (High)","model":"MODEL_PLACEHOLDER_M318"}}}`)
	}))
	defer upstream.Close()

	// Server without any custom models in config
	s := newTestServer(t, upstream.URL)

	resp := post(t, s, "/v1internal:fetchAvailableModels", `{}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	raw, _ := io.ReadAll(resp.Body)
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	modelsMap, ok := doc["models"].(map[string]any)
	if !ok {
		t.Fatalf("models not a map: %T", doc["models"])
	}

	sonarEntry, ok := modelsMap["gemini-4-hydrogen-sonar"].(map[string]any)
	if !ok {
		t.Fatalf("gemini-4-hydrogen-sonar missing from injected catalogue: %v", modelsMap)
	}

	if sonarEntry["displayName"] != "Gemini 4 Hydrogen (Sonar)" {
		t.Errorf("displayName = %v, want Gemini 4 Hydrogen (Sonar)", sonarEntry["displayName"])
	}
	if sonarEntry["supportsThinking"] != true {
		t.Errorf("supportsThinking = %v, want true", sonarEntry["supportsThinking"])
	}
	if sonarEntry["supportsImages"] != true {
		t.Errorf("supportsImages = %v, want true", sonarEntry["supportsImages"])
	}

	// Also verify tiers are injected
	for _, tier := range []string{"gemini-4-hydrogen-sonar-high", "gemini-4-hydrogen-sonar-low"} {
		if _, ok := modelsMap[tier]; !ok {
			t.Errorf("tier %q missing from injected catalogue", tier)
		}
	}
}

func TestStreamGenerateContentRewritesHydrogenSonarModelName(t *testing.T) {
	t.Setenv("OPENAGY_UNDOCUMENTED_MODELS", "on")

	var receivedModel atomic.Value
	var receivedLevel atomic.Value
	var receivedBudget atomic.Int64

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var env geminicode.GenerateContentRequest
		_ = json.Unmarshal(body, &env)
		receivedModel.Store(env.ModelName())
		if env.Inner() != nil && env.Inner().GenerationConfig != nil && env.Inner().GenerationConfig.ThinkingConfig != nil {
			receivedLevel.Store(env.Inner().GenerationConfig.ThinkingConfig.ThinkingLevel)
			receivedBudget.Store(int64(env.Inner().GenerationConfig.ThinkingConfig.ThinkingBudget))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"response\":{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"pong\"}]}}]}}\n\n")
	}))
	defer upstream.Close()

	s := newTestServer(t, upstream.URL)

	// Simulate vendor agy sending request with the placeholder of low tier
	var lowModel models.Model
	for _, m := range UndocumentedGoogleModels() {
		if m.Name == "gemini-4-hydrogen-sonar-low" {
			lowModel = m
			break
		}
	}
	if lowModel.Name == "" {
		t.Fatalf("lowModel not found in UndocumentedGoogleModels")
	}
	ph := Placeholder(lowModel)

	reqBody := `{"model":"` + ph + `","request":{"contents":[{"role":"user","parts":[{"text":"ping"}]}]}}`
	resp := post(t, s, "/v1internal:streamGenerateContent?alt=sse", reqBody)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	gotModel, _ := receivedModel.Load().(string)
	if gotModel != "gemini-4-hydrogen-sonar" {
		t.Errorf("upstream received model = %q, want gemini-4-hydrogen-sonar", gotModel)
	}

	gotLevel, _ := receivedLevel.Load().(string)
	if gotLevel != "low" {
		t.Errorf("upstream received thinkingLevel = %q, want low", gotLevel)
	}

	if budget := receivedBudget.Load(); budget != 1024 {
		t.Errorf("upstream received thinkingBudget = %d, want 1024", budget)
	}
}
