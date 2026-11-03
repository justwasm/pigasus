package cliapp

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/vim"
)

func (m *runMode) dispatchKeyAction(action string) (tea.Model, tea.Cmd, bool) {
	switch action {
	case "cli.clear_screen":
		if !m.resumePicker.Active() {
			m.scroll = 0
			return m, tea.ClearScreen, true
		}
	case "cli.exit":
		return m, tea.Quit, true
	case "edit.open_editor":
		if !m.resumePicker.Active() && m.panel.kind == panelregistry.None {
			return m, m.editPromptCmd(), true
		}
	case "view.review_artifact":
		if !m.resumePicker.Active() && m.panel.kind == panelregistry.None {
			if m.openSlashPanel("artifact") {
				return m, nil, true
			}
		}
	case "view.toggle_trajectory":
		if !m.resumePicker.Active() {
			if len(m.pendingMedia) > 0 {
				m.mediaExpanded = !m.mediaExpanded
				return m, nil, true
			}
			m.toggleTrajectoryExpanded()
			return m, tea.ClearScreen, true
		}
	case "subagent.jump_to_waiting":
		if !m.resumePicker.Active() {
			m.subAgentQuestionPanel.Toggle()
			return m, nil, true
		}
	case "voice.start_dictation":
		if !m.resumePicker.Active() && !m.voiceSurface.Active() && m.panel.kind == panelregistry.None {
			return m, m.voiceSurface.Start("keybind"), true
		}
	case "cli.cycle_mode":
		return m, m.toggleQueueMode(), true
	}
	return m, nil, false
}

func (m *runMode) handleGlobalOrModalKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.keybindings != nil {
		if act := m.keybindings.ActionForKey(key); act != "" {
			if model, cmd, handled := m.dispatchKeyAction(act); handled {
				return model, cmd, true
			}
		}
	}
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
		if r == 'j' {
			if m.activeItems.Count() > 0 {
				m.activeItems.SetFocus(!m.activeItems.Focused())
				return m, nil, true
			}
		}
	}
	if m.voiceSurface.Active() {
		if handled, cmd := m.voiceSurface.HandleKey(key); handled {
			return m, cmd, true
		}
	}
	if listPanelKind(m.panel.kind) {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if key.Code == 'l' && key.Mod.Contains(tea.ModCtrl) && !m.resumePicker.Active() {
		m.scroll = 0
		return m, tea.ClearScreen, true
	}
	if key.Code == 'o' && key.Mod.Contains(tea.ModCtrl) && !m.resumePicker.Active() {
		if m.panel.kind == panelregistry.Diff {
			// Cycle 381: the /diff panel owns the fold state, so the
			// toggle is the panel's.
			if m.diffPanel().View() != 0 {
				m.diffPanel().ToggleFoldAtCursor()
			}
			return m, nil, true
		}
		if m.panel.kind != panelregistry.None {
			return m, nil, false
		}
		if len(m.pendingMedia) > 0 {
			m.mediaExpanded = !m.mediaExpanded
			return m, nil, true
		}
		m.toggleTrajectoryExpanded()
		return m, tea.ClearScreen, true
	}
	if key.Code == 'r' && key.Mod.Contains(tea.ModCtrl) && !m.resumePicker.Active() {
		// Cycle 383 (A-22): the reference binds Ctrl+R to
		// `view.review_artifact` — it opens the Artifact Review panel
		// (official-docs/cli/reference.md:81). The clone's own /help
		// advertised the binding without implementing it.
		//
		// The binding belongs to the prompt, so an open panel keeps
		// the key: the reference's table lists Ctrl+R under the
		// prompt-focus keys.
		if m.panel.kind != panelregistry.None {
			return m, nil, false
		}
		if m.openSlashPanel("artifact") {
			return m, nil, true
		}
		return m, nil, false
	}
	if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) && !m.resumePicker.Active() {
		if m.panel.kind == panelregistry.Permissions {
			// Cycle 383 (A-30): the /permissions panel owns Ctrl+G —
			// it is the edit alias of `e`
			// (official-docs/cli/commands/permissions.md:41). The
			// host's Ctrl+G (open the prompt in $EDITOR) must not
			// swallow it first.
			if handled, cmd := m.permissionsPanel().HandleKey(key); handled {
				return m, cmd, true
			}
			// The panel is up but did not take the key (the scope
			// picker and the form do not edit a rule): the key is
			// swallowed rather than opening the editor over the
			// panel.
			return m, nil, true
		}
		return m, m.editPromptCmd(), true
	}
	if key.Code == 'k' && key.Mod.Contains(tea.ModCtrl) && !m.resumePicker.Active() {
		m.subAgentQuestionPanel.Toggle()
		return m, nil, true
	}
	return m, nil, false
}

