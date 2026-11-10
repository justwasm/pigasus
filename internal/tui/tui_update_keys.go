package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/core/settings"
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"
	"github.com/justwasm/pigasus/internal/ui/views/adddirpanel"
	"github.com/justwasm/pigasus/internal/ui/views/agentspanel"
	"github.com/justwasm/pigasus/internal/ui/views/codesearchpanel"
	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	"github.com/justwasm/pigasus/internal/ui/views/creditspanel"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
	"github.com/justwasm/pigasus/internal/ui/views/effortpanel"
	"github.com/justwasm/pigasus/internal/ui/views/feedbackpanel"
	"github.com/justwasm/pigasus/internal/ui/views/helppanel"
	"github.com/justwasm/pigasus/internal/ui/views/hookspanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
	"github.com/justwasm/pigasus/internal/ui/views/modelargpopup"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/permissionspanel"
	"github.com/justwasm/pigasus/internal/ui/views/pluginsui"
	"github.com/justwasm/pigasus/internal/ui/views/providerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/renamepanel"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
	"github.com/justwasm/pigasus/internal/ui/views/skillsui"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/ui/views/subagentquestionpanel"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
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
			m.scrollBy(-tuilib.ClampInt(m.viewport.vpRows-1, 1, 1<<20))
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
			m.scrollBy(tuilib.ClampInt(m.viewport.vpRows-1, 1, 1<<20))
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
	if key.Code == 'o' && key.Mod.Contains(tea.ModCtrl) && !m.overlays.resumePicker.Active() {
		if m.overlays.panel.kind == panelregistry.Diff {
			// Cycle 381: the /diff panel owns the fold state, so the
			// toggle is the panel's.
			if m.diffPanel().View() != 0 {
				m.diffPanel().ToggleFoldAtCursor()
			}
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
		m.help.Panel.Open(2) // shortcuts
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
		m.help.Panel.Cancel()
		return m, nil
	}
	if m.prompt.DismissPopups() {
		return m, nil
	}
	if m.auth.DismissMenu() {
		return m, nil
	}
	if cmd, handled := m.overlays.Dismiss(m); handled {
		return m, cmd
	}
	if m.prompt.CollapseMedia() {
		return m, nil
	}

	// Priority 2: Execution cancellation (in-flight bash command or streaming turn)
	if m.session.store.BashRunning() {
		m.cancelBashCommand()
		return m, nil
	}
	if m.conversation.streaming && m.turn.activeBridge != nil && m.turn.activeBridge.IsRunning() {
		m.cancelActiveTurn()
		return m, nil
	}

	// Priority 3: Composer input editing state (selection, draft text)
	compaction.TriggerCancel()
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
				m.prompt.picker.Reset()
			}
		}
		return true
	}
	if m.prompt.atFilePicker.HandleKey(key) {
		m.prompt.promptHistory.Reset()
		return true
	}
	return false
}

func (m *RootModel) handleCtrlAndTabKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
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
			return m, tea.Quit, true
		}
	}
	if key.Code == 'a' && key.Mod == 0 && m.prompt.input.Value() == "" &&
		!m.help.Panel.Active() && !m.prompt.picker.Visible() && !m.overlays.resumePicker.Active() &&
		m.overlays.panel.kind == panelregistry.None &&
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
	if cmd != nil {
		msg := cmd()
		switch ev := msg.(type) {
		case helppanel.CommandSelectedMsg:
			m.SelectCommand(ev.Command)
			return m, nil, true
		case helppanel.DismissedMsg:
			if ev.EchoLine {
				m.conversation.response = append(m.conversation.response, tuilib.SlashEchoLines("/help", "Exited /help command")...)
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

	// Global exit: Ctrl+D on empty input exits immediately.
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) && m.prompt.input.Value() == "" {
		m.display.quitting = true
		return m, tea.Quit
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

	// The active-items block owns the keyboard while it has the focus:
	// ↑/↓ move the cursor, Enter opens the selected row's detail view in
	// /tasks, Esc gives the focus back to the prompt, and any other key
	// also gives it back before the key is dispatched normally
	// (agy ActiveItemsModel.Update). This runs before the keybinding
	// dispatch on purpose — otherwise cli.enter submits the prompt and
	// the block never sees the Enter that opens the detail view.
	if handled, cmd := m.activeItems.Panel.HandleKey(key); handled {
		return m, cmd
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
	if m.vimEnabled() && !m.help.Panel.Active() && !m.prompt.picker.Visible() && !m.overlays.resumePicker.Active() &&
		!m.isBtwCardVisible() && m.overlays.announceQueue.Active() == nil && !m.prompt.mediaExpanded &&
		m.overlays.panel.kind == panelregistry.None {
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
	return m, m.submitOrQueuePrompt(), true
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
	if key.Code == 'v' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
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
		ClearEligibilityNotice: m.clearEligibilityNotice,
		DismissModeHint:        func() { m.prompt.modeHintDismissed = true },
		UndoMgr:                m.prompt.undoMgr,
	}
}

// ---- tui_update_mouse.go ----

func (m *RootModel) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	oldW := m.viewport.width
	oldH := m.viewport.height
	m.viewport.width, m.viewport.height = msg.Width, msg.Height
	mdrender.SetCurrentWidth(msg.Width)
	if w := msg.Width - len(inputPrompt); w > 10 {
		m.prompt.input.SetWidth(w)
	}
	if m.askQuestion.Active() {
		m.askQuestion.SetWidth(msg.Width)
	}
	if (oldW != msg.Width || oldW == 0) && len(m.conversation.response) > 0 {
		m.reflowTranscript(msg.Width)
	} else if oldH != msg.Height || oldW != msg.Width {
		m.viewport.rowsCacheDirty = true
		m.viewport.viewCacheValid = false
		m.viewport.extraRowsValid = false
		m.viewport.rowsCache = wrapSlot{}
		m.viewport.rowsCacheN1 = wrapSlot{}
		m.viewport.rowsView = composedRows{}
	}
	m.activeItems.Panel.Clamp()
	return m, nil
}

func (m *RootModel) updateMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.viewport.altScreen || ev.Button != tea.MouseLeft {
		return m, nil
	}
	if m.prompt.picker.HandleClick(ev) {
		return m, nil
	}
	if m.prompt.picker.Visible() && !m.prompt.IsInputY(ev.Y) {
		m.prompt.picker.Reset()
	}
	if m.viewport.pinnedPromptText != "" && (ev.Y == 0 || ev.Y == 1) {
		if m.jumpToPinnedPrompt() {
			return m, nil
		}
	}
	if m.prompt.IsInputY(ev.Y) {
		m.ResetSelection()
		offset := m.prompt.InputOffsetAt(ev.X, ev.Y)
		m.prompt.SetInputCursorOffset(offset)
		m.prompt.InputSelDragging = true
		m.prompt.InputSelStart = offset
		m.prompt.InputSelEnd = offset
		return m, nil
	}
	m.ResetInputSelection()
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.viewport.width)
	if !ok {
		return m, nil
	}
	m.viewport.SelDragging = true
	m.viewport.SelStartRow, m.viewport.SelStartCol = row, col
	m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
	return m, nil
}

