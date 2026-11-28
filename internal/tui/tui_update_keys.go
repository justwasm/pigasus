package tui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/internal/ui/views/wirehud"
	"github.com/justwasm/pigasus/vim"
)

// ---- tui_update_keys.go ----

func (m *RootModel) dispatchKeyAction(action string) (tea.Model, tea.Cmd, bool) {
	switch action {
	case "cli.clear_screen":
		if !m.overlays.resumePicker.Active() {
			m.viewport.scroll = 0
			return m, tea.ClearScreen, true
		}
	case "cli.exit":
		m.CloseAllTerminals()
		return m, tea.Quit, true
	case "edit.open_editor":
		if !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None {
			return m, m.editPromptCmd(), true
		}
	case "prompt.submit_alternate":
		if !m.hasActiveModal() && !m.prompt.picker.Visible() {
			return m, m.submitAlternate(), true
		}
	case "view.review_artifact":
		if !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None {
			if m.openSlashPanel("artifact") {
				return m, nil, true
			}
		}
	case "view.history":
		if !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None {
			if m.openSlashPanel("history") {
				return m, nil, true
			}
		}
	case "view.toggle_trajectory":
		if m.overlays.resumePicker.Active() {
			break
		}
		// A modal panel claims the key: the diff viewer folds, any other
		// panel ignores ctrl+o (the hardcoded path below has the same
		// rule).
		if m.overlays.panel.kind == panelregistry.Diff {
			if m.diffPanel().View() != 0 {
				m.diffPanel().ToggleFoldAtCursor()
			}
			return m, nil, true
		}
		if m.overlays.panel.kind == panelregistry.Agents && m.agentsPanel().DetailMode() {
			m.agentsPanel().ToggleExpandedDetail()
			return m, nil, true
		}
		if m.overlays.panel.kind == panelregistry.Tasks && m.tasksPanel().DetailSubagent() {
			m.tasksPanel().ToggleExpandedDetail()
			return m, nil, true
		}
		if m.overlays.panel.kind != panelregistry.None {
			break
		}
		if len(m.prompt.pendingMedia) > 0 {
			m.prompt.mediaExpanded = !m.prompt.mediaExpanded
			return m, nil, true
		}
		m.toggleTrajectoryExpanded()
		return m, tea.ClearScreen, true
	case "subagent.approve_fast":
		if !m.overlays.resumePicker.Active() {
			if m.askQuestion.panel != nil && m.askQuestion.panel.Active() {
				m.askQuestion.panel.Toggle()
				return m, nil, true
			}
			if m.fastApproveSubagent() {
				return m, nil, true
			}
			if m.askQuestion.manager != nil && m.askQuestion.manager.TotalPending() > 0 {
				waiting := m.askQuestion.manager.PendingSubagents()
				if len(waiting) > 0 && m.askQuestion.panel != nil {
					m.askQuestion.panel.SetScope(waiting[0])
					m.askQuestion.panel.Open()
					return m, nil, true
				}
			}
			return m, nil, true
		}
	case "subagent.jump_to_waiting":
		if !m.overlays.resumePicker.Active() {
			if m.askQuestion.manager != nil && m.askQuestion.manager.TotalPending() > 0 {
				waiting := m.askQuestion.manager.PendingSubagents()
				if len(waiting) > 0 {
					nextScope := waiting[0]
					if m.askQuestion.panel != nil && m.askQuestion.panel.Active() {
						currentScope := m.askQuestion.panel.Scope()
						if len(waiting) == 1 {
							m.askQuestion.panel.Toggle()
							return m, nil, true
						}
						for i, s := range waiting {
							if s == currentScope {
								nextScope = waiting[(i+1)%len(waiting)]
								break
							}
						}
					}
					if m.askQuestion.panel != nil {
						m.askQuestion.panel.SetScope(nextScope)
						m.askQuestion.panel.Open()
						return m, nil, true
					}
				}
			}
			if m.activeItems.Panel != nil && m.activeItems.Panel.Count() > 0 && !m.askQuestion.panel.Active() {
				m.activeItems.Panel.SetFocus(!m.activeItems.Panel.Focused())
				return m, nil, true
			}
			if m.askQuestion.panel != nil {
				m.askQuestion.panel.Toggle()
				return m, nil, true
			}
		}
	case "voice.start_dictation":
		if !m.overlays.resumePicker.Active() && !m.audio.Surface.Active() && m.overlays.panel.kind == panelregistry.None {
			return m, m.audio.Surface.Start("keybind"), true
		}
	case "cli.cycle_mode":
		if !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.cycleMode()
			return m, nil, true
		}
	case "item.rename":
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyF2}, m.viewport.height), true
		}
		if !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.openRenamePanel()
			return m, nil, true
		}
	case "item.delete":
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyF4}, m.viewport.height), true
		}
	case "navigation.go_to_top", "navigation.top":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyHome}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavTop, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.gotoTop()
			return m, nil, true
		}
	case "navigation.go_to_bottom", "navigation.bottom":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyEnd}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavBottom, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.gotoBottom()
			return m, nil, true
		}
	case "navigation.page_up":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyPgUp}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavPageUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.scrollPage(false)
			return m, nil, true
		}
	case "navigation.page_down":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyPgDown}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavPageDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.scrollPage(true)
			return m, nil, true
		}
	case "navigation.half_page_up":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyPgUp}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavHalfPageUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.scrollBy(-tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.half_page_down":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyPgDown}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavHalfPageDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.hasActiveModal() && !m.prompt.picker.Visible() {
			m.scrollBy(tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.up":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyUp}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyUp}, m.viewport.height), true
		}
	case "navigation.down":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyDown}, m.viewport.height), true
		}
	case "navigation.left":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyLeft}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavLeft, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyLeft}, m.viewport.height), true
		}
	case "navigation.right":
		if m.overlays.panel.kind != panelregistry.None {
			if handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyRight}); handled {
				return m, cmd, true
			}
		}
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavRight, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyRight}, m.viewport.height), true
		}
	case "confirm.yes":
		if m.hasActivePermissionPrompt() {
			_, handled := m.handlePermissionKey(tea.Key{Code: 'y', Text: "y"})
			if handled {
				return m, nil, true
			}
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: 'y', Text: "y"}, m.viewport.height), true
		}
		if m.display.exitConfirm {
			m.CloseAllTerminals()
			return m, tea.Quit, true
		}
	case "confirm.no":
		if m.hasActivePermissionPrompt() {
			_, handled := m.handlePermissionKey(tea.Key{Code: 'n', Text: "n"})
			if handled {
				return m, nil, true
			}
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: 'n', Text: "n"}, m.viewport.height), true
		}
		if m.display.exitConfirm {
			m.display.exitConfirm = false
			return m, nil, true
		}
	case "cli.suspend":
		return m, tea.Suspend, true
	case "cli.escape":
		if m.vimEnabled() && !m.hasActiveModal() && !m.prompt.picker.Visible() && !m.prompt.atFilePicker.Visible() {
			return m, nil, false
		}
		if !m.conversation.streaming && !m.session.store.BashRunning() && !m.hasActiveModal() && !m.prompt.picker.Visible() && !m.prompt.atFilePicker.Visible() && !m.prompt.mediaExpanded {
			if m.display.exitConfirm {
				m.display.exitConfirm = false
				return m, nil, true
			}
		}
		model, cmd := m.handleEscKeyMsg()
		return model, cmd, true
	case "cli.enter":
		if m.vimEnabled() && !m.hasActiveModal() && !m.prompt.picker.Visible() && !m.prompt.atFilePicker.Visible() {
			return m, nil, false
		}
		if !m.hasActiveModal() && !m.prompt.picker.Visible() {
			return m.handleEnterKey(tea.Key{Code: tea.KeyEnter})
		}
	case "prompt.insert_newline":
		if !m.hasActiveModal() {
			if m.vimEnabled() && m.prompt.vimEng != nil && m.prompt.vimEng.Mode == vim.Insert {
				m.prompt.vimEng.InsertNewline()
				m.syncVimInput()
			} else {
				m.prompt.input.InsertRune('\n')
			}
			m.prompt.modeHintDismissed = true
			m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
			m.prompt.atFilePicker.Update()
			m.prompt.promptHistory.Reset()
			return m, nil, true
		}
	case "edit.paste":
		if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
			return m, nil, true
		}
		m.prompt.promptHistory.Reset()
		return m, m.handlePasteContent(clipboardReadText()), true
	case "edit.undo":
		if m.prompt.undoMgr != nil {
			if snap, ok := m.prompt.undoMgr.Undo(m.prompt.input.Value(), tuilib.InputCursorByteOffset(m.prompt.input)); ok {
				m.ResetInputSelection()
				m.prompt.input.SetValue(snap.Value)
				m.prompt.SetInputCursorOffset(snap.CursorOffset)
				m.prompt.modeHintDismissed = true
				m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
				m.clearEligibilityNotice()
				m.prompt.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.redo":
		if m.prompt.undoMgr != nil {
			if snap, ok := m.prompt.undoMgr.Redo(m.prompt.input.Value(), tuilib.InputCursorByteOffset(m.prompt.input)); ok {
				m.ResetInputSelection()
				m.prompt.input.SetValue(snap.Value)
				m.prompt.SetInputCursorOffset(snap.CursorOffset)
				m.prompt.modeHintDismissed = true
				m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
				m.clearEligibilityNotice()
				m.prompt.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.yank":
		m.prompt.promptHistory.Reset()
		text := clipboardReadText()
		if text != "" {
			return m, m.insertPastedText(text), true
		}
		return m, nil, true
	case "navigation.tab":
		if m.handleTabCompletion(tea.Key{Code: tea.KeyTab}) {
			return m, nil, true
		}
		if m.isBtwCardVisible() && !m.prompt.picker.Visible() && !m.prompt.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil, true
		}
	case "vim.insert.submit", "vim.normal.submit":
		if m.vimEnabled() {
			m.syncVimInput()
			cmd := m.submitPrompt()
			m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
			m.syncVimInput()
			return m, cmd, true
		}
	case "vim.insert.insert_newline":
		if m.vimEnabled() && m.prompt.vimEng != nil && m.prompt.vimEng.Mode == vim.Insert {
			m.prompt.vimEng.InsertNewline()
			m.syncVimInput()
			return m, nil, true
		}
	case "cli.test":
		return m, nil, true
	case "swarm.board":
		if !m.hasActiveSwarm() {
			return m, nil, true
		}
		if m.swarm.Board == nil {
			m.swarm.Board = m.newSwarmBoard()
		}
		m.swarm.Board.Toggle()
		return m, nil, true
	case "swarm.dismiss":
		m.DismissSwarm()
		return m, nil, true
	}
	return m, nil, false
}

func (m *RootModel) fastApproveSubagent() bool {
	if m.tasks.subagentManager != nil {
		for _, sa := range m.runningSubagents() {
			if sa == nil {
				continue
			}
			if appr, ok := m.pendingSubagentAuthorization(sa.ID); ok {
				if m.answerSubagentAuthorization(appr.ID, true) {
					return true
				}
			}
		}
	}
	if m.askQuestion.manager != nil {
		all := m.askQuestion.manager.AllPending()
		for scope, qs := range all {
			if len(qs) > 0 {
				q := qs[0]
				if len(q.Options) == 0 {
					continue
				}
				ans := q.Options[0]
				if err := m.askQuestion.manager.Answer(scope, q.ID, ans); err == nil {
					label := scope
					if label == subagentquestions.ParentKey {
						label = "you"
					}
					m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines("", "  ⎿  Approved action for "+label)...)
					return true
				}
			}
		}
	}
	return false
}

