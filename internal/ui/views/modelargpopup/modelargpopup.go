// Package modelargpopup owns the "/model <argument>" completion's data:
// the catalogue rows the completion popup offers, the resolution of a
// typed argument against the catalogue, and the reference's answer to an
// argument that names no model.
//
// The reference's composer completes the /model *argument* with a popup
// of models: the display label in a 30-column field, the catalogue id two
// columns further right. 【一手】 agy 1.4.6, live tmux capture:
//
//	> /model claude
//	────────────────────────────────────────────────────────
//	> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//	  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking
//
//	  ↑/↓ Navigate · enter Select · tab Complete
//
// The filter is a case-insensitive *substring* of the label or of the id
// (the popup's own rule, internal/ui/views/slashpicker), the popup
// replaces the command rows while it is open, and Enter has three
// outcomes:
//
//   - the argument already names a model → the command runs and the model
//     changes ("Model set to <label>");
//   - the argument does not, but a row is highlighted → the row's label is
//     completed into the composer and the popup closes;
//   - the argument does not and nothing matches → the command runs and
//     reports `Invalid model "zzz" (available: ...)`.
//
// Tab always completes the highlighted row into the composer.
//
// This package owns the three pure decisions that answer is made of. The
// popup's rendering belongs to internal/ui/views/slashpicker; the panel
// of the "/model" *command* (its rows, its effort slider, its
// persistence) belongs to internal/ui/views/modelpicker; and what the
// host does with a resolved model — the settings write, the backend
// re-sync, the session's model — belongs to the host.
package modelargpopup

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
)

// Rows returns the catalogue the "/model <query>" popup offers: the
// reference's own models first, then the ones the user configured
// (catalog.ModelCatalogue).
//
// The rows are re-read on every keystroke, so a catalogue that changed
// (the user edited the provider configuration) shows up immediately.
func Rows() []slashpicker.ModelRow {
	entries := catalog.ModelCatalogue()
	out := make([]slashpicker.ModelRow, 0, len(entries))
	for _, e := range entries {
		out = append(out, slashpicker.ModelRow{Label: e.Label, ID: e.ID})
	}
	return out
}

// Resolve resolves a "/model" argument to a catalogue row.
//
// The reference matches the display label or the catalogue id,
// case-insensitively, and only in full: "/model gpt-oss" names no model
// (the popup completes the row instead), while "/model GPT-OSS 120B
// (Medium)", "/model gpt-oss-120b-medium" and "/model gpt-oss 120b
// (medium)" all resolve to the same row. 【一手】 agy 1.4.6, live.
func Resolve(arg string) (catalog.ModelEntry, bool) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return catalog.ModelEntry{}, false
	}
	for _, e := range catalog.ModelCatalogue() {
		if strings.EqualFold(e.Label, arg) || strings.EqualFold(e.ID, arg) {
			return e, true
		}
	}
	return catalog.ModelEntry{}, false
}

// InvalidMessage renders the reference's error for a "/model" argument
// that names no model.
//
// 【一手】 agy 1.4.6, live:
//
//	Invalid model "zzz" (available: gemini-3.8-flash-high, gemini-3.8-flash-medium, gemini-3.8-flash-low, claude-sonnet-4-6, claude-opus-4-6-thinking, gpt-oss-120b-medium)
func InvalidMessage(arg string) string {
	ids := make([]string, 0, 8)
	for _, e := range catalog.ModelCatalogue() {
		ids = append(ids, e.ID)
	}
	return fmt.Sprintf("Invalid model %q (available: %s)", arg, strings.Join(ids, ", "))
}
