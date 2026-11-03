// tui_alt_esc.go — Option/Alt key translation and Esc chord release.
package cliapp

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
)

// altKeyFire records one synthesised alt+letter dispatch.
type altKeyFire struct {
	Letter rune   // lowercase letter that was synthesised
	Action string // action name from keybindings.json
}

// altDispatchAction is the production action table.
func (m *runMode) altDispatchAction(letter rune, action string) {
	m.altKeyFires = append(m.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.response = append(m.response,
		uirows.NoticeDim("alt+"+string(letter)+" → "+action))
}

// handleEscKey runs the bare-Esc branch: delegates to handleEscKeyMsg
// to ensure unified priority across direct Esc and EscReleaseWindow tick.
func (m *runMode) handleEscKey() (tea.Cmd, bool) {
	_, cmd := m.handleEscKeyMsg()
	return cmd, true
}

// armEscReleaseTick returns a tea.Cmd that fires after keybind.EscReleaseWindow.
func (m *runMode) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
}

func (m *runMode) canConsumeAltEsc() bool {
	if m.panel.kind != panelregistry.None || m.helpPanel.Active() ||
		m.picker.Visible() || m.atFilePicker.Visible() ||
		m.resumePicker.Active() || m.voiceSurface.Active() ||
		m.mediaExpanded || m.isBtwCardVisible() ||
		m.streaming || m.store.BashCancel != nil ||
		m.hasInputSelection() {
		return false
	}
	return true
}

// consumeAltEsc is the alt-keytranslation entry point.
func (m *runMode) consumeAltEsc(msg tea.KeyMsg) (newMsg tea.KeyMsg, consumed bool, cmd tea.Cmd) {
	key := msg.Key()
	if !keybind.TermNeedsOptionMeta() {
		return msg, false, nil
	}

	if m.keybindings == nil || len(m.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	if !m.canConsumeAltEsc() {
		return msg, false, nil
	}

	if m.altEscBuf.Pending {
		consumedKey := m.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.keybindings != nil && m.keybindings.Has(r) },
			func(r rune) {
				key = tea.Key{Code: r, Mod: tea.ModAlt, Text: string(r)}
			},
			func() { _, _ = m.handleEscKey() },
			time.Now(),
		)
		if consumedKey {
			return tea.KeyPressMsg(key), false, nil
		}
		return msg, false, nil
	}

	if key.Code == tea.KeyEsc && key.Mod == 0 {
		if m.altEscBuf.Start(time.Now()) {
			return msg, true, m.armEscReleaseTick()
		}
	}

	return msg, false, nil
}
