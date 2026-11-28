package tui

// model_status_label_test.go — the status row's label for a configured
// model whose name carries its own parenthetical tag.
//
// The bug these tests pin (2026-11-26 report): selecting
// "DeepSeek V4.1 Flash (1017)" from /model left the status row showing
// "DeepSeek V4.1 Flash" — the "(1017)" was stripped as if it were an
// effort tier. It is not: "(1017)" is part of the model's name, and the
// reference shows the label verbatim. The same rule keeps Claude's
// "(Thinking)".

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// taggedProviders is the shape of the user's own configuration: one
// provider listing two DeepSeek variants whose *names* differ only in a
// parenthetical tag.
const taggedProviders = `{"providers": {"hiyo-free": {
	"type": "openai", "base_url": "https://example.invalid/v1", "api_key": "sk-x",
	"models": [
		{"id": "deepseek-v4.1-flash", "name": "DeepSeek V4.1 Flash", "model": "deepseek-v4.1-flash"},
		{"id": "deepseek-v4.1-flash-1017", "name": "DeepSeek V4.1 Flash (1017)", "model": "deepseek-v4.1-flash-1017"}
	]}}}`

// TestStatusLabelKeepsModelOwnParenthetical pins that a configured
// model's own "(1017)" survives on the status row.
func TestStatusLabelKeepsModelOwnParenthetical(t *testing.T) {
	configtest.WriteProviders(t, taggedProviders)

	m := newRootModel(&fakeClient{}, "u@t", "DeepSeek V4.1 Flash (1017)", "t", "/", false)
	if m.session.model != "DeepSeek V4.1 Flash (1017)" {
		t.Fatalf("session.model = %q, want the label verbatim", m.session.model)
	}
	if m.session.statusModel != "DeepSeek V4.1 Flash (1017)" {
		t.Errorf("statusModel = %q, want the label verbatim (the tag is not a tier)",
			m.session.statusModel)
	}
	// The model declares no reasoning tiers, so there is no " · high".
	if got := m.statusEffortSuffix(); got != "" {
		t.Errorf("statusEffortSuffix() = %q, want empty for a tier-less model", got)
	}
	if row := plainRow(m.statusRow()); !strings.Contains(row, "DeepSeek V4.1 Flash (1017)") {
		t.Errorf("status row = %q, want it to carry the full label", row)
	}
}

// TestStatusLabelStripsOnlyEffortTiers pins the two sides of the rule:
// an effort tier in brackets is stripped, any other bracket is kept.
func TestStatusLabelStripsOnlyEffortTiers(t *testing.T) {
	configtest.Isolate(t)
	for _, tc := range []struct{ in, want string }{
		{"Gemini 3.8 Flash (High)", "Gemini 3.8 Flash"},
		{"Gemini 4 Hydrogen (XHigh)", "Gemini 4 Hydrogen"},
		{"Claude Sonnet 4.6 (Thinking)", "Claude Sonnet 4.6 (Thinking)"},
		{"DeepSeek V4.1 Flash (1017)", "DeepSeek V4.1 Flash (1017)"},
		{"DeepSeek V4.1 Flash (alias)", "DeepSeek V4.1 Flash (alias)"},
		{"MiniMax M3", "MiniMax M3"},
	} {
		if got := tuilib.StripTier(tc.in); got != tc.want {
			t.Errorf("StripTier(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestStatusSuffixOnlyForMultiLevelModels pins that the " · <tier>"
// suffix belongs to a model that publishes more than one level. Claude's
// fixed "(Thinking)" row and GPT-OSS's single "Medium" have none.
func TestStatusSuffixOnlyForMultiLevelModels(t *testing.T) {
	configtest.Isolate(t)
	for _, tc := range []struct {
		label string
		want  bool
	}{
		{"Gemini 3.8 Flash (High)", true},
		{"Claude Sonnet 4.6 (Thinking)", false},
		{"Claude Opus 4.6 (Thinking)", false},
		{"GPT-OSS 120B (Medium)", false},
	} {
		m := newRootModel(&fakeClient{}, "u@t", tc.label, "t", "/", false)
		gotSuffix := m.statusEffortSuffix() != ""
		if gotSuffix != tc.want {
			t.Errorf("%q: effort suffix present = %v, want %v (row %q)",
				tc.label, gotSuffix, tc.want, plainRow(m.statusRow()))
		}
	}
}
