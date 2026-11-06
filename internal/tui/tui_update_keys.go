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
	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
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

func (m *runMode) dispatchKeyAction(action string) (tea.Model, tea.Cmd, bool) {
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
		if !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() {
			return m, m.submitAlternate(), true
		}
	case "view.review_artifact":
		if !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None {
			if m.openSlashPanel("artifact") {
				return m, nil, true
			}
		}
	case "view.toggle_trajectory":
		if !m.overlays.resumePicker.Active() {
			if len(m.composer.pendingMedia) > 0 {
				m.composer.mediaExpanded = !m.composer.mediaExpanded
				return m, nil, true
			}
			m.toggleTrajectoryExpanded()
			return m, tea.ClearScreen, true
		}
	case "subagent.approve_fast":
		if !m.overlays.resumePicker.Active() {
			m.fastApproveSubagent()
			return m, nil, true
		}
	case "subagent.jump_to_waiting":
		if !m.overlays.resumePicker.Active() {
			if m.engine.subAgentQuestionPanel != nil && m.engine.subAgentQuestionPanel.PendingCount() > 0 {
				m.engine.subAgentQuestionPanel.Toggle()
				return m, nil, true
			}
			if m.viewport.activeItems != nil && m.viewport.activeItems.Count() > 0 && !m.engine.subAgentQuestionPanel.Active() {
				m.viewport.activeItems.SetFocus(!m.viewport.activeItems.Focused())
				return m, nil, true
			}
			if m.engine.subAgentQuestionPanel != nil {
				m.engine.subAgentQuestionPanel.Toggle()
				return m, nil, true
			}
		}
	case "voice.start_dictation":
		if !m.overlays.resumePicker.Active() && !m.viewport.voiceSurface.Active() && m.overlays.panel.kind == panelregistry.None {
			return m, m.viewport.voiceSurface.Start("keybind"), true
		}
	case "cli.cycle_mode":
		if !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() {
			m.cycleMode()
			return m, nil, true
		}
	case "item.rename":
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyF2}, m.viewport.height), true
		}
		if !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() {
			m.openRenamePanel()
			return m, nil, true
		}
	case "item.delete":
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyF4}, m.viewport.height), true
		}
	case "navigation.go_to_top", "navigation.top":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavTop, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.gotoTop()
			return m, nil, true
		}
	case "navigation.go_to_bottom", "navigation.bottom":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavBottom, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.gotoBottom()
			return m, nil, true
		}
	case "navigation.page_up":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavPageUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.scrollBy(-tuilib.ClampInt(m.viewport.vpRows-1, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.page_down":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavPageDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.scrollBy(tuilib.ClampInt(m.viewport.vpRows-1, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.half_page_up":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavHalfPageUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.scrollBy(-tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.half_page_down":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavHalfPageDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.viewport.altScreen && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() {
			m.scrollBy(tuilib.ClampInt(m.viewport.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.up":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavUp, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyUp}, m.viewport.height), true
		}
	case "navigation.down":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavDown, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyDown}, m.viewport.height), true
		}
	case "navigation.left":
		if listPanelKind(m.overlays.panel.kind) {
			m.overlays.panel.cursor = tuiviewport.PanelCursor(m.overlays.panel.cursor, tuilib.NavLeft, m.panelRegistry().RowCount(m.overlays.panel.kind))
			return m, nil, true
		}
		if m.overlays.resumePicker.Active() {
			return m, m.overlays.resumePicker.HandleKey(tea.Key{Code: tea.KeyLeft}, m.viewport.height), true
		}
	case "navigation.right":
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
		if m.engine.exitConfirm {
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
		if m.engine.exitConfirm {
			m.engine.exitConfirm = false
			return m, nil, true
		}
	case "cli.suspend":
		return m, tea.Suspend, true
	case "cli.escape":
		if m.vimEnabled() && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.composer.atFilePicker.Visible() && m.overlays.panel.kind == panelregistry.None {
			return m, nil, false
		}
		if !m.engine.streaming && m.session.store.BashCancel == nil && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.composer.atFilePicker.Visible() && m.overlays.panel.kind == panelregistry.None && !m.composer.mediaExpanded {
			if m.engine.exitConfirm {
				m.engine.exitConfirm = false
				return m, nil, true
			}
		}
		model, cmd := m.handleEscKeyMsg()
		return model, cmd, true
	case "cli.enter":
		if m.vimEnabled() && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.composer.atFilePicker.Visible() && m.overlays.panel.kind == panelregistry.None {
			return m, nil, false
		}
		if !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() {
			return m.handleEnterKey(tea.Key{Code: tea.KeyEnter})
		}
	case "prompt.insert_newline":
		if !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None {
			if m.vimEnabled() && m.composer.vimEng != nil && m.composer.vimEng.Mode == vim.Insert {
				m.composer.vimEng.InsertNewline()
				m.syncVimInput()
			} else {
				m.composer.input.InsertRune('\n')
			}
			m.composer.modeHintDismissed = true
			m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
			m.composer.atFilePicker.Update()
			m.composer.promptHistory.Reset()
			return m, nil, true
		}
	case "edit.paste":
		if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
			return m, nil, true
		}
		m.composer.promptHistory.Reset()
		return m, m.handlePasteContent(clipboardReadText()), true
	case "edit.undo":
		if m.composer.undoMgr != nil {
			if snap, ok := m.composer.undoMgr.Undo(m.composer.input.Value(), tuilib.InputCursorByteOffset(m.composer.input)); ok {
				m.ResetInputSelection()
				m.composer.input.SetValue(snap.Value)
				m.setInputCursorOffset(snap.CursorOffset)
				m.composer.modeHintDismissed = true
				m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
				m.clearEligibilityNotice()
				m.composer.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.redo":
		if m.composer.undoMgr != nil {
			if snap, ok := m.composer.undoMgr.Redo(m.composer.input.Value(), tuilib.InputCursorByteOffset(m.composer.input)); ok {
				m.ResetInputSelection()
				m.composer.input.SetValue(snap.Value)
				m.setInputCursorOffset(snap.CursorOffset)
				m.composer.modeHintDismissed = true
				m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
				m.clearEligibilityNotice()
				m.composer.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.yank":
		m.composer.promptHistory.Reset()
		text := clipboardReadText()
		if text != "" {
			return m, m.insertPastedText(text), true
		}
		return m, nil, true
	case "navigation.tab":
		if m.handleTabCompletion(tea.Key{Code: tea.KeyTab}) {
			return m, nil, true
		}
		if m.isBtwCardVisible() && !m.composer.picker.Visible() && !m.composer.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil, true
		}
	case "vim.insert.submit", "vim.normal.submit":
		if m.vimEnabled() {
			m.syncVimInput()
			cmd := m.submitPrompt()
			m.composer.vimEng.SetTextKeepingPending(m.composer.input.Value())
			m.syncVimInput()
			return m, cmd, true
		}
	case "vim.insert.insert_newline":
		if m.vimEnabled() && m.composer.vimEng != nil && m.composer.vimEng.Mode == vim.Insert {
			m.composer.vimEng.InsertNewline()
			m.syncVimInput()
			return m, nil, true
		}
	case "cli.test":
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) fastApproveSubagent() bool {
	if m.engine.taskSubagentManager != nil {
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
	if m.engine.subAgentQuestions != nil {
		all := m.engine.subAgentQuestions.AllPending()
		for scope, qs := range all {
			if len(qs) > 0 {
				q := qs[0]
				if len(q.Options) == 0 {
					continue
				}
				ans := q.Options[0]
				if err := m.engine.subAgentQuestions.Answer(scope, q.ID, ans); err == nil {
					label := scope
					if label == subagentquestions.ParentKey {
						label = "you"
					}
					m.engine.response = append(m.engine.response, tuilib.SlashEchoLines("", "  ⎿  Approved action for "+label)...)
					return true
				}
			}
		}
	}
	return false
}

func (m *runMode) handleGlobalOrModalKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.composer.keybindings != nil {
		if act := m.composer.keybindings.ActionForKey(key); act != "" {
			if act == "cli.escape" && key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
				if m.session.store.BashCancel != nil {
					m.cancelBashCommand()
					return m, nil, true
				}
				if m.engine.streaming && m.engine.turnCancel != nil {
					model, cmd := m.handleEscKeyMsg()
					return model, cmd, true
				}
				if m.engine.exitConfirm {
					m.engine.quitting = true
					return m, tea.Quit, true
				}
				m.engine.exitConfirm = true
				m.engine.exitConfirmAt = time.Now()
				return m, nil, true
			}
			if model, cmd, handled := m.dispatchKeyAction(act); handled {
				return model, cmd, true
			}
		}
	}
	if r, ok := keybind.IsAltLetter(key); ok {
		if m.composer.keybindings != nil {
			if action, ok := m.composer.keybindings.LookupAltLetter(r); ok {
				m.altDispatchAction(r, action)
				return m, nil, true
			}
		}
		if r == 'm' {
			return m, m.submitAlternate(), true
		}
		if r == 'j' {
			if m.viewport.activeItems.Count() > 0 {
				m.viewport.activeItems.SetFocus(!m.viewport.activeItems.Focused())
				return m, nil, true
			}
		}
	}
	if m.viewport.voiceSurface.Active() {
		if handled, cmd := m.viewport.voiceSurface.HandleKey(key); handled {
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
		if len(m.composer.pendingMedia) > 0 {
			m.composer.mediaExpanded = !m.composer.mediaExpanded
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
		m.engine.subAgentQuestionPanel.Toggle()
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
	// '?' opens the shortcuts overlay only when the composer is empty.
	// As documented in the release notes and reference guide:
	// "Fixed `?` in Vim Normal mode opening the shortcuts panel while the
	// prompt already contained text... `?` still opens shortcuts on an empty prompt."
	//
	// When text has already been typed in the composer, '?' is a regular
	// character (or a vim search/motion argument in normal mode) and must
	// be handled by the editor, not intercepted by the shortcuts overlay.
	if key.Code == '?' && key.Mod == 0 && m.composer.input.Value() == "" && !m.composerInVimInsert() &&
		!m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None {
		m.viewport.helpPanel.Open(2) // shortcuts
		m.ResetInputSelection()
		m.composer.promptHistory.Reset()
		return m, nil, true
	}
	// In vim mode the "!" belongs to the engine: the reference's
	// NORMAL-mode "!" inserts the character and switches to INSERT mode
	// (official-docs/cli/vim-editor-mode.md), so the shell command is
	// typed into the buffer and the submit path runs it. Arming the
	// bash mode here would swallow the character before the engine ever
	// saw it.
	if key.Code == '!' && key.Mod == 0 && m.composer.input.Value() == "" && !m.session.store.BashMode &&
		!m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() && m.overlays.panel.kind == panelregistry.None &&
		!m.vimEnabled() {
		m.session.store.BashMode = true
		if m.composer.undoMgr != nil {
			m.composer.undoMgr.Clear()
		}
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.session.store.BashMode && m.composer.input.Value() == "" {
		m.session.store.BashMode = false
		if m.composer.undoMgr != nil {
			m.composer.undoMgr.Clear()
		}
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.composer.input.Value() == "" && len(m.composer.pendingMedia) > 0 {
		m.composer.pendingMedia = m.composer.pendingMedia[:len(m.composer.pendingMedia)-1]
		if len(m.composer.pendingMedia) == 0 {
			m.composer.mediaExpanded = false
		}
		return m, nil, true
	}
	if key.Code == tea.KeyF2 && !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() {
		m.openRenamePanel()
		return m, nil, true
	}
	if key.Code == tea.KeyF5 && !m.overlays.resumePicker.Active() && !m.viewport.helpPanel.Active() && m.overlays.panel.kind == panelregistry.None && !m.composer.picker.Visible() && !m.viewport.voiceSurface.Active() {
		return m, m.viewport.voiceSurface.Start("f5"), true
	}
	if m.overlays.panel.kind != panelregistry.None {
		if handled, cmd := m.handlePanelKey(key); handled {
			return m, cmd, true
		}
	}
	if !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() && m.handleScrollKey(key) {
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
	if !m.overlays.resumePicker.Active() {
		return m, nil, false
	}
	return m, m.overlays.resumePicker.HandleKey(key, m.viewport.height), true
}

func (m *runMode) handleEscKeyMsg() (tea.Model, tea.Cmd) {
	if m.engine.exitConfirm {
		m.engine.exitConfirm = false
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
	armed := m.composer.escArmedFor
	m.composer.escArmedFor = ""

	// Priority 1: Modals, overlays, pickers, and side-question cards
	if m.viewport.helpPanel.Active() {
		m.viewport.helpPanel.Cancel()
		return m, nil
	}
	if m.composer.picker.Cancel() {
		return m, nil
	}
	if m.composer.atFilePicker.Visible() {
		m.composer.atFilePicker.Reset()
		return m, nil
	}
	if mod, cmd, handled := m.dismissActiveOverlay(); handled {
		return mod, cmd
	}
	if m.composer.mediaExpanded {
		m.composer.mediaExpanded = false
		return m, nil
	}

	// Priority 2: Execution cancellation (in-flight bash command or streaming turn)
	if m.session.store.BashCancel != nil {
		m.cancelBashCommand()
		return m, nil
	}
	if m.engine.streaming && m.engine.turnCancel != nil {
		m.engine.turnCancel()
		m.engine.turnCancel = nil
		// The reference does not report a cancelled turn as an error: it
		// answers with the composer's interrupted hint instead
		// (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`).
		m.engine.turnInterrupted = true
		return m, nil
	}

	// Priority 3: Composer input editing state (selection, draft text)
	compaction.TriggerCancel()
	if m.hasInputSelection() {
		m.ResetInputSelection()
		return m, nil
	}
	if m.session.store.BashMode {
		m.session.store.BashMode = false
		if m.composer.input.Value() == "" {
			return m, nil
		}
	}
	if m.composer.input.Value() != "" {
		// Cycle 383 (B-21): the first Esc of the pair only arms the
		// clear; the second one empties the composer.
		if armed != m.composer.input.Value() {
			m.composer.escArmedFor = m.composer.input.Value()
			return m, nil
		}
		m.composer.input.Reset()
		m.ResetInputSelection()
		m.session.store.BashMode = false
		m.composer.pastedBlocks = nil
		m.composer.nextPasteID = 0
		return m, nil
	}

	// Priority 4: Background announcement cards (dismissed on empty prompt when not streaming)
	if m.overlays.announceQueue.Active() != nil {
		m.overlays.announceQueue.Dismiss()
		if msg := m.overlays.announceQueue.SaveErrorMessage(); msg != "" {
			m.engine.response = append(m.engine.response, msg)
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
	if handled, cmd := m.composer.picker.UpdateKey(key); handled {
		m.composer.promptHistory.Reset()
		if cmd != nil {
			msg := cmd()
			if comp, ok := msg.(slashpicker.CompletedMsg); ok {
				m.composer.input.SetValue(comp.Text)
				m.composer.picker.Reset()
			}
		}
		return true
	}
	if m.composer.atFilePicker.HandleKey(key) {
		m.composer.promptHistory.Reset()
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
		if m.session.store.BashCancel != nil {
			m.cancelBashCommand()
		}
		if m.engine.exitConfirm {
			m.engine.quitting = true
			return m, tea.Quit, true
		}
		m.engine.exitConfirm = true
		m.engine.exitConfirmAt = time.Now()
		return m, nil, true
	}
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) {
		if m.hasInputSelection() {
			m.deleteInputSelection()
			m.composer.modeHintDismissed = true
			m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
			m.clearEligibilityNotice()
			m.composer.atFilePicker.Update()
			return m, nil, true
		}
		if m.composer.input.Value() == "" {
			m.engine.quitting = true
			return m, tea.Quit, true
		}
	}
	if key.Code == 'a' && key.Mod == 0 && m.composer.input.Value() == "" &&
		!m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() &&
		m.overlays.panel.kind == panelregistry.None &&
		m.engine.subAgentQuestionPanel.PendingCount() > 0 {
		m.engine.subAgentQuestionPanel.Toggle()
		return m, nil, true
	}
	if m.engine.exitConfirm {
		m.engine.exitConfirm = false
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
func (m *runMode) handleHelpModeNavigation(key tea.Key) (tea.Model, tea.Cmd, bool) {
	handled, cmd := m.viewport.helpPanel.Update(key)
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
				m.engine.response = append(m.engine.response, tuilib.SlashEchoLines("/help", "Exited /help command")...)
			}
			return m, nil, true
		}
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

	// Global exit: Ctrl+D on empty input exits immediately.
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) && m.composer.input.Value() == "" {
		m.engine.quitting = true
		return m, tea.Quit
	}

	// Cycle 383 (B-21): the `esc esc` pair is broken by any other key —
	// the first Esc arms the clear, and only a second Esc that follows
	// it empties the composer.
	if key.Code != tea.KeyEsc {
		m.composer.escArmedFor = ""
	}

	// Reset rapid enter burst counter if any key other than Enter is pressed.
	if key.Code != tea.KeyEnter {
		m.composer.enterBurstCount = 0
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
	if handled, cmd := m.viewport.activeItems.HandleKey(key); handled {
		return m, cmd
	}
	// The question picker is modal: it claims every key while it is
	// open, so nothing reaches the vim, Esc or Ctrl handlers.
	if m.engine.subAgentQuestionPanel.HandleKey(key) {
		return m, nil
	}
	// Global terminal exit on empty prompt.
	if m.vimEnabled() && !m.viewport.helpPanel.Active() && !m.composer.picker.Visible() && !m.overlays.resumePicker.Active() &&
		!m.isBtwCardVisible() && m.overlays.announceQueue.Active() == nil && !m.composer.mediaExpanded &&
		m.overlays.panel.kind == panelregistry.None {
		if cmd, handled := m.handleVimKey(key); handled {
			m.engine.exitConfirm = false
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
		if m.isBtwCardVisible() && !m.composer.picker.Visible() && !m.composer.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil
		}
	}
	return m.updateEditorKey(msg)
}

// ---- tui_update_keys_edit.go ----

func (m *runMode) handleKeyUp(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.composer.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.composer.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.composer.input.Value() == "" {
		if m.btwScrollBy(-1) {
			return m, nil, true
		}
	}
	if m.composer.input.Value() == "" {
		// 1. If currently streaming and there is an in-flight pending steer instruction, recall it for editing
		if m.engine.streaming {
			if len(m.session.store.SendingMessages) > 0 {
				last := len(m.session.store.SendingMessages) - 1
				msg := m.session.store.SendingMessages[last]
				m.session.store.SendingMessages = m.session.store.SendingMessages[:last]
				_, _ = m.popActiveSteer()
				m.composer.input.SetValue(msg)
				m.composer.modeHintDismissed = true
				m.viewport.rowsCacheDirty = true
				return m, nil, true
			}
			if msg, ok := m.popActiveSteer(); ok && msg != "" {
				m.composer.input.SetValue(msg)
				m.composer.modeHintDismissed = true
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
			m.composer.input.SetValue(strings.Join(allQueued, "\n"))
			m.composer.modeHintDismissed = true
			m.saveSession()
			return m, nil, true
		}
	}
	if m.composer.promptHistory.Index() > 0 || history.IsBlank(m.composer.input.Value()) {
		m.historyUp()
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) handleKeyDown(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.composer.picker.HandleKey(key) {
		return m, nil, true
	}
	if m.composer.atFilePicker.HandleKey(key) {
		return m, nil, true
	}
	if m.isBtwCardVisible() && m.composer.input.Value() == "" {
		if m.btwScrollBy(1) {
			return m, nil, true
		}
	}
	if m.composer.promptHistory.Index() > 0 {
		m.historyDown()
		return m, nil, true
	}
	if !m.viewport.activeItems.Hidden() && m.viewport.activeItems.Count() > 0 {
		offset := tuilib.InputCursorByteOffset(m.composer.input)
		if tuilib.IsOnLastLine(m.composer.input.Value(), offset) {
			m.viewport.activeItems.SetFocus(true)
			m.viewport.activeItems.SelectFirst()
			return m, nil, true
		}
	}
	return m, nil, false
}

func (m *runMode) handleEnterKey(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code != tea.KeyEnter {
		return m, nil, false
	}
	if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
		if m.engine.streaming {
			prompt := strings.TrimSpace(m.composer.input.Value())
			if prompt != "" {
				prompt = m.expandPastedPlaceholders(prompt)
				m.pushHistory(prompt)
				if m.session.store.QueueMode {
					if m.steerActiveTurn(prompt) {
						m.composer.input.Reset()
						m.ResetInputSelection()
						m.composer.promptHistory.Reset()
						m.composer.picker.Reset()
						m.composer.atFilePicker.Reset()
						m.composer.modeHintDismissed = true
						return m, nil, true
					}
				} else {
					m.session.store.StreamQueue = append(m.session.store.StreamQueue, prompt)
					m.composer.input.Reset()
					m.ResetInputSelection()
					m.composer.promptHistory.Reset()
					m.composer.picker.Reset()
					m.composer.atFilePicker.Reset()
					m.composer.modeHintDismissed = true
					return m, nil, true
				}
			}
			return m, nil, true
		}
		m.composer.input.InsertRune('\n')
		m.composer.modeHintDismissed = true
		m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
		m.composer.atFilePicker.Update()
		m.composer.promptHistory.Reset()
		return m, nil, true
	}
	if key.Mod.Contains(tea.ModShift) && key.Mod&(tea.ModCtrl) == 0 {
		m.composer.input.InsertRune('\n')
		m.composer.modeHintDismissed = true
		m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
		m.composer.atFilePicker.Update()
		m.composer.promptHistory.Reset()
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
	if !m.composer.lastEnterTime.IsZero() && now.Sub(m.composer.lastEnterTime) < 10*time.Millisecond {
		m.composer.enterBurstCount++
	} else {
		m.composer.enterBurstCount = 0
	}
	m.composer.lastEnterTime = now

	if m.composer.enterBurstCount > 0 {
		m.composer.input.InsertRune('\n')
		m.composer.modeHintDismissed = true
		m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
		m.composer.atFilePicker.Update()
		m.composer.promptHistory.Reset()
		return m, nil, true
	}

	if m.viewport.altScreen && m.viewport.pinnedPromptText != "" && m.composer.input.Value() == "" {
		if m.jumpToPinnedPrompt() {
			return m, nil, true
		}
	}
	if m.isBtwCardVisible() && m.composer.input.Value() == "" {
		m.DismissBtwResponse()
		return m, nil, true
	}
	if composer.BackslashNewline(m.composerSeams()) {
		return m, nil, true
	}
	if m.composer.atFilePicker.Visible() {
		if m.composer.atFilePicker.Complete(false) {
			m.composer.promptHistory.Reset()
		}
		return m, nil, true
	}
	return m, m.submitOrQueuePrompt(), true
}

func (m *runMode) handleWordMoveAndChords(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if key.Code == 'j' && key.Mod.Contains(tea.ModCtrl) {
		m.composer.input.InsertRune('\n')
		m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
		m.composer.atFilePicker.Update()
		m.composer.promptHistory.Reset()
		return m, nil, true
	}
	if key.Code == tea.KeyLeft {
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
			m.moveWord(true)
			m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
			m.composer.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.composer.input.Value() == "" && len(m.overlays.btwCards) > 1 {
			m.NavigateBtw(-1)
			return m, nil, true
		}
	}
	if key.Code == tea.KeyRight {
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
			m.moveWord(false)
			m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
			m.composer.promptHistory.Reset()
			return m, nil, true
		}
		if m.isBtwCardVisible() && m.composer.input.Value() == "" && len(m.overlays.btwCards) > 1 {
			m.NavigateBtw(1)
			return m, nil, true
		}
	}
	if key.Code == 'v' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
		// The paste key's image half (agy 1.4.9): an image on the
		// clipboard is attached, a dropped image file is attached, and
		// only a clipboard with neither stays text.
		m.composer.promptHistory.Reset()
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
	start, end := m.InputSelectionRange()
	if m.composer.undoMgr == nil {
		m.composer.undoMgr = composer.NewUndoManager(100)
	}
	return composer.Seams{
		Editor:           composer.NewTextArea(&m.composer.input),
		Picker:           m.composer.picker,
		Files:            m.composer.atFilePicker,
		History:          &m.composer.promptHistory,
		Selection:        composer.Range{Start: start, End: end},
		SelectAll:        m.selectAllInput,
		ClearSelection:   m.ResetInputSelection,
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
		DismissModeHint:        func() { m.composer.modeHintDismissed = true },
		UndoMgr:                m.composer.undoMgr,
	}
}

// ---- tui_update_mouse.go ----

func (m *runMode) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	oldW := m.viewport.width
	oldH := m.viewport.height
	m.viewport.width, m.viewport.height = msg.Width, msg.Height
	mdrender.SetCurrentWidth(msg.Width)
	if w := msg.Width - len(inputPrompt); w > 10 {
		m.composer.input.SetWidth(w)
	}
	if (oldW != msg.Width || oldW == 0) && len(m.engine.response) > 0 {
		m.reflowTranscript(msg.Width)
	} else if oldH != msg.Height || oldW != msg.Width {
		m.viewport.rowsCacheDirty = true
		m.viewport.viewCacheValid = false
		m.viewport.extraRowsValid = false
		m.viewport.rowsCache = wrapSlot{}
		m.viewport.rowsCacheN1 = wrapSlot{}
		m.viewport.rowsView = composedRows{}
	}
	m.viewport.activeItems.Clamp()
	return m, nil
}

func (m *runMode) updateMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.viewport.altScreen || ev.Button != tea.MouseLeft {
		return m, nil
	}
	if m.composer.picker.HandleClick(ev) {
		return m, nil
	}
	if m.composer.picker.Visible() && !m.isInputY(ev.Y) {
		m.composer.picker.Reset()
	}
	if m.viewport.pinnedPromptText != "" && (ev.Y == 0 || ev.Y == 1) {
		if m.jumpToPinnedPrompt() {
			return m, nil
		}
	}
	if m.isInputY(ev.Y) {
		m.ResetSelection()
		offset := m.inputOffsetAt(ev.X, ev.Y)
		m.setInputCursorOffset(offset)
		m.viewport.InputSelDragging = true
		m.viewport.InputSelStart = offset
		m.viewport.InputSelEnd = offset
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

func (m *runMode) updateMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.composer.picker.HandleHover(ev)
	if m.viewport.InputSelDragging && m.viewport.altScreen {
		offset := m.inputOffsetAt(ev.X, ev.Y)
		if offset != m.viewport.InputSelEnd {
			m.viewport.InputSelEnd = offset
			m.setInputCursorOffset(offset)
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

func (m *runMode) updateMouseReleaseInputSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.viewport.InputSelDragging = false
	if ev.Button == tea.MouseLeft {
		m.viewport.InputSelEnd = m.inputOffsetAt(ev.X, ev.Y)
	}
	start, end := m.InputSelectionRange()
	if start < end {
		val := m.composer.input.Value()
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

func (m *runMode) updateMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.viewport.InputSelDragging {
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

func (m *runMode) updateEditorFinished(msg editorFinishedMsg) (tea.Model, tea.Cmd) {
	if msg.tempPath != "" {
		defer func() { _ = os.Remove(msg.tempPath) }()
		if msg.err == nil {
			if data, err := os.ReadFile(msg.tempPath); err == nil {
				m.composer.input.SetValue(strings.TrimRight(string(data), "\n"))
			}
		}
	}
	if msg.err != nil {
		m.engine.response = append(m.engine.response, "Editor exited with error: "+msg.err.Error())
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
		if m.overlays.resumePicker.Active() && m.overlays.resumePicker.Query() != "" {
			m.overlays.resumePicker.SetCursorOn(!m.overlays.resumePicker.CursorOn())
			return m, resumepicker.BlinkCmd(), true
		}
		m.overlays.resumePicker.SetCursorOn(false)
		return m, nil, true
	case keybind.EscTimeoutMsg:
		if m.composer.altEscBuf.Pending {
			m.composer.altEscBuf.Pending = false
			_, _ = m.handleEscKey()
		}
		return m, nil, true
	case voice.ConnectDoneMsg:
		return m, m.viewport.voiceSurface.HandleConnectDone(msg), true
	case voice.TickMsg:
		return m, m.viewport.voiceSurface.HandleTick(), true
	case tea.PasteMsg:
		cmd := m.handlePasteContent(msg.Content)
		return m, cmd, true
	case pasteMediaMsg:
		mod, cmd := m.updatePasteMedia(msg)
		return mod, cmd, true
	case helppanel.CommandSelectedMsg:
		m.SelectCommand(msg.Command)
		return m, nil, true
	case helppanel.DismissedMsg:
		m.viewport.helpPanel.Close()
		return m, nil, true
	case slashpicker.CompletedMsg:
		m.composer.input.SetValue(msg.Text)
		m.composer.picker.Reset()
		return m, nil, true
	case slashpicker.DismissedMsg:
		m.composer.picker.Reset()
		return m, nil, true
	case modelpicker.ModelSelectedMsg:
		m.ApplyModel(msg.Label)
		return m, m.closePanel(), true
	case modelpicker.DismissedMsg:
		return m, m.closePanel(), true
	case taskspanel.TaskKilledMsg:
		if msg.Task != nil {
			m.killSelectedTask(msg.Task)
		}
		return m, nil, true
	case taskspanel.DismissedMsg:
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
		m.openTasksDetail(msg.ID, msg.IsSubagent)
		return m, nil, true
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

func (m *runMode) updateCompactDoneMsg(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m.appendCompactResult(msg)
	if msg.CustomInstructions != "" && msg.Err == nil && !msg.Aborted {
		return m, m.streamPrompt(msg.CustomInstructions)
	}
	return m, nil
}

func (m *runMode) updateBashDoneMsg(msg bashDoneMsg) (tea.Model, tea.Cmd) {
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

func (m *runMode) updateSpinnerTickMsg(msg sp.TickMsg) (tea.Model, tea.Cmd) {
	if m.engine.streaming {
		var cmd tea.Cmd
		m.engine.spinner, cmd = m.engine.spinner.Update(msg)
		m.engine.spinFrame++
		return m, cmd
	}
	return m, nil
}

func (m *runMode) updateUsageQuotaTickMsg() (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind != panelregistry.Usage {
		return m, nil
	}
	m.engine.quotaFrame++
	if m.overlays.panel.quota.state == quotaLoading && m.engine.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usagepanel.QuotaTick()
}

func (m *runMode) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactStart = msg.StartedAt
	m.session.store.CompactEnd = time.Time{}
	m.session.store.CompactSpinnerFrame = 0
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = 0
	m.session.store.CompactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *runMode) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.session.store.CompactEnd = msg.EndedAt
	m.session.store.CompactReason = msg.Reason
	m.session.store.CompactTokensBefore = msg.TokensBefore
	m.session.store.CompactTokensAfter = msg.EstimatedTokensAfter

	if msg.Summary != "" {
		requests := compaction.UserRequests(m.engine.session, compaction.MaxUserRequests)
		carriedTokens := compaction.EstimateTokens(compaction.CarriedBlock(requests))
		m.engine.session = []backend.Message{{
			Role:    "user",
			Content: compaction.ResumingBlock(requests, msg.Summary),
		}}
		if m.session.supervisor != nil {
			m.session.supervisor.Replace(m.engine.session)
		}
		m.engine.liveReplyIdx = -1

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

		m.engine.response = append(m.engine.response,
			compaction.DividerRow(m.viewport.width),
			compaction.StatsRow(
				m.viewport.width,
				0,
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

func (m *runMode) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.session.store.CompactStart.IsZero() || !m.session.store.CompactEnd.IsZero() {
		return m, nil
	}
	m.session.store.CompactSpinnerFrame++
	if m.session.store.CompactSpinnerFrame >= 8 {
		m.session.store.CompactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *runMode) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
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
func (m *runMode) updateCSTickMsg() (tea.Model, tea.Cmd) {
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

func (m *runMode) updateCompactEvents(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
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

func (m *runMode) updateTicksAndCompactMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
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
func (m *runMode) updateTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
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
		if m.consumeTasksDirty() {
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
func (m *runMode) updatePanelTickMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
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

func (m *runMode) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.overlays.login == nil {
		return m, nil, false
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.viewport.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.viewport.width, m.viewport.height = msg.Width, msg.Height
		m.overlays.login.Width, m.overlays.login.Height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.overlays.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.overlays.login.State == loginui.StateSelectMethod {
			m.overlays.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case *auth.AntigravityToken:
		return m, m.finishLogin(msg), true
	}
	newLM, cmd := m.overlays.login.Update(msg)
	if lm, ok := newLM.(*loginui.Model); ok {
		m.overlays.login = lm
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
func (m *runMode) finishLogin(token *auth.AntigravityToken) tea.Cmd {
	m.overlays.login = nil
	m.session.email = auth.EffectiveUserEmail()
	if m.engine.backendFor != nil {
		m.engine.backend = m.engine.backendFor(m.session.model)
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
func (m *runMode) altDispatchAction(letter rune, action string) {
	m.composer.altKeyFires = append(m.composer.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.engine.response = append(m.engine.response,
		uirows.NoticeDim("alt+"+string(letter)+" → "+action))
}

// handleEscKey runs the bare-Esc branch: delegates to handleEscKeyMsg
// to ensure unified priority across direct Esc and EscReleaseWindow tick.
func (m *runMode) handleEscKey() (tea.Cmd, bool) {
	_, cmd := m.handleEscKeyMsg()
	return cmd, true
}

// armEscReleaseTick returns a tea.Cmd that fires after keybind.EscReleaseWindow.
func (m *runMode) armEscReleaseTick() tea.Cmd {
	return tea.Tick(keybind.EscReleaseWindow, func(time.Time) tea.Msg {
		return keybind.EscTimeoutMsg{}
	})
}

func (m *runMode) canConsumeAltEsc() bool {
	if m.vimEnabled() ||
		m.overlays.panel.kind != panelregistry.None || m.viewport.helpPanel.Active() ||
		m.composer.picker.Visible() || m.composer.atFilePicker.Visible() ||
		m.overlays.resumePicker.Active() || m.viewport.voiceSurface.Active() ||
		m.composer.mediaExpanded || m.isBtwCardVisible() ||
		m.engine.streaming || m.session.store.BashCancel != nil ||
		m.hasInputSelection() {
		return false
	}
	return true
}

// consumeAltEsc is the alt-keytranslation entry point.
func (m *runMode) consumeAltEsc(msg tea.KeyMsg) (newMsg tea.KeyMsg, consumed bool, cmd tea.Cmd) {
	key := msg.Key()
	if !keybind.TermNeedsOptionMeta() {
		return msg, false, nil
	}

	if m.composer.keybindings == nil || len(m.composer.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	if !m.canConsumeAltEsc() {
		return msg, false, nil
	}

	if m.composer.altEscBuf.Pending {
		consumedKey := m.composer.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.composer.keybindings != nil && m.composer.keybindings.Has(r) },
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
		if m.composer.altEscBuf.Start(time.Now()) {
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
func (m *runMode) vimEnabled() bool { return m.composer.vimOn }

func (m *runMode) VimEnabled() bool { return m.vimEnabled() }

// vimInsertFirstEnabled reports the "Editor Mode › Insert First" child
// setting: when on, the prompt opens in Insert mode and a bare Enter
// submits instead of inserting a newline.
func (m *runMode) vimInsertFirstEnabled() bool { return m.composer.vimFirstOn }

// reloadVimSettings reads the two vim settings from the live
// settings.json. It is called once at TUI start-up and again when a slash
// panel closes (/config can change both).
func (m *runMode) reloadVimSettings() {
	m.composer.vimOn = false
	if raw, err := settings.Field("editorMode"); err == nil {
		m.composer.vimOn = configpanel.RawString(raw) == "vim"
	}
	m.composer.vimFirstOn = false
	if raw, err := settings.Field("vimInsertFirst"); err == nil {
		switch v := raw.(type) {
		case bool:
			m.composer.vimFirstOn = v
		case string:
			m.composer.vimFirstOn = v == "on" || v == "true"
		}
	}
}

// vimEngine returns the engine, building it from the current prompt on
// first use. Whenever the host has replaced the buffer behind the
// engine's back — a submit reset, a history recall, the picker's Tab
// substitution, a slash rewrite — the engine is re-seeded so it always
// tracks what is actually on screen.
func (m *runMode) vimEngine() *vim.State {
	if m.composer.vimEng == nil {
		m.composer.vimEng = vim.New(m.composer.input.Value())
	} else if m.composer.vimEng.Text() != m.composer.input.Value() {
		m.composer.vimEng.SetText(m.composer.input.Value())
	}
	// SetInsertFirst is a no-op when the setting is unchanged, so this
	// does not yank the user back into Insert mode on every keystroke.
	m.composer.vimEng.SetInsertFirst(m.vimInsertFirstEnabled())
	return m.composer.vimEng
}

// vimMode reports the mode the status row's badge shows.
//
// The engine is seeded the way the host seeds it for a key press, so the
// badge agrees with the mode the next key will be handled in. It is not
// built when vim mode is off: the badge is empty then, and the host
// keeps its "no engine without vim" invariant.
func (m *runMode) vimMode() vim.Mode {
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
func (m *runMode) syncVimInput() {
	eng := m.composer.vimEng
	if eng == nil {
		return
	}
	m.composer.input.SetValue(eng.Text())
	for r := m.composer.input.LineCount() - 1; r > eng.Row; r-- {
		m.composer.input.CursorUp()
	}
	m.composer.input.SetCursorColumn(eng.Col)
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
func (m *runMode) handleVimKey(key tea.Key) (tea.Cmd, bool) {
	eng := m.vimEngine()

	// When on the last line of the buffer, Down arrow or 'j' (in Normal mode)
	// switches focus into ActiveItems if any are running, matching PromptModel.Update (0x102c91d60).
	if (eng.Mode == vim.Normal && eng.Pending == "") || eng.Mode == vim.Insert {
		isDown := key.Code == tea.KeyDown
		if eng.Mode == vim.Normal {
			isDown = isDown || ((key.Code == 'j' || key.Text == "j") && key.Mod == 0)
		}
		if isDown && eng.Row >= len(eng.Lines)-1 {
			if !m.viewport.activeItems.Hidden() && m.viewport.activeItems.Count() > 0 {
				m.viewport.activeItems.SetFocus(true)
				m.viewport.activeItems.SelectFirst()
				return nil, true
			}
		}
	}

	// Custom Vim submit bindings from keybindings.json
	if m.composer.keybindings != nil {
		ks := keybind.KeyString(key)
		if ks != "" {
			if eng.Mode == vim.Insert {
				if act := m.composer.keybindings.ActionForExactKeyInScope(ks, "vim.insert."); act == "vim.insert.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.composer.vimEng.SetTextKeepingPending(m.composer.input.Value())
					m.syncVimInput()
					return cmd, true
				}
			} else if eng.Mode == vim.Normal {
				if act := m.composer.keybindings.ActionForExactKeyInScope(ks, "vim.normal."); act == "vim.normal.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.composer.vimEng.SetTextKeepingPending(m.composer.input.Value())
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
		m.composer.vimEng.SetTextKeepingPending(m.composer.input.Value())
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

// isInputY reports whether the screen row y falls within the input field.
func (m *runMode) isInputY(y int) bool {
	if m.composer.promptScreenY < 0 {
		return false
	}
	return y >= m.composer.promptScreenY && y < m.composer.promptScreenY+m.inputLineCount()
}

// inputLineCount returns the visual line count of the input field (at least 1).
func (m *runMode) inputLineCount() int {
	if m.composer.input.LineCount() > 0 {
		return m.composer.input.LineCount()
	}
	return 1
}

// hasInputSelection reports whether a non-empty selection is active in the input field.
func (m *runMode) hasInputSelection() bool {
	return m.viewport.InputSelStart != m.viewport.InputSelEnd
}

// inputOffsetAt maps screen coordinate (x, y) to a byte offset within m.composer.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// values it cannot know.
func (m *runMode) inputOffsetAt(x, y int) int {
	return inputselection.OffsetAt(m.composer.input.Value(), m.inputLineCount(), m.composer.promptScreenY, x, y)
}

// setInputCursorOffset moves the textarea cursor to the given byte offset in m.composer.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// value it cannot know.
func (m *runMode) setInputCursorOffset(offset int) {
	line, col := inputselection.CursorPosition(m.composer.input.Value(), offset)
	m.composer.input.MoveToBegin()
	for l := 0; l < line; l++ {
		m.composer.input.CursorDown()
	}
	m.composer.input.SetCursorColumn(col)
}

// selectAllInput selects all text in the input field, moves cursor to the end,
// and auto-copies the text to clipboard.
func (m *runMode) selectAllInput() (tea.Cmd, bool) {
	val := m.composer.input.Value()
	if len(val) == 0 {
		return nil, false
	}
	m.viewport.InputSelStart = 0
	m.viewport.InputSelEnd = len(val)
	m.setInputCursorOffset(len(val))
	_ = clipboard.Write(val)
	return m.copySelectionCmd(val), true
}

// deleteInputSelection deletes the selected substring from m.composer.input and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) deleteInputSelection() {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.InputSelectionRange()
	val := m.composer.input.Value()
	if newVal, ok := inputselection.DeleteRange(val, start, end); ok {
		m.composer.input.SetValue(newVal)
		m.setInputCursorOffset(start)
	}
	m.ResetInputSelection()
}

// replaceInputSelection replaces the selected substring with replacement and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) replaceInputSelection(replacement string) {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.InputSelectionRange()
	val := m.composer.input.Value()
	if newVal, ok := inputselection.ReplaceRange(val, start, end, replacement); ok {
		m.composer.input.SetValue(newVal)
		m.setInputCursorOffset(start + len(replacement))
	}
	m.ResetInputSelection()
}

// applyInputSelectionHighlight applies reverse-video styling to the selected
// range within the rendered inputView string.
//
// The arithmetic is the inputselection package's; the host supplies the
// selection's range and the value it is a range in.
func (m *runMode) applyInputSelectionHighlight(inputView string) string {
	start, end := m.InputSelectionRange()
	return inputselection.ApplyHighlight(inputView, m.composer.input.Value(), start, end)
}

var clipboardReadText = clipboard.Read

// insertPastedText is the single insertion point for both the terminal's
// bracketed paste and ctrl+v. It never submits.
func (m *runMode) insertPastedText(s string) tea.Cmd {
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
	if m.viewport.helpPanel.Active() || m.overlays.panel.kind != panelregistry.None {
		return nil
	}
	if m.overlays.resumePicker.Active() {
		m.overlays.resumePicker.TypeText(s)
		return nil
	}
	if m.hasInputSelection() {
		m.deleteInputSelection()
	}
	if m.composer.undoMgr != nil {
		m.composer.undoMgr.RecordBeforeChange(m.composer.input.Value(), tuilib.InputCursorByteOffset(m.composer.input), "paste")
	}

	// Folding threshold: >= 16 lines or > 1000 characters.
	// When folded, becomes "[Pasted text #%d +%d lines]" or "[Pasted text #%d %d chars]".
	toInsert := s
	lines := strings.Count(s, "\n") + 1
	charCount := len([]rune(s))
	if lines >= 16 || charCount > 1000 {
		if m.composer.pastedBlocks == nil {
			m.composer.pastedBlocks = make(map[string]string)
		}
		m.composer.nextPasteID++
		var placeholder string
		if lines >= 16 {
			placeholder = fmt.Sprintf("[Pasted text #%d +%d lines]", m.composer.nextPasteID, lines)
		} else {
			placeholder = fmt.Sprintf("[Pasted text #%d %d chars]", m.composer.nextPasteID, charCount)
		}
		m.composer.pastedBlocks[placeholder] = s
		toInsert = placeholder
	}

	m.composer.input.InsertString(toInsert)
	m.composer.picker.Update([]rune(tuilib.FirstLine(m.composer.input.Value())))
	m.composer.atFilePicker.Update()
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
func (m *runMode) applyModelArgument(arg string) tea.Cmd {
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
					if m.engine.streaming && m.engine.turnCancel != nil {
						m.engine.turnCancel()
						m.engine.turnCancel = nil
						m.engine.turnInterrupted = true
					}
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
	if m.engine.streaming && m.engine.turnCancel != nil {
		m.engine.turnCancel()
		m.engine.turnCancel = nil
		m.engine.turnInterrupted = true
	}
	_ = settings.SetField("model", entry.ID)
	m.session.model = entry.Label
	m.session.statusModel = tuilib.StripTier(entry.Label)
	m.syncBackendForModel()
	m.slashRespond("/model", "Model set to "+entry.Label)
	return nil
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *runMode) submitModelCompletion() (bool, tea.Cmd) {
	if !m.composer.picker.IsModelMode() {
		return false, nil
	}
	arg := strings.TrimSpace(m.composer.picker.Query())
	if entry, ok := modelargpopup.Resolve(arg); ok {
		m.composer.picker.Reset()
		m.composer.input.Reset()
		m.ResetInputSelection()
		m.composer.promptHistory.Reset()
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
	if sel := m.composer.picker.Selected(); sel != nil {
		m.composer.input.SetValue("/model " + sel.Name)
		m.composer.picker.Reset()
		m.composer.promptHistory.Reset()
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	m.composer.picker.Reset()
	m.composer.input.Reset()
	m.ResetInputSelection()
	m.composer.promptHistory.Reset()
	m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
	return true, nil
}
