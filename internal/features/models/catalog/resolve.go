package catalog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/settings"
)

// DefaultModel is the reference's fallback when nothing usable is configured.
const DefaultModel = "Gemini 3.8 Flash (High)"

// FormatModelLabel maps a raw model name or slug to the display label.
//
// The catalogue is consulted first, so two configured models that share a
// prefix keep their own labels. A name the catalogue does not know falls
// back to the upstream spelling the provider uses, and a name neither knows
// is returned unchanged: the clone has no hard-coded fallback table, so a
// model the user configured under a name the catalogue does not carry still
// shows the name the user typed.
func FormatModelLabel(name string) string {
	if label := CanonicalModelLabel(name); label != "" {
		return label
	}
	if label, ok := WireLabel(name); ok {
		return label
	}
	return name
}

// ResolveEffectiveModel applies flag → settings → default precedence and availability check.
func ResolveEffectiveModel(flagModel string) (effective, unavailableFrom string) {
	requested := strings.TrimSpace(flagModel)
	if requested == "" {
		requested = strings.TrimSpace(settings.ResolveModel(""))
	}
	if requested == "" {
		return DefaultModel, ""
	}
	// A catalogue entry is available by definition. This is the path every
	// configured model takes — it is in the catalogue because the user put
	// it there — so selecting one does not raise the "no longer available"
	// warning.
	if label := CanonicalModelLabel(requested); label != "" {
		return label, ""
	}
	// The upstream spelling of a configured model resolves to the same
	// entry: the user may have pasted the id the provider documents.
	if label, ok := WireLabel(requested); ok {
		return label, ""
	}
	return DefaultModel, requested
}

// CanonicalModelLabel maps a catalogue slug or display label to display label.
func CanonicalModelLabel(name string) string {
	name = strings.TrimSpace(name)
	for _, m := range ModelCatalogue() {
		if m.ID == name || m.Label == name {
			return m.Label
		}
	}
	switch strings.ToLower(name) {
	case "gemini-3.8-flash", "gemini 3.8 flash":
		return "Gemini 3.8 Flash (High)"
	case "gemini-4-hydrogen-sonar", "gemini 4 hydrogen sonar", "gemini-4-sonar", "hydrogen-sonar":
		return "Gemini 4 Hydrogen (Sonar)"
	case "gemini-4-hydrogen-sonar-high", "gemini 4 hydrogen sonar high":
		return "Gemini 4 Hydrogen (Sonar) (High)"
	case "gemini-4-hydrogen-sonar-medium", "gemini 4 hydrogen sonar medium":
		return "Gemini 4 Hydrogen (Sonar) (Medium)"
	case "gemini-4-hydrogen-sonar-low", "gemini 4 hydrogen sonar low":
		return "Gemini 4 Hydrogen (Sonar) (Low)"
	case "gemini-4-hydrogen-sonar-xhigh", "gemini 4 hydrogen sonar xhigh":
		return "Gemini 4 Hydrogen (Sonar) (XHigh)"
	case "claude-opus-4-6", "claude opus 4.6":
		return "Claude Opus 4.6 (Thinking)"
	case "claude-sonnet-4-6-thinking", "claude sonnet 4.6 thinking":
		return "Claude Sonnet 4.6 (Thinking)"
	case "gpt-oss-120b", "gpt oss 120b":
		return "GPT-OSS 120B (Medium)"
	}
	// The bare id of a configured model that declares reasoning tiers is
	// not a catalogue row of its own — the catalogue lists one row per
	// tier ("minimax-m3.1-flash-max"). The id still names the model the
	// user configured, at its default tier, and a model the user
	// configured must not fall through to the reference's default: that
	// sends the request to the CloudCode account instead of the
	// configured endpoint.
	if label := ConfiguredModelDefaultLabel(name); label != "" {
		return label
	}
	return ""
}

// ModelAvailable reports whether name appears in the catalogue.
func ModelAvailable(name string) bool {
	name = strings.TrimSpace(name)
	for _, m := range ModelCatalogue() {
		if m.Label == name || m.ID == name {
			return true
		}
	}
	return false
}
