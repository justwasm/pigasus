package tui

// overlay_host.go — the RootModel side of the OverlayModel seam.
//
// The overlay model owns the dismissal chain; the root supplies the two
// facts it cannot know: the terminal height (the resume picker pages by it)
// and the panel key handler (the panel owns its own keys).

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// ViewportHeight is the overlay host seam.
func (m *RootModel) ViewportHeight() int { return m.viewport.height }

// HandlePanelKey is the overlay host seam: the modal panel owns its keys.
func (m *RootModel) HandlePanelKey(key tea.Key) (bool, tea.Cmd) { return m.handlePanelKey(key) }

// HelpActive reports whether the /help overlay is currently active.
func (m *RootModel) HelpActive() bool {
	return m != nil && ((m.help.Panel != nil && m.help.Panel.Active()) || m.overlays.panel.kind == panelregistry.Help)
}

// CloseHelp cancels the /help overlay.
func (m *RootModel) CloseHelp() {
	if m != nil {
		if m.help.Panel != nil {
			m.help.Panel.Cancel()
		}
		if m.overlays.panel.kind == panelregistry.Help {
			m.overlays.panel = slashPanel{}
		}
	}
}

// hasActiveModal reports whether any modal overlay (slash panel, help overlay,
// or resume picker) currently claims the keyboard.
func (m *RootModel) hasActiveModal() bool {
	if m == nil {
		return false
	}
	return m.overlays.HasActiveModal() || (m.help.Panel != nil && m.help.Panel.Active())
}