func (m *RootModel) updateMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.prompt.picker.HandleHover(ev)
	if m.prompt.InputSelDragging && m.viewport.altScreen {
		offset := m.prompt.InputOffsetAt(ev.X, ev.Y)
		if offset != m.prompt.InputSelEnd {
			m.prompt.InputSelEnd = offset
			m.prompt.SetInputCursorOffset(offset)
		}
		return m, nil
	}
	if !m.viewport.SelDragging || !m.viewport.altScreen {
		return m, nil
	}
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.viewport.width)
	if !ok {
		return m, nil
	}
	if row != m.viewport.SelEndRow || col != m.viewport.SelEndCol {
		m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
	}
	return m, nil
}

func (m *RootModel) updateMouseReleaseInputSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.prompt.InputSelDragging = false
	if ev.Button == tea.MouseLeft {
		m.prompt.InputSelEnd = m.prompt.InputOffsetAt(ev.X, ev.Y)
	}
	start, end := m.prompt.InputSelectionRange()
	if start < end {
		val := m.prompt.input.Value()
		if start < len(val) && end <= len(val) {
			text := val[start:end]
			_ = clipboard.Write(text)
			return m, m.copySelectionCmd(text)
		}
	} else {
		m.ResetInputSelection()
	}
	return m, nil
}

func (m *RootModel) updateMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.prompt.InputSelDragging {
		return m.updateMouseReleaseInputSel(ev)
	}
	if !m.viewport.SelDragging {
		return m, nil
	}
	m.viewport.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.ResetSelection()
		return m, nil
	}
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, m.viewportHeight(), m.viewport.width)
	if ok {
		m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
	}
	text := m.selectionText()
	m.ResetSelection()
	if text != "" {
		_ = clipboard.Write(text)
		return m, m.copySelectionCmd(text)
	}
	return m, nil
}

func (m *RootModel) updateEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.tempPath != "" {
		defer func() { _ = os.Remove(msg.tempPath) }()
		if msg.err == nil {
			if data, err := os.ReadFile(msg.tempPath); err == nil {
				m.prompt.input.SetValue(strings.TrimRight(string(data), "\n"))
			}
		}
	}
	if msg.err != nil {
		m.conversation.response = append(m.conversation.response, "Editor exited with error: "+msg.err.Error())
	}
	return m, nil
}

