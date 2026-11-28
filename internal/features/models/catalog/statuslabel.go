// statuslabel.go — the label a status row shows, and the tier it shows
// beside it.
//
// # The rule
//
// A model's display label is shown verbatim ("DeepSeek V4.1 Flash (1017)",
// "Claude Sonnet 4.6 (Thinking)") *unless* the model publishes more than
// one reasoning level. Only then does the bracket name a tier: the row
// drops it from the label and re-attaches it as the " · <tier>" segment,
// the way the reference shows "Gemini 3.8 Flash · high" rather than
// "Gemini 3.8 Flash (High) · high".
//
// # Why it is one decision, not two
//
// The label and the suffix are two halves of the same answer: "is this
// bracket an effort tier, or part of the model's name?". Answering it
// separately in the stripping helper and in the suffix helper is what let
// the two disagree — a configured model named "... (1017)" lost its tag
// while a Claude row kept "(Thinking)", and GPT-OSS's single "(Medium)"
// was stripped and then re-added as " · medium". Both halves now come
// from StatusLabel.

package catalog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// StatusLabel splits a model label into the name the status row shows and
// the effort tier it shows beside it.
//
//	tier == ""  → the label is shown verbatim, no " · <tier>" segment
//	tier != ""  → the label's own bracket is the tier; the row shows the
//	              name without it, then " · <tier>"
//
// The effort a model runs at is taken from its label when the label
// carries a tier, and from the model's default otherwise. A model with
// no adjustable effort always answers tier "".
func StatusLabel(label string) (name, tier string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", ""
	}
	if !EffortAdjustableFor(label) {
		return label, ""
	}
	if t := models.TierFromLabel(label); t != "" {
		return models.BaseLabel(label), t
	}
	// The label carries no tier ("gemini-3.8-flash", "MiniMax M3.1 Flash"):
	// the row runs at the model's default level.
	return label, defaultTierFor(label)
}

// defaultTierFor is the level a tiered model runs at when its label names
// none: the tier its configuration states, else the catalogue's default
// for the model, else the first level it offers.
func defaultTierFor(label string) string {
	if m, ok := configuredModel(label); ok {
		if t := models.DefaultEffort(m); t != "" {
			return t
		}
	}
	base := effortBaseID(label)
	if spec, ok := BaseModelEfforts[base]; ok {
		if t := ResolveDefaultEffort(spec); t != "" {
			return t
		}
	}
	if tiers := EffortTiersFor(label); len(tiers) > 0 {
		return tiers[0]
	}
	return ""
}
