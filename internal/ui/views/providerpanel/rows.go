// rows.go — provider and model row generation for the /provider panel.
package providerpanel

import (
	"fmt"

	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
)

// BuildRows constructs the panel's row list: one row per provider,
// one per model it serves, the built-in reference group, and preset providers.
func BuildRows(providers map[string]models.ProviderConfig) []Row {
	rows := []Row{}
	for _, id := range models.SortedProviderIDs(providers) {
		p := providers[id]
		rows = append(rows, Row{Kind: RowProvider, Provider: id})
		for _, m := range p.EffectiveModels() {
			rows = append(rows, Row{Kind: RowModel, Provider: id, Model: m.ID})
		}
	}
	// The reference's own catalogue, so the panel shows every model the
	// clone can run: the built-ins come from the server, the configured
	// ones from the file above.
	rows = append(rows, Row{Kind: RowBuiltin, Provider: BuiltinProviderLabel})
	for _, m := range catalog.ReferenceModels() {
		rows = append(rows, Row{Kind: RowBuiltin, Model: m.ID, Label: m.Label})
	}

	// Preset providers from the Catwalk catalog that have not been configured yet.
	presetProviders := models.CatwalkProviders()
	if len(presetProviders) > 0 {
		rows = append(rows, Row{Kind: RowPreset, Provider: PresetsProviderLabel})
		for _, p := range presetProviders {
			id := string(p.ID)
			if _, already := providers[id]; already {
				continue
			}
			auth := models.CatwalkAuthMethod(id)
			name := p.Name
			if name == "" {
				name = id
			}
			label := fmt.Sprintf("%s (%s · %d models)", name, auth, len(p.Models))
			rows = append(rows, Row{
				Kind:     RowPreset,
				Provider: id,
				Label:    label,
			})
		}
	}
	return rows
}
