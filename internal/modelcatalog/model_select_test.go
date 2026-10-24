package modelcatalog

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// availableAll is the "Available models:" block the clone appends — the six
// the reference prints.
//
// The provider configuration is isolated in these tests, so the block is the
// reference's own catalogue and nothing else: a test that depended on what
// the machine happened to have configured would pass here and fail
// everywhere else. A gated model is not in the block: the plan gate is not
// an advertisement.
const availableAll = "Available models:\n  Gemini 3.8 Flash (High)\n  Gemini 3.8 Flash (Medium)\n  Gemini 3.8 Flash (Low)\n  Claude Sonnet 4.6 (Thinking)\n  Claude Opus 4.6 (Thinking)\n  GPT-OSS 120B (Medium)"

// TestResolveModelSelection pins the 1.3.6 --model/--effort validation. Every
// "wantErr" string below was captured verbatim from the reference binary.
func TestResolveModelSelection(t *testing.T) {
	configtest.Isolate(t)
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
			"error: invalid model selection (--model \"Gemini\" --effort \"\"): model Gemini is not recognized as a known model or custom model in settings\n" + availableAll},
		{"claude-sonnet-4.6-thinking", "", "",
			"error: invalid model selection (--model \"claude-sonnet-4.6-thinking\" --effort \"\"): model claude-sonnet-4.6-thinking is not recognized as a known model or custom model in settings\n" + availableAll},
		// Gated model (一手, live 1.4.4): the server advertises it, the plan
		// refuses it. The id and the display label take the same path, and
		// --effort does not change the answer.
		{"gemini-4-argon-disabled", "", "",
			`error: invalid model selection (--model "gemini-4-argon-disabled" --effort ""): "Gemini 4 Argon" is unavailable: Gemini 4 Argon is currently available to Ultra and paid Pro plan subscribers` + "\n" + availableAll},
		{"Gemini 4 Argon", "", "",
			`error: invalid model selection (--model "Gemini 4 Argon" --effort ""): "Gemini 4 Argon" is unavailable: Gemini 4 Argon is currently available to Ultra and paid Pro plan subscribers` + "\n" + availableAll},
		{"gemini-4-argon-disabled", "high", "",
			`error: invalid model selection (--model "gemini-4-argon-disabled" --effort "high"): "Gemini 4 Argon" is unavailable: Gemini 4 Argon is currently available to Ultra and paid Pro plan subscribers` + "\n" + availableAll},
		// The bare name is *not* a known model — the gate is keyed on the
		// advertised id (and its label), not on a prefix.
		{"gemini-4-argon", "", "",
			"error: invalid model selection (--model \"gemini-4-argon\" --effort \"\"): model gemini-4-argon is not recognized as a known model or custom model in settings\n" + availableAll},
		// ...but a bogus --effort is still refused first.
		{"gemini-4-argon-disabled", "bogus", "",
			`error: invalid model selection (--model "gemini-4-argon-disabled" --effort "bogus"): invalid --effort "bogus" (valid: low, medium, high, xhigh, max)`},
	} {
		gotID, err := ResolveModelSelection(tc.model, tc.effort)
		if tc.wantErr == "" {
			if err != nil {
				t.Errorf("ResolveModelSelection(%q, %q) = error %v, want id %q", tc.model, tc.effort, err, tc.wantID)
				continue
			}
			if gotID != tc.wantID {
				t.Errorf("ResolveModelSelection(%q, %q) = %q, want %q", tc.model, tc.effort, gotID, tc.wantID)
			}
			continue
		}
		if err == nil {
			t.Errorf("ResolveModelSelection(%q, %q) = %q, want error", tc.model, tc.effort, gotID)
			continue
		}
		if err.Error() != tc.wantErr {
			t.Errorf("ResolveModelSelection(%q, %q) error =\n%q\nwant\n%q", tc.model, tc.effort, err.Error(), tc.wantErr)
		}
	}
}

// TestAvailableModelsBlock pins the "Available models:" list appended to the
// unknown-model error. It is the reference's own catalogue.
func TestAvailableModelsBlock(t *testing.T) {
	configtest.Isolate(t)
	want := availableAll
	if got := availableModelsBlock(); got != want {
		t.Errorf("availableModelsBlock() =\n%s\nwant\n%s", got, want)
	}
}

// TestAvailableModelsBlockIncludesConfiguredModels pins that a model the user
// configured is advertised after the reference's own rows. The clone has no
// hard-coded model table: an extra row exists because the user configured it.
func TestAvailableModelsBlockIncludesConfiguredModels(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}]}}}`)
	want := availableAll + "\n  MiniMax M3"
	if got := availableModelsBlock(); got != want {
		t.Errorf("availableModelsBlock() =\n%s\nwant\n%s", got, want)
	}
}

// TestCatalogueHelpers checks the slug/label/id helpers the selection logic
// relies on.
func TestCatalogueHelpers(t *testing.T) {
	if id, ok := CatalogueIDBySlug("claude-sonnet-4-6"); !ok || id != "claude-sonnet-4-6" {
		t.Errorf("CatalogueIDBySlug(claude-sonnet-4-6) = %q, %v", id, ok)
	}
	if _, ok := CatalogueIDBySlug("Claude Sonnet 4.6 (Thinking)"); ok {
		t.Error("CatalogueIDBySlug must not match a display label")
	}
	if got := CatalogueLabel("claude-opus-4-6-thinking"); got != "Claude Opus 4.6 (Thinking)" {
		t.Errorf("CatalogueLabel = %q", got)
	}
	if !strings.Contains(ValidEfforts[0], "low") {
		t.Errorf("ValidEfforts[0] = %q", ValidEfforts[0])
	}
}
