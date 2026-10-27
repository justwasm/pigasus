package modelcatalog

// model_resolve_test.go — cycle 99 effective-model resolution tests.
//
// These pin the reference's flag → settings → default precedence and its
// catalogue check, which is what makes the TUI render
//
//	⚠ Warning
//	  ⎿  "<requested>" is no longer available. Using "Gemini 3.8 Flash (High)".
//
// for a configured model the catalogue no longer offers (live 1.3.6 capture
// with `"model": "MiniMax-M3"` in settings.json).

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/configtest"
)

// withSettingsModel points HOME at a scratch tree whose settings.json carries
// the given model, then restores the environment.
//
// The provider configuration is isolated too: the catalogue a test sees must
// be the reference's own and nothing else, or the test would depend on what
// the machine running it happens to have configured.
func withSettingsModel(t *testing.T, model string) {
	t.Helper()
	configtest.Isolate(t)
	home := t.TempDir()
	if model != "" {
		dir := filepath.Join(home, ".gemini", "antigravity-cli")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		body := `{"model": "` + model + `"}`
		if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	oldHome, hadHome := os.LookupEnv("HOME")
	oldAH, hadAH := os.LookupEnv("ANTIGRAVITY_HOME")
	os.Setenv("HOME", home)
	os.Unsetenv("ANTIGRAVITY_HOME")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("HOME", oldHome)
		} else {
			os.Unsetenv("HOME")
		}
		if hadAH {
			os.Setenv("ANTIGRAVITY_HOME", oldAH)
		} else {
			os.Unsetenv("ANTIGRAVITY_HOME")
		}
	})
}

// TestResolveEffectiveModel_FlagWins pins that an explicit --model is used
// verbatim when it is in the catalogue.
func TestResolveEffectiveModel_FlagWins(t *testing.T) {
	withSettingsModel(t, "MiniMax-M3")
	got, from := ResolveEffectiveModel("Claude Sonnet 4.6 (Thinking)")
	if got != "Claude Sonnet 4.6 (Thinking)" || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestResolveEffectiveModel_UnavailableSettingsFallsBack is the recovered
// behaviour: a settings.json model that is not in the catalogue is replaced
// by the reference's default model and reported as the fallback source so
// the TUI can render the "is no longer available" warning.
//
// A model the user configured is in the catalogue by construction, so this
// path is only reached for a model nobody offers any more.
func TestResolveEffectiveModel_UnavailableSettingsFallsBack(t *testing.T) {
	withSettingsModel(t, "MiniMax-M3")
	got, from := ResolveEffectiveModel("")
	if got != DefaultModel {
		t.Errorf("effective = %q, want %q", got, DefaultModel)
	}
	if from != "MiniMax-M3" {
		t.Errorf("fallbackFrom = %q, want MiniMax-M3", from)
	}
}

// TestResolveEffectiveModel_ConfiguredModelResolves pins that a model the
// user configured is selectable and keeps its own label — the clone has no
// hard-coded model table, so the configuration is the only source.
func TestResolveEffectiveModel_ConfiguredModelResolves(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "custom-model", "name": "Custom Model", "model": "Upstream-1"}]}}}`)
	got, from := ResolveEffectiveModel("custom-model")
	if got != "Custom Model" || from != "" {
		t.Fatalf("got (%q, %q), want (%q, %q)", got, from, "Custom Model", "")
	}
	// The upstream spelling resolves to the same entry: the user may have
	// pasted the id the provider documents.
	got, from = ResolveEffectiveModel("Upstream-1")
	if got != "Custom Model" || from != "" {
		t.Fatalf("upstream spelling: got (%q, %q), want (%q, %q)", got, from, "Custom Model", "")
	}
}

// TestResolveEffectiveModel_ConfiguredTieredModelBareID pins that the bare
// id of a configured model that declares reasoning tiers resolves to that
// model's default tier.
//
// The catalogue lists one row per tier ("custom-model-max"), so the bare id
// ("custom-model") is not a row of its own. It still names the model the
// user configured, and falling through to the reference's default model
// would send the request to the CloudCode account instead of the configured
// endpoint — with the reference's own quota as the limit.
func TestResolveEffectiveModel_ConfiguredTieredModelBareID(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "custom-model", "name": "Custom Model", "model": "Upstream-1",
		            "reasoning_efforts": ["low", "medium", "high"], "reasoning_effort": "medium"}]}}}`)
	got, from := ResolveEffectiveModel("custom-model")
	if got != "Custom Model (Medium)" || from != "" {
		t.Fatalf("got (%q, %q), want (%q, %q)", got, from, "Custom Model (Medium)", "")
	}
}

