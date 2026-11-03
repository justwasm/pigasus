// effort.go — the reasoning tiers a model publishes.
//
// # Why this file exists
//
// The slider draws the levels the *model* publishes, not a fixed list:
// the reference's own gauge lists the model's efforts
// (【反编译】 agy 1.4.12, backend.EffortsForBase, sorted by
// model.sortEfforts, and model.EffortSelectorModel.currentEffortIndex
// indexes into that list). So the question "which levels does this model
// publish?" has to be answered somewhere, and it is answered here: the
// catalogue owns the reference's own table, and the registry owns the
// models the user configured.
//
// A model that publishes no level at all has a *fixed* reasoning budget —
// the reference's proto says so (google.gca.aicode.v1main's
// ThinkingConfig carries a fixed `thinking_budget` next to the
// adjustable `thinking_level`) — and the reference answers such a model
// with "Effort isn't adjustable for the current model".

package catalog

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// EffortTiersFor returns the reasoning levels a model publishes, in the
// order the slider shows them.
//
// The name may be a catalogue id ("gemini-3.8-flash-high"), a display
// label ("Gemini 3.8 Flash (High)") or the name of a model the user
// configured ("MiniMax M3.1 Flash (High)"). The tier a name carries is
// stripped before the lookup: the table is keyed by the *base* id, and a
// model publishes the same levels at every one of its tiers.
//
// A model that publishes no level returns nil — that is the reference's
// own "fixed reasoning budget" answer, and it is what makes the effort
// not adjustable.
func EffortTiersFor(name string) []string {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil
	}
	// The names the lookup tries: the name itself, the catalogue id it
	// names, and that id without its tier suffix.
	names := []string{name}
	if id, ok := CatalogueID(name); ok {
		names = append(names, id, models.BaseModelID(id))
	}
	// The reference's own catalogue: a model's id carries the tier
	// ("gemini-3.8-flash-high"), so the table is keyed by the base id.
	for _, n := range names {
		if spec, ok := BaseModelEfforts[models.BaseModelID(n)]; ok {
			return spec.Levels
		}
	}
	// A model the user configured: the tiers its own configuration
	// states.
	for _, n := range names {
		if tiers, ok := configuredTiers(n); ok {
			return tiers
		}
	}
	return nil
}

// EffortAdjustableFor reports whether the named model's effort can be
// adjusted.
//
// The rule is the reference's own: the effort is adjustable when the
// model publishes more than one level
// (【反编译】 backend.CurrentEffortSelection, 0x10274e9b0 — see
// models.EffortAdjustable, which owns the rule).
func EffortAdjustableFor(name string) bool {
	return models.EffortAdjustable(EffortTiersFor(name))
}

// EffortLabelFor returns the catalogue label of the named model at the
// named tier — "Gemini 3.8 Flash (Medium)" for
// ("Gemini 3.8 Flash (High)", "medium").
//
// The catalogue lists one row per tier and the tier rides the row's id
// ("gemini-3.8-flash-medium"), so the row is found by rebuilding the id
// from the model's *base* id and the tier. The base id is the row's id
// without its tier suffix, so a model the user configured
// ("minimax-m3.1-flash") resolves the way the reference's own models do.
//
// A model the catalogue does not know, and a tier it does not list, are
// answered with "".
func EffortLabelFor(model, tier string) string {
	tier = strings.ToLower(strings.TrimSpace(tier))
	base := effortBaseID(model)
	if base == "" || tier == "" {
		return ""
	}
	want := base + "-" + tier
	for _, e := range ModelCatalogue() {
		if e.ID == want {
			return e.Label
		}
	}
	return ""
}

// effortBaseID returns the catalogue id of a model without its tier
// suffix: "Gemini 3.8 Flash (High)" -> "gemini-3.8-flash".
//
// A name the catalogue does not carry is returned with its own suffix
// stripped, so a model the user configured under a name the catalogue
// does not know still resolves its own tiers.
func effortBaseID(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return ""
	}
	for _, e := range ModelCatalogue() {
		if e.ID == model || e.Label == model {
			return models.BaseModelID(e.ID)
		}
	}
	return models.BaseModelID(model)
}
