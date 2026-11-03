// slash_viewer_spec.go — the documents the three viewer panels scroll.
//
// The three isViewer() panels — panelregistry.Usage, panelregistry.Changelog
// and panelregistry.FeatDiff — each show a document with a window over it.
// The documents themselves
// are the panels' facts: the /usage quota document, the /changelog
// release notes and the /feature-diff table.
//
// The window over a document, the pager keybindings and the scroll
// arithmetic live in internal/ui/views/viewerpanel; what is left here
// is the document, its visible row count and its own line count — the
// three things the panel asks the host for.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
)

// usageViewerSpec is the /usage document and its window.
//
// The terminal's height decides the shape: a known height renders the
// reference's own layout, an unknown one the cycle-193 fallback. The
// distinction is the host's — the height is the host's fact.
func (m *runMode) usageViewerSpec() tuiviewer.Spec {
	if m.height > 0 {
		return usagepanel.Spec(m.usageState())
	}
	return usagepanel.StaticSpec()
}

// changelogViewerSpec is the /changelog document and its window.
func (m *runMode) changelogViewerSpec() tuiviewer.Spec {
	return tuiviewer.Spec{
		Lines:   changelog.Document(m.width, m.height),
		Visible: changelog.VisibleRows(m.height),
		Total:   changelog.TotalLines,
	}
}

// viewerWindow is the visible slice of the document at the host's window
// offset. The offset is clamped into the document.
func (m *runMode) viewerWindow(spec tuiviewer.Spec) (rows []string, start, end int) {
	r, s, e, clamped := tuiviewer.Window(spec, m.panel.window)
	m.panel.window = clamped
	return r, s, e
}
