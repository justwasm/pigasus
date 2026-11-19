package slashcommands

import (
	"testing"
)

func TestSlashCommands_GeminiAPIKeyExclusion(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key")

	ctx := ScopeContext{IsGeminiAPIKey: true}

	excluded := []string{"/usage", "/quota", "/credits", "/remote-control", "/voice", "/record"}
	for _, name := range excluded {
		if !IsGeminiAPIKeyExcluded(name, ctx) {
			t.Errorf("IsGeminiAPIKeyExcluded(%q) = false, want true", name)
		}
		if cmd, found := Find(name); found {
			if IsAvailable(cmd, ctx) {
				t.Errorf("IsAvailable(%q) = true under GEMINI_API_KEY, want false", name)
			}
		}
	}

	filtered := FilterScoped("", ctx)
	for _, c := range filtered {
		for _, name := range excluded {
			if c.Name == name {
				t.Errorf("FilterScoped contains %s under GEMINI_API_KEY", c.Name)
			}
		}
	}

	sorted := Sorted()
	for _, c := range sorted {
		if c.Name == "/usage" || c.Name == "/quota" || c.Name == "/credits" {
			t.Errorf("Sorted contains %s under GEMINI_API_KEY", c.Name)
		}
	}
}
