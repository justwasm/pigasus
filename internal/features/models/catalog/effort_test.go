// Package modelcatalog — effort_test.go
//
// Tests for the reasoning tiers a model publishes: the catalogue's own
// table for the reference's models, and the tiers a model the user
// configured declares.
//
// The slider draws the levels the *model* publishes, not a fixed list —
// 【反编译】 agy 1.4.12, backend.EffortsForBase, sorted by
// model.sortEfforts, and model.EffortSelectorModel.currentEffortIndex
// indexes into that list — so the question "which levels does this model
// publish?" has an owner, and it is the catalogue.
package catalog

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/configtest"
)

// TestEffortTiersFor pins the levels a model publishes.
//
// The reference's own catalogue publishes three levels for Gemini 3.8
// Flash and exactly one for GPT-OSS 120B — the binary's own model list
// carries `MODEL_OPENAI_GPT_OSS_120B_MEDIUM` and no other GPT-OSS tier
// (【strings】 agy 1.4.12, out/models-gemini.txt) — so a model with a
// single level has a fixed reasoning budget.
func TestEffortTiersFor(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	for _, tc := range []struct {
		model string
		want  []string
	}{
		{"Gemini 3.8 Flash (High)", []string{"low", "medium", "high"}},
		{"gemini-3.8-flash-medium", []string{"low", "medium", "high"}},
		{"gemini-3.8-flash", []string{"low", "medium", "high"}},
		// The reference publishes one level for GPT-OSS: its effort is
		// fixed.
		{"GPT-OSS 120B (Medium)", []string{"medium"}},
		// A model the user configured publishes the tiers its own
		// configuration declares.
		{"MiniMax M3.1 Flash (High)", []string{"low", "medium", "high", "xhigh", "max"}},
		{"minimax-m3.1-flash", []string{"low", "medium", "high", "xhigh", "max"}},
		{"minimax-m3.1-flash-max", []string{"low", "medium", "high", "xhigh", "max"}},
		// A model that declares no tiers publishes none.
		{"minimax-m3", nil},
		{"", nil},
		{"no-such-model", nil},
	} {
		got := EffortTiersFor(tc.model)
		if len(got) != len(tc.want) {
			t.Errorf("EffortTiersFor(%q) = %v, want %v", tc.model, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("EffortTiersFor(%q) = %v, want %v", tc.model, got, tc.want)
				break
			}
		}
	}
}

// TestEffortAdjustableFor pins the reference's own rule at the catalogue's
// level: the effort is adjustable when the model publishes more than one
// level (【反编译】 backend.CurrentEffortSelection, 0x10274e9b0 — `cmp x1,
// 1; cset x7, gt`).
func TestEffortAdjustableFor(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	for _, tc := range []struct {
		model string
		want  bool
	}{
		{"Gemini 3.8 Flash (High)", true},
		{"GPT-OSS 120B (Medium)", false},
		{"MiniMax M3.1 Flash (High)", true},
		{"minimax-m3", false},
		{"", false},
	} {
		if got := EffortAdjustableFor(tc.model); got != tc.want {
			t.Errorf("EffortAdjustableFor(%q) = %v, want %v", tc.model, got, tc.want)
		}
	}
}

// TestEffortLabelFor pins the catalogue row a tier resolves to.
//
// The catalogue lists one row per tier and the tier rides the row's id
// ("gemini-3.8-flash-medium"), so the label is found by rebuilding the
// id from the model's *base* id and the tier.
func TestEffortLabelFor(t *testing.T) {
	configtest.WriteProviders(t, tieredProviders)
	for _, tc := range []struct {
		model, tier, want string
	}{
		{"Gemini 3.8 Flash (High)", "medium", "Gemini 3.8 Flash (Medium)"},
		{"Gemini 3.8 Flash (High)", "low", "Gemini 3.8 Flash (Low)"},
		{"gemini-3.8-flash", "high", "Gemini 3.8 Flash (High)"},
		{"MiniMax M3.1 Flash (High)", "max", "MiniMax M3.1 Flash (Max)"},
		{"minimax-m3.1-flash", "xhigh", "MiniMax M3.1 Flash (Xhigh)"},
		// A tier the model does not publish names no row.
		{"Gemini 3.8 Flash (High)", "max", ""},
		{"no-such-model", "high", ""},
		{"Gemini 3.8 Flash (High)", "", ""},
	} {
		if got := EffortLabelFor(tc.model, tc.tier); got != tc.want {
			t.Errorf("EffortLabelFor(%q, %q) = %q, want %q", tc.model, tc.tier, got, tc.want)
		}
	}
}
