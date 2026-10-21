package main

import (
	"strings"
	"testing"
)

// TestResolveModelSelection pins the 1.3.6 --model/--effort validation. Every
// "wantErr" string below was captured verbatim from the reference binary.
func TestResolveModelSelection(t *testing.T) {
	for _, tc := range []struct {
		model   string
		effort  string
		wantID  string
		wantErr string
	}{
		// Valid selections.
		{"", "", "", ""},
		{"gemini-3.8-flash", "", "gemini-3.8-flash-high", ""},
		{"gemini-3.8-flash", "low", "gemini-3.8-flash-low", ""},
		{"gemini-3.8-flash", "medium", "gemini-3.8-flash-medium", ""},
		{"gemini-3.8-flash", "high", "gemini-3.8-flash-high", ""},
		{"gpt-oss-120b", "", "gpt-oss-120b-medium", ""},
		{"gpt-oss-120b", "medium", "gpt-oss-120b-medium", ""},
		{"gemini-3.8-flash-high", "", "gemini-3.8-flash-high", ""},
		{"claude-opus-4-6-thinking", "", "claude-opus-4-6-thinking", ""},
		{"Claude Sonnet 4.6 (Thinking)", "", "claude-sonnet-4-6", ""},
		{"Gemini 3.8 Flash (High)", "", "gemini-3.8-flash-high", ""},
		// --effort with no --model resolves against the default base model.
		{"", "low", "gemini-3.8-flash-low", ""},
		{"", "high", "gemini-3.8-flash-high", ""},
		// Unknown effort name.
		{"", "bogus", "",
			`error: invalid model selection (--model "" --effort "bogus"): invalid --effort "bogus" (valid: low, medium, high, xhigh, max)`},
		// Known effort name the base model does not expose.
		{"", "xhigh", "",
			`error: invalid model selection (--model "" --effort "xhigh"): gemini-3.8-flash has no "xhigh" effort (available: low, medium, high)`},
		{"", "max", "",
			`error: invalid model selection (--model "" --effort "max"): gemini-3.8-flash has no "max" effort (available: low, medium, high)`},
		{"gpt-oss-120b", "high", "",
			`error: invalid model selection (--model "gpt-oss-120b" --effort "high"): gpt-oss-120b has no "high" effort (available: medium)`},
		{"gpt-oss-120b", "low", "",
			`error: invalid model selection (--model "gpt-oss-120b" --effort "low"): gpt-oss-120b has no "low" effort (available: medium)`},
		// --effort with a full slug conflicts; with a label or an unknown name
		// it is "not supported".
		{"gemini-3.8-flash-high", "low", "",
			`error: invalid model selection (--model "gemini-3.8-flash-high" --effort "low"): --model gemini-3.8-flash-high conflicts with --effort=low`},
		{"Gemini 3.8 Flash (High)", "low", "",
			`error: invalid model selection (--model "Gemini 3.8 Flash (High)" --effort "low"): --effort is not supported for model "Gemini 3.8 Flash (High)"`},
		{"bogus", "low", "",
			`error: invalid model selection (--model "bogus" --effort "low"): --effort is not supported for model "bogus"`},
		{"Gemini 3.8 Flash", "low", "",
			`error: invalid model selection (--model "Gemini 3.8 Flash" --effort "low"): --effort is not supported for model "Gemini 3.8 Flash"`},
		// Unknown model without --effort: message plus the available list.
		{"Gemini", "", "",
			"error: invalid model selection (--model \"Gemini\" --effort \"\"): model Gemini is not recognized as a known model or custom model in settings\nAvailable models:\n  Gemini 3.8 Flash (High)\n  Gemini 3.8 Flash (Medium)\n  Gemini 3.8 Flash (Low)\n  Claude Sonnet 4.6 (Thinking)\n  Claude Opus 4.6 (Thinking)\n  GPT-OSS 120B (Medium)\n  MiniMax M3"},
		{"claude-sonnet-4.6-thinking", "", "",
			"error: invalid model selection (--model \"claude-sonnet-4.6-thinking\" --effort \"\"): model claude-sonnet-4.6-thinking is not recognized as a known model or custom model in settings\nAvailable models:\n  Gemini 3.8 Flash (High)\n  Gemini 3.8 Flash (Medium)\n  Gemini 3.8 Flash (Low)\n  Claude Sonnet 4.6 (Thinking)\n  Claude Opus 4.6 (Thinking)\n  GPT-OSS 120B (Medium)\n  MiniMax M3"},
	} {
		gotID, err := resolveModelSelection(tc.model, tc.effort)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("resolveModelSelection(%q, %q) = error %v, want id %q", tc.model, tc.effort, err, tc.wantID)
				continue
			}
			if gotID != tc.wantID {
				t.Errorf("resolveModelSelection(%q, %q) = %q, want %q", tc.model, tc.effort, gotID, tc.wantID)
			}
			continue
		}
		if err == nil {
			t.Errorf("resolveModelSelection(%q, %q) = %q, want error", tc.model, tc.effort, gotID)
			continue
		}
		if err.Error() != tc.wantErr {
			t.Errorf("resolveModelSelection(%q, %q) error =\n%q\nwant\n%q", tc.model, tc.effort, err.Error(), tc.wantErr)
		}
	}
}

// TestAvailableModelsBlock pins the "Available models:" list appended to the
// unknown-model error. Includes MiniMax M3 since the catalogue now lists it.
func TestAvailableModelsBlock(t *testing.T) {
	want := "Available models:\n  Gemini 3.8 Flash (High)\n  Gemini 3.8 Flash (Medium)\n  Gemini 3.8 Flash (Low)\n  Claude Sonnet 4.6 (Thinking)\n  Claude Opus 4.6 (Thinking)\n  GPT-OSS 120B (Medium)\n  MiniMax M3"
	if got := availableModelsBlock(); got != want {
		t.Errorf("availableModelsBlock() =\n%s\nwant\n%s", got, want)
	}
}

// TestCatalogueHelpers checks the slug/label/id helpers the selection logic
// relies on.
func TestCatalogueHelpers(t *testing.T) {
	if id, ok := catalogueIDBySlug("claude-sonnet-4-6"); !ok || id != "claude-sonnet-4-6" {
		t.Errorf("catalogueIDBySlug(claude-sonnet-4-6) = %q, %v", id, ok)
	}
	if _, ok := catalogueIDBySlug("Claude Sonnet 4.6 (Thinking)"); ok {
		t.Error("catalogueIDBySlug must not match a display label")
	}
	if got := catalogueLabel("claude-opus-4-6-thinking"); got != "Claude Opus 4.6 (Thinking)" {
		t.Errorf("catalogueLabel = %q", got)
	}
	if !strings.Contains(validEfforts[0], "low") {
		t.Errorf("validEfforts[0] = %q", validEfforts[0])
	}
}