func (m *RootModel) handleGlobalOrModalKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.hasPanelSelection() && (key.Code == 'c' || key.Code == 'C') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModAlt) {
		text := m.panelSelectionText()
		m.ResetPanelSelection()
		if text != "" {
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text), true
		}
		return m, nil, true
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
	if m.overlays.panel.kind == panelregistry.Terminal {
		if mod, cmd, handled := m.handleTerminalPanelShortcuts(key); handled {
			return mod, cmd, true
		}
		handled, cmd := m.handlePanelKey(key)
		return m, cmd, handled
	}
	if m.prompt.keybindings != nil {
		if act := m.prompt.keybindings.ActionForKey(key); act != "" {
			if act == "cli.escape" && key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
				if m.session.store.BashRunning() {
					// agy kills a running shell command on Ctrl+C and
					// still arms the two-step exit confirmation with
					// the same press (【一手】 agy 1.4.5).
					m.cancelBashCommand()
					if m.display.exitConfirm {
						m.display.quitting = true
						m.CloseAllTerminals()
						return m, tea.Quit, true
					}
					m.display.exitConfirm = true
					m.display.exitConfirmAt = time.Now()
					return m, nil, true
				}
				if m.conversation.streaming && m.turn.activeBridge != nil && m.turn.activeBridge.IsRunning() {
					model, cmd := m.handleEscKeyMsg()
					return model, cmd, true
				}
				if m.display.exitConfirm {
					m.display.quitting = true
					m.CloseAllTerminals()
					return m, tea.Quit, true
				}
				m.display.exitConfirm = true
				m.display.exitConfirmAt = time.Now()
				return m, nil, true
			}
			// Ctrl+D is Emacs forward-delete while the prompt has text
			// (official-docs/changelog.md: "act as a forward-delete when
			// the input prompt contains text, only triggering the exit
			// flow when the prompt is empty"). The default keybindings
			// bind ctrl+d to cli.exit, so the action must not fire until
			// the composer is empty; the key falls through to the
			// editor (or the vim engine) instead.
			if act == "cli.exit" && key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) &&
				(m.prompt.input.Value() != "" || m.prompt.HasInputSelection()) {
				// fall through
			} else if model, cmd, handled := m.dispatchKeyAction(act); handled {
				return model, cmd, true
			}
		}
	}
	if r, ok := keybind.IsAltLetter(key); ok {
		if m.prompt.keybindings != nil {
			if action, ok := m.prompt.keybindings.LookupAltLetter(r); ok {
				m.altDispatchAction(r, action)
				return m, nil, true
			}
		}
		if r == 'm' {
			return m, m.submitAlternate(), true
		}
		if r == 'j' {
			if m.activeItems.Panel.Count() > 0 {
				m.activeItems.Panel.SetFocus(!m.activeItems.Panel.Focused())
				return m, nil, true
			}
		}
		if r == 's' {
			if !m.hasActiveSwarm() {
				return m, nil, true
			}
			if m.swarm.Board == nil {
				m.swarm.Board = m.newSwarmBoard()
			}
			m.swarm.Board.Toggle()
			return m, nil, true
		}
		if r == 'x' {
			m.DismissSwarm()
			return m, nil, true
		}
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		if key.Code == tea.KeyEsc {
			m.swarm.Board.Close()
			return m, nil, true
		}
		handled, cmd := m.swarm.Board.HandleKey(key)
		if handled {
			return m, cmd, true
		}
		return m, nil, true
	}
	if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
		if key.Code == tea.KeyEsc {
			m.agents.battle.Panel.Close()
			return m, nil, true
		}
		handled, cmd := m.agents.battle.Panel.HandleKey(key)
		if handled {
			return m, cmd, true
		}
		return m, nil, true
	}
	if m.audio.Surface.Active() {
		if handled, cmd := m.audio.Surface.HandleKey(key); handled {
			return m, cmd, true
		}
	}
	if listPanelKind(m.overlays.panel.kind) {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if key.Code == 'l' && key.Mod.Contains(tea.ModCtrl) && !m.overlays.resumePicker.Active() {
		m.viewport.scroll = 0
		return m, tea.ClearScreen, true
	}
	if ((key.Code == 'o' || key.Code == 'O') && key.Mod.Contains(tea.ModCtrl) || key.String() == "ctrl+o") && !m.overlays.resumePicker.Active() {
		if m.overlays.panel.kind == panelregistry.Diff {
			// Cycle 381: the /diff panel owns the fold state, so the
			// toggle is the panel's.
			if m.diffPanel().View() != 0 {
				m.diffPanel().ToggleFoldAtCursor()
			}
			return m, nil, true
		}
		if m.overlays.panel.kind == panelregistry.Agents && m.agentsPanel().DetailMode() {
			m.agentsPanel().ToggleExpandedDetail()
			return m, nil, true
		}
		if m.overlays.panel.kind == panelregistry.Tasks && m.tasksPanel().DetailSubagent() {
			m.tasksPanel().ToggleExpandedDetail()
			return m, nil, true
		}
		if m.overlays.panel.kind != panelregistry.None {
			return m, nil, false
		}
		if len(m.prompt.pendingMedia) > 0 {
			m.prompt.mediaExpanded = !m.prompt.mediaExpanded
			return m, nil, true
		}
		m.toggleTrajectoryExpanded()
		return m, tea.ClearScreen, true
	}
	if key.Code == 'r' && key.Mod.Contains(tea.ModCtrl) && !m.overlays.resumePicker.Active() {
		// Cycle 383 (A-22): the reference binds Ctrl+R to
		// `view.review_artifact` — it opens the Artifact Review panel
		// (official-docs/cli/reference.md:81). The clone's own /help
		// advertised the binding without implementing it.
		//
		// The binding belongs to the prompt, so an open panel keeps
		// the key: the reference's table lists Ctrl+R under the
		// prompt-focus keys.
		if m.overlays.panel.kind != panelregistry.None {
			return m, nil, false
		}
		if m.openSlashPanel("artifact") {
			return m, nil, true
		}
		return m, nil, false
	}
	if key.Code == 'h' && (key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModAlt)) && !m.overlays.resumePicker.Active() {
		if m.overlays.panel.kind != panelregistry.None {
			return m, nil, false
		}
		if m.openSlashPanel("history") {
			return m, nil, true
		}
		return m, nil, false
	}
	if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) && !m.overlays.resumePicker.Active() {
		if m.overlays.panel.kind == panelregistry.Permissions {
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
	if key.Code == 'k' && key.Mod.Contains(tea.ModCtrl) && !m.overlays.resumePicker.Active() {
		if m.askQuestion.panel != nil {
			m.askQuestion.panel.Toggle()
			return m, nil, true
		}
		return m, nil, false
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
func (m *RootModel) composerInVimInsert() bool {
	return m.vimEnabled() && m.vimMode() == vim.Insert
}

func (m *RootModel) handleFunctionAndModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	// '?' opens the shortcuts overlay only when the composer is empty.
	// As documented in the release notes and reference guide:
	// "Fixed `?` in Vim Normal mode opening the shortcuts panel while the
	// prompt already contained text... `?` still opens shortcuts on an empty prompt."
	//
	// When text has already been typed in the composer, '?' is a regular
	// character (or a vim search/motion argument in normal mode) and must
	// be handled by the editor, not intercepted by the shortcuts overlay.
	if key.Code == '?' && key.Mod == 0 && m.prompt.input.Value() == "" && !m.composerInVimInsert() &&
		!m.hasActiveModal() && !m.prompt.picker.Visible() {
		m.openHelp(2) // shortcuts
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		return m, nil, true
	}
	// In vim mode the "!" belongs to the engine: the reference's
	// NORMAL-mode "!" inserts the character and switches to INSERT mode
	// (official-docs/cli/vim-editor-mode.md), so the shell command is
	// typed into the buffer and the submit path runs it. Arming the
	// bash mode here would swallow the character before the engine ever
	// saw it.
	if key.Code == '!' && key.Mod == 0 && m.prompt.input.Value() == "" && !m.session.store.BashMode &&
		!m.hasActiveModal() && !m.prompt.picker.Visible() &&
		!m.vimEnabled() {
		m.session.store.BashMode = true
		if m.prompt.undoMgr != nil {
			m.prompt.undoMgr.Clear()
		}
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.session.store.BashMode && m.prompt.input.Value() == "" {
		m.session.store.BashMode = false
		if m.prompt.undoMgr != nil {
			m.prompt.undoMgr.Clear()
		}
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.prompt.input.Value() == "" && len(m.prompt.pendingMedia) > 0 {
		m.prompt.pendingMedia = m.prompt.pendingMedia[:len(m.prompt.pendingMedia)-1]
		if len(m.prompt.pendingMedia) == 0 {
			m.prompt.mediaExpanded = false
		}
		return m, nil, true
	}
	if key.Code == tea.KeyF2 && !m.hasActiveModal() && !m.prompt.picker.Visible() {
		m.openRenamePanel()
		return m, nil, true
	}
	if key.Code == tea.KeyF5 && !m.hasActiveModal() && !m.prompt.picker.Visible() && !m.audio.Surface.Active() {
		return m, m.audio.Surface.Start("f5"), true
	}
	if m.overlays.panel.kind != panelregistry.None {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if !m.hasActiveModal() && !m.prompt.picker.Visible() && m.handleScrollKey(key) {
		return m, nil, true
	}
	return m, nil, false
}

// handleResumeModeKeys hands a key to the /resume picker.
//
// The picker owns its own state — the list, the cursor, the search query
// and the tree — so the host only routes the key and carries the picker's
// command back.
func (m *RootModel) handleResumeModeKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if !m.overlays.resumePicker.Active() {
		return m, nil, false
	}
	return m, m.overlays.resumePicker.HandleKey(key, m.viewport.height), true
}

func (m *RootModel) handleEscKeyMsg() (tea.Model, tea.Cmd) {
	if m.display.exitConfirm {
		m.display.exitConfirm = false
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
	armed := m.prompt.escArmedFor
	m.prompt.escArmedFor = ""

	// Priority 1: Modals, overlays, pickers, and side-question cards
	if m.help.Panel.Active() {
		m.CloseHelp()
		return m, nil
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		m.swarm.Board.Close()
		return m, nil
	}
	if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
		m.agents.battle.Panel.Close()
		return m, nil
	}
	if m.prompt.DismissPopups() {
		return m, nil
	}
	if m.auth.DismissMenu() {
		return m, nil
	}
	if cmd, handled := m.overlays.Dismiss(m); handled {
		if m.prompt.vimEng != nil {
			m.prompt.vimEng.Mode = vim.Normal
		}
		return m, cmd
	}
	if m.prompt.CollapseMedia() {
		return m, nil
	}

	// Priority 2a: a running compaction. The reference's live row says
	// "esc to cancel", so Esc has to mean it; the summarizer's own cancel
	// channel is what the row's hint promises.
	if !m.session.store.CompactStart.IsZero() && m.session.store.CompactEnd.IsZero() {
		compaction.TriggerCancel()
		return m, nil
	}

	// Priority 2: Execution cancellation (in-flight bash command or streaming turn)
	if m.session.store.BashRunning() {
		m.cancelBashCommand()
		return m, nil
	}
	if m.conversation.streaming {
		if m.conversation.turnInterrupted {
			// AGY 1.5.14: Pressing Esc to cancel a turn while it is still tearing down
			// keeps the Interrupted status hint visible and does not clear it or wipe the composer.
			return m, nil
		}
		if m.turn.activeBridge != nil && m.turn.activeBridge.IsRunning() {
			m.cancelActiveTurn()
			return m, nil
		}
	}

	// Priority 3: Composer input editing state (selection, draft text)
	compaction.TriggerCancel()
	if m.hasBtwSelection() {
		m.ResetBtwSelection()
		return m, nil
	}
	if m.prompt.HasInputSelection() {
		m.ResetInputSelection()
		return m, nil
	}
	if m.session.store.BashMode {
		m.session.store.BashMode = false
		if m.prompt.input.Value() == "" {
			return m, nil
		}
	}
	if m.prompt.input.Value() != "" {
		// Cycle 383 (B-21): the first Esc of the pair only arms the
		// clear; the second one empties the composer.
		if armed != m.prompt.input.Value() {
			m.prompt.escArmedFor = m.prompt.input.Value()
			return m, nil
		}
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.session.store.BashMode = false
		m.prompt.pastedBlocks = nil
		m.prompt.nextPasteID = 0
		return m, nil
	}

	// Priority 3.5: Empty prompt Esc in Live mode hangs up the voice session (agy parity).
	if m.audio.Surface != nil && m.audio.Surface.Active() && m.audio.Surface.Phase() == voice.PhaseLive {
		return m, m.audio.Surface.Stop()
	}

	// Priority 4: Background announcement cards (dismissed on empty prompt when not streaming)
	if m.overlays.announceQueue.Active() != nil {
		m.overlays.announceQueue.Dismiss()
		if msg := m.overlays.announceQueue.SaveErrorMessage(); msg != "" {
			m.conversation.response = append(m.conversation.response, msg)
		}
		return m, nil
	}

	return m, nil
}

func (m *RootModel) handleTabCompletion(key tea.Key) bool {
	if key.Code != tea.KeyTab || key.Mod.Contains(tea.ModShift) {
		return false
	}
	// The popup owns Tab: the command rows replace the buffer with the
	// command name, the "/model <query>" rows complete the argument
	// (the command stays, the display label is appended) and close the
	// popup.
	if handled, cmd := m.prompt.picker.UpdateKey(key); handled {
		m.prompt.promptHistory.Reset()
		if cmd != nil {
			msg := cmd()
			if comp, ok := msg.(slashpicker.CompletedMsg); ok {
				m.prompt.input.SetValue(comp.Text)
				m.prompt.SetInputCursorOffset(len(comp.Text))
				m.prompt.input.CursorEnd()
				if !m.prompt.picker.Visible() {
					m.prompt.picker.Reset()
				}
				if m.prompt.vimEng != nil {
					m.prompt.vimEng.SetTextKeepingPending(comp.Text)
					m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
					if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
						m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
					}
					m.syncVimInput()
				}
			}
		}
		return true
	}
	if m.prompt.atFilePicker.HandleKey(key) {
		m.prompt.promptHistory.Reset()
		return true
	}
	// Accept argument ghost text when present (e.g. /swarm -> /swarm board, /copy -> /copy btw).
	val := m.prompt.input.Value()
	if strings.HasPrefix(val, "/swarm") {
		rest := strings.TrimPrefix(val, "/swarm")
		if rest == "" {
			m.prompt.input.SetValue("/swarm ")
			m.prompt.SetInputCursorOffset(len("/swarm "))
			m.prompt.input.CursorEnd()
			m.syncTabVimInput()
			return true
		}
		if rest == " " {
			m.prompt.input.SetValue("/swarm board")
			m.prompt.SetInputCursorOffset(len("/swarm board"))
			m.prompt.input.CursorEnd()
			m.syncTabVimInput()
			return true
		}
		trimmedRest := strings.TrimPrefix(rest, " ")
		if strings.HasPrefix("board", trimmedRest) && trimmedRest != "board" {
			m.prompt.input.SetValue("/swarm board")
			m.prompt.SetInputCursorOffset(len("/swarm board"))
			m.prompt.input.CursorEnd()
			m.syncTabVimInput()
			return true
		}
	}
	if strings.HasPrefix(val, "/copy") {
		rest := strings.TrimPrefix(val, "/copy")
		if rest == " " {
			m.prompt.input.SetValue("/copy btw")
			m.prompt.SetInputCursorOffset(len("/copy btw"))
			m.prompt.input.CursorEnd()
			m.syncTabVimInput()
			return true
		}
		trimmedRest := strings.TrimPrefix(rest, " ")
		if strings.HasPrefix("btw", trimmedRest) && trimmedRest != "btw" {
			m.prompt.input.SetValue("/copy btw")
			m.prompt.SetInputCursorOffset(len("/copy btw"))
			m.prompt.input.CursorEnd()
			m.syncTabVimInput()
			return true
		}
	}
	// /yolo takes an on|off argument; Tab completes the word the user is
	// typing ("/yolo " → nothing yet, "/yolo o" → "/yolo on").
	if completed, ok := completeWordArg(val, "/yolo", []string{"on", "off"}); ok {
		m.prompt.input.SetValue(completed)
		m.prompt.SetInputCursorOffset(len(completed))
		m.prompt.input.CursorEnd()
		m.syncTabVimInput()
		return true
	}
	return false
}

// completeWordArg completes the first word of a command's argument
// against a closed set of choices.
//
// It serves the commands whose argument is one of a few words (/yolo
// on|off, and the on|off style commands that follow). The open-ended
// ones (/model's catalogue rows, /voice's modes) keep their own paths.
//
//	value "/yolo"    → ("/yolo ", true)         reveal the argument
//	value "/yolo "   → ("", false)              nothing typed yet
//	value "/yolo o"  → ("/yolo on", true)       unique prefix
//	value "/yolo on" → ("", false)              already complete
func completeWordArg(value, cmd string, choices []string) (string, bool) {
	rest, ok := strings.CutPrefix(value, cmd)
	if !ok {
		return "", false
	}
	if rest == "" {
		return cmd + " ", true
	}
	if !strings.HasPrefix(rest, " ") {
		return "", false
	}
	typed := strings.TrimPrefix(rest, " ")
	if typed == "" || strings.Contains(typed, " ") {
		return "", false
	}
	for _, choice := range choices {
		if choice == typed {
			return "", false
		}
		if strings.HasPrefix(choice, typed) {
			return cmd + " " + choice, true
		}
	}
	return "", false
}

func (m *RootModel) syncTabVimInput() {
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
		m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
		if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
			m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
		}
		m.syncVimInput()
	}
}

func (m *RootModel) handleCtrlAndTabKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	// Ctrl+X cuts the composer selection: the text goes to the clipboard
	// at the moment of the cut (and nowhere else), and the range leaves
	// the value. agy 1.5.17: "`Ctrl+X` cut without overwriting your
	// clipboard on release".
	//
	// A vim Normal/Visual engine owns "x" (it is a delete operator
	// there), so the host only cuts when the engine is absent or in
	// Insert mode.
	if key.Code == 'x' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) &&
		m.prompt.HasInputSelection() && !m.hasActiveModal() &&
		m.overlays.panel.kind != panelregistry.Terminal &&
		(!m.vimEnabled() || m.vimMode() == vim.Insert) {
		if text, ok := m.prompt.CutInputSelection(); ok {
			m.display.exitConfirm = false
			m.prompt.modeHintDismissed = true
			m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
			m.clearEligibilityNotice()
			m.prompt.atFilePicker.Update()
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text), true
		}
		return m, nil, true
	}
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		// agy kills a running shell command on Ctrl+C and still arms the
		// exit confirmation with the same press (【一手】: the block
		// finalises with no body while "press ctrl+c again to exit"
		// appears). The clone used to only arm the confirmation.
		if m.session.store.BashRunning() {
			m.cancelBashCommand()
		}
		if m.display.exitConfirm {
			m.display.quitting = true
			m.CloseAllTerminals()
			return m, tea.Quit, true
		}
		m.display.exitConfirm = true
		m.display.exitConfirmAt = time.Now()
		return m, nil, true
	}
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) {
		if m.prompt.HasInputSelection() {
			m.prompt.DeleteInputSelection()
			m.prompt.modeHintDismissed = true
			m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
			m.clearEligibilityNotice()
			m.prompt.atFilePicker.Update()
			return m, nil, true
		}
		if m.prompt.input.Value() == "" {
			m.display.quitting = true
			m.CloseAllTerminals()
			return m, tea.Quit, true
		}
	}
	if key.Code == 'a' && key.Mod == 0 && m.prompt.input.Value() == "" &&
		!m.hasActiveModal() && !m.prompt.picker.Visible() &&
		m.askQuestion.manager != nil && m.askQuestion.manager.TotalPending() > 0 {
		waiting := m.askQuestion.manager.PendingSubagents()
		if len(waiting) > 0 && m.askQuestion.panel != nil {
			m.askQuestion.panel.SetScope(waiting[0])
			m.askQuestion.panel.Toggle()
			return m, nil, true
		}
	}
	if m.display.exitConfirm {
		m.display.exitConfirm = false
	}
	if m.handleTabCompletion(key) {
		return m, nil, true
	}
	return m, nil, false
}

