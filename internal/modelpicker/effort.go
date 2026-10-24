// effort.go — effort levels, naming, and tier checking for models.

package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
)

// effortLevelNames maps the slider index onto the word the reference
// prints in `⎿  Effort set to <word>`.
var effortLevelNames = [3]string{"low", "medium", "high"}

// EffortLevelName returns the word for a slider index, clamped to the
// three known levels.
func EffortLevelName(level int) string {
	if level < EffortLow || level > EffortHigh {
		level = EffortHigh
	}
	return effortLevelNames[level]
}

// EffortIndexForName returns the slider index for a canonical level
// word. The mapping is the same one EffortLevelName uses in reverse; an
// out-of-vocab word falls back to high so the picker never indexes off
// the end of the bar.
func EffortIndexForName(name string) int {
	switch name {
	case "low":
		return EffortLow
	case "medium":
		return EffortMedium
	}
	return EffortHigh
}

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
	name = strings.TrimSpace(name)
	if name == "" {
		return true
	}
	id := ResolveCatalogueID(name)
	if id == "" {
		return true
	}
	for _, m := range cli.ReferenceModels() {
		if m.ID == id {
			return true
		}
	}
	return false
}
