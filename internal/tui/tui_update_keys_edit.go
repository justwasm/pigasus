package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// ---- tui_update_keys_edit.go ----

func (m *RootModel) handleKeyUp(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.prompt.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.prompt.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.prompt.input.Value() == "" {
		if m.btwScrollBy(-1) {
			return m, nil, true
		}
	}
	if m.prompt.input.Value() == "" {
		// 1. If currently streaming and there is an in-flight pending steer instruction, recall it for editing
		if m.conversation.streaming {
			if len(m.session.store.SendingMessages) > 0 {
				last := len(m.session.store.SendingMessages) - 1
				msg := m.session.store.SendingMessages[last]
				m.session.store.SendingMessages = m.session.store.SendingMessages[:last]
				_, _ = m.popActiveSteer()
				m.prompt.input.SetValue(msg)
				m.prompt.modeHintDismissed = true
				m.viewport.rowsCacheDirty = true
				return m, nil, true
			}
			if msg, ok := m.popActiveSteer(); ok && msg != "" {
				m.prompt.input.SetValue(msg)
				m.prompt.modeHintDismissed = true
				return m, nil, true
			}
		}

		// 2. If prompts are queued while streaming or idle, recall ALL queued prompts combined
		if len(m.session.store.StreamQueue) > 0 || m.session.store.QueuedPrompt != "" {
			var allQueued []string
			if m.session.store.QueuedPrompt != "" {
				allQueued = append(allQueued, m.session.store.QueuedPrompt)
				m.session.store.QueuedPrompt = ""
			}
			if len(m.session.store.StreamQueue) > 0 {
				allQueued = append(allQueued, m.session.store.StreamQueue...)
				m.session.store.StreamQueue = nil
			}
			m.prompt.input.SetValue(strings.Join(allQueued, "\n"))
			m.prompt.modeHintDismissed = true
			m.saveSession()
			return m, nil, true
		}
	}
	if m.prompt.promptHistory.Index() > 0 || history.IsBlank(m.prompt.input.Value()) {
		m.historyUp()
		return m, nil, true
	}
	return m, nil, false
}

