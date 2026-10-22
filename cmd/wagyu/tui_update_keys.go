package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/keybind"
)

func (m *runMode) handleGlobalOrModalKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if r, ok := keybind.IsAltLetter(key); ok {
		if m.keybindings != nil {
			if action, ok := m.keybindings.LookupAltLetter(r); ok {
				m.altDispatchAction(r, action)
				return m, nil, true
			}
		}
		if r == 'm' {
			return m, m.toggleQueueMode(), true
		}
	}
	if m.voiceMode {
		if handled, cmd := m.handleVoiceKey(key); handled {
			return m, cmd, true
		}
	}
	if listPanelKind(m.panel.kind) {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if key.Code == 'l' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
		m.scroll = 0
		return m, tea.ClearScreen, true
	}
	if key.Code == 'o' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
		m.expanded = !m.expanded
		m.rerenderBlocks()
		m.rerenderLiveSteps()
		return m, nil, true
	}
	if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
		return m, m.editPromptCmd(), true
	}
	if key.Code == 'k' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
		return m, m.openSubAgentQuestionPicker(), true
	}
	return m, nil, false
}

func (m *runMode) handleFunctionAndModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == '?' && key.Mod == 0 && m.input.Value() == "" &&
		!m.helpMode && !m.picker.Visible() && !m.resumeMode {
		m.helpMode = true
		m.helpTab = 2 // shortcuts
		m.helpWindow = 0
		m.helpCursor = 0
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		return m, nil, true
	}
	if key.Code == '!' && key.Mod == 0 && m.input.Value() == "" && !m.bashMode &&
		!m.helpMode && !m.picker.Visible() && !m.resumeMode && m.panel.kind == panelNone {
		m.bashMode = true
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.bashMode && m.input.Value() == "" {
		m.bashMode = false
		return m, nil, true
	}
	if key.Code == tea.KeyF2 && !m.resumeMode && !m.helpMode && m.panel.kind == panelNone && !m.picker.Visible() {
		m.openRenamePanel()
		return m, nil, true
	}
	if key.Code == tea.KeyF5 && !m.resumeMode && !m.helpMode && m.panel.kind == panelNone && !m.picker.Visible() && !m.voiceMode {
		return m, m.startVoiceDictation("f5"), true
	}
	if m.panel.kind != panelNone {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if !m.helpMode && !m.picker.Visible() && !m.resumeMode && m.handleScrollKey(key) {
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleResumeDeleteOrRename(key tea.Key, rows []convstore.ConversationMeta, total int) (tea.Model, tea.Cmd, bool) {
	if key.Code == tea.KeyF4 {
		if m.resumeTab == 0 && total > 0 && m.resumeCursor >= 0 && m.resumeCursor < total {
			picked := rows[m.resumeCursor]
			if picked.ID != "" {
				_ = convstore.DeleteConversation(picked.ID)
				m.reloadResumeList()
				rows = m.resumeFiltered()
				total = len(rows)
				if m.resumeCursor >= total && total > 0 {
					m.resumeCursor = total - 1
				}
				if m.resumeWindow > m.resumeCursor {
					m.resumeWindow = m.resumeCursor
				}
			}
			return m, nil, true
		}
	}
	if key.Code == tea.KeyF2 {
		if m.resumeTab == 0 && total > 0 && m.resumeCursor >= 0 && m.resumeCursor < total {
			picked := rows[m.resumeCursor]
			m.resumeMode = false
			m.resumeList = nil
			m.conversationName = picked.Title
			m.openRenamePanel()
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m *runMode) handleResumeNav(key tea.Key, total int) bool {
	switch key.Code {
	case tea.KeyUp:
		m.resumeListUp(total)
		return true
	case tea.KeyDown:
		m.resumeListDown(total)
		return true
	case tea.KeyRight:
		m.resumeListPage(total, 1)
		return true
	case tea.KeyLeft:
		m.resumeListPage(total, -1)
		return true
	case tea.KeyTab:
		m.resumeTab = (m.resumeTab + 1) % 2
		m.resumeWindow, m.resumeCursor = 0, 0
		return true
	}
	return false
}

func (m *runMode) handleResumeActionKeys(key tea.Key, rows []convstore.ConversationMeta, total int) (tea.Model, tea.Cmd, bool) {
	switch key.Code {
	case tea.KeyEsc:
		if m.resumeQuery != "" {
			m.resumeQuery = ""
			m.resumeWindow, m.resumeCursor = 0, 0
			return m, nil, true
		}
		m.closeResumePicker()
		return m, nil, true
	case tea.KeyEnter:
		if total == 0 || m.resumeTab != 0 || m.resumeCursor < 0 || m.resumeCursor >= total {
			m.closeResumePicker()
			return m, nil, true
		}
		picked := rows[m.resumeCursor]
		m.closeResumePicker()
		return m, m.resumeConversation(picked), true
	case tea.KeyBackspace:
		if m.resumeQuery != "" {
			r := []rune(m.resumeQuery)
			m.resumeQuery = string(r[:len(r)-1])
			m.resumeWindow, m.resumeCursor = 0, 0
			m.resumeCursorOn = true
			return m, resumeBlink(), true
		}
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) closeResumePicker() {
	m.resumeMode = false
	m.resumeList = nil
	m.resumeCursor, m.resumeWindow, m.resumeTab = 0, 0, 0
}

func (m *runMode) handleResumeModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !m.resumeMode {
		return m, nil, false
	}
	rows := m.resumeFiltered()
	total := len(rows)
	if m.handleResumeNav(key, total) {
		return m, nil, true
	}
	if mod, cmd, handled := m.handleResumeActionKeys(key, rows, total); handled {
		return mod, cmd, true
	}
	if mod, cmd, handled := m.handleResumeDeleteOrRename(key, rows, total); handled {
		return mod, cmd, true
	}
	if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
		m.resumeQuery = ""
		m.resumeWindow, m.resumeCursor = 0, 0
		return m, nil, true
	}
	if isTypedText(key) {
		m.resumeQuery += key.Text
		m.resumeWindow, m.resumeCursor = 0, 0
		m.resumeCursorOn = true
		return m, resumeBlink(), true
	}
	return m, nil, true
}

func (m *runMode) handleEscInputAndAnnouncements() bool {
	if m.hasInputSelection() {
		m.clearInputSelection()
		return true
	}
	if m.input.Value() != "" {
		m.input.Reset()
		m.clearInputSelection()
		m.bashMode = false
		return true
	}
	if m.bashMode {
		m.bashMode = false
		return true
	}
	if m.activeAnnouncement() != nil {
		m.dismissCurrentAnnouncement()
		if msg := m.announcementSaveErrorMessage(); msg != "" {
			m.response = append(m.response, msg)
		}
		return true
	}
	if m.isBtwCardVisible() {
		m.DismissBtwResponse()
		return true
	}
	return false
}

func (m *runMode) handleEscKeyMsg() (tea.Model, tea.Cmd) {
	if m.helpMode {
		m.helpMode = false
		m.response = append(m.response, slashEchoLines("/help", "Exited /help command")...)
		return m, nil
	}
	if m.picker.Visible() {
		m.picker.Reset()
		return m, nil
	}
	if m.atFileDropdown.Visible() {
		m.atFileDropdown.Reset()
		return m, nil
	}
	if m.streaming && m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
		return m, nil
	}
	if m.bashCancel != nil {
		m.cancelBashCommand()
		return m, nil
	}
	TriggerCompactCancel()
	m.handleEscInputAndAnnouncements()
	return m, nil
}

func (m *runMode) handleTabCompletion(key tea.Key) bool {
	if key.Code != tea.KeyTab || key.Mod.Contains(tea.ModShift) {
		return false
	}
	if m.picker.Selected() != nil {
		sel := m.picker.Selected()
		m.input.SetValue(sel.Name)
		m.picker.Update([]rune(sel.Name))
		m.historyIdx = 0
		m.historyEdit = ""
		return true
	}
	if m.atFileDropdown.Visible() && m.atFileDropdown.Selected() != "" {
		if m.tabSpliceAtFile(true) {
			m.historyIdx = 0
			m.historyEdit = ""
			return true
		}
	}
	return false
}

func (m *runMode) handleCtrlAndTabKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		// agy kills a running shell command on Ctrl+C and still arms the
		// exit confirmation with the same press (【一手】: the block
		// finalises with no body while "press ctrl+c again to exit"
		// appears). The clone used to only arm the confirmation.
		if m.bashCancel != nil {
			m.cancelBashCommand()
		}
		if m.exitConfirm {
			m.quitting = true
			return m, tea.Quit, true
		}
		m.exitConfirm = true
		m.exitConfirmAt = time.Now()
		return m, nil, true
	}
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) {
		if m.hasInputSelection() {
			m.deleteInputSelection()
			m.modeHintDismissed = true
			m.picker.Update([]rune(firstLine(m.input.Value())))
			m.clearEligibilityNotice()
			m.atFileDropdown.Update(m.input.Value(), m.cwd)
			return m, nil, true
		}
		if m.input.Value() == "" {
			m.quitting = true
			return m, tea.Quit, true
		}
	}
	if key.Code == 'a' && key.Mod == 0 && m.input.Value() == "" &&
		!m.helpMode && !m.picker.Visible() && !m.resumeMode &&
		m.panel.kind == panelNone &&
		m.subAgentQuestions != nil &&
		m.subAgentQuestions.PendingCount(m.subAgentQuestionsActive) > 0 {
		return m, m.openSubAgentQuestionPicker(), true
	}
	if m.exitConfirm {
		m.exitConfirm = false
	}
	if m.handleTabCompletion(key) {
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleHelpModeNavigation(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !m.helpMode {
		return m, nil, false
	}
	switch key.Code {
	case tea.KeyTab, tea.KeyRight:
		m.helpTab = (m.helpTab + 1) % len(helpTabs(m))
		m.helpWindow, m.helpCursor = 0, 0
		return m, nil, true
	case tea.KeyLeft:
		n := len(helpTabs(m))
		m.helpTab = (m.helpTab + n - 1) % n
		m.helpWindow, m.helpCursor = 0, 0
		return m, nil, true
	case tea.KeyUp:
		m.helpListUp(helpListLen(m.helpTab))
		return m, nil, true
	case tea.KeyDown:
		m.helpListDown(helpListLen(m.helpTab))
		return m, nil, true
	case tea.KeyHome:
		if key.Mod.Contains(tea.ModCtrl) {
			m.helpListHome()
		}
		return m, nil, true
	case tea.KeyEnd:
		if key.Mod.Contains(tea.ModCtrl) {
			m.helpListEnd(helpListLen(m.helpTab))
		}
		return m, nil, true
	}
	return m, nil, true
}

func (m *runMode) updateKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	newMsg, consumed, escCmd := m.consumeAltEsc(msg)
	if consumed {
		return m, escCmd
	}
	msg = newMsg
	key := msg.Key()

	if mod, cmd, handled := m.handleGlobalOrModalKeys(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleFunctionAndModeKeys(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleResumeModeKeys(key); handled {
		return mod, cmd
	}
	if m.subAgentQuestionPickerOpen {
		return m, m.handleSubAgentQuestionPickerKey(key)
	}
	if m.vimEnabled() && !m.helpMode && !m.picker.Visible() && !m.resumeMode && m.panel.kind == panelNone {
		if cmd, handled := m.handleVimKey(key); handled {
			m.exitConfirm = false
			return m, cmd
		}
	}
	if key.Code == tea.KeyEsc {
		return m.handleEscKeyMsg()
	}
	if mod, cmd, handled := m.handleCtrlAndTabKeys(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleHelpModeNavigation(key); handled {
		return mod, cmd
	}
	if key.Code == tea.KeyTab && key.Mod.Contains(tea.ModShift) {
		m.cycleMode()
		return m, nil
	}
	if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) && m.isBtwCardVisible() && !m.picker.Visible() && !m.atFileDropdown.Visible() {
		m.ToggleBtwExpanded()
		return m, nil
	}
	return m.updateEditorKey(msg)
}
