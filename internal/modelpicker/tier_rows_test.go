// Tests for the picker's rows of a model that declares reasoning tiers.
//
// The tiers are rows, not a slider position: the reference's slider has
// three places, and a model may offer five levels. The row's id carries
// the tier, so the picker needs no change of its own.
package modelpicker

import (
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestConfiguredRowsCarryTheTiers pins the picker's rows: one per tier,
// the tier in the label and in the row's id.
func TestConfiguredRowsCarryTheTiers(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "high"},
    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
  ]}}}`)
	got := map[string]string{}
	for _, r := range ConfiguredRows() {
		got[r.Slug] = r.Display
	}
	want := map[string]string{
		"minimax-m3.1-flash-low":    "MiniMax M3.1 Flash (Low)",
		"minimax-m3.1-flash-medium": "MiniMax M3.1 Flash (Medium)",
		"minimax-m3.1-flash-high":   "MiniMax M3.1 Flash (High)",
		"minimax-m3.1-flash-xhigh":  "MiniMax M3.1 Flash (Xhigh)",
		"minimax-m3.1-flash-max":    "MiniMax M3.1 Flash (Max)",
		"minimax-m3":                "MiniMax M3",
	}
	for slug, display := range want {
		if got[slug] != display {
			t.Errorf("the picker row %q = %q, want %q", slug, got[slug], display)
		}
	}
	if _, ok := got["minimax-m3.1-flash"]; ok {
		t.Errorf("the bare id is still a picker row: %q", got["minimax-m3.1-flash"])
	}
}

// TestCatalogueEntryFindsAConfiguredTierRow pins the label lookup the
// /model panel does when a row is chosen: the row's id resolves to its
// own label, tier and all.
func TestCatalogueEntryFindsAConfiguredTierRow(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["high", "xhigh"]}
  ]}}}`)
	entry, ok := CatalogueEntry("minimax-m3.1-flash-xhigh")
	if !ok {
		t.Fatal("CatalogueEntry did not find the tier row")
	}
	if entry.Label != "MiniMax M3.1 Flash (Xhigh)" {
		t.Errorf("the label = %q, want %q", entry.Label, "MiniMax M3.1 Flash (Xhigh)")
	}
	// The reference's own rows still resolve.
	entry, ok = CatalogueEntry("gemini-3.8-flash-high")
	if !ok || entry.Label != "Gemini 3.8 Flash (High)" {
		t.Errorf("the reference's row = %+v, %v", entry, ok)
	}
}
