package tui

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// Composer edits change the frame/layout, not the transcript wrap slot. Keep
// unknown/custom actions on the conservative path: Enter, Esc, Vim commands,
// modal keys and trajectory toggles can mutate conversation rows.
func (m *RootModel) composerOnlyMsg(msg tea.Msg) bool {
	keyMsg, ok := msg.(tea.KeyPressMsg)
	if !ok || m.vimEnabled() || !m.prompt.input.Focused() || m.hasActiveModal() ||
		m.hasActivePermissionPrompt() || m.askQuestion.Active() ||
		m.overlays.panel.kind != panelregistry.None || m.help.Panel.Active() ||
		m.overlays.resumePicker.Active() || m.prompt.altEscBuf.Pending ||
		(m.askQuestion.panel != nil && m.askQuestion.panel.Active()) ||
		(m.activeItems.Panel != nil && m.activeItems.Panel.Focused()) {
		return false
	}
	key := tea.Key(keyMsg)
	if _, translated := keybind.TranslateMacOptionToAlt(key); translated {
		return false
	}
	if key.Mod & ^tea.ModShift != 0 {
		return false
	}
	if m.prompt.keybindings != nil {
		action := m.prompt.keybindings.ActionForKey(key)
		switch action {
		case "", "navigation.left", "navigation.right":
		default:
			return false
		}
	}
	if key.Text != "" && key.Code != tea.KeyEnter && key.Code != tea.KeyTab {
		return true
	}
	switch key.Code {
	case tea.KeyBackspace, tea.KeyDelete, tea.KeyLeft, tea.KeyRight, tea.KeyHome, tea.KeyEnd:
		return true
	}
	return false
}

// invalidateFrameLayout refreshes prompt/status geometry without rebuilding the
// transcript wrap slot. InvalidateViewCache intentionally dirties both and is
// reserved for transcript/content changes.
func (m *RootModel) invalidateFrameLayout() {
	m.viewport.viewCacheValid = false
	m.viewport.extraRowsValid = false
}
