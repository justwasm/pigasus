package tui

// overlay_host.go — the RootModel side of the OverlayModel seam.
//
// The overlay model owns the dismissal chain; the root supplies the two
// facts it cannot know: the terminal height (the resume picker pages by it)
// and the panel key handler (the panel owns its own keys).

import tea "charm.land/bubbletea/v2"

// ViewportHeight is the overlay host seam.
func (m *RootModel) ViewportHeight() int { return m.viewport.height }

// HandlePanelKey is the overlay host seam: the modal panel owns its keys.
func (m *RootModel) HandlePanelKey(key tea.Key) (bool, tea.Cmd) { return m.handlePanelKey(key) }
