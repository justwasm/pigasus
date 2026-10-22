package main

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/mdrender"
)

func (m *runMode) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.login == nil {
		return m, nil, false
	}
	if !isScrollMsg(msg) {
		m.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.login.width, m.login.height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.login.state == loginStateSelectMethod {
			m.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case exchangeSuccessMsg:
		m.login = nil
		m.email = auth.EffectiveUserEmail()
		if m.backendFor != nil {
			m.backend = m.backendFor(m.model)
		}
		m.slashRespond("/login", "Authentication successful!")
		return m, nil, true
	}
	newLM, cmd := m.login.Update(msg)
	if lm, ok := newLM.(*loginModel); ok {
		m.login = lm
		if lm.authenticated {
			m.login = nil
			m.email = auth.EffectiveUserEmail()
			if m.backendFor != nil {
				m.backend = m.backendFor(m.model)
			}
			m.slashRespond("/login", "Authentication successful!")
			return m, nil, true
		}
	}
	return m, cmd, true
}

func (m *runMode) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	m.width, m.height = msg.Width, msg.Height
	mdrender.SetCurrentWidth(msg.Width)
	if w := msg.Width - len(inputPrompt); w > 10 {
		m.input.SetWidth(w)
	}
	return m, nil
}

func (m *runMode) updateMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.altScreen || ev.Button != tea.MouseLeft {
		return m, nil
	}
	if m.handlePickerClick(ev) {
		return m, nil
	}
	if m.picker.Visible() && !m.isInputY(ev.Y) {
		m.picker.Reset()
	}
	if m.pinnedPromptText != "" && ev.Y == 0 {
		if m.jumpToPinnedPrompt() {
			return m, nil
		}
	}
	if m.isInputY(ev.Y) {
		m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
		m.selDragging = false
		offset := m.inputOffsetAt(ev.X, ev.Y)
		m.setInputCursorOffset(offset)
		m.inputSelDragging = true
		m.inputSelStart = offset
		m.inputSelEnd = offset
		return m, nil
	}
	m.clearInputSelection()
	row, col, ok := m.transcriptCellAt(ev.X, ev.Y)
	if !ok {
		return m, nil
	}
	m.selDragging = true
	m.selStartRow, m.selStartCol = row, col
	m.selEndRow, m.selEndCol = row, col
	return m, nil
}

func (m *runMode) updateMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.altScreen {
		m.handlePickerHover(ev)
	}
	if m.inputSelDragging && m.altScreen {
		offset := m.inputOffsetAt(ev.X, ev.Y)
		if offset != m.inputSelEnd {
			m.inputSelEnd = offset
			m.setInputCursorOffset(offset)
		}
		return m, nil
	}
	if !m.selDragging || !m.altScreen {
		return m, nil
	}
	row, col, ok := m.transcriptCellAt(ev.X, ev.Y)
	if !ok {
		return m, nil
	}
	if row != m.selEndRow || col != m.selEndCol {
		m.selEndRow, m.selEndCol = row, col
	}
	return m, nil
}

func (m *runMode) updateMouseReleaseInputSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.inputSelDragging = false
	if ev.Button == tea.MouseLeft {
		m.inputSelEnd = m.inputOffsetAt(ev.X, ev.Y)
	}
	start, end := m.inputSelectionRange()
	if start < end {
		val := m.input.Value()
		if start < len(val) && end <= len(val) {
			text := val[start:end]
			_ = copyToClipboard(text)
			return m, m.copySelectionCmd(text)
		}
	} else {
		m.clearInputSelection()
	}
	return m, nil
}

func (m *runMode) updateMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.inputSelDragging {
		return m.updateMouseReleaseInputSel(ev)
	}
	if !m.selDragging {
		return m, nil
	}
	m.selDragging = false
	if ev.Button != tea.MouseLeft {
		m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
		return m, nil
	}
	row, col, ok := m.transcriptCellAt(ev.X, ev.Y)
	if ok {
		m.selEndRow, m.selEndCol = row, col
	}
	text := m.selectionText()
	m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
	if text != "" {
		_ = copyToClipboard(text)
		return m, m.copySelectionCmd(text)
	}
	return m, nil
}

func (m *runMode) updateEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.tempPath != "" {
		defer func() { _ = os.Remove(msg.tempPath) }()
		if msg.err == nil {
			if data, err := os.ReadFile(msg.tempPath); err == nil {
				m.input.SetValue(strings.TrimRight(string(data), "\n"))
			}
		}
	}
	if msg.err != nil {
		m.response = append(m.response, "Editor exited with error: "+msg.err.Error())
	}
	return m, nil
}

func (m *runMode) updateMouseAndMiscMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		mod, cmd := m.updateWindowSize(msg)
		return mod, cmd, true
	case tea.MouseClickMsg:
		mod, cmd := m.updateMouseClick(msg.Mouse())
		return mod, cmd, true
	case tea.MouseMotionMsg:
		mod, cmd := m.updateMouseMotion(msg.Mouse())
		return mod, cmd, true
	case tea.MouseReleaseMsg:
		mod, cmd := m.updateMouseRelease(msg.Mouse())
		return mod, cmd, true
	case tea.MouseMsg:
		if m.handleMouseWheel(msg) {
			return m, nil, true
		}
		return m, nil, true
	case editorFinishedMsg:
		mod, cmd := m.updateEditorFinished(msg)
		return mod, cmd, true
	case resumeBlinkMsg:
		if m.resumeMode && m.resumeQuery != "" {
			m.resumeCursorOn = !m.resumeCursorOn
			return m, resumeBlink(), true
		}
		m.resumeCursorOn = false
		return m, nil, true
	case keybind.EscTimeoutMsg:
		if m.altEscBuf.Pending {
			m.altEscBuf.Pending = false
			_, _ = m.handleEscKey()
		}
		return m, nil, true
	case voiceConnectDoneMsg:
		mod, cmd := m.handleVoiceConnectDone(msg)
		return mod, cmd, true
	case voiceTickMsg:
		mod, cmd := m.handleVoiceTick()
		return mod, cmd, true
	case tea.PasteMsg:
		cmd := m.insertPastedText(msg.Content)
		return m, cmd, true
	}
	return m, nil, false
}
