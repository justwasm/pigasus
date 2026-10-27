// Package models — tiers_test.go
//
// Tests for the per-model reasoning tiers: the vocabulary, the expansion
// of a model that declares tiers into one row per tier, and the tier's
// ride on the catalogue id into the registry's lookup.
package models

import (
	"strings"
	"testing"
)

// tieredModel is a model that declares the five tiers, the way an
// operator's own model would.
func tieredModel() Model {
	return Model{
		Name:              "minimax-m3.1-flash",
		DisplayName:       "MiniMax M3.1 Flash",
		Provider:          "openai",
		APIURL:            "https://gw.example/v1/chat/completions",
		ExternalModelName: "MiniMax-M3.1-Flash",
		ReasoningEffort:   "high",
		ReasoningEfforts:  []string{"low", "medium", "high", "xhigh", "max"},
	}
}

// TestEffortLevels pins the vocabulary. It is the closed set the Web UI's
// dispatcher accepts, and the one a configuration may declare against.
func TestEffortLevels(t *testing.T) {
	want := []string{"low", "medium", "high", "xhigh", "max"}
	if len(EffortLevels) != len(want) {
		t.Fatalf("EffortLevels = %v, want %v", EffortLevels, want)
	}
	for i, w := range want {
		if EffortLevels[i] != w {
			t.Errorf("EffortLevels[%d] = %q, want %q", i, EffortLevels[i], w)
		}
	}
	for _, ok := range want {
		if !IsEffortLevel(ok) {
			t.Errorf("IsEffortLevel(%q) = false, want true", ok)
		}
	}
	// The predicate is lenient about the case and the blanks — the
	// tiers are normalised before they are used.
	if !IsEffortLevel(" HIGH ") {
		t.Error(`IsEffortLevel(" HIGH ") = false, want true`)
	}
	for _, bad := range []string{"", "off", "minimal", "ultra", "highest"} {
		if IsEffortLevel(bad) {
			t.Errorf("IsEffortLevel(%q) = true, want false", bad)
		}
	}
}

// TestNormalizeEfforts pins the cleanup: the case, the blanks and the
// duplicates go, the order the operator wrote stays.
func TestNormalizeEfforts(t *testing.T) {
	got := NormalizeEfforts([]string{" High", "low", "", "high", "  ", "MAX"})
	want := []string{"high", "low", "max"}
	if len(got) != len(want) {
		t.Fatalf("NormalizeEfforts = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("NormalizeEfforts = %v, want %v", got, want)
		}
	}
	if NormalizeEfforts(nil) != nil {
		t.Errorf("NormalizeEfforts(nil) = %v, want nil", NormalizeEfforts(nil))
	}
}

// TestValidateEfforts pins the refusal of a tier nobody can select.
func TestValidateEfforts(t *testing.T) {
	if err := ValidateEfforts([]string{"low", "high", "xhigh"}); err != nil {
		t.Errorf("ValidateEfforts(valid) = %v, want nil", err)
	}
	err := ValidateEfforts([]string{"low", "ultra"})
	if err == nil {
		t.Fatal("ValidateEfforts(unknown) = nil, want an error")
	}
	if !strings.Contains(err.Error(), "ultra") {
		t.Errorf("the error does not name the offending tier: %v", err)
	}
	if !strings.Contains(err.Error(), "xhigh") {
		t.Errorf("the error does not name the valid set: %v", err)
	}
}

