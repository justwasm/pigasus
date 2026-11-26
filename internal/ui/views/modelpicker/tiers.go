// tiers.go — the catalogue's answer to "does this model offer tiers?".
//
// The question belongs to the catalogue, not to the slider: the slider
// draws the tiers a model publishes, and it is the catalogue that knows
// which models publish any. So the check stays here, in the package
// that owns the catalogue mapping, and the slider's own geometry lives
// in internal/ui/views/effortpanel.

package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

// HasEffortTiers reports whether a model carries the reference's effort
// tiers (low / medium / high).
//
// The tiers are a property of the reference's own catalogue: Gemini, Claude
// and GPT-OSS each publish three. A model the user configured has none —
// the reference never saw it — so the status row carries no " · high" and
// the effort slider has nothing to move.
//
// A model nobody can name keeps the tiers: the reference publishes them
// for everything it knows, and the only models it does not are the ones
// the user added.
func HasEffortTiers(name string) bool {
	return hasEffortTiers(name, ResolveCatalogueID)
}

// HasEffortTiersSnapshot avoids a provider filesystem check on every status
// frame. Non-render callers retain HasEffortTiers' immediate edit detection.
func HasEffortTiersSnapshot(name string) bool {
	return hasEffortTiers(name, func(value string) string {
		for _, entry := range catalog.ModelCatalogueSnapshot() {
			if entry.ID == value || entry.Label == value {
				return entry.ID
			}
		}
		return ""
	})
}

func hasEffortTiers(name string, resolve func(string) string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	for _, m := range catalog.ReferenceModels() {
		if m.ID == name || m.Label == name {
			return true
		}
	}
	id := resolve(name)
	if id == "" {
		return true
	}
	for _, m := range catalog.ReferenceModels() {
		if m.ID == id {
			return true
		}
	}
	return false
}
