// Tests for the per-model reasoning tiers (`--reasoning-efforts`) and the
// catalogue rows they produce.
//
// A model that declares tiers is listed once per tier — the reference's own
// shape ("Gemini 3.8 Flash (High)") — and the tier rides the row's id, so
// the Web UI's picker and the TUI's /model panel need no change of their
// own.
package cli

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
	"github.com/justwasm/pigasus/internal/models"
)

// TestModelAdd_CarriesTheReasoningTiers pins the write: the tiers land in
// the configuration under `reasoning_efforts`, and the level the model runs
// at stays in `reasoning_effort`.
func TestModelAdd_CarriesTheReasoningTiers(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "tiered", "--provider", "gateway",
		"--reasoning-efforts", "low, medium, high, xhigh, max",
		"--reasoning-effort", "high"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	body := readProviders(t)
	if !strings.Contains(body, `"reasoning_efforts"`) {
		t.Errorf("the tiers did not reach the configuration:\n%s", body)
	}
	if !strings.Contains(body, `"reasoning_effort"`) {
		t.Errorf("the level did not reach the configuration:\n%s", body)
	}

	// The configuration round-trips: the model reads back with its tiers.
	providers, err := models.ProviderEditor().List()
	if err != nil {
		t.Fatalf("provider list: %v", err)
	}
	var found *models.ProviderModel
	for i := range providers["gateway"].Models {
		if providers["gateway"].Models[i].ID == "tiered" {
			found = &providers["gateway"].Models[i]
		}
	}
	if found == nil {
		t.Fatalf("the model did not reach the configuration:\n%s", body)
	}
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(found.ReasoningEfforts) != len(want) {
		t.Fatalf("the tiers = %v, want %v", found.ReasoningEfforts, want)
	}
	for i := range want {
		if found.ReasoningEfforts[i] != want[i] {
			t.Fatalf("the tiers = %v, want %v", found.ReasoningEfforts, want)
		}
	}
	if found.ReasoningEffort != "high" {
		t.Errorf("the level = %q, want high", found.ReasoningEffort)
	}
}

// TestModelAdd_RefusesAnUnknownTier pins the refusal: a tier nobody can
// select is a typo, and the file is left alone.
func TestModelAdd_RefusesAnUnknownTier(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	_, _, err := runProviderCmd(t, runModelAdd, "tiered", "--provider", "gateway",
		"--reasoning-efforts", "low,ultra")
	if err == nil {
		t.Fatal("model add with an unknown tier succeeded, want a refusal")
	}
	if !strings.Contains(err.Error(), "ultra") {
		t.Errorf("the error does not name the offending tier: %v", err)
	}
	if strings.Contains(readProviders(t), "ultra") {
		t.Errorf("the refused tier reached the configuration:\n%s", readProviders(t))
	}
}

// TestModelAdd_AcceptsTheUnderscoreSpelling pins the flag's second
// spelling, the one the configuration's own key uses.
func TestModelAdd_AcceptsTheUnderscoreSpelling(t *testing.T) {
	configtest.Isolate(t)
	if _, _, err := runProviderCmd(t, runProviderAdd,
		"gateway", "--base-url", "https://gw.example/v1"); err != nil {
		t.Fatalf("provider add: %v", err)
	}
	if _, _, err := runProviderCmd(t, runModelAdd, "tiered", "--provider", "gateway",
		"--reasoning_efforts", "low,high"); err != nil {
		t.Fatalf("model add: %v", err)
	}
	if !strings.Contains(readProviders(t), `"reasoning_efforts"`) {
		t.Errorf("the tiers did not reach the configuration:\n%s", readProviders(t))
	}
}

// TestModelCatalogueExpandsTheTiers pins the catalogue's rows: one per
// tier, the tier in the id and in the label, and the models without tiers
// left alone.
func TestModelCatalogueExpandsTheTiers(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "minimax-m3.1-flash", "name": "MiniMax M3.1 Flash", "model": "MiniMax-M3.1-Flash",
     "reasoning_efforts": ["low", "medium", "high", "xhigh", "max"],
     "reasoning_effort": "high"},
    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}
  ]}}}`)

	want := map[string]string{
		"minimax-m3.1-flash-low":    "MiniMax M3.1 Flash (Low)",
		"minimax-m3.1-flash-medium": "MiniMax M3.1 Flash (Medium)",
		"minimax-m3.1-flash-high":   "MiniMax M3.1 Flash (High)",
		"minimax-m3.1-flash-xhigh":  "MiniMax M3.1 Flash (Xhigh)",
		"minimax-m3.1-flash-max":    "MiniMax M3.1 Flash (Max)",
		"minimax-m3":                "MiniMax M3",
	}
	got := map[string]string{}
	for _, e := range ModelCatalogue() {
		got[e.ID] = e.Label
	}
	for id, label := range want {
		if got[id] != label {
			t.Errorf("the catalogue row %q = %q, want %q", id, got[id], label)
		}
	}
	// The bare id is not a row of its own: the tiers replaced it.
	if _, ok := got["minimax-m3.1-flash"]; ok {
		t.Errorf("the bare id is still a catalogue row: %q", got["minimax-m3.1-flash"])
	}
	// The reference's own rows are untouched.
	if got["gemini-3.8-flash-high"] != "Gemini 3.8 Flash (High)" {
		t.Errorf("the reference's row changed: %q", got["gemini-3.8-flash-high"])
	}
}

// TestModelCatalogueKeepsTheDeclaredTierOrder pins the order of the rows:
// the tiers as the configuration declared them.
func TestModelCatalogueKeepsTheDeclaredTierOrder(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
  "type": "openai",
  "api_url": "https://gw.example/v1/chat/completions",
  "models": [
    {"id": "tiered", "name": "Tiered", "model": "Tiered-v1",
     "reasoning_efforts": ["max", "low", "xhigh"]}
  ]}}}`)
	var ids []string
	for _, e := range ModelCatalogue() {
		if strings.HasPrefix(e.ID, "tiered-") {
			ids = append(ids, e.ID)
		}
	}
	want := []string{"tiered-max", "tiered-low", "tiered-xhigh"}
	if len(ids) != len(want) {
		t.Fatalf("the tier rows = %v, want %v", ids, want)
	}
	for i := range want {
		if ids[i] != want[i] {
			t.Fatalf("the tier rows = %v, want %v", ids, want)
		}
	}
}
