package modelpicker_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

func TestModelPickerCustomRowsFromEngine(t *testing.T) {
	customList := []modelpicker.Row{
		{
			Display:     "GPT-6.1 Sol",
			Slug:        "gpt-6.1-sol",
			Tiers:       []string{"low", "medium", "high", "xhigh"},
			DefaultTier: "high",
		},
		{
			Display: "GPT-6 Astra",
			Slug:    "gpt-6-astra",
		},
	}

	p := modelpicker.New(modelpicker.Seams{
		CurrentModelLabel: func() string { return "GPT-6.1 Sol" },
		CustomRows: func() []modelpicker.Row {
			return customList
		},
	})

	rows := p.Rows()
	if len(rows) != 2 {
		t.Fatalf("expected 2 custom rows, got %d", len(rows))
	}
	if rows[0].Display != "GPT-6.1 Sol" {
		t.Errorf("expected rows[0] = GPT-6.1 Sol, got %s", rows[0].Display)
	}
	if rows[1].Display != "GPT-6 Astra" {
		t.Errorf("expected rows[1] = GPT-6 Astra, got %s", rows[1].Display)
	}

	filtered := modelpicker.FilterRows(customList, "astra")
	if len(filtered) != 1 || filtered[0].Slug != "gpt-6-astra" {
		t.Errorf("expected 1 filtered match for 'astra', got %+v", filtered)
	}
}