func (m *RootModel) updateMouseAndMiscMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case askQuestionOpenMsg:
		m.askQuestion.OpenQuestions(msg.questions, m.viewport.width, msg.replyCh)
		return m, nil, true
	case askQuestionCancelMsg:
		m.askQuestion.Cancel()
		return m, nil, true
	case askQuestionPendingMsg:
		if m.askQuestion.panel != nil {
			if msg.scope != "" && msg.scope != subagentquestions.ParentKey {
				m.askQuestion.panel.SetScope(msg.scope)
			} else {
				m.askQuestion.OpenParent()
			}
		}
		return m, tea.ClearScreen, true
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
		if m.overlays.resumePicker.Active() && m.overlays.resumePicker.Query() != "" {
			m.overlays.resumePicker.SetCursorOn(!m.overlays.resumePicker.CursorOn())
			return m, resumepicker.BlinkCmd(), true
		}
		m.overlays.resumePicker.SetCursorOn(false)
		return m, nil, true
	case keybind.EscTimeoutMsg:
		if m.prompt.altEscBuf.Pending {
			m.prompt.altEscBuf.Pending = false
			m.releaseBufferedEsc()
		}
		return m, nil, true
	case voice.ConnectDoneMsg:
		return m, m.audio.Surface.HandleConnectDone(msg), true
	case voice.TickMsg:
		return m, m.audio.Surface.HandleTick(), true
	case tea.PasteMsg:
		// The question form owns the input area while it is up. A paste
		// goes to its write-in field (when that field has the focus)
		// instead of the hidden composer behind it.
		if m.askQuestion.Active() {
			cmd := m.askQuestion.Update(msg)
			return m, cmd, true
		}
		cmd := m.handlePasteContent(msg.Content)
		return m, cmd, true
	case pasteMediaMsg:
		// A paused question form cannot show media; do not attach it to
		// the hidden composer.
		if m.askQuestion.Active() {
			return m, nil, true
		}
		mod, cmd := m.updatePasteMedia(msg)
		return mod, cmd, true
	case helppanel.CommandSelectedMsg:
		m.SelectCommand(msg.Command)
		return m, nil, true
	case helppanel.DismissedMsg:
		m.help.Panel.Close()
		return m, nil, true
	case slashpicker.CompletedMsg:
		m.prompt.input.SetValue(msg.Text)
		m.prompt.picker.Reset()
		return m, nil, true
	case slashpicker.DismissedMsg:
		m.prompt.picker.Reset()
		return m, nil, true
	case modelpicker.ModelSelectedMsg:
		m.ApplyModel(msg.Label)
		return m, m.closePanel(), true
	case modelpicker.DismissedMsg:
		return m, m.closePanel(), true
	case taskspanel.DetailOpenedMsg:
		m.activeItems.Panel.SetFocus(false)
		return m, armTasksTick(), true
	case taskspanel.TaskKilledMsg:
		if msg.Task != nil {
			m.killSelectedTask(msg.Task)
		}
		return m, armTasksTick(), true
	case taskspanel.TaskRemovedMsg:
		return m, armTasksTick(), true
	case taskspanel.DismissedMsg:
		if msg.FromActiveItems && m.activeItems.Panel != nil && m.activeItems.Panel.Count() > 0 {
			m.activeItems.Panel.SetFocus(true)
		}
		return m, m.closePanel(), true
	case codesearchpanel.SendCommentsMsg:
		return m, m.codesearchSendComments(msg.Text), true
	case codesearchpanel.OpenEditorMsg:
		return m, m.codesearchOpenEditor(msg.Path, msg.Line), true
	case codesearchpanel.DismissedMsg:
		return m, m.closePanel(), true
	case effortpanel.AppliedMsg:
		return m, tea.Batch(m.ApplyEffortSelection(msg.Level), m.closePanel()), true
	case effortpanel.DismissedMsg:
		return m, m.closePanel(), true
	case renamepanel.RenamedMsg:
		return m, tea.Batch(m.SubmitRename(msg.Name), m.closePanel()), true
	case renamepanel.DismissedMsg:
		return m, m.closePanel(), true
	case feedbackpanel.SubmittedMsg:
		m.SubmitFeedback()
		return m, m.closePanel(), true
	case feedbackpanel.DismissedMsg:
		return m, m.closePanel(), true
	case adddirpanel.AddedMsg:
		return m, tea.Batch(m.SubmitAddDir(msg.Path), m.closePanel()), true
	case adddirpanel.DismissedMsg:
		return m, m.closePanel(), true
	case rewindpanel.RewoundMsg:
		m.RewindPickStep(msg.Index)
		return m, m.closePanel(), true
	case rewindpanel.DismissedMsg:
		return m, m.closePanel(), true
	case providerpanel.DismissedMsg:
		return m, m.closePanel(), true
	case permissionspanel.DismissedMsg:
		return m, m.closePanel(), true
	case hookspanel.DismissedMsg:
		return m, m.closePanel(), true
	case accountspanel.DismissedMsg:
		return m, m.closePanel(), true
	case configpanel.DismissedMsg:
		return m, m.closePanel(), true
	case creditspanel.MsgClose:
		return m, m.closePanel(), true
	case creditspanel.MsgOpenURL:
		_ = m.OpenURL(msg.URL)
		return m, nil, true
	case activeitemspanel.MsgOpenDetail:
		cmd := m.openTasksDetail(msg.ID, msg.IsSubagent)
		return m, cmd, true
	case activeitemspanel.MsgDefocused:
		return m, nil, true
	case viewerpanel.MsgClose:
		return m, m.closePanel(), true
	case subagentquestionpanel.MsgDismissed:
		return m, nil, true
	case subagentquestionpanel.MsgQuestionStaged:
		return m, nil, true
	case diffpanel.MsgDismissed:
		return m, m.closePanel(), true
	case agentspanel.MsgClose:
		return m, m.closePanel(), true
	case agentspanel.MsgSwitchAgent:
		m.applyAgentSwitch(msg.Name, msg.Body)
		return m, m.closePanel(), true
	case agentspanel.MsgKillSubagent:
		if msg.Subagent != nil {
			m.killAgentsSubagent(msg.Subagent)
		}
		return m, nil, true
	case agentspanel.MsgAuthorizeSubagent:
		m.answerSubagentAuthorization(msg.ApprovalID, msg.Approved)
		return m, nil, true
	case mentionpicker.MsgCompleted:
		return m, nil, true
	case mentionpicker.MsgDismissed:
		return m, nil, true
	case pluginsui.MsgDismissed:
		return m, m.closePanel(), true
	case skillsui.MsgDismissed:
		return m, m.closePanel(), true
	}
	return m, nil, false
}

// ---- tui_update_ticks.go ----

func (m *RootModel) updateCompactDoneMsg(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.appendCompactResult(msg)
	if msg.CustomInstructions != "" && msg.Err == nil && !msg.Aborted {
		return m, m.streamPrompt(msg.CustomInstructions)
	}
	return m, nil
}

func (m *RootModel) updateBashDoneMsg(msg bashDoneMsg) (tea.Model, tea.Cmd) {
	// A preempted command's completion arrives after its successor started;
	// its generation is stale, so it must not touch the live block.
	if msg.gen != m.session.store.BashGen {
		return m, nil
	}
	m.finishBashCommand(msg)
	if len(m.session.store.BashQueue) > 0 {
		next := m.session.store.BashQueue[0]
		m.session.store.BashQueue = m.session.store.BashQueue[1:]
		return m, tea.Batch(m.startBashCommand(next), tea.ClearScreen)
	}
	return m, tea.ClearScreen
}

func (m *RootModel) updateSpinnerTickMsg(msg sp.TickMsg) (tea.Model, tea.Cmd) {
	if m.conversation.streaming {
		var cmd tea.Cmd
		m.conversation.spinner, cmd = m.conversation.spinner.Update(msg)
		m.conversation.spinFrame++
		return m, cmd
	}
	return m, nil
}

func (m *RootModel) updateUsageQuotaTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.Usage {
		return m, nil
	}
	m.display.quotaFrame++
	if m.overlays.panel.quota.state == quotaLoading && m.display.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usagepanel.QuotaTick()
}

