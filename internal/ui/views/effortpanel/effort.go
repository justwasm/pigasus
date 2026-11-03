// effort.go — the effort levels' names.
//
// # Where the words come from
//
// The words are the catalogue's vocabulary — low, medium, high, xhigh and
// max (internal/features/models) — so the words the slider draws, the
// words the transcript echoes (`⎿  Effort set to <word>`) and the words
// the command accepts cannot drift apart.
//
// The slider's *index* is an index into the list the model publishes,
// not into the vocabulary: the reference's own gauge has three places
// (low, medium and high) because Gemini 3.8 Flash publishes three
// levels, and a model that publishes five gets five places. The helpers
// below therefore come in two shapes — the vocabulary mapping
// (EffortLevelName, EffortIndexForName) here, and the model's own list
// (models.TierNameAt, models.TierIndexOf) in the package that owns the
// tier vocabulary.

package effortpanel

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// effortDefaultLevel is the level the vocabulary falls back to: the
// reference's own default effort.
const effortDefaultLevel = "high"

// effortDefaultIndex is the index effortDefaultLevel sits at in the
// vocabulary.
var effortDefaultIndex = effortLevelIndex(effortDefaultLevel)

// EffortLevelName returns the word for a slider index in the catalogue's
// vocabulary.
//
// An index outside the vocabulary is answered with the vocabulary's own
// default ("high") — the way the slider answered an out-of-range index
// before the vocabulary grew past the reference's three tiers.
func EffortLevelName(level int) string {
	if level < 0 || level >= len(models.EffortLevels) {
		return effortDefaultLevel
	}
	return models.EffortLevels[level]
}

// EffortIndexForName returns the slider index for a canonical level word.
//
// The mapping is the same one EffortLevelName uses in reverse. A word
// outside the vocabulary falls back to the default level ("high") so the
// picker never indexes off the end of the bar.
//
// The two levels the reference's own three-tier slider cannot name are
// part of the vocabulary: "xhigh" is index 3 and "max" is index 4, so a
// model that publishes them has a slider that can reach them.
func EffortIndexForName(name string) int {
	if i := effortLevelIndex(name); i >= 0 {
		return i
	}
	return effortDefaultIndex
}

// effortLevelIndex returns the index of a word in the vocabulary, or -1
// when the word names no level.
func effortLevelIndex(name string) int {
	word := strings.ToLower(strings.TrimSpace(name))
	if word == "" {
		return -1
	}
	for i, e := range models.EffortLevels {
		if word == e {
			return i
		}
	}
	return -1
}