// composerInVimInsert reports whether the composer is in the vim
// engine's INSERT mode.
//
// The engine owns the mode (cycle 216), so the host asks it: in INSERT
// mode a `?` is a character of the buffer, and in NORMAL mode it is the
// shortcuts-overlay key (official-docs/cli/vim-editor-mode.md:73).
//
// Nothing is built when vim is off — the engine must not exist then
// (cycle 381, tranche 58) — and when vim is on the engine is seeded on
// demand exactly as the key path seeds it, so a fresh engine already
// answers the question ("Insert First" decides the first mode).
func (m *runMode) composerInVimInsert() bool {
	return m.vimEnabled() && m.vimMode() == vim.Insert
}

func (m *runMode) handleFunctionAndModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	// Cycle 383 (B-22): `?` opens the shortcuts overlay whether the
	// composer holds text or not. The reference's own quick-tips table
	// says "Type `?` to get help and list all slash commands"
	// (official-docs/cli/using.md:24), and the editor-mode guide says
	// the same for the vim surface: "Press `?` in NORMAL mode to open the
	// shortcuts overlay" (official-docs/cli/vim-editor-mode.md:73).
	//
	// The one exception is the vim engine's INSERT mode, where `?` is a
	// character of the buffer. The engine owns that mode, so the host
	// asks it (composerInVimInsert) instead of guessing from the text.
	//
	// The composer keeps its text: the overlay is a view over the
	// session, and the draft the user typed is theirs.
	if key.Code == '?' && key.Mod == 0 && !m.composerInVimInsert() &&
		!m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
		m.helpPanel.Open(2) // shortcuts
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		return m, nil, true
	}
	// In vim mode the "!" belongs to the engine: the reference's
	// NORMAL-mode "!" inserts the character and switches to INSERT mode
	// (official-docs/cli/vim-editor-mode.md), so the shell command is
	// typed into the buffer and the submit path runs it. Arming the
	// bash mode here would swallow the character before the engine ever
	// saw it.
	if key.Code == '!' && key.Mod == 0 && m.input.Value() == "" && !m.store.BashMode &&
		!m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() && m.panel.kind == panelregistry.None &&
		!m.vimEnabled() {
		m.store.BashMode = true
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.store.BashMode && m.input.Value() == "" {
		m.store.BashMode = false
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.input.Value() == "" && len(m.pendingMedia) > 0 {
		m.pendingMedia = m.pendingMedia[:len(m.pendingMedia)-1]
		if len(m.pendingMedia) == 0 {
			m.mediaExpanded = false
		}
		return m, nil, true
	}
	if key.Code == tea.KeyF2 && !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() {
		m.openRenamePanel()
		return m, nil, true
	}
	if key.Code == tea.KeyF5 && !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() && !m.voiceSurface.Active() {
		return m, m.voiceSurface.Start("f5"), true
	}
	if m.panel.kind != panelregistry.None {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() && m.handleScrollKey(key) {
		return m, nil, true
	}
	return m, nil, false
}

// handleResumeModeKeys hands a key to the /resume picker.
//
// The picker owns its own state — the list, the cursor, the search query
// and the tree — so the host only routes the key and carries the picker's
// command back.
func (m *runMode) handleResumeModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !m.resumePicker.Active() {
		return m, nil, false
	}
	return m, m.resumePicker.HandleKey(key, m.height), true
}

func (m *runMode) handleEscKeyMsg() (tea.Model, tea.Cmd) {
	if m.exitConfirm {
		m.exitConfirm = false
	}

	// Cycle 383 (B-21): the reference clears the prompt box on `esc esc`
	// — "Type `esc esc` to clear your prompt box (when no streaming is
	// active)" (official-docs/cli/using.md:22). The first Esc arms the
	// clear and the second one performs it.
	//
	// An Esc that ends up somewhere else — closing an overlay, cancelling
	// a turn, dismissing a card — is never the second half of the pair,
	// so the arming is dropped here and only the composer branch
	// re-arms it. The armed value is the composer content the pair was
	// armed on, so a composer that changed in the meantime (a submit, a
	// history recall) is not the one the user armed.
	armed := m.escArmedFor
	m.escArmedFor = ""

	// Priority 1: Modals, overlays, pickers, and side-question cards
	if m.helpPanel.Active() {
		m.helpPanel.Cancel()
		return m, nil
	}
	if m.picker.Cancel() {
		return m, nil
	}
	if m.atFilePicker.Visible() {
		m.atFilePicker.Reset()
		return m, nil
	}
	if m.panel.kind != panelregistry.None {
		if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); handled {
			return m, cmd
		}
		return m, m.closePanel()
	}
	if m.resumePicker.Active() {
		return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyEsc}, m.height)
	}
	if m.mediaExpanded {
		m.mediaExpanded = false
		return m, nil
	}
	if m.isBtwCardVisible() {
		m.DismissBtwResponse()
		return m, nil
	}

	// Priority 2: Execution cancellation (in-flight bash command or streaming turn)
	if m.store.BashCancel != nil {
		m.cancelBashCommand()
		return m, nil
	}
	if m.streaming && m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
		// The reference does not report a cancelled turn as an error: it
		// answers with the composer's interrupted hint instead
		// (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`).
		m.turnInterrupted = true
		return m, nil
	}

	// Priority 3: Composer input editing state (selection, draft text)
	compaction.TriggerCancel()
	if m.hasInputSelection() {
		m.store.ResetInputSelection()
		return m, nil
	}
	if m.store.BashMode {
		m.store.BashMode = false
		if m.input.Value() == "" {
			return m, nil
		}
	}
	if m.input.Value() != "" {
		// Cycle 383 (B-21): the first Esc of the pair only arms the
		// clear; the second one empties the composer.
		if armed != m.input.Value() {
			m.escArmedFor = m.input.Value()
			return m, nil
		}
		m.input.Reset()
		m.store.ResetInputSelection()
		m.store.BashMode = false
		return m, nil
	}

	// Priority 4: Background announcement cards (dismissed on empty prompt when not streaming)
	if m.announceQueue.Active() != nil {
		m.announceQueue.Dismiss()
		if msg := m.announceQueue.SaveErrorMessage(); msg != "" {
			m.response = append(m.response, msg)
		}
		return m, nil
	}

	return m, nil
}

