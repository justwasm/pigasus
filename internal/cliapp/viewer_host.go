// viewer_host.go — the host's side of the viewer panels.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/viewerpanel: it owns the
// viewport, the pager keybindings and the scroll arithmetic. The host
// supplies the four facts the panel cannot answer itself — the document,
// the terminal width, the page size and the way out of the panel.
//
// # The three viewers
//
// The reference shows three scrollable documentary panels — /usage,
// /changelog and /feature-diff — and they share the machinery. They
// differ in their document (the host builds it) and in their chrome (the
// host draws it).
//
// # The offset
//
// The scroll offset is the host's: the host's panel struct carries one
// window offset that several panels share, so the panel takes the offset
// as an argument and hands the clamped one back.

package cliapp

import (
	tea "charm.land/bubbletea/v2"

	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/changelog"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
)

// newViewerPanel builds the viewer panel with the host's seams.
func (m *runMode) newViewerPanel() *viewerpanel.Panel {
	return viewerpanel.New(viewerpanel.Seams{
		// The document the panel scrolls. The host builds it: the /usage
		// quota document, the /changelog release notes and the
		// /feature-diff table are the host's facts. The registry holds
		// the open panel's document; the second value is false when the
		// open panel is not a viewer.
		Spec: func() (tuiviewer.Spec, bool) {
			return m.getPanelRegistry().ViewerSpec(m.panel.kind)
		},
		// The viewport's width. The viewer panels render
		// full-terminal-width lines, so the width only has to be wide
		// enough not to clip them.
		Width: m.viewerVPWidth,
		// How many rows a page key scrolls.
		PageRows: m.viewerPageRows,
		// Esc (and q) leave the panel.
		Close: m.closePanel,
	})
}

// viewerPanel returns the open viewer panel.
//
// The panel is built when a viewer panel opens — the host's slashPanel
// struct is reset whenever a panel closes — so a caller that finds it
// missing (a test that sets the panel kind directly) gets one that
// answers.
func (m *runMode) viewerPanel() *viewerpanel.Panel {
	if m.panel.vw == nil {
		p := m.newViewerPanel()
		p.Open()
		m.panel.vw = p
	}
	return m.panel.vw
}

// initViewerViewport builds the viewer panel for the open panel.
//
// Called from openSlashPanel after the panel kind is set.
func (m *runMode) initViewerViewport() {
	p := m.newViewerPanel()
	p.Open()
	m.panel.vw = p
}

// viewerVPWidth returns the width to use for the viewport. The viewer
// panels render full-terminal-width lines, so the viewport width is set
// to a generous value that won't clip content.
//
// The fallback number belongs to the panel (viewerpanel.DefaultWidth);
// the host only knows whether the terminal's width is known.
func (m *runMode) viewerVPWidth() int {
	if m.width > 0 {
		return m.width
	}
	return viewerpanel.DefaultWidth
}

// viewerPageRows is how many rows a page key scrolls.
//
// A pager scrolls a whole window. The reference's /changelog scrolls one
// row less on the path where the terminal size is unknown, which is the
// path the unit tests take; that rule belongs to the /changelog panel,
// so the host asks it.
func (m *runMode) viewerPageRows(spec tuiviewer.Spec) int {
	if m.panel.kind == panelregistry.Changelog {
		return changelog.PageRows(spec, m.height)
	}
	return spec.Visible
}

// handleViewerPanelKey routes one key to the viewer panel.
//
// The panel owns the viewport's scroll state; the host owns the shared
// window offset, so the offset goes in and the clamped one comes back.
func (m *runMode) handleViewerPanelKey(key tea.Key) (bool, tea.Cmd) {
	handled, cmd, offset := m.viewerPanel().HandleKey(key, m.panel.window)
	m.panel.window = offset
	return handled, cmd
}
