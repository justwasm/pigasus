// Package modelcatalog — tier_select_test.go
//
// Tests for the resolution of a model the user configured against the
// reasoning tiers its own configuration declares.
//
// The reference spells the tier into the catalogue id ("gemini-3.8-flash-
// high"), so a configured model that declares tiers is addressed the same
// way: the bare id stands for the model's default tier, and an explicit
// tier is spelled onto the id.
package modelcatalog

import (
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// tieredProviders is a configuration with one model that declares the five
// tiers and one that declares none.
const tieredProviders = `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "high"},
    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
  ]}}}`

// TestResolveModelSelectionWithConfiguredTiers pins the resolution of a
// configured model that declares tiers.
func TestResolveModelSelectionWithConfiguredTiers(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	for _, tc := range []struct {
		model   string
		effort  string
		wantID  string
		wantErr string
	}{
		// The bare id stands for the model's default tier — the tier
		// the configuration itself names.
		{"minimax-m3.1-flash", "", "minimax-m3.1-flash-high", ""},
		// An explicit tier is spelled onto the id.
		{"minimax-m3.1-flash", "low", "minimax-m3.1-flash-low", ""},
		{"minimax-m3.1-flash", "xhigh", "minimax-m3.1-flash-xhigh", ""},
		{"minimax-m3.1-flash", "max", "minimax-m3.1-flash-max", ""},
		// A tier row resolves to itself.
		{"minimax-m3.1-flash-xhigh", "", "minimax-m3.1-flash-xhigh", ""},
		// The display label resolves too.
		{"MiniMax M3.1 Flash", "", "minimax-m3.1-flash-high", ""},
		{"MiniMax M3.1 Flash", "max", "minimax-m3.1-flash-max", ""},
		// A tier the model does not offer is refused, in the
		// reference's own words.
		{"minimax-m3.1-flash", "minimal", "",
			`error: invalid model selection (--model "minimax-m3.1-flash" --effort "minimal"): invalid --effort "minimal" (valid: low, medium, high, xhigh, max)`},
		// A model with no tiers keeps the reference's own rule: the
		// effort conflicts with the model.
		{"minimax-m3", "high", "",
			`error: invalid model selection (--model "minimax-m3" --effort "high"): --model minimax-m3 conflicts with --effort=high`},
		{"minimax-m3", "", "minimax-m3", ""},
	} {
		gotID, err := ResolveModelSelection(tc.model, tc.effort)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("ResolveModelSelection(%q, %q) = error %v, want id %q", tc.model, tc.effort, err, tc.wantID)
				continue
			}
			if gotID != tc.wantID {
				t.Errorf("ResolveModelSelection(%q, %q) = %q, want %q", tc.model, tc.effort, gotID, tc.wantID)
			}
			continue
		}
		if err == nil {
			t.Errorf("ResolveModelSelection(%q, %q) = %q, want error", tc.model, tc.effort, gotID)
			continue
		}
		if err.Error() != tc.wantErr {
			t.Errorf("ResolveModelSelection(%q, %q) error =\n%q\nwant\n%q", tc.model, tc.effort, err.Error(), tc.wantErr)
		}
	}
}

// TestResolveModelSelectionRefusesAnUndeclaredTier pins the refusal of a
// tier the model's own configuration does not list.
func TestResolveModelSelectionRefusesAnUndeclaredTier(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "limited", "name": "Limited", "model": "Limited-v1",
     "reasoning_efforts": ["low", "high"]}
  ]}}}`)
	_, err := ResolveModelSelection("limited", "medium")
	if err == nil {
		t.Fatal("ResolveModelSelection(limited, medium) = nil error, want a refusal")
	}
	want := `error: invalid model selection (--model "limited" --effort "medium"): limited has no "medium" effort (available: low, high)`
	if err.Error() != want {
		t.Errorf("error =\n%q\nwant\n%q", err.Error(), want)
	}
	if id, err := ResolveModelSelection("limited", "high"); err != nil || id != "limited-high" {
		t.Errorf("ResolveModelSelection(limited, high) = %q, %v, want limited-high", id, err)
	}
}

// TestAvailableModelsBlockListsTheTiers pins the catalogue the unknown-model
// error advertises: a model that declares tiers is listed once per tier.
func TestAvailableModelsBlockListsTheTiers(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	got := availableModelsBlock()
	want := "Available models:\n" +
		"  Gemini 3.8 Flash (High)\n" +
		"  Gemini 3.8 Flash (Medium)\n" +
		"  Gemini 3.8 Flash (Low)\n" +
		"  Claude Sonnet 4.6 (Thinking)\n" +
		"  Claude Opus 4.6 (Thinking)\n" +
		"  GPT-OSS 120B (Medium)\n" +
		"  MiniMax M3\n" +
		"  MiniMax M3.1 Flash (Low)\n" +
		"  MiniMax M3.1 Flash (Medium)\n" +
		"  MiniMax M3.1 Flash (High)\n" +
		"  MiniMax M3.1 Flash (Xhigh)\n" +
		"  MiniMax M3.1 Flash (Max)"
	if got != want {
		t.Errorf("availableModelsBlock() =\n%s\nwant\n%s", got, want)
	}
}
