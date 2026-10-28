// Tests for the picker's rows of a model that declares reasoning tiers.
//
// The tiers are a slider, not a row each: the reference's own picker
// shows Gemini 3.8 Flash as one row and puts its three tiers on the
// slider underneath. A model the user configured gets the same shape —
// one row, the tiers on the slider — even when it offers five levels.
package modelpicker

import (
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestConfiguredRowsCarryTheTiers pins the picker's rows: one row per
// model, the tiers on the row, the tier spelled into the row's id.
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
	got := map[string]Row{}
	for _, r := range ConfiguredRows() {
		got[r.Slug] = r
	}
	flash, ok := got["minimax-m3.1-flash"]
	if !ok {
		t.Fatalf("the picker has no row for the configured model: %+v", got)
	}
	if flash.Display != "MiniMax M3.1 Flash" {
		t.Errorf("the row's label = %q, want %q (the tier is on the slider)",
			flash.Display, "MiniMax M3.1 Flash")
	}
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(flash.Tiers) != len(want) {
		t.Fatalf("the row's tiers = %v, want %v", flash.Tiers, want)
	}
	for i, tier := range want {
		if flash.Tiers[i] != tier {
			t.Errorf("the row's tier %d = %q, want %q", i, flash.Tiers[i], tier)
		}
	}
	if flash.DefaultTier != "high" {
		t.Errorf("the row's default tier = %q, want %q (the configuration's own)",
			flash.DefaultTier, "high")
	}
	// The tier rides the row's id, one id per tier.
	for i, tier := range want {
		if id := flash.CatalogueID(i); id != "minimax-m3.1-flash-"+tier {
			t.Errorf("CatalogueID(%d) = %q, want %q", i, id, "minimax-m3.1-flash-"+tier)
		}
	}
	// A model without tiers keeps its own row and its own id.
	m3, ok := got["minimax-m3"]
	if !ok {
		t.Fatalf("the picker has no row for the tierless model: %+v", got)
	}
	if len(m3.Tiers) != 0 {
		t.Errorf("the tierless model's tiers = %v, want none", m3.Tiers)
	}
	if id := m3.CatalogueID(0); id != "minimax-m3" {
		t.Errorf("the tierless model's id = %q, want %q", id, "minimax-m3")
	}
	// The tiered model is one row, not one row per tier.
	if len(ConfiguredRows()) != 2 {
		t.Errorf("the picker has %d configured rows, want 2 (one per model)",
			len(ConfiguredRows()))
	}
}

// TestCatalogueEntryFindsAConfiguredTierRow pins the label lookup the
// /model panel does when a row is chosen: the row's id resolves to its
// own label, tier and all.
//
// The catalogue still lists one row per tier — that is the shape the
// id↔label lookup needs — even though the picker shows one row.
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
