package cliapp

import (
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func (m *runMode) handleKeyUp() (tea.Model, tea.Cmd, bool) {
	if m.picker.Visible() {
		m.picker.MoveUp()
		return m, nil, true
	}
	if m.atFileDropdown.Visible() {
		m.atFileDropdown.MoveUp()
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.input.Value() == "" {
		if m.BtwScrollBy(-1) {
			return m, nil, true
		}
	}
	if len(m.streamQueue) > 0 && m.input.Value() == "" {
		last := len(m.streamQueue) - 1
		m.input.SetValue(m.streamQueue[last])
		m.streamQueue = m.streamQueue[:last]
		m.modeHintDismissed = true
		return m, nil, true
	}
	if isOnFirstLine(m.input.Value(), inputCursorByteOffset(m.input)) {
		m.historyUp()
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleKeyDown() (tea.Model, tea.Cmd, bool) {
	if m.picker.Visible() {
		m.picker.MoveDown()
		return m, nil, true
	}
	if m.atFileDropdown.Visible() {
		m.atFileDropdown.MoveDown()
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.input.Value() == "" {
		if m.BtwScrollBy(1) {
			return m, nil, true
		}
	}
	if isOnLastLine(m.input.Value(), inputCursorByteOffset(m.input)) {
		m.historyDown()
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleBackslashNewlineFallback() bool {
	offset := inputCursorByteOffset(m.input)
	val := m.input.Value()
	if offset > 0 && offset <= len(val) && val[offset-1] == '\\' {
		m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
		m.input.InsertRune('\n')
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return true
	}
	return false
}

func (m *runMode) handleEnterKey(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code != tea.KeyEnter {
		return m, nil, false
	}
	if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModShift)) && key.Mod&(tea.ModCtrl) == 0 {
		m.input.InsertRune('\n')
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, nil, true
	}
	if key.Mod.Contains(tea.ModCtrl) {
		return m, m.submitOrQueuePrompt(), true
	}
	if key.Mod != 0 {
		return m, nil, false
	}
	if m.altScreen && m.pinnedPromptText != "" && m.input.Value() == "" {
		if m.jumpToPinnedPrompt() {
			return m, nil, true
		}
	}
	if m.isBtwCardVisible() && m.input.Value() == "" {
		m.DismissBtwResponse()
		return m, nil, true
	}
	if m.handleBackslashNewlineFallback() {
		return m, nil, true
	}
	if m.atFileDropdown.Visible() {
		if m.tabSpliceAtFile(false) {
			m.historyIdx = 0
			m.historyEdit = ""
		}
		return m, nil, true
	}
	if m.queueMode {
		m.input.InsertRune('\n')
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, nil, true
	}
	return m, m.submitPrompt(), true
}

func (m *runMode) handleInputSelectionKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if (key.Code == 'a' || key.Code == 'A') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta) || (key.Mod.Contains(tea.ModCtrl) && key.Mod.Contains(tea.ModShift))) {
		if cmd, ok := m.selectAllInput(); ok {
			return m, cmd, true
		}
		return m, nil, true
	}
	if (key.Code == 'c' || key.Code == 'C') && key.Mod.Contains(tea.ModSuper) {
		if m.hasInputSelection() {
			start, end := m.inputSelectionRange()
			val := m.input.Value()
			if start < len(val) && end <= len(val) {
				text := val[start:end]
				_ = copyToClipboard(text)
				return m, m.copySelectionCmd(text), true
			}
		}
		return m, nil, true
	}
	if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && m.hasInputSelection() {
		m.deleteInputSelection()
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.clearEligibilityNotice()
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, nil, true
	}
	if (key.Code == tea.KeyLeft || key.Code == tea.KeyRight) && m.hasInputSelection() && !key.Mod.Contains(tea.ModShift) {
		start, end := m.inputSelectionRange()
		m.clearInputSelection()
		if key.Code == tea.KeyLeft {
			m.setInputCursorOffset(start)
		} else {
			m.setInputCursorOffset(end)
		}
		return m, nil, true
	}
	if m.hasInputSelection() && key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModSuper) {
		m.replaceInputSelection(key.Text)
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.clearEligibilityNotice()
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleWordMoveAndChords(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'j' && key.Mod.Contains(tea.ModCtrl) {
		m.input.InsertRune('\n')
		m.picker.Update([]rune(firstLine(m.input.Value())))
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, nil, true
	}
	if key.Code == tea.KeyLeft {
		if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
			m.moveWordLeft()
			m.picker.Update([]rune(firstLine(m.input.Value())))
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
			m.NavigateBtw(-1)
			return m, nil, true
		}
	}
	if key.Code == tea.KeyRight {
		if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
			m.moveWordRight()
			m.picker.Update([]rune(firstLine(m.input.Value())))
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
			m.NavigateBtw(1)
			return m, nil, true
		}
	}
	if key.Code == 'v' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		return m, m.insertPastedText(clipboardReadText()), true
	}
	return m, nil, false
}

func (m *runMode) updateTextareaAndDropdown(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	m.modeHintDismissed = true
	m.picker.Update([]rune(firstLine(m.input.Value())))
	m.clearEligibilityNotice()
	m.atFileDropdown.Update(m.input.Value(), m.cwd)
	return m, cmd
}

func (m *runMode) updateEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()
	switch key.Code {
	case tea.KeyUp:
		if mod, cmd, handled := m.handleKeyUp(); handled {
			return mod, cmd
		}
	case tea.KeyDown:
		if mod, cmd, handled := m.handleKeyDown(); handled {
			return mod, cmd
		}
	}
	if mod, cmd, handled := m.handleEnterKey(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleWordMoveAndChords(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleInputSelectionKeys(key); handled {
		return mod, cmd
	}
	return m.updateTextareaAndDropdown(msg)
}

func cursorLine(value string, cursor int) int {
	return tuilib.CursorLine(value, cursor)
}

func isOnFirstLine(value string, cursor int) bool {
	return tuilib.IsOnFirstLine(value, cursor)
}

func isOnLastLine(value string, cursor int) bool {
	return tuilib.IsOnLastLine(value, cursor)
}

func inputCursorByteOffset(m tas.Model) int {
	return tuilib.InputCursorByteOffset(m)
}

