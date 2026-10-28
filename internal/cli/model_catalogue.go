package cli

import (
	"sort"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// ModelCatalogue returns every model the CLI can select: the reference's own
// catalogue plus the models the user configured.
//
// The two halves are different in kind. The reference's catalogue is what
// the CloudCode server advertises — six models, fixed, and the same on every
// machine. The configured models are the user's own: whatever endpoints and
// model ids they wrote into their provider configuration. The clone has no
// hard-coded list of the latter — an extra model exists because the user
// configured it, not because the clone shipped it.
//
// The reference's rows come first, in the reference's order. The configured
// rows follow, sorted by id, so the list is stable across runs.
//
// A configured model whose id collides with a reference model is dropped:
// the reference's row wins, because that id is the reference's own.
func ModelCatalogue() []ModelEntry {
	out := ReferenceModels()
	seen := make(map[string]bool, len(out)*2)
	for _, m := range out {
		seen[m.ID] = true
		seen[m.Label] = true
	}
	for _, m := range configuredModelEntries() {
		if seen[m.ID] || seen[m.Label] {
			continue
		}
		seen[m.ID] = true
		seen[m.Label] = true
		out = append(out, m)
	}
	return out
}

// ConfiguredModelDefaultLabel maps the bare id of a configured model to the
// label of its default tier.
//
// A configured model that declares reasoning tiers is listed in the
// catalogue one row per tier ("minimax-m3.1-flash-max"), so its bare id
// ("minimax-m3.1-flash") is not a row of its own. The id still names a model
// — the one the registry holds, at the tier the configuration declares as its
// default — and a user who typed that id meant that model.
//
// The reference resolves a bare id the same way: its own default model's id,
// "gemini-3.8-flash", answers "Gemini 3.8 Flash (High)" (see
// modelcatalog.CanonicalModelLabel).
//
// A model without tiers keeps its own label.
func ConfiguredModelDefaultLabel(id string) string {
	reg := models.CachedRegistry()
	if reg == nil {
		return ""
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	for _, m := range reg.List() {
		if m.Name != id {
			continue
		}
		label := strings.TrimSpace(m.DisplayName)
		if label == "" {
			label = id
		}
		tier := models.DefaultEffort(m)
		if tier == "" {
			return label
		}
		return label + " (" + models.TierLabel(tier) + ")"
	}
	return ""
}

// TieredModelEntry is one configured model: its own id and label, plus
// the reasoning tiers it declares.
//
// The catalogue (ModelCatalogue) lists a model that declares tiers once
// per tier — the reference's own shape ("Gemini 3.8 Flash (High)"). This
// entry is the other view of the same model: one row, with the tiers
// spelled out. The /model picker's slider needs the second one; the
// id↔label lookup needs the first.
type TieredModelEntry struct {
	// ID is the model's own catalogue id — no tier suffix.
	ID string
	// Label is the model's display name — no tier in brackets.
	Label string
	// Tiers are the reasoning levels the model offers, in the order
	// the configuration declared them.
	Tiers []string
	// DefaultTier is the tier the model runs at when nothing else
	// says: the configuration's own `reasoning_effort`, or medium when
	// it offers one.
	DefaultTier string
}

// ConfiguredModelEntries returns one entry per model the user configured,
// each carrying the reasoning tiers it declares.
//
// The rows are grouped by the model they belong to, and the tiers keep
// the order the configuration declared them in.
func ConfiguredModelEntries() []TieredModelEntry {
	reg := models.CachedRegistry()
	if reg == nil {
		return nil
	}
	var out []TieredModelEntry
	for _, m := range reg.List() {
		id := strings.TrimSpace(m.Name)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(m.DisplayName)
		if label == "" {
			label = id
		}
		out = append(out, TieredModelEntry{
			ID:          id,
			Label:       label,
			Tiers:       models.NormalizeEfforts(m.ReasoningEfforts),
			DefaultTier: models.DefaultEffort(m),
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		return models.BaseModelID(out[i].ID) < models.BaseModelID(out[j].ID)
	})
	return out
}

// configuredModelEntries reads the user's provider configuration and turns
// it into catalogue rows.
//
// The configuration is read through the models package's cache, so a
// catalogue lookup does not re-read the file every time. A configuration
// that does not parse yields no rows: the reference's catalogue is still
// complete and the CLI still works.
//
// A model that declares reasoning tiers is listed once per tier — the
// reference's own shape ("Gemini 3.8 Flash (High)"). The tier rides the
// row's id, so the Web UI's picker and the TUI's /model panel can offer
// the tiers without either of them knowing what a tier is.
func configuredModelEntries() []ModelEntry {
	reg := models.CachedRegistry()
	if reg == nil {
		return nil
	}
	var out []ModelEntry
	for _, m := range models.TierRows(reg.List()) {
		id := strings.TrimSpace(m.Name)
		if id == "" {
			continue
		}
		label := strings.TrimSpace(m.DisplayName)
		if label == "" {
			label = id
		}
		out = append(out, ModelEntry{ID: id, Label: label})
	}
	// The rows are grouped by the model they belong to, and the tiers
	// keep the order the configuration declared them in.
	sort.SliceStable(out, func(i, j int) bool {
		return models.BaseModelID(out[i].ID) < models.BaseModelID(out[j].ID)
	})
	return out
}
