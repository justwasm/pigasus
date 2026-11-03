// render.go — text formatting and display row values for the /provider panel.
package providerpanel

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/features/models"
)

// RowText renders a name and value with optional cursor marker, indentation,
// and optional maxWidth truncation to prevent line wrapping (Bubbletea Golden Rule #2).
func RowText(name, value string, cursor, indent bool, blueOpen, blueClose string, maxWidth ...int) string {
	nameWidth := ansi.StringWidth(name)
	pad := 24 - nameWidth
	if pad < 1 {
		pad = 1
	}
	var res string
	if cursor {
		lead := ""
		if indent {
			lead = "  "
		}
		res = lead + blueOpen + "> " + name + blueClose + strings.Repeat(" ", pad) + value
	} else {
		prefix := "  "
		if indent {
			prefix = "    "
		}
		res = prefix + name + strings.Repeat(" ", pad) + value
	}
	if len(maxWidth) > 0 && maxWidth[0] > 0 {
		return ansi.Truncate(res, maxWidth[0], "…")
	}
	return res
}

// ProviderRowValue formats the value string for a provider row.
func ProviderRowValue(p models.ProviderConfig) string {
	wire := p.WireType()
	endpoint := Endpoint(p)
	modelsNote := ""
	if n := len(p.EffectiveModels()); n == 1 {
		modelsNote = " (1 model)"
	} else if n > 1 {
		modelsNote = fmt.Sprintf(" (%d models)", n)
	}
	return fmt.Sprintf("%-10s %s%s", wire, endpoint, modelsNote)
}

// ModelRowValue formats the value string for a model row with optional width adaptation.
func ModelRowValue(p models.ProviderConfig, modelID string, width ...int) string {
	label := ""
	wire := ""
	ctx := 0
	effort := ""
	for _, m := range p.EffectiveModels() {
		if m.ID != modelID {
			continue
		}
		label = m.Name
		wire = m.Model
		ctx = m.ContextWindow
		effort = m.ReasoningEffort
	}
	if label == "" {
		label = modelID
	}
	if wire == "" {
		wire = modelID
	}
	ctxStr := "-"
	if ctx > 0 {
		if ctx >= 1024 && ctx%1024 == 0 {
			ctxStr = fmt.Sprintf("%dk", ctx/1024)
		} else if ctx >= 1000 && ctx%1000 == 0 {
			ctxStr = fmt.Sprintf("%dk", ctx/1000)
		} else {
			ctxStr = fmt.Sprintf("%d", ctx)
		}
	}
	w := 0
	if len(width) > 0 {
		w = width[0]
	}
	var val string
	if w > 0 && w < 96 {
		// Responsive compact layout on standard/narrow terminals
		val = fmt.Sprintf("%-16s model=%-16s ctx=%s", label, wire, ctxStr)
	} else {
		val = fmt.Sprintf("%-24s model=%-24s ctx=%s", label, wire, ctxStr)
	}
	if effort != "" {
		val += "  effort=" + effort
	}
	return val
}