func (m *RootModel) handleKeyDown(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.prompt.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.prompt.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.prompt.input.Value() == "" {
		if m.btwScrollBy(1) {
			return m, nil, true
		}
	}
	if m.prompt.promptHistory.Index() > 0 {
		m.historyDown()
		return m, nil, true
	}
	if !m.activeItems.Panel.Hidden() && m.activeItems.Panel.Count() > 0 {
		offset := tuilib.InputCursorByteOffset(m.prompt.input)
		if tuilib.IsOnLastLine(m.prompt.input.Value(), offset) {
			m.activeItems.Panel.SetFocus(true)
			m.activeItems.Panel.SelectFirst()
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m *RootModel) handleEnterKey(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code != tea.KeyEnter {
		return m, nil, false
	}
	if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
		if m.conversation.streaming {
			prompt := strings.TrimSpace(m.prompt.input.Value())
			if prompt != "" {
				prompt = m.expandPastedPlaceholders(prompt)
				m.pushHistory(prompt)
				if m.session.store.QueueMode {
					if m.steerActiveTurn(prompt) {
						m.prompt.input.Reset()
						m.ResetInputSelection()
						m.prompt.promptHistory.Reset()
						m.prompt.picker.Reset()
						m.prompt.atFilePicker.Reset()
						m.prompt.modeHintDismissed = true
						return m, nil, true
					}
				} else {
					m.session.store.StreamQueue = append(m.session.store.StreamQueue, prompt)
					m.prompt.input.Reset()
					m.ResetInputSelection()
					m.prompt.promptHistory.Reset()
					m.prompt.picker.Reset()
					m.prompt.atFilePicker.Reset()
					m.prompt.modeHintDismissed = true
					return m, nil, true
				}
			}
			return m, nil, true
		}
		m.prompt.input.InsertRune('\n')
		m.prompt.modeHintDismissed = true
		m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
		m.prompt.atFilePicker.Update()
		m.prompt.promptHistory.Reset()
		return m, nil, true
	}
	if key.Mod.Contains(tea.ModShift) && key.Mod&(tea.ModCtrl) == 0 {
		m.prompt.input.InsertRune('\n')
		m.prompt.modeHintDismissed = true
		m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
		m.prompt.atFilePicker.Update()
		m.prompt.promptHistory.Reset()
		return m, nil, true
	}
	if key.Mod.Contains(tea.ModCtrl) {
		return m, m.submitOrQueuePrompt(), true
	}
	if key.Mod != 0 {
		return m, nil, false
	}

	// Reference agy absorbUnbracketedPasteNewline (0x102da6f00):
	// In terminals lacking bracketed paste, multiline paste is emitted as a burst of
	// rapid keystrokes. Unmodified Enter keys arriving < 10ms apart increment
	// enterBurstCount. When enterBurstCount > 0, the Enter is absorbed as a newline
	// in the composer and does not submit the prompt.
	now := time.Now()
	if !m.prompt.lastEnterTime.IsZero() && now.Sub(m.prompt.lastEnterTime) < 10*time.Millisecond {
		m.prompt.enterBurstCount++
	} else {
		m.prompt.enterBurstCount = 0
	}
	m.prompt.lastEnterTime = now

	if m.prompt.enterBurstCount > 0 {
		m.prompt.input.InsertRune('\n')
		m.prompt.modeHintDismissed = true
		m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
		m.prompt.atFilePicker.Update()
		m.prompt.promptHistory.Reset()
		return m, nil, true
	}

	if m.viewport.altScreen && m.viewport.pinnedPromptText != "" && m.prompt.input.Value() == "" {
		if m.jumpToPinnedPrompt() {
			return m, nil, true
		}
	}
	if m.isBtwCardVisible() && m.prompt.input.Value() == "" {
		m.DismissBtwResponse()
		return m, nil, true
	}
	if composer.BackslashNewline(m.composerSeams()) {
		return m, nil, true
	}
	if m.prompt.atFilePicker.Visible() {
		if m.prompt.atFilePicker.Complete(false) {
			m.prompt.promptHistory.Reset()
		}
		return m, nil, true
	}
	if m.prompt.picker != nil && m.prompt.picker.Visible() {
		if handled, cmd := m.handleSlashPickerEnter(); handled {
			return m, cmd, true
		}
	}
	return m, m.submitOrQueuePrompt(), true
}

func (m *RootModel) handleSlashPickerEnter() (bool, tea.Cmd) {
	if m.prompt.picker == nil || !m.prompt.picker.Visible() {
		return false, nil
	}
	if m.prompt.picker.IsModelMode() {
		return m.submitModelCompletion()
	}
	wasTerminalMode := m.prompt.picker.IsTerminalMode()
	sel := m.prompt.picker.Selected()
	if sel == nil {
		return false, nil
	}
	handled, compCmd := m.prompt.picker.CommitSelectionCmd()
	if !handled {
		return false, nil
	}
	completedText := sel.Name + " "
	if compCmd != nil {
		if comp, ok := compCmd().(slashpicker.CompletedMsg); ok && comp.Text != "" {
			completedText = comp.Text
		}
	}
	m.prompt.input.SetValue(completedText)
	m.prompt.SetInputCursorOffset(len(completedText))
	m.prompt.input.CursorEnd()
	m.prompt.promptHistory.Reset()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.SetTextKeepingPending(completedText)
		m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
		if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
			m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
		}
		m.syncVimInput()
	}
	if wasTerminalMode && (m.prompt.picker.Visible() || strings.HasSuffix(completedText, " ")) {
		return true, nil
	}
	m.prompt.picker.Reset()

	cleanCmd, cmdArgs := slashparse.Split(strings.TrimSpace(completedText))
	if m.openSlashPanelArgs(cleanCmd, cmdArgs) {
		if m.prompt.vimEng != nil {
			m.prompt.input.Reset()
			m.prompt.vimEng.SetText("")
			m.syncVimInput()
		}
		syncCmd := m.syncAccountIdentity()
		if m.overlays.panel.kind == panelregistry.Usage && m.viewport.height > 0 {
			m.display.quotaFrame = 0
			return true, tea.Batch(syncCmd, usagepanel.QuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.overlays.panel.kind == panelregistry.Accounts {
			return true, tea.Batch(syncCmd, m.accountsPanel().QueueQuota())
		}
		if m.overlays.panel.kind == panelregistry.CodeSearch && m.viewport.height > 0 {
			return true, tea.Batch(syncCmd, m.codesearchPanel().Tick())
		}
		if m.overlays.panel.kind == panelregistry.Debug {
			return true, tea.Batch(syncCmd, debugpanel.Tick())
		}
		return true, syncCmd
	}
	if cleanCmd == "clear" || cleanCmd == "terminal" {
		_, cmd := m.handleSlashCommand(strings.TrimSpace(completedText))
		return true, cmd
	}
	// Enter on a command the user has already spelled out in full is a
	// submit, not a completion.
	//
	// Measured (file trace through the real TUI): typing `/learn` and
	// pressing Enter reached this function with value="/learn", selected
	// `/learn`, wrote "/learn " into the composer, and returned (true, nil)
	// — so no turn started and the command looked broken. A second Enter
	// then worked, because by then the picker was dismissed and the key went
	// down the ordinary submit path.
	//
	// Completing and stopping is right while the user is still choosing: the
	// row they picked is not what they typed (`/l` → `/learn`), or the
	// command takes an argument they have not typed yet (`/model `). It is
	// wrong when the completion is a no-op — the typed token already IS the
	// selected row's name — because then Enter added nothing and the user's
	// press is lost. Only the bare form is affected: the typed token is
	// taken up to the first space, so `/model` is bare while `/model gpt`
	// is not.
	typed := strings.TrimSpace(m.prompt.input.Value())
	if typed == sel.Name {
		m.prompt.picker.Reset()
		_, cmd := m.handleSlashCommand(sel.Name)
		return true, cmd
	}
	return true, nil
}

func (m *RootModel) handleWordMoveAndChords(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'j' && key.Mod.Contains(tea.ModCtrl) {
		m.prompt.input.InsertRune('\n')
		m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
		m.prompt.atFilePicker.Update()
		m.prompt.promptHistory.Reset()
		return m, nil, true
	}
	if key.Code == tea.KeyLeft {
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
			m.moveWord(true)
			m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
			m.prompt.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.prompt.input.Value() == "" && len(m.overlays.btwCards) > 1 {
			m.NavigateBtw(-1)
			return m, nil, true
		}
	}
	if key.Code == tea.KeyRight {
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
			m.moveWord(false)
			m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
			m.prompt.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.prompt.input.Value() == "" && len(m.overlays.btwCards) > 1 {
			m.NavigateBtw(1)
			return m, nil, true
		}
	}
	if m.hasBtwSelection() && (key.Code == 'c' || key.Code == 'C') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModAlt) {
		text := m.btwSelectionText()
		m.ResetBtwSelection()
		if text != "" {
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text), true
		}
		return m, nil, true
	}
	if m.hasPanelSelection() && (key.Code == 'c' || key.Code == 'C') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModAlt) {
		text := m.panelSelectionText()
		m.ResetPanelSelection()
		if text != "" {
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text), true
		}
		return m, nil, true
	}
	if (key.Code == 'v' || key.Code == 'V') && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModSuper)) && !key.Mod.Contains(tea.ModAlt) {
		// The paste key's image half (agy 1.4.9): an image on the
		// clipboard is attached, a dropped image file is attached, and
		// only a clipboard with neither stays text.
		m.prompt.promptHistory.Reset()
		return m, m.handlePasteContent(clipboardReadText()), true
	}
	return m, nil, false
}

