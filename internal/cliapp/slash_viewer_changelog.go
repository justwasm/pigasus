// slash_viewer_changelog.go — the /changelog panel's document and frame.
//
// The panel's document, the window over it and the counter under it
// live in internal/ui/views/changelog. What stays here is the host's
// wiring: the panel registry hands over the document, and the window
// offset is the host's — its panel struct carries one offset that
// several panels share.
package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
)

// panelChangelogLines renders the /changelog viewer.
func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.getPanelRegistry().ViewerSpec(m.panel.kind)
	rows, start, end := m.viewerWindow(spec)
	return changelog.PanelLines(rows, start, end, spec.Total)
}