func (m *RootModel) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactStart = msg.StartedAt
	m.session.store.CompactEnd = time.Time{}
	m.session.store.CompactSpinnerFrame = 0
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = 0
	m.session.store.CompactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *RootModel) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactEnd = msg.EndedAt
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = msg.TokensBefore
	m.session.store.CompactTokensAfter = msg.EstimatedTokensAfter

	if msg.Summary != "" || msg.Resume != "" {
		requests := compaction.UserRequests(m.SessionMessages(), compaction.MaxUserRequests)
		m.session.queueCheckpoint(convstore.Step{
			Type:            convstore.StepCheckpoint,
			Status:          3,
			Summary:         msg.Summary,
			UserRequests:    requests,
			Strategy:        msg.Strategy,
			CheckpointModel: msg.SummaryModel,
			TokensBefore:    msg.TokensBefore,
			TokensAfter:     msg.EstimatedTokensAfter,
		}, m.checkpointInsertionPoint(msg.KeepRecent))
		resume := msg.Resume
		if resume == "" {
			resume = compaction.ResumingBlock(requests, msg.Summary)
		}
		carriedTokens := compaction.EstimateTokens(compaction.CarriedBlock(requests))
		currentSteps := convstore.BuildSteps(m.trajectoryFacts())
		m.setSessionMessages([]backend.Message{{
			Role:    "user",
			Content: resume,
		}})
		if sup := m.session.sup(); sup != nil {
			sup.SetSeededConversation(currentSteps, m.session.messageCount())
		}
		m.conversation.liveReplyIdx = -1

		overhead := compaction.ContextOverheadTokens(m.systemPrompt(), modelDeclarationsJSON())
		beforeTokens, afterTokens, summaryTokens := compaction.ComputeCompactTokens(
			msg.TokensBefore,
			0,
			msg.EstimatedTokensAfter,
			0,
			overhead,
			msg.Summary,
			carriedTokens,
		)

		took := msg.EndedAt.Sub(m.session.store.CompactStart)
		if took < 0 {
			took = 0
		}

		m.conversation.response = append(m.conversation.response,
			compaction.DividerRow(m.viewport.width),
			compaction.StatsRow(
				m.viewport.width,
				msg.Steps,
				beforeTokens,
				afterTokens,
				summaryTokens,
				took,
			),
			"",
		)
		m.viewport.rowsCacheDirty = true
	}

	return m, nil
}

func (m *RootModel) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.session.store.CompactStart.IsZero() || !m.session.store.CompactEnd.IsZero() {
		return m, nil
	}
	m.session.store.CompactSpinnerFrame++
	if m.session.store.CompactSpinnerFrame >= 8 {
		m.session.store.CompactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *RootModel) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
	// Cycle 381: the blink phase is the panel's own state — the panel
	// lives in internal/ui/views/pluginsui — so the host only flips it
	// and re-arms the tick while a blinking cursor is on screen.
	p := m.overlays.panel.plg
	if p == nil {
		return m, nil
	}
	if m.overlays.panel.kind == panelregistry.Plugin && p.Blinking() {
		p.ToggleBlink()
		return m, pluginsui.Blink()
	}
	p.SetBlink(false)
	return m, nil
}

// updateCSTickMsg drains the matches the runner has produced and arms
// the next tick.
//
// Cycle 381: the drain, the spinner and the tick belong to the
// /codesearch panel (internal/ui/views/codesearchpanel); the host only
// carries the tick message.
func (m *RootModel) updateCSTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.CodeSearch {
		return m, nil
	}
	p := m.codesearchPanel()
	if p == nil || p.Search() == nil {
		return m, nil
	}
	_, closed := p.Drain()
	if closed {
		return m, nil
	}
	return m, p.Tick()
}

func (m *RootModel) updateCompactEvents(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case compactDoneMsg:
		mod, cmd := m.updateCompactDoneMsg(msg)
		return mod, cmd, true
	case genericCompactDoneMsg:
		cmd := m.applyGenericCompactResult(msg, msg.CustomInstructions)
		return m, cmd, true
	case compactStartMsg:
		mod, cmd := m.updateCompactStartMsg(msg)
		return mod, cmd, true
	case compactEndMsg:
		mod, cmd := m.updateCompactEndMsg(msg)
		return mod, cmd, true
	case compactSpinnerTickMsg:
		mod, cmd := m.updateCompactSpinnerTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}

func (m *RootModel) updateTicksAndCompactMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if mod, cmd, handled := m.updateCompactEvents(msg); handled {
		return mod, cmd, true
	}
	if mod, cmd, handled := m.updateTickMsg(msg); handled {
		return mod, cmd, true
	}
	return m.updatePanelTickMsg(msg)
}

// updateTickMsg handles the messages that only advance the transcript's
// own timers: the plan tier, the bash mode, the spinner and the task
// list.
func (m *RootModel) updateTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case planTierMsg:
		// The tier belongs to the account it was read for. A session
		// that follows a switch or a sign-in gets the new account's
		// tier; until that answer lands the session shows the email
		// alone, never the previous account's tier.
		if msg.email != "" {
			m.session.tierAccount = msg.email
			m.session.tierFetched = msg.tier != ""
			if msg.tier != "" {
				m.session.tier = msg.tier
				// The tier was cached in the registry, and the /accounts
				// panel shows the cached tier, so its rows have to be
				// re-read.
				if m.overlays.panel.kind == panelregistry.Accounts {
					m.accountsPanel().Reload()
				}
			} else {
				// The server named no tier for this account (a free
				// or an ineligible one): the session shows the email
				// alone, not the previous account's tier and not a
				// placeholder.
				m.session.tier = ""
			}
		}
		return m, nil, true
	case bashDoneMsg:
		mod, cmd := m.updateBashDoneMsg(msg)
		return mod, cmd, true
	case btwTickMsg:
		if m.IsBtwActive() {
			return m, btwTick(), true
		}
		return m, nil, true
	case btwDoneMsg:
		mod, cmd := m.updateBtwDoneMsg(msg)
		return mod, cmd, true
	case sp.TickMsg:
		mod, cmd := m.updateSpinnerTickMsg(msg)
		return mod, cmd, true
	case tasksTickMsg:
		dirty := m.consumeTasksDirty()
		if dirty || m.shouldTickTasks() {
			return m, armTasksTick(), true
		}
		return m, nil, true
	case statusLineExternalTickMsg:
		m.ensureExternalState().MarkTick()
		// The tick builds the payload on this goroutine — the session's
		// state is not safe to read from another one — and runs the
		// scripts in a goroutine of their own.
		m.runExternalStatusLineTick()
		return m, armExternalStatusLineTick(), true
	}
	return m, nil, false
}