func (m *RootModel) updateEditorKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
func (m *RootModel) composerSeams() composer.Seams {
	start, end := m.prompt.InputSelectionRange()
	if m.prompt.undoMgr == nil {
		m.prompt.undoMgr = composer.NewUndoManager(100)
	}
	return composer.Seams{
		Editor:           composer.NewTextArea(&m.prompt.input),
		Picker:           m.prompt.picker,
		Files:            m.prompt.atFilePicker,
		History:          &m.prompt.promptHistory,
		Selection:        composer.Range{Start: start, End: end},
		SelectAll:        m.selectAllInput,
		ClearSelection:   m.prompt.ResetInputSelection,
		DeleteSelection:  m.prompt.DeleteInputSelection,
		ReplaceSelection: m.prompt.ReplaceInputSelection,
		Copy: func(text string) tea.Cmd {
			// The clipboard write is OS I/O and stays on the host's
			// side of the seam: the composer decides that the
			// selection is copied, the host performs the write.
			_ = clipboard.Write(text)
			return m.copySelectionCmd(text)
		},
		Paste:                  m.handlePasteContent,
		ClearEligibilityNotice: m.clearEligibilityNotice,
		DismissModeHint:        func() { m.prompt.modeHintDismissed = true },
		UndoMgr:                m.prompt.undoMgr,
	}
}
