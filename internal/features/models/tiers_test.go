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

// TestInvalidEffortMessage pins the reference's answer to a level that is
// not one of the effort words.
//
// 【一手】 agy 1.4.12: the binary carries the format string
// `Invalid effort %q (valid: %s)` (offset 0x3680e49), and the documented
// instance is `Invalid effort "turbo" (valid: low, medium, high, xhigh,
// max)` (official-docs/cli/commands/effort.md:57).
//
// The valid list is the vocabulary above, so the message and the accepted
// set cannot drift apart.
func TestInvalidEffortMessage(t *testing.T) {
	want := `Invalid effort "turbo" (valid: low, medium, high, xhigh, max)`
	if got := InvalidEffortMessage("turbo"); got != want {
		t.Errorf("InvalidEffortMessage(%q) = %q, want %q", "turbo", got, want)
	}
	// The word is quoted, so an empty or odd word is still readable.
	if got := InvalidEffortMessage(""); got != `Invalid effort "" (valid: low, medium, high, xhigh, max)` {
		t.Errorf("InvalidEffortMessage(\"\") = %q", got)
	}
	// The list in the message is the vocabulary.
	if !strings.Contains(InvalidEffortMessage("x"), strings.Join(EffortLevels, ", ")) {
		t.Errorf("InvalidEffortMessage does not carry the vocabulary: %q", InvalidEffortMessage("x"))
	}
}

// TestEffortAdjustable pins the reference's own rule: the effort is
// adjustable when the model publishes more than one level.
//
// 【反编译】 agy 1.4.12, backend.CurrentEffortSelection at 0x10274e920:
// the flag the reference hands to the /effort command is computed as
// `cmp x1, 1; cset x7, gt` at 0x10274e9b0 — "more than one level".
func TestEffortAdjustable(t *testing.T) {
	cases := []struct {
		name  string
		tiers []string
		want  bool
	}{
		{"no levels", nil, false},
		{"one level", []string{"medium"}, false},
		{"two levels", []string{"medium", "high"}, true},
		{"three levels", []string{"low", "medium", "high"}, true},
		{"five levels", []string{"low", "medium", "high", "xhigh", "max"}, true},
		// The list is normalised first: a repeated level is one level.
		{"one level twice", []string{"medium", "medium"}, false},
		{"one level, padded", []string{" medium "}, false},
	}
	for _, c := range cases {
		if got := EffortAdjustable(c.tiers); got != c.want {
			t.Errorf("EffortAdjustable(%v) = %v, want %v", c.tiers, got, c.want)
		}
	}
}

// TestNotAdjustableMessage pins the reference's answer to an effort
// change it refuses.
//
// The reference's own format string is `Effort isn't adjustable for %s.`
// — 【strings】 agy 1.4.12, file offset 0x36945ec, and the binary stores
// the length 31 beside it — and the command fills the %s with the
// model's name
// (【反编译】 the fmt.Sprintf call at 0x102b092c0 in
// commands.(*effortCommand).Execute.func1).
//
// A model with no name of its own is answered with the reference's own
// "the current model" — 【strings】 file offset 0x35c7811, the length 17
// beside it.
func TestNotAdjustableMessage(t *testing.T) {
	cases := []struct{ model, want string }{
		{"Gemini 3.8 Flash", "Effort isn't adjustable for Gemini 3.8 Flash."},
		{"", "Effort isn't adjustable for the current model."},
		{"   ", "Effort isn't adjustable for the current model."},
	}
	for _, c := range cases {
		if got := NotAdjustableMessage(c.model); got != c.want {
			t.Errorf("NotAdjustableMessage(%q) = %q, want %q", c.model, got, c.want)
		}
	}
	// The panel's own answer is a different string: it carries no
	// period and names no model. The two are not interchangeable.
	if NotAdjustableMessage("") == "Effort isn't adjustable for the current model" {
		t.Error("the command's answer must not be the panel's answer")
	}
}

// TestBaseLabel pins the model's name without its tier.
//
// The reference's answer to an effort change it refuses names the model
// ("Effort isn't adjustable for %s."), and the tier is the model's
// setting rather than part of its name.
func TestBaseLabel(t *testing.T) {
	cases := []struct{ label, want string }{
		{"Gemini 3.8 Flash (High)", "Gemini 3.8 Flash"},
		{"MiniMax M3.1 Flash (Xhigh)", "MiniMax M3.1 Flash"},
		{"MiniMax M3", "MiniMax M3"},
		{"Claude Sonnet 4.6 (Thinking)", "Claude Sonnet 4.6 (Thinking)"},
		{"", ""},
	}
	for _, c := range cases {
		if got := BaseLabel(c.label); got != c.want {
			t.Errorf("BaseLabel(%q) = %q, want %q", c.label, got, c.want)
		}
	}
}

// TestTierNameAtAndTierIndexOf pins the two halves of the model's own
// list of tiers: the word at an index, and the index of a word.
//
// The list is the model's own — a model that publishes "max" but not
// "xhigh" has "max" at index 3 — so the word cannot come from the
// vocabulary alone.
func TestTierNameAtAndTierIndexOf(t *testing.T) {
	tiers := []string{"low", "medium", "high", "xhigh", "max"}
	for i, want := range tiers {
		if got := TierNameAt(tiers, i); got != want {
			t.Errorf("TierNameAt(%v, %d) = %q, want %q", tiers, i, got, want)
		}
	}
	if got := TierNameAt(tiers, -1); got != "" {
		t.Errorf("TierNameAt(-1) = %q, want empty", got)
	}
	if got := TierNameAt(tiers, 5); got != "" {
		t.Errorf("TierNameAt(5) = %q, want empty", got)
	}
	for i, want := range tiers {
		if got := TierIndexOf(tiers, want); got != i {
			t.Errorf("TierIndexOf(%q) = %d, want %d", want, got, i)
		}
	}
	// The comparison is the vocabulary's own: trimmed and lowercased.
	if got := TierIndexOf(tiers, " MAX "); got != 4 {
		t.Errorf("TierIndexOf(\" MAX \") = %d, want 4", got)
	}
	if got := TierIndexOf(tiers, "ultra"); got != -1 {
		t.Errorf("TierIndexOf(\"ultra\") = %d, want -1", got)
	}
	if got := TierIndexOf(tiers, ""); got != -1 {
		t.Errorf("TierIndexOf(\"\") = %d, want -1", got)
	}
}
