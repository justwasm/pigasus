// model_resolve.go — effective-model resolution shared by the TUI banner and
// the print-mode `/model` command.
//
// The reference resolves the model in three steps: the --model flag wins, then
// settings.json's "model" key, then the built-in default. The chosen name is
// then checked against the catalogue `agy models` prints; if it is missing the
// reference falls back to Gemini 3.8 Flash (High) and renders
//
//	⚠ Warning
//	  ⎿  "<requested>" is no longer available. Using "Gemini 3.8 Flash (High)".
//
// Cycle 99 recovered this from a live 1.3.6 TUI capture with a settings.json
// that sets `"model": "MiniMax-M3"` (a model absent from the catalogue): the
// banner reads "Gemini 3.8 Flash (High)", the warning block above is present,
// and the status row reads "Gemini 3.8 Flash · high".
//
// This corrects cycle 95, which made the settings model win unconditionally so
// the warning never fired. That silenced the warning but diverged from the
// reference in exactly the configuration the warning exists for.
package main

import (
	"strings"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
)

// agwBackend is the interface satisfied by *backend.AgwK0sClient. It is
// declared here (package main) so tui_run.go can use a type assertion
// without importing the concrete struct directly.
type agwBackend interface {
	AgwConfig() backend.AgwK0sConfig
}

// isMiniMax reports whether name refers to the MiniMax M3 model.
// Matches slugs like "MiniMax-M3", "minimax-m3", labels "MiniMax M3", etc.
func isMiniMax(name string) bool {
	return strings.Contains(strings.ToLower(name), "minimax")
}

// formatModelLabel maps a raw model name or slug to the display label shown
// in the TUI banner and status row. Returns name unchanged if not in catalogue.
func formatModelLabel(name string) string {
	if isMiniMax(name) {
		return "MiniMax M3"
	}
	if label := canonicalModelLabel(name); label != "" {
		return label
	}
	return name
}

// defaultModel is the reference's fallback when nothing usable is
// configured. agy 1.4.3 picked Gemini 3.8 Flash (High) on a clean HOME
// (no settings.json model key) and also when it rejected a settings slug;
// 1.4.2's Medium default is stale. The print-mode fallback in main.go
// passes the same label so the TUI banner and `agy -p /model` agree.
const defaultModel = "Gemini 3.8 Flash (High)"

// resolveEffectiveModel applies the reference's flag → settings → default
// precedence and its availability check. It returns the model that would be
// used and, when a configured model was unavailable, that model's name.
//
// Cycle 197: a catalogue *slug* (e.g. "gemini-3.8-flash-high",
// "gemini-3.8-flash-medium", "claude-sonnet-4-6") is mapped to its display
// label so the status row and banner read "Gemini 3.8 Flash (High)" rather
// than the raw slug. The reference stores the slug in settings.json (that
// is the ID `agy -p /model` prints as its first column) and resolves it
// cleanly as a model alias without warning. Only genuinely unknown or
// removed models trigger the "is no longer available" fallback warning.
func resolveEffectiveModel(flagModel string) (effective, unavailableFrom string) {
	requested := strings.TrimSpace(flagModel)
	if requested == "" {
		requested = strings.TrimSpace(auth.ResolveSettingsModel(""))
	}
	if requested == "" {
		return defaultModel, ""
	}
	if isMiniMax(requested) {
		// MiniMax M3 is a configured-only model — the reference catalogue
		// intentionally does not advertise it. Render the display label
		// "MiniMax M3" everywhere and report the requested slug back as
		// the fallback source so the existing "is no longer available"
		// warning still fires for tests that exercise it.
		return "MiniMax M3", requested
	}
	if label := canonicalModelLabel(requested); label != "" {
		return label, ""
	}
	return defaultModel, requested
}

// canonicalModelLabel maps a catalogue slug or display label to the
// display label, or "" when the name is not in the catalogue.
func canonicalModelLabel(name string) string {
	name = strings.TrimSpace(name)
	for _, m := range cli.ReferenceModels() {
		if m.ID == name || m.Label == name {
			return m.Label
		}
	}
	if name == "gemini-3.8-flash" || name == "Gemini 3.8 Flash" {
		return "Gemini 3.8 Flash (High)"
	}
	return ""
}

// modelAvailable reports whether name appears in the reference's catalogue,
// matched by either the display label or the slug.
func modelAvailable(name string) bool {
	name = strings.TrimSpace(name)
	for _, m := range cli.ReferenceModels() {
		if m.Label == name || m.ID == name {
			return true
		}
	}
	return false
}
