package cliapp

import (
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

func (m *runMode) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	oldW := m.width
	oldH := m.height
	m.width, m.height = msg.Width, msg.Height
	mdrender.SetCurrentWidth(msg.Width)
	if w := msg.Width - len(inputPrompt); w > 10 {
		m.input.SetWidth(w)
	}
	if (oldW != msg.Width || oldW == 0) && len(m.response) > 0 {
		m.reflowTranscript(msg.Width)
	} else if oldH != msg.Height || oldW != msg.Width {
		m.rowsCacheDirty = true
		m.viewCacheValid = false
		m.extraRowsValid = false
		m.rowsCache = wrapSlot{}
		m.rowsCacheN1 = wrapSlot{}
		m.rowsView = composedRows{}
	}
	m.activeItems.Clamp()
	return m, nil
}

func (m *runMode) updateMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.altScreen || ev.Button != tea.MouseLeft {
		return m, nil
	}
	if m.picker.HandleClick(ev) {
		return m, nil
	}
	if m.picker.Visible() && !m.isInputY(ev.Y) {
		m.picker.Reset()
	}
	if m.pinnedPromptText != "" && (ev.Y == 0 || ev.Y == 1) {
		if m.jumpToPinnedPrompt() {
			return m, nil
		}
	}
	if m.isInputY(ev.Y) {
		m.store.ResetSelection()
		offset := m.inputOffsetAt(ev.X, ev.Y)
		m.setInputCursorOffset(offset)
		m.store.InputSelDragging = true
		m.store.InputSelStart = offset
		m.store.InputSelEnd = offset
		return m, nil
	}
	m.store.ResetInputSelection()
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.width)
	if !ok {
		return m, nil
	}
	m.store.SelDragging = true
	m.store.SelStartRow, m.store.SelStartCol = row, col
	m.store.SelEndRow, m.store.SelEndCol = row, col
	return m, nil
}

func (m *runMode) updateMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.picker.HandleHover(ev)
	if m.store.InputSelDragging && m.altScreen {
		offset := m.inputOffsetAt(ev.X, ev.Y)
		if offset != m.store.InputSelEnd {
			m.store.InputSelEnd = offset
			m.setInputCursorOffset(offset)
		}
		return m, nil
	}
	if !m.store.SelDragging || !m.altScreen {
		return m, nil
	}
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.width)
	if !ok {
		return m, nil
	}
	if row != m.store.SelEndRow || col != m.store.SelEndCol {
		m.store.SelEndRow, m.store.SelEndCol = row, col
	}
	return m, nil
}

func (m *runMode) updateMouseReleaseInputSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.store.InputSelDragging = false
	if ev.Button == tea.MouseLeft {
		m.store.InputSelEnd = m.inputOffsetAt(ev.X, ev.Y)
	}
	start, end := m.store.InputSelectionRange()
	if start < end {
		val := m.input.Value()
		if start < len(val) && end <= len(val) {
			text := val[start:end]
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text)
		}
	} else {
		m.store.ResetInputSelection()
	}
	return m, nil
}

func (m *runMode) updateMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.store.InputSelDragging {
		return m.updateMouseReleaseInputSel(ev)
	}
	if !m.store.SelDragging {
		return m, nil
	}
	m.store.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.store.ResetSelection()
		return m, nil
	}
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.width)
	if ok {
		m.store.SelEndRow, m.store.SelEndCol = row, col
	}
	text := m.selectionText()
	m.store.ResetSelection()
	if text != "" {
		_ = clipboard.Write(text)
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
	case resumepicker.BlinkMsg:
		if m.resumePicker.Active() && m.resumePicker.Query() != "" {
			m.resumePicker.SetCursorOn(!m.resumePicker.CursorOn())
			return m, resumepicker.BlinkCmd(), true
		}
		m.resumePicker.SetCursorOn(false)
		return m, nil, true
	case keybind.EscTimeoutMsg:
		if m.altEscBuf.Pending {
			m.altEscBuf.Pending = false
			_, _ = m.handleEscKey()
		}
		return m, nil, true
	case voice.ConnectDoneMsg:
		return m, m.voiceSurface.HandleConnectDone(msg), true
	case voice.TickMsg:
		return m, m.voiceSurface.HandleTick(), true
	case tea.PasteMsg:
		cmd := m.handlePasteContent(msg.Content)
		return m, cmd, true
	case pasteMediaMsg:
		mod, cmd := m.updatePasteMedia(msg)
		return mod, cmd, true
	}
	return m, nil, false
}
