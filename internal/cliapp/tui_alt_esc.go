// tui_alt_esc.go — Option/Alt key translation and Esc chord release.
package cliapp

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/keybind"
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
		cWarningBlueOpen+"  ⎿  "+cWarningBlueClose+
			cDimGrayOpen+"alt+"+string(letter)+" → "+action+cDimGrayClose)
}

// handleEscKey runs the bare-Esc branch: dismiss help, dismiss
// picker, clear input, dismiss the head announcement, or absorb
// silently. Returns (cmd, true) when the Esc was consumed.
func (m *runMode) handleEscKey() (tea.Cmd, bool) {
	if m.helpMode {
		m.helpMode = false
		m.response = append(m.response,
			cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
				cDimGrayOpen+"Exited /help command"+cDimGrayClose)
		return nil, true
	}
	if m.picker.Visible() {
		m.picker.Reset()
		return nil, true
	}
	if m.input.Value() != "" {
		m.input.Reset()
		return nil, true
	}
	if m.activeAnnouncement() != nil {
		m.dismissCurrentAnnouncement()
		if msg := m.announcementSaveErrorMessage(); msg != "" {
			m.response = append(m.response, msg)
		}
		return nil, true
	}
	// Absorb Esc silently — ref binary does not quit on Esc,
	// only on Ctrl+C and Ctrl+D.
	return nil, true
}

// armEscReleaseTick returns a tea.Cmd that fires after keybind.EscReleaseWindow.
func (m *runMode) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
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