// updatePanelTickMsg handles the ticks that refresh a panel's own state:
// the usage panel's quota summary, the /provider panel's verification
// probe, the usage quota timer, the plugin blink and the codesearch
// spinner.
func (m *RootModel) updatePanelTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case quotaSummaryMsg:
		if m.overlays.panel.kind == panelregistry.Usage {
			if msg.summary != nil {
				m.overlays.panel.quota.summary = msg.summary
				m.overlays.panel.quota.loadedAt = time.Now()
				m.overlays.panel.quota.state = quotaLoaded
				// The summary is cached for the session, so the
				// /accounts panel can show the active account's
				// quota without querying it a second time.
				m.cacheQuota(m.session.email, msg.summary)
			} else if msg.err != nil {
				m.overlays.panel.quota.fetchErr = msg.err.Error()
			}
		}
		return m, nil, true
	case accountsQuotaMsg:
		// The queue, the error line and the refreshed rows are the
		// panel's.
		return m, m.accountsPanel().QuotaDone(msg.email, msg.summary != nil, msg.err), true
	case providerpanel.VerifyMsg:
		// The /provider panel's verification probe came back. The panel
		// may have been closed while the request was in flight.
		if m.overlays.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyVerifyMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthResultMsg:
		if m.overlays.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyOAuthResultMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthTickMsg:
		if m.overlays.panel.kind == panelregistry.Provider && m.overlays.panel.prov != nil {
			return m, m.overlays.panel.prov.ApplyOAuthTick(), true
		}
		return m, nil, true
	case usageQuotaTickMsg:
		mod, cmd := m.updateUsageQuotaTickMsg()
		return mod, cmd, true
	case pluginsui.BlinkMsg:
		mod, cmd := m.updatePluginBlinkMsg()
		return mod, cmd, true
	case codesearchpanel.TickMsg:
		mod, cmd := m.updateCSTickMsg()
		return mod, cmd, true
	}
	return m, nil, false
}

// ---- tui_update_login.go ----

func (m *RootModel) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.auth.login == nil {
		return m, nil, false
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.viewport.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.width, m.viewport.height = msg.Width, msg.Height
		m.auth.login.Width, m.auth.login.Height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.auth.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.auth.login.State == loginui.StateSelectMethod {
			m.auth.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case *auth.AntigravityToken:
		return m, m.finishLogin(msg), true
	}
	newLM, cmd := m.auth.login.Update(msg)
	if lm, ok := newLM.(*loginui.Model); ok {
		m.auth.login = lm
		if lm.Authenticated {
			// Cycle multi-account-switch: the OAuth sign-in becomes a
			// known account (the *auth.AntigravityToken case above).
			// The ADC path carries no token, so there is nothing to
			// register here.
			return m, m.finishLogin(nil), true
		}
	}
	return m, cmd, true
}

// finishLogin closes the login panel and applies the signed-in identity.
//
// The token is the OAuth path's credential; it is nil on the ADC path,
// which carries no token to register.
//
// It returns the command that reads the new account's tier: the tier
// belongs to an account, so the session that just signed in as another
// account has to ask about *that* account — otherwise the previous
// account's tier would be shown against the new one.
func (m *RootModel) finishLogin(token *auth.AntigravityToken) tea.Cmd {
	m.auth.login = nil
	m.session.email = auth.EffectiveUserEmail()
	if m.turn.backendFor != nil {
		m.turn.backend = m.turn.backendFor(m.session.model)
	}
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// Cycle multi-account-switch: the sign-in becomes a known
	// account, so it can be switched back to later.
	if token != nil {
		acc, _, err := accounts.Register(token)
		if err == nil && acc.Email != "" {
			m.session.email = acc.Email
		}
	}
	// The sign-in completed, so the shared credential changed: the
	// panel's rows have to be refreshed even when no panel is open.
	m.accountsPanel().Reload()
	m.slashRespond("/login", "Authentication successful!")
	return m.fetchPlanTierCmd()
}

// ---- tui_alt_esc.go ----

// tui_alt_esc.go — Option/Alt key translation and Esc chord release.

// altKeyFire records one synthesised alt+letter dispatch.
type altKeyFire struct {
	Letter rune   // lowercase letter that was synthesised
	Action string // action name from keybindings.json
}

// altDispatchAction is the production action table.
func (m *RootModel) altDispatchAction(letter rune, action string) {
	m.prompt.altKeyFires = append(m.prompt.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.conversation.response = append(m.conversation.response,
		uirows.NoticeDim("alt+"+string(letter)+" → "+action))
}

// releaseBufferedEsc replays a buffered bare Esc that was not the first
// half of an alt+letter sequence.
//
// In vim mode the editor owns Esc — a bare Esc leaves Insert mode and
// returns to Normal — so the release goes to the vim engine. Everywhere
// else it is the host's Esc.
func (m *RootModel) releaseBufferedEsc() {
	if m.vimEnabled() {
		if _, handled := m.handleVimKey(tea.Key{Code: tea.KeyEsc}); handled {
			return
		}
	}
	_, _ = m.handleEscKey()
}

// handleEscKey runs the bare-Esc branch: delegates to handleEscKeyMsg
// to ensure unified priority across direct Esc and EscReleaseWindow tick.
func (m *RootModel) handleEscKey() (tea.Cmd, bool) {
	_, cmd := m.handleEscKeyMsg()
	return cmd, true
}

// armEscReleaseTick returns a tea.Cmd that fires after keybind.EscReleaseWindow.
func (m *RootModel) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
}