// TestEffortTierRows pins the expansion: one row per tier, the tier in the
// id and in the label, and the tier as the row's reasoning effort.
func TestEffortTierRows(t *testing.T) {
	rows := EffortTierRows(tieredModel())
	if len(rows) != 5 {
		t.Fatalf("EffortTierRows returned %d rows, want 5", len(rows))
	}
	wantID := []string{
		"minimax-m3.1-flash-low",
		"minimax-m3.1-flash-medium",
		"minimax-m3.1-flash-high",
		"minimax-m3.1-flash-xhigh",
		"minimax-m3.1-flash-max",
	}
	wantLabel := []string{
		"MiniMax M3.1 Flash (Low)",
		"MiniMax M3.1 Flash (Medium)",
		"MiniMax M3.1 Flash (High)",
		"MiniMax M3.1 Flash (Xhigh)",
		"MiniMax M3.1 Flash (Max)",
	}
	for i, row := range rows {
		if row.Name != wantID[i] {
			t.Errorf("row %d id = %q, want %q", i, row.Name, wantID[i])
		}
		if row.DisplayName != wantLabel[i] {
			t.Errorf("row %d label = %q, want %q", i, row.DisplayName, wantLabel[i])
		}
		if row.ReasoningEffort != strings.TrimPrefix(wantID[i], "minimax-m3.1-flash-") {
			t.Errorf("row %d effort = %q, want the tier", i, row.ReasoningEffort)
		}
		// The upstream id is the model's own — the tier is the clone's
		// word, not the endpoint's.
		if row.ExternalModelName != "MiniMax-M3.1-Flash" {
			t.Errorf("row %d upstream id = %q, want MiniMax-M3.1-Flash", i, row.ExternalModelName)
		}
	}
}

// TestEffortTierRowsWithoutTiers pins the plain model: one row, itself.
func TestEffortTierRowsWithoutTiers(t *testing.T) {
	m := Model{Name: "minimax-m3", DisplayName: "MiniMax M3"}
	rows := EffortTierRows(m)
	if len(rows) != 1 {
		t.Fatalf("EffortTierRows returned %d rows, want 1", len(rows))
	}
	if rows[0].Name != "minimax-m3" || rows[0].DisplayName != "MiniMax M3" {
		t.Errorf("the row changed: %+v", rows[0])
	}
}

// TestTierRowsKeepsTheOrder pins the catalogue's order: the models as they
// come, the tiers as they were declared.
func TestTierRowsKeepsTheOrder(t *testing.T) {
	rows := TierRows([]Model{
		tieredModel(),
		{Name: "aaa", DisplayName: "Aaa"},
	})
	if len(rows) != 6 {
		t.Fatalf("TierRows returned %d rows, want 6", len(rows))
	}
	if rows[0].Name != "minimax-m3.1-flash-low" || rows[4].Name != "minimax-m3.1-flash-max" {
		t.Errorf("the tier order changed: %+v", rows[:5])
	}
	if rows[5].Name != "aaa" {
		t.Errorf("the model order changed: %+v", rows[5])
	}
}

// TestDefaultEffort pins the tier a model's bare id stands for: the
// configuration's own word when it names a tier, else the reference's own
// preference (medium), else the first tier.
func TestDefaultEffort(t *testing.T) {
	if got := DefaultEffort(tieredModel()); got != "high" {
		t.Errorf("DefaultEffort = %q, want high", got)
	}
	m := tieredModel()
	m.ReasoningEffort = ""
	if got := DefaultEffort(m); got != "medium" {
		t.Errorf("DefaultEffort = %q, want medium", got)
	}
	m = tieredModel()
	m.ReasoningEffort = "default"
	m.ReasoningEfforts = []string{"low", "xhigh"}
	if got := DefaultEffort(m); got != "low" {
		t.Errorf("DefaultEffort = %q, want low", got)
	}
	if got := DefaultEffort(Model{Name: "plain"}); got != "" {
		t.Errorf("DefaultEffort(no tiers) = %q, want empty", got)
	}
}

// TestBaseModelIDAndTierFromID pin the id's two halves.
func TestBaseModelIDAndTierFromID(t *testing.T) {
	cases := []struct{ id, base, tier string }{
		{"minimax-m3.1-flash-xhigh", "minimax-m3.1-flash", "xhigh"},
		{"gemini-3.8-flash-high", "gemini-3.8-flash", "high"},
		{"claude-sonnet-4-6", "claude-sonnet-4-6", ""},
		{"claude-opus-4-6-thinking", "claude-opus-4-6-thinking", ""},
	}
	for _, c := range cases {
		if got := BaseModelID(c.id); got != c.base {
			t.Errorf("BaseModelID(%q) = %q, want %q", c.id, got, c.base)
		}
		if got := TierFromID(c.id); got != c.tier {
			t.Errorf("TierFromID(%q) = %q, want %q", c.id, got, c.tier)
		}
	}
}

