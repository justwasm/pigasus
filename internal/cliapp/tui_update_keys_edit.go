package cliapp

import (
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

func (m *runMode) handleKeyUp(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.input.Value() == "" {
		if m.btwScrollBy(-1) {
			return m, nil, true
		}
	}
	if len(m.store.StreamQueue) > 0 && m.input.Value() == "" {
		last := len(m.store.StreamQueue) - 1
		m.input.SetValue(m.store.StreamQueue[last])
		m.store.StreamQueue = m.store.StreamQueue[:last]
		m.modeHintDismissed = true
		return m, nil, true
	}
	if m.promptHistory.Index() > 0 || history.IsBlank(m.input.Value()) {
		m.historyUp()
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleKeyDown(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.input.Value() == "" {
		if m.btwScrollBy(1) {
			return m, nil, true
		}
	}
	if m.promptHistory.Index() > 0 {
		m.historyDown()
		return m, nil, true
	}
	if !m.activeItems.Hidden() && m.activeItems.Count() > 0 {
		offset := tuilib.InputCursorByteOffset(m.input)
		if tuilib.IsOnLastLine(m.input.Value(), offset) {
			m.activeItems.SetFocus(true)
			m.activeItems.SelectFirst()
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m *runMode) handleEnterKey(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code != tea.KeyEnter {
		return m, nil, false
	}
	if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModShift)) && key.Mod&(tea.ModCtrl) == 0 {
		m.input.InsertRune('\n')
		m.modeHintDismissed = true
		m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
		m.atFilePicker.Update()
		m.promptHistory.Reset()
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
	if composer.BackslashNewline(m.composerSeams()) {
		return m, nil, true
	}
	if m.atFilePicker.Visible() {
		if m.atFilePicker.Complete(false) {
			m.promptHistory.Reset()
		}
		return m, nil, true
	}
	if m.store.QueueMode {
		m.input.InsertRune('\n')
		m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
		m.atFilePicker.Update()
		return m, nil, true
	}
	return m, m.submitPrompt(), true
}

func (m *runMode) handleWordMoveAndChords(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'j' && key.Mod.Contains(tea.ModCtrl) {
		m.input.InsertRune('\n')
		m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
		m.atFilePicker.Update()
		m.promptHistory.Reset()
		return m, nil, true
	}
	if key.Code == tea.KeyLeft {
		if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
			m.moveWord(true)
			m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
			m.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
			m.NavigateBtw(-1)
			return m, nil, true
		}
	}
	if key.Code == tea.KeyRight {
		if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
			m.moveWord(false)
			m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
			m.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
			m.NavigateBtw(1)
			return m, nil, true
		}
	}
	if key.Code == 'v' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		// The paste key's image half (agy 1.4.9): an image on the
		// clipboard is attached, a dropped image file is attached, and
		// only a clipboard with neither stays text.
		m.promptHistory.Reset()
		return m, m.handlePasteContent(clipboardReadText()), true
	}
	return m, nil, false
}

func (m *runMode) updateEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.Key()
	switch key.Code {
	case tea.KeyUp:
		if mod, cmd, handled := m.handleKeyUp(key); handled {
			return mod, cmd
		}
	case tea.KeyDown:
		if mod, cmd, handled := m.handleKeyDown(key); handled {
			return mod, cmd
		}
	}
	if mod, cmd, handled := m.handleEnterKey(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleWordMoveAndChords(key); handled {
		return mod, cmd
	}
	seams := m.composerSeams()
	if cmd, handled := composer.HandleKey(seams, key); handled {
		return m, cmd
	}
	return m, composer.UpdateTextarea(seams, msg)
}

// composerSeams assembles the facts the composer package needs.
//
// The host owns the widget, the selection's store, the two dropdown
// panels and the session's own notices; the composer asks for them
// here and does the editing.
func (m *runMode) composerSeams() composer.Seams {
	start, end := m.store.InputSelectionRange()
	return composer.Seams{
		Editor:           composer.NewTextArea(&m.input),
		Picker:           m.picker,
		Files:            m.atFilePicker,
		History:          &m.promptHistory,
		Selection:        composer.Range{Start: start, End: end},
		SelectAll:        m.selectAllInput,
		ClearSelection:   m.store.ResetInputSelection,
		DeleteSelection:  m.deleteInputSelection,
		ReplaceSelection: m.replaceInputSelection,
		Copy: func(text string) tea.Cmd {
			// The clipboard write is OS I/O and stays on the host's
			// side of the seam: the composer decides that the
			// selection is copied, the host performs the write.
			_ = clipboard.Write(text)
			return m.copySelectionCmd(text)
		},
		ClearEligibilityNotice: m.clearEligibilityNotice,
		DismissModeHint:        func() { m.modeHintDismissed = true },
	}
}