func (m *RootModel) canConsumeAltEsc() bool {
	if m.overlays.panel.kind != panelregistry.None || m.help.Panel.Active() ||
		m.prompt.picker.Visible() || m.prompt.atFilePicker.Visible() ||
		m.overlays.resumePicker.Active() || m.audio.Surface.Active() ||
		m.prompt.mediaExpanded || m.isBtwCardVisible() ||
		m.conversation.streaming || m.session.store.BashRunning() ||
		m.prompt.HasInputSelection() {
		return false
	}
	return true
}

// consumeAltEsc is the alt-keytranslation entry point.
func (m *RootModel) consumeAltEsc(msg tea.KeyMsg) (newMsg tea.KeyMsg, consumed bool, cmd tea.Cmd) {
	key := msg.Key()
	if !keybind.TermNeedsOptionMeta() {
		return msg, false, nil
	}

	if m.prompt.keybindings == nil || len(m.prompt.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	if !m.canConsumeAltEsc() {
		return msg, false, nil
	}

	if m.prompt.altEscBuf.Pending {
		consumedKey := m.prompt.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.prompt.keybindings != nil && m.prompt.keybindings.Has(r) },
			func(r rune) {
				key = tea.Key{Code: r, Mod: tea.ModAlt, Text: string(r)}
			},
			m.releaseBufferedEsc,
			time.Now(),
		)
		if consumedKey {
			return tea.KeyPressMsg(key), false, nil
		}
		return msg, false, nil
	}

	if key.Code == tea.KeyEsc && key.Mod == 0 {
		if m.prompt.altEscBuf.Start(time.Now()) {
			return msg, true, m.armEscReleaseTick()
		}
	}

	return msg, false, nil
}

// ---- vim_mode.go ----

// Cycle 216: the TUI half of the Vim editing mode. The engine lives in
// vim and knows nothing about bubbles or the run loop. This file is the
// host's half of the bridge: it keeps the textarea in step with the
// engine's buffer and cursor, feeds the engine one key at a time and
// hands a submit result to the shared submitPrompt path.
//
// Cycle 381, tranche 58: the translation itself — the bubbletea key to
// the engine's normalised Key — moved to internal/ui/editor/vimkeys,
// which is where a key that knows about the terminal but not about the
// engine belongs. This file names that package; it keeps no copy of
// the mapping.
//
// Everything here is inert unless the "Editor Mode" setting is vim, so the
// default (non-vim) prompt behaves exactly as it did before.

// vimEnabled reports whether "Editor Mode" selects vim. The value is read
// from settings.json at TUI start-up (and again whenever a slash panel
// closes, so toggling Editor Mode in /config applies without a restart);
// it is kept in a field rather than re-read per keystroke so the tests
// stay hermetic and the render path does no file I/O.
func (m *RootModel) vimEnabled() bool { return m.prompt.vimOn }

func (m *RootModel) VimEnabled() bool { return m.vimEnabled() }

// vimInsertFirstEnabled reports the "Editor Mode › Insert First" child
// setting: when on, the prompt opens in Insert mode and a bare Enter
// submits instead of inserting a newline.
func (m *RootModel) vimInsertFirstEnabled() bool { return m.prompt.vimFirstOn }

// reloadVimSettings reads the two vim settings from the live
// settings.json. It is called once at TUI start-up and again when a slash
// panel closes (/config can change both).
func (m *RootModel) reloadVimSettings() {
	m.prompt.vimOn = false
	if raw, err := settings.Field("editorMode"); err == nil {
		m.prompt.vimOn = configpanel.RawString(raw) == "vim"
	}
	m.prompt.vimFirstOn = false
	if raw, err := settings.Field("vimInsertFirst"); err == nil {
		switch v := raw.(type) {
		case bool:
			m.prompt.vimFirstOn = v
		case string:
			m.prompt.vimFirstOn = v == "on" || v == "true"
		}
	}
}

// vimEngine returns the engine, building it from the current prompt on
// first use. Whenever the host has replaced the buffer behind the
// engine's back — a submit reset, a history recall, the picker's Tab
// substitution, a slash rewrite — the engine is re-seeded so it always
// tracks what is actually on screen.
func (m *RootModel) vimEngine() *vim.State {
	if m.prompt.vimEng == nil {
		m.prompt.vimEng = vim.New(m.prompt.input.Value())
	} else if m.prompt.vimEng.Text() != m.prompt.input.Value() {
		m.prompt.vimEng.SetText(m.prompt.input.Value())
	}
	// SetInsertFirst is a no-op when the setting is unchanged, so this
	// does not yank the user back into Insert mode on every keystroke.
	m.prompt.vimEng.SetInsertFirst(m.vimInsertFirstEnabled())
	return m.prompt.vimEng
}

// vimMode reports the mode the status row's badge shows.
//
// The engine is seeded the way the host seeds it for a key press, so the
// badge agrees with the mode the next key will be handled in. It is not
// built when vim mode is off: the badge is empty then, and the host
// keeps its "no engine without vim" invariant.
func (m *RootModel) vimMode() vim.Mode {
	if !m.vimEnabled() {
		return vim.Normal
	}
	return m.vimEngine().Mode
}