// TestModelCatalogueIncludesConfiguredModels pins that a configured model
// reaches the catalogue, and that it does not displace a reference model
// whose id it collides with.
func TestModelCatalogueIncludesConfiguredModels(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://host/v1",
		"models": [{"id": "custom-model", "name": "Custom Model"},
		           {"id": "gemini-3.8-flash-low", "name": "Collides"}]}}}`)
	found := false
	for _, m := range cli.ModelCatalogue() {
		if m.ID == "custom-model" {
			found = true
			if m.Label != "Custom Model" {
				t.Errorf("label = %q, want %q", m.Label, "Custom Model")
			}
		}
		if m.ID == "gemini-3.8-flash-low" && m.Label != "Gemini 3.8 Flash (Low)" {
			t.Errorf("the reference row must win the collision, got label %q", m.Label)
		}
	}
	if !found {
		t.Error("the configured model is missing from the catalogue")
	}
}

// TestResolveEffectiveModel_SettingsSlugAccepted verifies that valid catalogue
// slugs stored in settings.json (such as "gemini-3.8-flash-high" or
// "claude-sonnet-4-6", which agy itself writes) resolve cleanly to their
// display label without triggering a spurious "no longer available" warning.
func TestResolveEffectiveModel_SettingsSlugAccepted(t *testing.T) {
	for _, tc := range []struct {
		slug      string
		wantLabel string
	}{
		{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
		{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
		{"gemini-3.8-flash", "Gemini 3.8 Flash (High)"},
	} {
		withSettingsModel(t, tc.slug)
		got, from := ResolveEffectiveModel("")
		if got != tc.wantLabel || from != "" {
			t.Fatalf("slug %q: got (%q, %q), want (%q, %q)", tc.slug, got, from, tc.wantLabel, "")
		}
	}
}

// TestResolveEffectiveModel_UnavailableSlugWarns verifies that a model slug
// that is genuinely not in the catalogue falls back to DefaultModel and
// produces the unavailableFrom warning.
func TestResolveEffectiveModel_UnavailableSlugWarns(t *testing.T) {
	withSettingsModel(t, "gemini-1.5-pro")
	got, from := ResolveEffectiveModel("")
	if got != DefaultModel || from != "gemini-1.5-pro" {
		t.Fatalf("got (%q, %q), want (%q, %q)", got, from, DefaultModel, "gemini-1.5-pro")
	}
}

// TestResolveEffectiveModel_AvailableSettingsIsUsed pins that a settings model
// the catalogue still offers is used with no warning.
func TestResolveEffectiveModel_AvailableSettingsIsUsed(t *testing.T) {
	withSettingsModel(t, "Gemini 3.8 Flash (Low)")
	got, from := ResolveEffectiveModel("")
	if got != "Gemini 3.8 Flash (Low)" || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestResolveEffectiveModel_DefaultsWithNoSettings pins the empty case.
func TestResolveEffectiveModel_DefaultsWithNoSettings(t *testing.T) {
	withSettingsModel(t, "")
	got, from := ResolveEffectiveModel("")
	if got != DefaultModel || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestModelAvailable pins the catalogue lookup by both label and slug.
func TestModelAvailable(t *testing.T) {
	for _, name := range []string{"Gemini 3.8 Flash (High)", "gemini-3.8-flash-high", "GPT-OSS 120B (Medium)"} {
		if !ModelAvailable(name) {
			t.Errorf("ModelAvailable(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"MiniMax-M3", "", "nope"} {
		if ModelAvailable(name) {
			t.Errorf("ModelAvailable(%q) = true, want false", name)
		}
	}
}
