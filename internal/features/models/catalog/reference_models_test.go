// Package catalog — reference_models_test.go
//
// The pins for the three model families the official documentation's
// reasoning-model table names and the reference's own binary carries:
// Gemini 3.7 Flash, Gemini 3.6 Flash and Gemini 3.1 Pro.
//
// The ids are the contract — a guessed id routes a turn to a model that
// does not exist — so each row is pinned by its id *and* its display
// label, and the order is pinned too: the reference prints its catalogue
// in one order and the clone prints it in the same one.
package catalog

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// referenceCatalogue is the reference's own catalogue, in its own order.
//
// 【一手】 agy 1.4.2, the vendor binary's `agy models` output
// (docs/cycles/cycle-274/deliverable.md §7.3) — fourteen rows, the eight
// of them the three families publish:
//
//	gemini-3.7-flash-high	Gemini 3.7 Flash (High)
//	gemini-3.7-flash-medium	Gemini 3.7 Flash (Medium)
//	gemini-3.7-flash-low	Gemini 3.7 Flash (Low)
//	gemini-3.6-flash-high	Gemini 3.6 Flash (High)
//	gemini-3.6-flash-medium	Gemini 3.6 Flash (Medium)
//	gemini-3.6-flash-low	Gemini 3.6 Flash (Low)
//	gemini-3.1-pro-high	Gemini 3.1 Pro (High)
//	gemini-3.1-pro-low	Gemini 3.1 Pro (Low)
//
// 【strings】 agy 1.4.12 carries the same eight ids and the same eight
// labels in its own tier table; "Gemini 3.1 Pro (Medium)" is not in the
// binary, so the Pro family publishes two tiers and not three.
var referenceCatalogue = []ModelEntry{
	{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
	{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
	{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
	{"gemini-3.7-flash-high", "Gemini 3.7 Flash (High)"},
	{"gemini-3.7-flash-medium", "Gemini 3.7 Flash (Medium)"},
	{"gemini-3.7-flash-low", "Gemini 3.7 Flash (Low)"},
	{"gemini-3.6-flash-high", "Gemini 3.6 Flash (High)"},
	{"gemini-3.6-flash-medium", "Gemini 3.6 Flash (Medium)"},
	{"gemini-3.6-flash-low", "Gemini 3.6 Flash (Low)"},
	{"gemini-3.1-pro-high", "Gemini 3.1 Pro (High)"},
	{"gemini-3.1-pro-low", "Gemini 3.1 Pro (Low)"},
	{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
	{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
	{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
}

// TestReferenceModelsCarriesTheDocumentedFamilies pins that the three
// families the documentation names are in the catalogue, with the
// reference's own ids and labels, in the reference's own order.
func TestReferenceModelsCarriesTheDocumentedFamilies(t *testing.T) {
	configtest.Isolate(t)
	got := ReferenceModels()
	if len(got) != len(referenceCatalogue) {
		t.Fatalf("ReferenceModels() = %d rows, want %d", len(got), len(referenceCatalogue))
	}
	for i := range referenceCatalogue {
		if got[i].ID != referenceCatalogue[i].ID || got[i].Label != referenceCatalogue[i].Label {
			t.Errorf("ReferenceModels()[%d] = %+v, want %+v", i, got[i], referenceCatalogue[i])
		}
	}
}

// TestTheThreeFamiliesResolve pins that every row of the three families
// resolves the way the reference resolves a catalogue row: by id and by
// display label, to the same id.
func TestTheThreeFamiliesResolve(t *testing.T) {
	configtest.Isolate(t)
	for _, m := range []ModelEntry{
		{"gemini-3.7-flash-high", "Gemini 3.7 Flash (High)"},
		{"gemini-3.7-flash-medium", "Gemini 3.7 Flash (Medium)"},
		{"gemini-3.7-flash-low", "Gemini 3.7 Flash (Low)"},
		{"gemini-3.6-flash-high", "Gemini 3.6 Flash (High)"},
		{"gemini-3.6-flash-medium", "Gemini 3.6 Flash (Medium)"},
		{"gemini-3.6-flash-low", "Gemini 3.6 Flash (Low)"},
		{"gemini-3.1-pro-high", "Gemini 3.1 Pro (High)"},
		{"gemini-3.1-pro-low", "Gemini 3.1 Pro (Low)"},
	} {
		id, ok := CatalogueID(m.ID)
		if !ok || id != m.ID {
			t.Errorf("CatalogueID(%q) = %q, %v, want %q", m.ID, id, ok, m.ID)
		}
		id, ok = CatalogueID(m.Label)
		if !ok || id != m.ID {
			t.Errorf("CatalogueID(%q) = %q, %v, want %q", m.Label, id, ok, m.ID)
		}
		if got := FormatModelLabel(m.Label); got != m.Label {
			t.Errorf("FormatModelLabel(%q) = %q, want %q", m.Label, got, m.Label)
		}
		if !ModelAvailable(m.ID) || !ModelAvailable(m.Label) {
			t.Errorf("ModelAvailable(%q/%q) = false, want true", m.ID, m.Label)
		}
	}
}

func TestGemini4CarbonADC(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("AGY_ADC_AUTH", "")
	if ADCAuthAvailable() {
		t.Fatalf("expected ADCAuthAvailable() false when unset")
	}
	for _, m := range ReferenceModels() {
		if m.ID == "gemini-4-carbon-high" {
			t.Fatalf("gemini-4-carbon should not appear without ADC auth")
		}
	}

	t.Setenv("AGY_ADC_AUTH", "true")
	if !ADCAuthAvailable() {
		t.Fatalf("expected ADCAuthAvailable() true when AGY_ADC_AUTH=true")
	}
	found := false
	for _, m := range ReferenceModels() {
		if m.ID == "gemini-4-carbon-high" && m.Label == "Gemini 4 Carbon (High)" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("gemini-4-carbon-high missing from ReferenceModels() under AGY_ADC_AUTH=true")
	}

	tiers := EffortTiersFor("gemini-4-carbon")
	if len(tiers) != 3 || tiers[0] != "low" || tiers[1] != "medium" || tiers[2] != "high" {
		t.Fatalf("EffortTiersFor(gemini-4-carbon) = %v, want [low, medium, high]", tiers)
	}

	foundHydrogen := false
	for _, m := range ReferenceModels() {
		if m.ID == "gemini-4-hydrogen-high" && m.Label == "Gemini 4 Hydrogen (High)" {
			foundHydrogen = true
			break
		}
	}
	if !foundHydrogen {
		t.Fatalf("gemini-4-hydrogen-high missing from ReferenceModels() under AGY_ADC_AUTH=true")
	}

	hTiers := EffortTiersFor("gemini-4-hydrogen")
	if len(hTiers) != 4 || hTiers[0] != "low" || hTiers[1] != "medium" || hTiers[2] != "high" || hTiers[3] != "xhigh" {
		t.Fatalf("EffortTiersFor(gemini-4-hydrogen) = %v, want [low, medium, high, xhigh]", hTiers)
	}
}

func TestGemini4HydrogenAndCarbonAPIKey(t *testing.T) {
	configtest.Isolate(t)
	t.Setenv("AGY_ADC_AUTH", "")
	t.Setenv("GEMINI_API_KEY", "")
	for _, m := range ReferenceModels() {
		if m.ID == "gemini-4-hydrogen-high" || m.ID == "gemini-4-carbon-high" {
			t.Fatalf("model %s should not appear without GEMINI_API_KEY or ADC", m.ID)
		}
	}

	t.Setenv("GEMINI_API_KEY", "test-key")
	if !GeminiAPIKeyAvailable() {
		t.Fatalf("expected GeminiAPIKeyAvailable() true")
	}
	foundHydrogen := false
	foundCarbon := false
	for _, m := range ReferenceModels() {
		if m.ID == "gemini-4-hydrogen-xhigh" && m.Label == "Gemini 4 Hydrogen (XHigh)" {
			foundHydrogen = true
		}
		if m.ID == "gemini-4-carbon-high" && m.Label == "Gemini 4 Carbon (High)" {
			foundCarbon = true
		}
	}
	if !foundHydrogen {
		t.Errorf("gemini-4-hydrogen-xhigh missing under GEMINI_API_KEY")
	}
	if !foundCarbon {
		t.Errorf("gemini-4-carbon-high missing under GEMINI_API_KEY")
	}
}

// TestTheThreeFamiliesAreSelectable pins that a pinned id selects itself:
// the reference answers a known model with its own id, and a tier that is
// spelled into the id conflicts with an explicit --effort the way the
// reference's own three-tier model does.
func TestTheThreeFamiliesAreSelectable(t *testing.T) {
	configtest.Isolate(t)
	for _, id := range []string{
		"gemini-3.7-flash-high",
		"gemini-3.7-flash-medium",
		"gemini-3.7-flash-low",
		"gemini-3.6-flash-high",
		"gemini-3.6-flash-medium",
		"gemini-3.6-flash-low",
		"gemini-3.1-pro-high",
		"gemini-3.1-pro-low",
	} {
		got, err := ResolveModelSelection(id, "")
		if err != nil {
			t.Errorf("ResolveModelSelection(%q, \"\") = error %v, want %q", id, err, id)
			continue
		}
		if got != id {
			t.Errorf("ResolveModelSelection(%q, \"\") = %q, want %q", id, got, id)
		}
	}
}

// TestTheProFamilyHasNoMediumTier pins the reference's own tier count for
// Gemini 3.1 Pro: two tiers, not three. "Gemini 3.1 Pro (Medium)" is not
// in the reference's binary, so the clone must not invent the row.
func TestTheProFamilyHasNoMediumTier(t *testing.T) {
	configtest.Isolate(t)
	if _, ok := CatalogueID("Gemini 3.1 Pro (Medium)"); ok {
		t.Error("CatalogueID(Gemini 3.1 Pro (Medium)) resolved; the reference publishes no medium tier")
	}
	if _, ok := CatalogueID("gemini-3.1-pro-medium"); ok {
		t.Error("CatalogueID(gemini-3.1-pro-medium) resolved; the reference publishes no medium tier")
	}
}
