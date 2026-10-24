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

// configuredModelEntries reads the user's provider configuration and turns
// it into catalogue rows.
//
// The configuration is read through the models package's cache, so a
// catalogue lookup does not re-read the file every time. A configuration
// that does not parse yields no rows: the reference's catalogue is still
// complete and the CLI still works.
func configuredModelEntries() []ModelEntry {
	reg := models.CachedRegistry()
	if reg == nil {
		return nil
	}
	var out []ModelEntry
	for _, m := range reg.List() {
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
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