// syncVimInput mirrors the engine onto the textarea. The textarea is only
// a display surface in vim mode; the engine owns the buffer and cursor.
//
// SetValue leaves the textarea cursor at the end of the buffer, so the
// row is walked back up before the column is set. A column past the end
// of the line is legal in Insert mode (the append point), and the
// textarea clamps it itself.
func (m *RootModel) syncVimInput() {
	eng := m.prompt.vimEng
	if eng == nil {
		return
	}
	m.prompt.input.SetValue(eng.Text())
	for r := m.prompt.input.LineCount() - 1; r > eng.Row; r-- {
		m.prompt.input.CursorUp()
	}
	m.prompt.input.SetCursorColumn(eng.Col)
}

// handleVimKey feeds one key to the engine. handled is false when the
// engine declined the key, which is the signal for the caller to fall
// through to the host's own binding (ctrl+c, ctrl+d, ctrl+o, Tab, …).
//
// The order matters: the special codes are checked first because a
// driver may attach text to them too (Enter arrives with Text "\n" in
// some terminals, and in the clone's own synthetic key events). Only
// then is the literal text consumed — a paste can deliver several runes
// in one event, so each is fed through the engine in order.
func (m *RootModel) handleVimKey(key tea.Key) (tea.Cmd, bool) {
	eng := m.vimEngine()

	// When on the last line of the buffer, Down arrow or 'j' (in Normal mode)
	// switches focus into ActiveItems if any are running, matching PromptModel.Update (0x102c91d60).
	if (eng.Mode == vim.Normal && eng.Pending == "") || eng.Mode == vim.Insert {
		isDown := key.Code == tea.KeyDown
		if eng.Mode == vim.Normal {
			isDown = isDown || ((key.Code == 'j' || key.Text == "j") && key.Mod == 0)
		}
		if isDown && eng.Row >= len(eng.Lines)-1 {
			if !m.activeItems.Panel.Hidden() && m.activeItems.Panel.Count() > 0 {
				m.activeItems.Panel.SetFocus(true)
				m.activeItems.Panel.SelectFirst()
				return nil, true
			}
		}
	}

	// Custom Vim submit bindings from keybindings.json
	if m.prompt.keybindings != nil {
		ks := keybind.KeyString(key)
		if ks != "" {
			if eng.Mode == vim.Insert {
				if act := m.prompt.keybindings.ActionForExactKeyInScope(ks, "vim.insert."); act == "vim.insert.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
					m.syncVimInput()
					return cmd, true
				}
			} else if eng.Mode == vim.Normal {
				if act := m.prompt.keybindings.ActionForExactKeyInScope(ks, "vim.normal."); act == "vim.normal.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
					m.syncVimInput()
					return cmd, true
				}
			}
		}
	}

	var res vim.Result
	handled := false

	if nk := vimkeys.Named(key); nk != (vim.Key{}) {
		res = eng.Handle(nk)
		handled = res.Consumed
	} else if key.Text != "" && key.Mod&(tea.ModCtrl|tea.ModAlt) == 0 {
		for _, r := range key.Text {
			res = eng.Handle(vim.Key{Rune: r})
			if res.Consumed {
				handled = true
			}
			if res.Submit {
				break
			}
		}
	} else {
		vk, ok := vimkeys.Rune(key)
		if !ok {
			return nil, false
		}
		res = eng.Handle(vk)
		handled = res.Consumed
	}

	if res.Submit {
		m.syncVimInput()
		cmd := m.submitPrompt()
		// 【一手】 agy 1.4.9, the vendor's own release notes: "a
		// half-typed operator such as `d` carrying over after you submit
		// a prompt". The submit clears the composer, so the engine is
		// re-seeded from it — with the half-typed operator intact.
		m.prompt.vimEng.SetTextKeepingPending(m.prompt.input.Value())
		m.syncVimInput()
		return cmd, true
	}
	if !handled {
		return nil, false
	}
	m.syncVimInput()
	return nil, true
}

// Cycle 381: the /help overlay's vim tab moved to
// internal/ui/views/helppanel, which owns the tab list and the
// binding catalogue it draws.

// ---- tui_input_selection.go ----

// tui_input_selection.go — mouse selection, auto copy, and select-all in the prompt input.
//
// In addition to in-app transcript text selection (tui_selection.go), the
// reference input box allows selecting text by mouse drag, automatically
// copying the selection to the system clipboard upon mouse release, and
// selecting the entire input buffer via Cmd+A (or Ctrl+Shift+A).
//
// When an input selection is active:
//   - Visual feedback is rendered in reverse video (\x1b[7m...\x1b[0m), matching
//     transcript selection.
//   - Mouse release immediately writes to the clipboard (OSC 52 + pbcopy).
//   - Typing a character or pasting replaces the selected text.
//   - Backspace or Delete removes the selected text.
//   - Arrow keys or Escape clear the selection.

var clipboardReadText = clipboard.Read

// insertPastedText is the single insertion point for both the terminal's
// bracketed paste and ctrl+v. It never submits.
func (m *RootModel) insertPastedText(s string) tea.Cmd {
	s = clipboard.NormalizeText(s)
	if s == "" {
		return nil
	}
	if m.overlays.panel.kind == panelregistry.Rename {
		// Cycle 381: the /rename panel owns its input, so the paste
		// goes through the panel.
		return m.renamePanel().HandlePaste(s)
	}
	if m.overlays.panel.kind == panelregistry.Provider {
		return m.providerPanel().HandlePaste(s)
	}
	if m.help.Panel.Active() || m.overlays.panel.kind != panelregistry.None {
		return nil
	}
	if m.overlays.resumePicker.Active() {
		m.overlays.resumePicker.TypeText(s)
		return nil
	}
	if m.prompt.HasInputSelection() {
		m.prompt.DeleteInputSelection()
	}
	if m.prompt.undoMgr != nil {
		m.prompt.undoMgr.RecordBeforeChange(m.prompt.input.Value(), tuilib.InputCursorByteOffset(m.prompt.input), "paste")
	}

	// Folding threshold: >= 16 lines or > 1000 characters.
	// When folded, becomes "[Pasted text #%d +%d lines]" or "[Pasted text #%d %d chars]".
	toInsert := s
	lines := strings.Count(s, "\n") + 1
	charCount := len([]rune(s))
	if lines >= 16 || charCount > 1000 {
		if m.prompt.pastedBlocks == nil {
			m.prompt.pastedBlocks = make(map[string]string)
		}
		m.prompt.nextPasteID++
		var placeholder string
		if lines >= 16 {
			placeholder = fmt.Sprintf("[Pasted text #%d +%d lines]", m.prompt.nextPasteID, lines)
		} else {
			placeholder = fmt.Sprintf("[Pasted text #%d %d chars]", m.prompt.nextPasteID, charCount)
		}
		m.prompt.pastedBlocks[placeholder] = s
		toInsert = placeholder
	}

	m.prompt.input.InsertString(toInsert)
	m.prompt.picker.Update([]rune(tuilib.FirstLine(m.prompt.input.Value())))
	m.prompt.atFilePicker.Update()
	m.clearEligibilityNotice()
	return nil
}

