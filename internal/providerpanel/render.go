// render.go — text formatting and display row values for the /provider panel.
package providerpanel

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/models"
)

// RowText renders a name and value with optional cursor marker and indentation.
func RowText(name, value string, cursor, indent bool, blueOpen, blueClose string) string {
	pad := 24 - len(name)
	if pad < 1 {
		pad = 1
	}
	prefix := "  "
	if indent {
		prefix = "    "
	}
	if cursor {
		return blueOpen + "> " + name + blueClose + strings.Repeat(" ", pad) +
			blueOpen + value + blueClose
	}
	return prefix + name + strings.Repeat(" ", pad) + value
}

// ProviderRowValue formats the value string for a provider row.
func ProviderRowValue(p models.ProviderConfig) string {
	return fmt.Sprintf("%-10s %s", p.WireType(), Endpoint(p))
}

// ModelRowValue formats the value string for a model row.
func ModelRowValue(p models.ProviderConfig, modelID string) string {
	label := ""
	wire := ""
	ctx := 0
	for _, m := range p.Models {
		if m.ID != modelID {
			continue
		}
		label = m.Name
		wire = m.Model
		ctx = m.ContextWindow
	}
	if label == "" {
		label = modelID
	}
	if wire == "" {
		wire = modelID
	}
	return fmt.Sprintf("%-24s model=%-24s ctx=%d", label, wire, ctx)
}