// TestTierFromLabel pins the label's tier.
func TestTierFromLabel(t *testing.T) {
	cases := []struct{ label, tier string }{
		{"MiniMax M3.1 Flash (Xhigh)", "xhigh"},
		{"Gemini 3.8 Flash (High)", "high"},
		{"Claude Sonnet 4.6 (Thinking)", ""},
		{"MiniMax M3", ""},
		{"", ""},
	}
	for _, c := range cases {
		if got := TierFromLabel(c.label); got != c.tier {
			t.Errorf("TierFromLabel(%q) = %q, want %q", c.label, got, c.tier)
		}
	}
}

// TestRegistryGetByNameFindsATierRow pins the tier's ride on the
// catalogue id: the id the picker sends back names the base model at that
// tier, and the model that comes back carries the tier.
func TestRegistryGetByNameFindsATierRow(t *testing.T) {
	r := &Registry{providers: map[string]ProviderConfig{}}
	if err := r.SetProvider("agw", ProviderConfig{
		Type:   "openai",
		APIURL: "https://gw.example/v1/chat/completions",
		APIKey: "-",
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if err := r.AddProviderModel("agw", ProviderModel{
		ID:               "minimax-m3.1-flash",
		Name:             "MiniMax M3.1 Flash",
		Model:            "MiniMax-M3.1-Flash",
		ReasoningEffort:  "high",
		ReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"},
	}); err != nil {
		t.Fatalf("AddProviderModel: %v", err)
	}

	m, ok := r.GetByName("minimax-m3.1-flash")
	if !ok {
		t.Fatal("the base model was not found")
	}
	if m.ReasoningEffort != "high" {
		t.Errorf("the base model's effort = %q, want high", m.ReasoningEffort)
	}

	m, ok = r.GetByName("minimax-m3.1-flash-xhigh")
	if !ok {
		t.Fatal("the tier row was not found")
	}
	if m.ReasoningEffort != "xhigh" {
		t.Errorf("the tier row's effort = %q, want xhigh", m.ReasoningEffort)
	}
	if m.ExternalModelName != "MiniMax-M3.1-Flash" {
		t.Errorf("the tier row's upstream id = %q, want MiniMax-M3.1-Flash", m.ExternalModelName)
	}
	if m.Name != "minimax-m3.1-flash-xhigh" {
		t.Errorf("the tier row's name = %q, want the row's own id", m.Name)
	}

	// A tier the model does not offer names no model.
	if _, ok := r.GetByName("minimax-m3.1-flash-ultra"); ok {
		t.Error("an unknown tier was found")
	}
	// A model that declares no tiers is not reachable through a suffix
	// either: its id is its own.
	if _, ok := r.GetByName("minimax-m3.1-flash-max"); !ok {
		t.Error("a declared tier was not found")
	}
}

// TestRegistryGetByNameIgnoresASuffixOnAPlainModel pins that a model which
// declares no tiers keeps its own id: nothing is addressed by a suffix it
// never offered.
func TestRegistryGetByNameIgnoresASuffixOnAPlainModel(t *testing.T) {
	r := &Registry{providers: map[string]ProviderConfig{}}
	if err := r.SetProvider("agw", ProviderConfig{
		Type:   "openai",
		APIURL: "https://gw.example/v1/chat/completions",
		APIKey: "-",
	}); err != nil {
		t.Fatalf("SetProvider: %v", err)
	}
	if err := r.AddProviderModel("agw", ProviderModel{
		ID:    "minimax-m3",
		Name:  "MiniMax M3",
		Model: "MiniMax-M3",
	}); err != nil {
		t.Fatalf("AddProviderModel: %v", err)
	}
	if _, ok := r.GetByName("minimax-m3"); !ok {
		t.Fatal("the model was not found")
	}
	if _, ok := r.GetByName("minimax-m3-high"); ok {
		t.Error("a model with no tiers was found through a tier suffix")
	}
}