func (m *runMode) handleTabCompletion(key tea.Key) bool {
	if key.Code != tea.KeyTab || key.Mod.Contains(tea.ModShift) {
		return false
	}
	// The popup owns Tab: the command rows replace the buffer with the
	// command name, the "/model <query>" rows complete the argument
	// (the command stays, the display label is appended) and close the
	// popup.
	if m.picker.HandleKey(key) {
		m.promptHistory.Reset()
		return true
	}
	if m.atFilePicker.HandleKey(key) {
		m.promptHistory.Reset()
		return true
	}
	return false
}

func (m *runMode) handleCtrlAndTabKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		// agy kills a running shell command on Ctrl+C and still arms the
		// exit confirmation with the same press (【一手】: the block
		// finalises with no body while "press ctrl+c again to exit"
		// appears). The clone used to only arm the confirmation.
		if m.store.BashCancel != nil {
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
			m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
			m.clearEligibilityNotice()
			m.atFilePicker.Update()
			return m, nil, true
		}
		if m.input.Value() == "" {
			m.quitting = true
			return m, tea.Quit, true
		}
	}
	if key.Code == 'a' && key.Mod == 0 && m.input.Value() == "" &&
		!m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() &&
		m.panel.kind == panelregistry.None &&
		m.subAgentQuestionPanel.PendingCount() > 0 {
		m.subAgentQuestionPanel.Toggle()
		return m, nil, true
	}
	if m.exitConfirm {
		m.exitConfirm = false
	}
	if m.handleTabCompletion(key) {
		return m, nil, true
	}
	return m, nil, false
}

// handleHelpModeNavigation hands a key to the /help overlay.
//
// The overlay owns its own state — the tab, the cursor and the window
// — so the host only routes the key and carries the panel's answer.
func (m *runMode) handleHelpModeNavigation(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !m.helpPanel.HandleKey(key) {
		return m, nil, false
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

	// Cycle 383 (B-21): the `esc esc` pair is broken by any other key —
	// the first Esc arms the clear, and only a second Esc that follows
	// it empties the composer.
	if key.Code != tea.KeyEsc {
		m.escArmedFor = ""
	}

	if mod, cmd, handled := m.handleGlobalOrModalKeys(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleFunctionAndModeKeys(key); handled {
		return mod, cmd
	}
	if mod, cmd, handled := m.handleResumeModeKeys(key); handled {
		return mod, cmd
	}
	if handled, cmd := m.activeItems.HandleKey(key); handled {
		return m, cmd
	}
	// The question picker is modal: it claims every key while it is
	// open, so nothing reaches the vim, Esc or Ctrl handlers.
	if m.subAgentQuestionPanel.HandleKey(key) {
		return m, nil
	}
	if m.vimEnabled() && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() &&
		!m.isBtwCardVisible() && m.announceQueue.Active() == nil && !m.mediaExpanded &&
		m.panel.kind == panelregistry.None {
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
	if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) && m.isBtwCardVisible() && !m.picker.Visible() && !m.atFilePicker.Visible() {
		m.ToggleBtwExpanded()
		return m, nil
	}
	return m.updateEditorKey(msg)
}
