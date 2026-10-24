// rows.go — provider and model row generation for the /provider panel.
package providerpanel

import (
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/models"
)

// BuildRows constructs the panel's row list: one row per provider,
// one per model it serves, and the built-in reference group at the end.
func BuildRows(providers map[string]models.ProviderConfig) []Row {
	rows := []Row{}
	for _, id := range models.SortedProviderIDs(providers) {
		p := providers[id]
		rows = append(rows, Row{Kind: RowProvider, Provider: id})
		for _, m := range p.Models {
			rows = append(rows, Row{Kind: RowModel, Provider: id, Model: m.ID})
		}
	}
	// The reference's own catalogue, so the panel shows every model the
	// clone can run: the built-ins come from the server, the configured
	// ones from the file above.
	rows = append(rows, Row{Kind: RowBuiltin, Provider: BuiltinProviderLabel})
	for _, m := range cli.ReferenceModels() {
		rows = append(rows, Row{Kind: RowBuiltin, Model: m.ID, Label: m.Label})
	}
	return rows
}