// ---- picker_model.go ----

// picker_model.go — the "/model <query>" completion popup.
//
// The reference's composer completes the /model *argument* with a popup
// of models: the display label in a 30-column field, the catalogue id two
// columns further right. 【一手】 agy 1.4.6, live tmux capture:
//
//	> /model claude
//	────────────────────────────────────────────────────────
//	> Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//	  Claude Opus 4.6 (Thinking)    claude-opus-4-6-thinking
//
//	  ↑/↓ Navigate · enter Select · tab Complete
//
// The filter is a case-insensitive *substring* of the label or of the id
// (internal/ui/views/slashpicker), the popup replaces the command rows
// while it is open, and Enter has three outcomes:
//
//   - the argument already names a model → the command runs and the model
//     changes ("Model set to <label>");
//   - the argument does not, but a row is highlighted → the row's label is
//     completed into the composer and the popup closes;
//   - the argument does not and nothing matches → the command runs and
//     reports `Invalid model "zzz" (available: ...)`.
//
// Tab always completes the highlighted row into the composer.
//
// The popup itself is a panel (internal/ui/views/slashpicker), and the
// argument's resolution against the catalogue belongs to the ui tier
// (internal/ui/views/modelargpopup). This file owns what the host has
// to do with a *resolved* model — making it the session's model and
// echoing the reference's answer.

// applyModelArgument makes the model the argument names the session's
// model and echoes the reference's confirmation.
//
// 【一手】 agy 1.4.6, live: "/model Claude Sonnet 4.6 (Thinking)" answers
// "Model set to Claude Sonnet 4.6 (Thinking)" and the status row follows;
// naming the model that is already selected answers "Model already set to
// <label>".
func (m *RootModel) applyModelArgument(arg string) tea.Cmd {
	entry, ok := modelargpopup.Resolve(arg)
	if !ok {
		// If attached to a remote backend with dynamic models, check its catalogue:
		if lister, ok := m.turnEngine().(agentturn.ModelLister); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			models, _ := lister.ListModels(ctx)
			cancel()
			argLower := strings.ToLower(strings.TrimSpace(arg))
			for _, mdl := range models {
				if strings.ToLower(mdl.ID) == argLower || strings.ToLower(mdl.DisplayName) == argLower {
					if m.session.model == mdl.ID || m.session.model == mdl.DisplayName {
						m.slashRespond("/model", "Model already set to "+mdl.DisplayName)
						return nil
					}
					m.cancelActiveTurn()
					_ = settings.SetField("model", mdl.ID)
					m.session.model = mdl.ID
					m.session.statusModel = tuilib.StripTier(mdl.DisplayName)
					m.slashRespond("/model", "Model set to "+mdl.DisplayName)
					return nil
				}
			}
		}
		m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
		return nil
	}
	if m.session.model == entry.Label {
		m.slashRespond("/model", "Model already set to "+entry.Label)
		return nil
	}
	m.cancelActiveTurn()
	_ = settings.SetField("model", entry.ID)
	m.session.model = entry.Label
	m.session.statusModel = tuilib.StripTier(entry.Label)
	m.syncBackendForModel()
	m.slashRespond("/model", "Model set to "+entry.Label)
	return nil
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *RootModel) submitModelCompletion() (bool, tea.Cmd) {
	if !m.prompt.picker.IsModelMode() {
		return false, nil
	}
	arg := strings.TrimSpace(m.prompt.picker.Query())
	if entry, ok := modelargpopup.Resolve(arg); ok {
		m.prompt.picker.Reset()
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		if m.session.model == entry.Label {
			m.slashRespond("/model", "Model already set to "+entry.Label)
			return true, nil
		}
		_ = settings.SetField("model", entry.ID)
		m.session.model = entry.Label
		m.session.statusModel = tuilib.StripTier(entry.Label)
		m.syncBackendForModel()
		m.slashRespond("/model", "Model set to "+entry.Label)
		return true, nil
	}
	// The argument names no model: the highlighted row is completed into
	// the composer instead of running the command.
	if sel := m.prompt.picker.Selected(); sel != nil {
		m.prompt.input.SetValue("/model " + sel.Name)
		m.prompt.picker.Reset()
		m.prompt.promptHistory.Reset()
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	m.prompt.picker.Reset()
	m.prompt.input.Reset()
	m.ResetInputSelection()
	m.prompt.promptHistory.Reset()
	m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
	return true, nil
}

// checkpointInsertionPoint is the call count a compact checkpoint is
// inserted at: the calls before the turn plus the turn's live steps,
// keeping the recent tail the compaction retained.
func (m *RootModel) checkpointInsertionPoint(keepRecent int) int {
	_, calls, _ := m.trajectoryCounts()
	after := calls + len(m.conversation.liveSteps) - keepRecent
	if after < 0 {
		return 0
	}
	return after
}
