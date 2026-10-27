// Tests for the /provider panel's reasoning-tier field.
//
// The panel is the TUI's front-end on the provider configuration, so the
// tiers a model offers can be declared there — and a tier nobody can
// select is refused before the file is written.
package providerpanel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/models"
)

// TestModelFieldsCarryTheTiers pins the field: the tiers as the
// configuration holds them, and the five words as the model's own level
// choices.
func TestModelFieldsCarryTheTiers(t *testing.T) {
	fields := ModelFields(models.ProviderModel{
		ID:               "m3",
		Name:             "M3",
		ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"},
	}, false)
	var tiers *FormField
	var effort *FormField
	for i := range fields {
		switch fields[i].Label {
		case FieldTiers:
			tiers = &fields[i]
		case FieldEffort:
			effort = &fields[i]
		}
	}
	if tiers == nil {
		t.Fatal("the model form has no tiers field")
	}
	if tiers.Value != "low, medium, high, xhigh, max" {
		t.Errorf("the tiers field = %q, want the comma separated list", tiers.Value)
	}
	if tiers.Kind != FieldText {
		t.Errorf("the tiers field's kind = %v, want a text field", tiers.Kind)
	}
	if effort == nil {
		t.Fatal("the model form has no effort field")
	}
	for _, want := range []string{"xhigh", "max"} {
		found := false
		for _, c := range effort.Choices {
			if c == want {
				found = true
			}
		}
		if !found {
			t.Errorf("the effort field does not offer %q: %v", want, effort.Choices)
		}
	}
}

// TestProviderModelReadsTheTiers pins the form's own value: the tiers
// field becomes the model's list.
func TestProviderModelReadsTheTiers(t *testing.T) {
	f := &Form{
		Model: "tiered",
		Fields: []FormField{
			{Label: FieldTiers, Value: " Low , high , , xhigh "},
		},
	}
	entry, ok := ProviderModel(f, models.ProviderModel{ID: "tiered"})
	if !ok {
		t.Fatal("ProviderModel returned ok=false, want true")
	}
	want := []string{"low", "high", "xhigh"}
	if len(entry.ReasoningEfforts) != len(want) {
		t.Fatalf("the tiers = %v, want %v", entry.ReasoningEfforts, want)
	}
	for i := range want {
		if entry.ReasoningEfforts[i] != want[i] {
			t.Fatalf("the tiers = %v, want %v", entry.ReasoningEfforts, want)
		}
	}
}

// TestValidateAndSaveModelRefusesAnUnknownTier pins the refusal: the file
// is left alone and the tier is named.
func TestValidateAndSaveModelRefusesAnUnknownTier(t *testing.T) {
	path := filepath.Join(t.TempDir(), "providers.json")
	ed := models.NewEditor(path)
	if err := ed.PutProvider("myprov", models.ProviderConfig{ID: "myprov", BaseURL: "https://x/v1"}); err != nil {
		t.Fatalf("PutProvider: %v", err)
	}
	providers := map[string]models.ProviderConfig{"myprov": {ID: "myprov"}}
	form := &Form{
		Provider: "myprov",
		Model:    "tiered",
		Fields: []FormField{
			{Label: FieldTiers, Value: "low,ultra"},
		},
	}
	_, _, err := ValidateAndSaveModel(ed, providers, form)
	if err == nil {
		t.Fatal("an unknown tier was accepted, want a refusal")
	}
	if !strings.Contains(err.Error(), "ultra") {
		t.Errorf("the error does not name the offending tier: %v", err)
	}
}
