package main

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
)

// withSettingsModel points HOME at a scratch tree whose settings.json carries
// the given model, then restores the environment.
func withSettingsModel(t *testing.T, model string) {
	t.Helper()
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
	got, from := resolveEffectiveModel("Claude Sonnet 4.6 (Thinking)")
	if got != "Claude Sonnet 4.6 (Thinking)" || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestResolveEffectiveModel_UnavailableSettingsFallsBack is the recovered
// behaviour: a settings.json model that is not in the catalogue is replaced by
// the configured model's display label and reported as the fallback source
// so the TUI can render the "is no longer available" warning.
func TestResolveEffectiveModel_UnavailableSettingsFallsBack(t *testing.T) {
	withSettingsModel(t, "MiniMax-M3")
	got, from := resolveEffectiveModel("")
	if got != "MiniMax M3" {
		t.Errorf("effective = %q, want %q", got, "MiniMax M3")
	}
	if from != "MiniMax-M3" {
		t.Errorf("fallbackFrom = %q, want MiniMax-M3", from)
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
		got, from := resolveEffectiveModel("")
		if got != tc.wantLabel || from != "" {
			t.Fatalf("slug %q: got (%q, %q), want (%q, %q)", tc.slug, got, from, tc.wantLabel, "")
		}
	}
}

// TestResolveEffectiveModel_UnavailableSlugWarns verifies that a model slug
// that is genuinely not in the catalogue falls back to defaultModel and
// produces the unavailableFrom warning.
func TestResolveEffectiveModel_UnavailableSlugWarns(t *testing.T) {
	withSettingsModel(t, "gemini-1.5-pro")
	got, from := resolveEffectiveModel("")
	if got != defaultModel || from != "gemini-1.5-pro" {
		t.Fatalf("got (%q, %q), want (%q, %q)", got, from, defaultModel, "gemini-1.5-pro")
	}
}

// TestResolveEffectiveModel_AvailableSettingsIsUsed pins that a settings model
// the catalogue still offers is used with no warning.
func TestResolveEffectiveModel_AvailableSettingsIsUsed(t *testing.T) {
	withSettingsModel(t, "Gemini 3.8 Flash (Low)")
	got, from := resolveEffectiveModel("")
	if got != "Gemini 3.8 Flash (Low)" || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestResolveEffectiveModel_DefaultsWithNoSettings pins the empty case.
func TestResolveEffectiveModel_DefaultsWithNoSettings(t *testing.T) {
	withSettingsModel(t, "")
	got, from := resolveEffectiveModel("")
	if got != defaultModel || from != "" {
		t.Fatalf("got (%q, %q)", got, from)
	}
}

// TestModelAvailable pins the catalogue lookup by both label and slug.
func TestModelAvailable(t *testing.T) {
	for _, name := range []string{"Gemini 3.8 Flash (High)", "gemini-3.8-flash-high", "GPT-OSS 120B (Medium)"} {
		if !modelAvailable(name) {
			t.Errorf("modelAvailable(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"MiniMax-M3", "", "nope"} {
		if modelAvailable(name) {
			t.Errorf("modelAvailable(%q) = true, want false", name)
		}
	}
}
