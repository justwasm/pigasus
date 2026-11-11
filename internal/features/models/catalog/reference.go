// reference.go — the reference's own model catalogue.
//
// The catalogue is a fact about the *reference*, not about the CLI: the
// model picker lists it, the backend picks a transport from it, the
// provider panel renders it and `agy models` prints it. It used to live
// in the CLI host, which meant every one of those surfaces had to
// import a host to ask "which models does the reference advertise?".
//
// The catalogue is therefore a feature: it belongs with the models
// feature, in the tier that already owns the user's configured models.
package catalog

import (
	"os"
	"strings"
)

// ModelEntry is one row of the reference's model catalogue.
type ModelEntry struct{ ID, Label string }

// ADCAuthAvailable reports whether Application Default Credentials (AGY_ADC_AUTH)
// sign-in is active.
func ADCAuthAvailable() bool {
	v := strings.TrimSpace(os.Getenv("AGY_ADC_AUTH"))
	return strings.EqualFold(v, "true") || v == "1"
}

// ReferenceModels is the catalogue `agy models` prints. The reference also
// uses it to decide whether a configured model is still available: a model
// that is not in the catalogue triggers the "is no longer available" warning
// and a fallback to Gemini 3.8 Flash (High).
//
// It is the reference's own catalogue and nothing else — the fourteen models
// the reference's binary carries, plus the two Claude 5.5 entries when the
// account's project can reach them. Models the user added are not here:
// they come from the user's provider configuration, and ModelCatalogue
// merges the two.
//
// AGY 1.3.6 changelog line 1: "Added Claude Opus 5.5 and Claude Sonnet
// 5.5 for Application Default Credentials (AGY_ADC_AUTH) sign-in. Claude
// models now appear in /model only if your Google Cloud project can use
// them." The two 5.5 entries are kept in the static table for downstream
// lookups (so `agy --model claude-opus-5-5` still resolves when the gate
// is open) and conditionally dropped from the returned slice via
// claude55Available() — see claude55.go for the gate's predicate.
//
// # The three families between Gemini 3.8 Flash and the Claude rows
//
// Gemini 3.7 Flash, Gemini 3.6 Flash and Gemini 3.1 Pro are in the
// reference's own catalogue, and their ids are recovered — never guessed:
//
//   - 【一手】 agy 1.4.2, the vendor binary's own `agy models` output
//     (docs/cycles/cycle-274/deliverable.md §7.3): fourteen rows, in this
//     order, with these ids and these labels.
//   - 【strings】 agy 1.4.12 (the current reference binary): its own tier
//     table carries the ids `gemini-3.7-flash-high`, `-medium`, `-low`,
//     `gemini-3.6-flash-high`, `-medium`, `-low`, `gemini-3.1-pro-high`
//     and `gemini-3.1-pro-low`, and the labels "Gemini 3.7 Flash
//     (High)", "(Medium)", "(Low)", the same three for 3.6, and "Gemini
//     3.1 Pro (High)" / "(Low)". "Gemini 3.1 Pro (Medium)" is *not* in
//     the binary: the Pro family publishes two tiers, not three.
//   - 【strings】 agy 1.4.11: `gemini-3.1-pro-high`
//     (out/local-macos-1.4.11/strings-index.txt:422).
//
// The documentation's reasoning-model table names the three families
// (official-docs/models.md:23-25) and marks them Enterprise-only, and the
// server's own catalogue for a free account does not carry them
// (out/refbackend-custom-models/available_models.json, 2026-10-26). The
// reference prints them when the account's server advertises them; the
// clone's catalogue is static and carries no plan dimension — it already
// lists Claude Sonnet 4.6, which the same table marks unavailable on
// Enterprise — so the rows are the union of the models the reference
// knows. A server that does not carry one refuses the turn.
func ReferenceModels() []ModelEntry {
	out := []ModelEntry{
		{"gemini-3.8-flash-high", "Gemini 3.8 Flash (High)"},
		{"gemini-3.8-flash-medium", "Gemini 3.8 Flash (Medium)"},
		{"gemini-3.8-flash-low", "Gemini 3.8 Flash (Low)"},
		{"gemini-3.7-flash-high", "Gemini 3.7 Flash (High)"},
		{"gemini-3.7-flash-medium", "Gemini 3.7 Flash (Medium)"},
		{"gemini-3.7-flash-low", "Gemini 3.7 Flash (Low)"},
		{"gemini-3.6-flash-high", "Gemini 3.6 Flash (High)"},
		{"gemini-3.6-flash-medium", "Gemini 3.6 Flash (Medium)"},
		{"gemini-3.6-flash-low", "Gemini 3.6 Flash (Low)"},
		{"gemini-3.1-pro-high", "Gemini 3.1 Pro (High)"},
		{"gemini-3.1-pro-low", "Gemini 3.1 Pro (Low)"},
		{"claude-sonnet-4-6", "Claude Sonnet 4.6 (Thinking)"},
		{"claude-opus-4-6-thinking", "Claude Opus 4.6 (Thinking)"},
		{"gpt-oss-120b-medium", "GPT-OSS 120B (Medium)"},
	}
	if ADCAuthAvailable() {
		out = append(out,
			ModelEntry{"gemini-4-carbon-high", "Gemini 4 Carbon (High)"},
			ModelEntry{"gemini-4-carbon-medium", "Gemini 4 Carbon (Medium)"},
			ModelEntry{"gemini-4-carbon-low", "Gemini 4 Carbon (Low)"},
		)
	}
	if Claude55Available() {
		out = append(out,
			ModelEntry{"claude-opus-5-5", "Claude Opus 5.5"},
			ModelEntry{"claude-sonnet-5-5", "Claude Sonnet 5.5"},
		)
	}
	return out
}