// handleHelpModeNavigation hands a key to the /help overlay.
//
// The overlay owns its own state — the tab, the cursor and the window
// — and emits typed messages (CommandSelectedMsg, DismissedMsg) back to the host.
func (m *RootModel) handleHelpModeNavigation(key tea.Key) (tea.Model, tea.Cmd, bool) {
	handled, cmd := m.help.Panel.Update(key)
	if !handled {
		return m, nil, false
	}
	if !m.help.Panel.Active() && m.overlays.panel.kind == panelregistry.Help {
		m.overlays.panel = slashPanel{}
	}
	if cmd != nil {
		msg := cmd()
		switch ev := msg.(type) {
		case helppanel.CommandSelectedMsg:
			m.SelectCommand(ev.Command)
			if m.overlays.panel.kind == panelregistry.Help {
				m.overlays.panel = slashPanel{}
			}
			return m, nil, true
		case helppanel.DismissedMsg:
			if ev.EchoLine {
				m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines("/help", "Exited /help command")...)
			}
			if m.overlays.panel.kind == panelregistry.Help {
				m.overlays.panel = slashPanel{}
			}
			return m, nil, true
		}
	}
	return m, nil, true
}

func (m *RootModel) updateKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if translatedKey, ok := keybind.TranslateMacOptionToAlt(msg.Key()); ok {
		msg = tea.KeyPressMsg(translatedKey)
	}
	newMsg, consumed, escCmd := m.consumeAltEsc(msg)
	if consumed {
		return m, escCmd
	}
	msg = newMsg
	key := msg.Key()

	// A permission modal is modal: it claims every key while it is up, so
	// Esc resolves it instead of cancelling the turn, and a stray keystroke
	// cannot edit the composer behind it.
	if m.hasActivePermissionPrompt() {
		m.handlePermissionKey(key)
		return m, nil
	}

	if m.askQuestion.Active() {
		if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
			m.askQuestion.Cancel()
			return m, nil
		}
		cmd := m.askQuestion.Update(msg)
		return m, cmd
	}

	// The question picker is modal too, and it must be checked before the
	// global key handlers: otherwise `esc` runs cli.escape and cancels the
	// turn instead of closing the picker (the same rule the harness states
	// for every overlay).
	if m.askQuestion.panel != nil {
		if handled, cmd := m.askQuestion.panel.HandleKeyMsg(key); handled {
			return m, cmd
		}
	}

	// Global exit: Ctrl+D on empty input exits immediately (unless terminal modal owns Ctrl+D).
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) && m.prompt.input.Value() == "" && m.overlays.panel.kind != panelregistry.Terminal {
		m.display.quitting = true
		m.CloseAllTerminals()
		return m, tea.Quit
	}

	// Wire diagnostics toggle: Ctrl+Shift+W or Alt+W (Alt+W closes terminal session when terminal panel is open).
	if (key.Code == 'w' || key.Code == 'W') && ((key.Mod.Contains(tea.ModCtrl) && key.Mod.Contains(tea.ModShift)) || (key.Mod.Contains(tea.ModAlt) && m.overlays.panel.kind != panelregistry.Terminal)) {
		if m.overlays.wireHUD == nil {
			m.overlays.wireHUD = wirehud.New()
		}
		m.overlays.wireHUD.Toggle()
		return m, nil
	}

	// Cycle 383 (B-21): the `esc esc` pair is broken by any other key —
	// the first Esc arms the clear, and only a second Esc that follows
	// it empties the composer.
	if key.Code != tea.KeyEsc {
		m.prompt.escArmedFor = ""
	}

	// Reset rapid enter burst counter if any key other than Enter is pressed.
	if key.Code != tea.KeyEnter {
		m.prompt.enterBurstCount = 0
	}

	// A composer selection owns Ctrl+C: it copies the selected text and
	// the press is not the exit chord.
	//
	// agy 1.5.17 gives the prompt box's mouse selection three keys —
	// "`Ctrl+C` copy, and `Ctrl+X` cut without overwriting your clipboard
	// on release". Before this, Ctrl+C always ran cli.escape and armed
	// the two-step exit even with text highlighted, so a highlighted
	// range could not be copied at all.
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) &&
		m.prompt.HasInputSelection() && !m.hasActiveModal() &&
		m.overlays.panel.kind != panelregistry.Terminal {
		if text, ok := m.prompt.CopyInputSelection(); ok {
			m.display.exitConfirm = false
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text)
		}
		m.ResetInputSelection()
		return m, nil
	}

	// The active-items block owns the keyboard while it has the focus:
	// ↑/↓ move the cursor, Enter opens the selected row's detail view in
	// /tasks, Esc gives the focus back to the prompt, and any other key
	// also gives it back before the key is dispatched normally
	// (agy ActiveItemsModel.Update). This runs before the keybinding
	// dispatch on purpose — otherwise cli.enter submits the prompt and
	// the block never sees the Enter that opens the detail view.
	if m.overlays.panel.kind != panelregistry.Terminal {
		if handled, cmd := m.activeItems.Panel.HandleKey(key); handled {
			return m, cmd
		}
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
	// Global terminal exit on empty prompt.
	if m.vimEnabled() && !m.help.Panel.Active() && !m.overlays.resumePicker.Active() &&
		!m.isBtwCardVisible() && m.overlays.announceQueue.Active() == nil && !m.prompt.mediaExpanded &&
		m.overlays.panel.kind == panelregistry.None {
		// When picker (slash or @file) is visible, navigation/completion/dismiss keys belong to the picker.
		if m.prompt.picker.Visible() || m.prompt.atFilePicker.Visible() {
			switch key.Code {
			case tea.KeyUp, tea.KeyDown:
				if m.prompt.picker.HandleKey(key) || m.prompt.atFilePicker.HandleKey(key) {
					return m, nil
				}
			case tea.KeyTab:
				if !key.Mod.Contains(tea.ModShift) {
					if m.handleTabCompletion(key) {
						return m, nil
					}
				}
			case tea.KeyEsc:
				if m.prompt.DismissPopups() {
					return m, nil
				}
			case tea.KeyEnter:
				if m.prompt.picker.Visible() {
					if handled, cmd := m.handleSlashPickerEnter(); handled {
						return m, cmd
					}
				}
				m.syncVimInput()
				cmd := m.submitPrompt()
				if m.prompt.vimEng != nil {
					m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
				}
				m.syncVimInput()
				return m, cmd
			}
		}

		if cmd, handled := m.handleVimKey(key); handled {
			m.display.exitConfirm = false
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
	if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) {
		if m.isBtwCardVisible() && !m.prompt.picker.Visible() && !m.prompt.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil
		}
	}
	return m.updateEditorKey(msg)
}
