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
	case "prompt.submit_alternate":
		if !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() {
			return m, m.submitAlternate(), true
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
	case "subagent.approve_fast":
		if !m.resumePicker.Active() {
			m.fastApproveSubagent()
			return m, nil, true
		}
	case "subagent.jump_to_waiting":
		if !m.resumePicker.Active() {
			if m.subAgentQuestionPanel != nil && m.subAgentQuestionPanel.PendingCount() > 0 {
				m.subAgentQuestionPanel.Toggle()
				return m, nil, true
			}
			if m.activeItems != nil && m.activeItems.Count() > 0 && !m.subAgentQuestionPanel.Active() {
				m.activeItems.SetFocus(!m.activeItems.Focused())
				return m, nil, true
			}
			if m.subAgentQuestionPanel != nil {
				m.subAgentQuestionPanel.Toggle()
				return m, nil, true
			}
		}
	case "voice.start_dictation":
		if !m.resumePicker.Active() && !m.voiceSurface.Active() && m.panel.kind == panelregistry.None {
			return m, m.voiceSurface.Start("keybind"), true
		}
	case "cli.cycle_mode":
		if !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() {
			m.cycleMode()
			return m, nil, true
		}
	case "item.rename":
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyF2}, m.height), true
		}
		if !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() {
			m.openRenamePanel()
			return m, nil, true
		}
	case "item.delete":
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyF4}, m.height), true
		}
	case "navigation.go_to_top", "navigation.top":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavTop, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.gotoTop()
			return m, nil, true
		}
	case "navigation.go_to_bottom", "navigation.bottom":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavBottom, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.gotoBottom()
			return m, nil, true
		}
	case "navigation.page_up":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavPageUp, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.scrollBy(-tuilib.ClampInt(m.vpRows-1, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.page_down":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavPageDown, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.scrollBy(tuilib.ClampInt(m.vpRows-1, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.half_page_up":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavHalfPageUp, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.scrollBy(-tuilib.ClampInt(m.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.half_page_down":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavHalfPageDown, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.altScreen && !m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() {
			m.scrollBy(tuilib.ClampInt(m.vpRows/2, 1, 1<<20))
			return m, nil, true
		}
	case "navigation.up":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavUp, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyUp}, m.height), true
		}
	case "navigation.down":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavDown, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyDown}, m.height), true
		}
	case "navigation.left":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavLeft, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyLeft}, m.height), true
		}
	case "navigation.right":
		if listPanelKind(m.panel.kind) {
			m.panel.cursor = tuiviewport.PanelCursor(m.panel.cursor, tuilib.NavRight, m.panelRegistry().RowCount(m.panel.kind))
			return m, nil, true
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: tea.KeyRight}, m.height), true
		}
	case "confirm.yes":
		if m.hasActivePermissionPrompt() {
			_, handled := m.handlePermissionKey(tea.Key{Code: 'y', Text: "y"})
			if handled {
				return m, nil, true
			}
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: 'y', Text: "y"}, m.height), true
		}
		if m.exitConfirm {
			return m, tea.Quit, true
		}
	case "confirm.no":
		if m.hasActivePermissionPrompt() {
			_, handled := m.handlePermissionKey(tea.Key{Code: 'n', Text: "n"})
			if handled {
				return m, nil, true
			}
		}
		if m.resumePicker.Active() {
			return m, m.resumePicker.HandleKey(tea.Key{Code: 'n', Text: "n"}, m.height), true
		}
		if m.exitConfirm {
			m.exitConfirm = false
			return m, nil, true
		}
	case "cli.suspend":
		return m, tea.Suspend, true
	case "cli.escape":
		if m.vimEnabled() && !m.helpPanel.Active() && !m.picker.Visible() && !m.atFilePicker.Visible() && m.panel.kind == panelregistry.None {
			return m, nil, false
		}
		if !m.streaming && m.store.BashCancel == nil && !m.helpPanel.Active() && !m.picker.Visible() && !m.atFilePicker.Visible() && m.panel.kind == panelregistry.None && !m.mediaExpanded {
			if m.exitConfirm {
				m.exitConfirm = false
				return m, nil, true
			}
		}
		model, cmd := m.handleEscKeyMsg()
		return model, cmd, true
	case "cli.enter":
		if m.vimEnabled() && !m.helpPanel.Active() && !m.picker.Visible() && !m.atFilePicker.Visible() && m.panel.kind == panelregistry.None {
			return m, nil, false
		}
		if !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None && !m.picker.Visible() {
			return m.handleEnterKey(tea.Key{Code: tea.KeyEnter})
		}
	case "prompt.insert_newline":
		if !m.resumePicker.Active() && !m.helpPanel.Active() && m.panel.kind == panelregistry.None {
			if m.vimEnabled() && m.vimEng != nil && m.vimEng.Mode == vim.Insert {
				m.vimEng.InsertNewline()
				m.syncVimInput()
			} else {
				m.input.InsertRune('\n')
			}
			m.modeHintDismissed = true
			m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
			m.atFilePicker.Update()
			m.promptHistory.Reset()
			return m, nil, true
		}
	case "edit.paste":
		m.promptHistory.Reset()
		return m, m.handlePasteContent(clipboardReadText()), true
	case "edit.undo":
		if m.undoMgr != nil {
			if snap, ok := m.undoMgr.Undo(m.input.Value(), tuilib.InputCursorByteOffset(m.input)); ok {
				m.store.ResetInputSelection()
				m.input.SetValue(snap.Value)
				m.setInputCursorOffset(snap.CursorOffset)
				m.modeHintDismissed = true
				m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
				m.clearEligibilityNotice()
				m.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.redo":
		if m.undoMgr != nil {
			if snap, ok := m.undoMgr.Redo(m.input.Value(), tuilib.InputCursorByteOffset(m.input)); ok {
				m.store.ResetInputSelection()
				m.input.SetValue(snap.Value)
				m.setInputCursorOffset(snap.CursorOffset)
				m.modeHintDismissed = true
				m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
				m.clearEligibilityNotice()
				m.atFilePicker.Update()
				return m, nil, true
			}
		}
	case "edit.yank":
		m.promptHistory.Reset()
		text := clipboardReadText()
		if text != "" {
			return m, m.insertPastedText(text), true
		}
		return m, nil, true
	case "navigation.tab":
		if m.handleTabCompletion(tea.Key{Code: tea.KeyTab}) {
			return m, nil, true
		}
		if m.isBtwCardVisible() && !m.picker.Visible() && !m.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil, true
		}
	case "vim.insert.submit", "vim.normal.submit":
		if m.vimEnabled() {
			m.syncVimInput()
			cmd := m.submitPrompt()
			m.vimEng.SetTextKeepingPending(m.input.Value())
			m.syncVimInput()
			return m, cmd, true
		}
	case "vim.insert.insert_newline":
		if m.vimEnabled() && m.vimEng != nil && m.vimEng.Mode == vim.Insert {
			m.vimEng.InsertNewline()
			m.syncVimInput()
			return m, nil, true
		}
	case "cli.test":
		return m, nil, true
	}
	return m, nil, false
}

func (m *runMode) fastApproveSubagent() bool {
	if m.taskSubagentManager != nil {
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
	if m.subAgentQuestions != nil {
		all := m.subAgentQuestions.AllPending()
		for scope, qs := range all {
			if len(qs) > 0 {
				q := qs[0]
				if len(q.Options) == 0 {
					continue
				}
				ans := q.Options[0]
				if err := m.subAgentQuestions.Answer(scope, q.ID, ans); err == nil {
					label := scope
					if label == subagentquestions.ParentKey {
						label = "you"
					}
					m.response = append(m.response, tuilib.SlashEchoLines("", "  ⎿  Approved action for "+label)...)
					return true
				}
			}
		}
	}
	return false
}

func (m *runMode) handleGlobalOrModalKeys(key tea.Key) (tea.Model, tea.Cmd, bool) {
	if m.keybindings != nil {
		if act := m.keybindings.ActionForKey(key); act != "" {
			if act == "cli.escape" && key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
				if m.store.BashCancel != nil {
					m.cancelBashCommand()
					return m, nil, true
				}
				if m.streaming && m.turnCancel != nil {
					model, cmd := m.handleEscKeyMsg()
					return model, cmd, true
				}
				if m.exitConfirm {
					m.quitting = true
					return m, tea.Quit, true
				}
				m.exitConfirm = true
				m.exitConfirmAt = time.Now()
				return m, nil, true
			}
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
			return m, m.submitAlternate(), true
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
	// '?' opens the shortcuts overlay only when the composer is empty.
	// As documented in the release notes and reference guide:
	// "Fixed `?` in Vim Normal mode opening the shortcuts panel while the
	// prompt already contained text... `?` still opens shortcuts on an empty prompt."
	//
	// When text has already been typed in the composer, '?' is a regular
	// character (or a vim search/motion argument in normal mode) and must
	// be handled by the editor, not intercepted by the shortcuts overlay.
	if key.Code == '?' && key.Mod == 0 && m.input.Value() == "" && !m.composerInVimInsert() &&
		!m.helpPanel.Active() && !m.picker.Visible() && !m.resumePicker.Active() && m.panel.kind == panelregistry.None {
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
		if m.undoMgr != nil {
			m.undoMgr.Clear()
		}
		return m, nil, true
	}
	if key.Code == tea.KeyBackspace && m.store.BashMode && m.input.Value() == "" {
		m.store.BashMode = false
		if m.undoMgr != nil {
			m.undoMgr.Clear()
		}
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
	if mod, cmd, handled := m.dismissActiveOverlay(); handled {
		return mod, cmd
	}
	if m.mediaExpanded {
		m.mediaExpanded = false
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
		m.pastedBlocks = nil
		m.nextPasteID = 0
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
	if handled, cmd := m.picker.UpdateKey(key); handled {
		m.promptHistory.Reset()
		if cmd != nil {
			msg := cmd()
			if comp, ok := msg.(slashpicker.CompletedMsg); ok {
				m.input.SetValue(comp.Text)
				m.picker.Reset()
			}
		}
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
// — and emits typed messages (CommandSelectedMsg, DismissedMsg) back to the host.
func (m *runMode) handleHelpModeNavigation(key tea.Key) (tea.Model, tea.Cmd, bool) {
	handled, cmd := m.helpPanel.Update(key)
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
				m.response = append(m.response, tuilib.SlashEchoLines("/help", "Exited /help command")...)
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
	if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) && m.input.Value() == "" {
		m.quitting = true
		return m, tea.Quit
	}

	// Cycle 383 (B-21): the `esc esc` pair is broken by any other key —
	// the first Esc arms the clear, and only a second Esc that follows
	// it empties the composer.
	if key.Code != tea.KeyEsc {
		m.escArmedFor = ""
	}

	// Reset rapid enter burst counter if any key other than Enter is pressed.
	if key.Code != tea.KeyEnter {
		m.enterBurstCount = 0
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
	// Global terminal exit on empty prompt.
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
	if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) {
		if m.isBtwCardVisible() && !m.picker.Visible() && !m.atFilePicker.Visible() {
			m.ToggleBtwExpanded()
			return m, nil
		}
	}
	return m.updateEditorKey(msg)
}

// ---- tui_update_keys_edit.go ----

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
	if m.input.Value() == "" {
		// 1. If currently streaming and there is an in-flight pending steer instruction, recall it for editing
		if m.streaming {
			if len(m.store.SendingMessages) > 0 {
				last := len(m.store.SendingMessages) - 1
				msg := m.store.SendingMessages[last]
				m.store.SendingMessages = m.store.SendingMessages[:last]
				_, _ = m.popActiveSteer()
				m.input.SetValue(msg)
				m.modeHintDismissed = true
				m.rowsCacheDirty = true
				return m, nil, true
			}
			if msg, ok := m.popActiveSteer(); ok && msg != "" {
				m.input.SetValue(msg)
				m.modeHintDismissed = true
				return m, nil, true
			}
		}

		// 2. If prompts are queued while streaming or idle, recall ALL queued prompts combined
		if len(m.store.StreamQueue) > 0 || m.store.QueuedPrompt != "" {
			var allQueued []string
			if m.store.QueuedPrompt != "" {
				allQueued = append(allQueued, m.store.QueuedPrompt)
				m.store.QueuedPrompt = ""
			}
			if len(m.store.StreamQueue) > 0 {
				allQueued = append(allQueued, m.store.StreamQueue...)
				m.store.StreamQueue = nil
			}
			m.input.SetValue(strings.Join(allQueued, "\n"))
			m.modeHintDismissed = true
			m.saveSession()
			return m, nil, true
		}
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
	if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
		if m.streaming {
			prompt := strings.TrimSpace(m.input.Value())
			if prompt != "" {
				prompt = m.expandPastedPlaceholders(prompt)
				m.pushHistory(prompt)
				if m.store.QueueMode {
					if m.steerActiveTurn(prompt) {
						m.input.Reset()
						m.store.ResetInputSelection()
						m.promptHistory.Reset()
						m.picker.Reset()
						m.atFilePicker.Reset()
						m.modeHintDismissed = true
						return m, nil, true
					}
				} else {
					m.store.StreamQueue = append(m.store.StreamQueue, prompt)
					m.input.Reset()
					m.store.ResetInputSelection()
					m.promptHistory.Reset()
					m.picker.Reset()
					m.atFilePicker.Reset()
					m.modeHintDismissed = true
					return m, nil, true
				}
			}
			return m, nil, true
		}
		m.input.InsertRune('\n')
		m.modeHintDismissed = true
		m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
		m.atFilePicker.Update()
		m.promptHistory.Reset()
		return m, nil, true
	}
	if key.Mod.Contains(tea.ModShift) && key.Mod&(tea.ModCtrl) == 0 {
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

	// Reference agy absorbUnbracketedPasteNewline (0x102da6f00):
	// In terminals lacking bracketed paste, multiline paste is emitted as a burst of
	// rapid keystrokes. Unmodified Enter keys arriving < 10ms apart increment
	// enterBurstCount. When enterBurstCount > 0, the Enter is absorbed as a newline
	// in the composer and does not submit the prompt.
	now := time.Now()
	if !m.lastEnterTime.IsZero() && now.Sub(m.lastEnterTime) < 10*time.Millisecond {
		m.enterBurstCount++
	} else {
		m.enterBurstCount = 0
	}
	m.lastEnterTime = now

	if m.enterBurstCount > 0 {
		m.input.InsertRune('\n')
		m.modeHintDismissed = true
		m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
		m.atFilePicker.Update()
		m.promptHistory.Reset()
		return m, nil, true
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
	return m, m.submitOrQueuePrompt(), true
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
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
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
		if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
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
	if m.undoMgr == nil {
		m.undoMgr = composer.NewUndoManager(100)
	}
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
		UndoMgr:                m.undoMgr,
	}
}

// ---- tui_update_mouse.go ----

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
	case helppanel.CommandSelectedMsg:
		m.SelectCommand(msg.Command)
		return m, nil, true
	case helppanel.DismissedMsg:
		m.helpPanel.Close()
		return m, nil, true
	case slashpicker.CompletedMsg:
		m.input.SetValue(msg.Text)
		m.picker.Reset()
		return m, nil, true
	case slashpicker.DismissedMsg:
		m.picker.Reset()
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
	if msg.gen != m.store.BashGen {
		return m, nil
	}
	m.finishBashCommand(msg)
	if len(m.store.BashQueue) > 0 {
		next := m.store.BashQueue[0]
		m.store.BashQueue = m.store.BashQueue[1:]
		return m, tea.Batch(m.startBashCommand(next), tea.ClearScreen)
	}
	return m, tea.ClearScreen
}

func (m *runMode) updateSpinnerTickMsg(msg sp.TickMsg) (tea.Model, tea.Cmd) {
	if m.streaming {
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		m.spinFrame++
		return m, cmd
	}
	return m, nil
}

func (m *runMode) updateUsageQuotaTickMsg() (tea.Model, tea.Cmd) {
	if m.panel.kind != panelregistry.Usage {
		return m, nil
	}
	m.quotaFrame++
	if m.panel.quota.state == quotaLoading && m.quotaFrame >= usageQuotaLoadingTicks {
		quotaStubRefills(m)
	}
	return m, usagepanel.QuotaTick()
}

func (m *runMode) updateCompactStartMsg(msg compactStartMsg) (tea.Model, tea.Cmd) {
	m.store.CompactStart = msg.StartedAt
	m.store.CompactEnd = time.Time{}
	m.store.CompactSpinnerFrame = 0
	m.store.CompactReason = msg.Reason
	m.store.CompactTokensBefore = 0
	m.store.CompactTokensAfter = 0
	return m, compactSpinnerTick()
}

func (m *runMode) updateCompactEndMsg(msg compactEndMsg) (tea.Model, tea.Cmd) {
	m.store.CompactEnd = msg.EndedAt
	m.store.CompactReason = msg.Reason
	m.store.CompactTokensBefore = msg.TokensBefore
	m.store.CompactTokensAfter = msg.EstimatedTokensAfter
	return m, nil
}

func (m *runMode) updateCompactSpinnerTickMsg() (tea.Model, tea.Cmd) {
	if m.store.CompactStart.IsZero() || !m.store.CompactEnd.IsZero() {
		return m, nil
	}
	m.store.CompactSpinnerFrame++
	if m.store.CompactSpinnerFrame >= 8 {
		m.store.CompactSpinnerFrame = 0
	}
	return m, compactSpinnerTick()
}

func (m *runMode) updatePluginBlinkMsg() (tea.Model, tea.Cmd) {
	// Cycle 381: the blink phase is the panel's own state — the panel
	// lives in internal/ui/views/pluginsui — so the host only flips it
	// and re-arms the tick while a blinking cursor is on screen.
	p := m.panel.plg
	if p == nil {
		return m, nil
	}
	if m.panel.kind == panelregistry.Plugin && p.Blinking() {
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
	if m.panel.kind != panelregistry.CodeSearch {
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
			m.tierAccount = msg.email
			m.tierFetched = msg.tier != ""
			if msg.tier != "" {
				m.tier = msg.tier
				// The tier was cached in the registry, and the /accounts
				// panel shows the cached tier, so its rows have to be
				// re-read.
				if m.panel.kind == panelregistry.Accounts {
					m.accountsPanel().Reload()
				}
			} else {
				// The server named no tier for this account (a free
				// or an ineligible one): the session shows the email
				// alone, not the previous account's tier and not a
				// placeholder.
				m.tier = ""
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
		if m.panel.kind == panelregistry.Usage {
			if msg.summary != nil {
				m.panel.quota.summary = msg.summary
				m.panel.quota.loadedAt = time.Now()
				m.panel.quota.state = quotaLoaded
				// The summary is cached for the session, so the
				// /accounts panel can show the active account's
				// quota without querying it a second time.
				m.cacheQuota(m.email, msg.summary)
			} else if msg.err != nil {
				m.panel.quota.fetchErr = msg.err.Error()
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
		if m.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyVerifyMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthResultMsg:
		if m.panel.kind == panelregistry.Provider {
			m.providerPanel().ApplyOAuthResultMsg(msg)
		}
		return m, nil, true
	case providerpanel.OAuthTickMsg:
		if m.panel.kind == panelregistry.Provider && m.panel.prov != nil {
			return m, m.panel.prov.ApplyOAuthTick(), true
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
	if m.login == nil {
		return m, nil, false
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.login.Width, m.login.Height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.login.State == loginui.StateSelectMethod {
			m.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case *auth.AntigravityToken:
		return m, m.finishLogin(msg), true
	}
	newLM, cmd := m.login.Update(msg)
	if lm, ok := newLM.(*loginui.Model); ok {
		m.login = lm
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
	m.login = nil
	m.email = auth.EffectiveUserEmail()
	if m.backendFor != nil {
		m.backend = m.backendFor(m.model)
	}
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// Cycle multi-account-switch: the sign-in becomes a known
	// account, so it can be switched back to later.
	if token != nil {
		acc, _, err := accounts.Register(token)
		if err == nil && acc.Email != "" {
			m.email = acc.Email
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
	m.altKeyFires = append(m.altKeyFires, altKeyFire{Letter: letter, Action: action})
	m.response = append(m.response,
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
	if m.panel.kind != panelregistry.None || m.helpPanel.Active() ||
		m.picker.Visible() || m.atFilePicker.Visible() ||
		m.resumePicker.Active() || m.voiceSurface.Active() ||
		m.mediaExpanded || m.isBtwCardVisible() ||
		m.streaming || m.store.BashCancel != nil ||
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

	if m.keybindings == nil || len(m.keybindings.RawBindings()) == 0 {
		return msg, false, nil
	}

	if !m.canConsumeAltEsc() {
		return msg, false, nil
	}

	if m.altEscBuf.Pending {
		consumedKey := m.altEscBuf.Feed(
			key,
			func(r rune) bool { return m.keybindings != nil && m.keybindings.Has(r) },
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
		if m.altEscBuf.Start(time.Now()) {
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
func (m *runMode) vimEnabled() bool { return m.vimOn }

func (m *runMode) VimEnabled() bool { return m.vimEnabled() }

// vimInsertFirstEnabled reports the "Editor Mode › Insert First" child
// setting: when on, the prompt opens in Insert mode and a bare Enter
// submits instead of inserting a newline.
func (m *runMode) vimInsertFirstEnabled() bool { return m.vimFirstOn }

// reloadVimSettings reads the two vim settings from the live
// settings.json. It is called once at TUI start-up and again when a slash
// panel closes (/config can change both).
func (m *runMode) reloadVimSettings() {
	m.vimOn = false
	if raw, err := settings.Field("editorMode"); err == nil {
		m.vimOn = configpanel.RawString(raw) == "vim"
	}
	m.vimFirstOn = false
	if raw, err := settings.Field("vimInsertFirst"); err == nil {
		switch v := raw.(type) {
		case bool:
			m.vimFirstOn = v
		case string:
			m.vimFirstOn = v == "on" || v == "true"
		}
	}
}

// vimEngine returns the engine, building it from the current prompt on
// first use. Whenever the host has replaced the buffer behind the
// engine's back — a submit reset, a history recall, the picker's Tab
// substitution, a slash rewrite — the engine is re-seeded so it always
// tracks what is actually on screen.
func (m *runMode) vimEngine() *vim.State {
	if m.vimEng == nil {
		m.vimEng = vim.New(m.input.Value())
	} else if m.vimEng.Text() != m.input.Value() {
		m.vimEng.SetText(m.input.Value())
	}
	// SetInsertFirst is a no-op when the setting is unchanged, so this
	// does not yank the user back into Insert mode on every keystroke.
	m.vimEng.SetInsertFirst(m.vimInsertFirstEnabled())
	return m.vimEng
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
	eng := m.vimEng
	if eng == nil {
		return
	}
	m.input.SetValue(eng.Text())
	for r := m.input.LineCount() - 1; r > eng.Row; r-- {
		m.input.CursorUp()
	}
	m.input.SetCursorColumn(eng.Col)
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
			if !m.activeItems.Hidden() && m.activeItems.Count() > 0 {
				m.activeItems.SetFocus(true)
				m.activeItems.SelectFirst()
				return nil, true
			}
		}
	}

	// Custom Vim submit bindings from keybindings.json
	if m.keybindings != nil {
		ks := keybind.KeyString(key)
		if ks != "" {
			if eng.Mode == vim.Insert {
				if act := m.keybindings.ActionForExactKeyInScope(ks, "vim.insert."); act == "vim.insert.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.vimEng.SetTextKeepingPending(m.input.Value())
					m.syncVimInput()
					return cmd, true
				}
			} else if eng.Mode == vim.Normal {
				if act := m.keybindings.ActionForExactKeyInScope(ks, "vim.normal."); act == "vim.normal.submit" {
					m.syncVimInput()
					cmd := m.submitPrompt()
					m.vimEng.SetTextKeepingPending(m.input.Value())
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
		m.vimEng.SetTextKeepingPending(m.input.Value())
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
	if m.promptScreenY < 0 {
		return false
	}
	return y >= m.promptScreenY && y < m.promptScreenY+m.inputLineCount()
}

// inputLineCount returns the visual line count of the input field (at least 1).
func (m *runMode) inputLineCount() int {
	if m.input.LineCount() > 0 {
		return m.input.LineCount()
	}
	return 1
}

// hasInputSelection reports whether a non-empty selection is active in the input field.
func (m *runMode) hasInputSelection() bool {
	return m.store.InputSelStart != m.store.InputSelEnd
}

// inputOffsetAt maps screen coordinate (x, y) to a byte offset within m.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// values it cannot know.
func (m *runMode) inputOffsetAt(x, y int) int {
	return inputselection.OffsetAt(m.input.Value(), m.inputLineCount(), m.promptScreenY, x, y)
}

// setInputCursorOffset moves the textarea cursor to the given byte offset in m.input.Value().
//
// The arithmetic is the inputselection package's; the host supplies the
// value it cannot know.
func (m *runMode) setInputCursorOffset(offset int) {
	line, col := inputselection.CursorPosition(m.input.Value(), offset)
	m.input.MoveToBegin()
	for l := 0; l < line; l++ {
		m.input.CursorDown()
	}
	m.input.SetCursorColumn(col)
}

// selectAllInput selects all text in the input field, moves cursor to the end,
// and auto-copies the text to clipboard.
func (m *runMode) selectAllInput() (tea.Cmd, bool) {
	val := m.input.Value()
	if len(val) == 0 {
		return nil, false
	}
	m.store.InputSelStart = 0
	m.store.InputSelEnd = len(val)
	m.setInputCursorOffset(len(val))
	_ = clipboard.Write(val)
	return m.copySelectionCmd(val), true
}

// deleteInputSelection deletes the selected substring from m.input and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) deleteInputSelection() {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.store.InputSelectionRange()
	val := m.input.Value()
	if newVal, ok := inputselection.DeleteRange(val, start, end); ok {
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start)
	}
	m.store.ResetInputSelection()
}

// replaceInputSelection replaces the selected substring with replacement and clears selection.
//
// The value arithmetic is the inputselection package's; the host supplies
// the value and the range, and owns the textarea's write.
func (m *runMode) replaceInputSelection(replacement string) {
	if !m.hasInputSelection() {
		return
	}
	start, end := m.store.InputSelectionRange()
	val := m.input.Value()
	if newVal, ok := inputselection.ReplaceRange(val, start, end, replacement); ok {
		m.input.SetValue(newVal)
		m.setInputCursorOffset(start + len(replacement))
	}
	m.store.ResetInputSelection()
}

// applyInputSelectionHighlight applies reverse-video styling to the selected
// range within the rendered inputView string.
//
// The arithmetic is the inputselection package's; the host supplies the
// selection's range and the value it is a range in.
func (m *runMode) applyInputSelectionHighlight(inputView string) string {
	start, end := m.store.InputSelectionRange()
	return inputselection.ApplyHighlight(inputView, m.input.Value(), start, end)
}

var clipboardReadText = clipboard.Read

// insertPastedText is the single insertion point for both the terminal's
// bracketed paste and ctrl+v. It never submits.
func (m *runMode) insertPastedText(s string) tea.Cmd {
	s = clipboard.NormalizeText(s)
	if s == "" {
		return nil
	}
	if m.panel.kind == panelregistry.Rename {
		// Cycle 381: the /rename panel owns its input, so the paste
		// goes through the panel.
		return m.renamePanel().HandlePaste(s)
	}
	if m.panel.kind == panelregistry.Provider {
		return m.providerPanel().HandlePaste(s)
	}
	if m.helpPanel.Active() || m.panel.kind != panelregistry.None {
		return nil
	}
	if m.resumePicker.Active() {
		m.resumePicker.TypeText(s)
		return nil
	}
	if m.hasInputSelection() {
		m.deleteInputSelection()
	}
	if m.undoMgr != nil {
		m.undoMgr.RecordBeforeChange(m.input.Value(), tuilib.InputCursorByteOffset(m.input), "paste")
	}

	// Folding threshold: >= 16 lines or > 1000 characters.
	// When folded, becomes "[Pasted text #%d +%d lines]" or "[Pasted text #%d %d chars]".
	toInsert := s
	lines := strings.Count(s, "\n") + 1
	charCount := len([]rune(s))
	if lines >= 16 || charCount > 1000 {
		if m.pastedBlocks == nil {
			m.pastedBlocks = make(map[string]string)
		}
		m.nextPasteID++
		var placeholder string
		if lines >= 16 {
			placeholder = fmt.Sprintf("[Pasted text #%d +%d lines]", m.nextPasteID, lines)
		} else {
			placeholder = fmt.Sprintf("[Pasted text #%d %d chars]", m.nextPasteID, charCount)
		}
		m.pastedBlocks[placeholder] = s
		toInsert = placeholder
	}

	m.input.InsertString(toInsert)
	m.picker.Update([]rune(tuilib.FirstLine(m.input.Value())))
	m.atFilePicker.Update()
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
					if m.model == mdl.ID || m.model == mdl.DisplayName {
						m.slashRespond("/model", "Model already set to "+mdl.DisplayName)
						return nil
					}
					if m.streaming && m.turnCancel != nil {
						m.turnCancel()
						m.turnCancel = nil
						m.turnInterrupted = true
					}
					_ = settings.SetField("model", mdl.ID)
					m.model = mdl.ID
					m.statusModel = tuilib.StripTier(mdl.DisplayName)
					m.slashRespond("/model", "Model set to "+mdl.DisplayName)
					return nil
				}
			}
		}
		m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
		return nil
	}
	if m.model == entry.Label {
		m.slashRespond("/model", "Model already set to "+entry.Label)
		return nil
	}
	if m.streaming && m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
		m.turnInterrupted = true
	}
	_ = settings.SetField("model", entry.ID)
	m.model = entry.Label
	m.statusModel = tuilib.StripTier(entry.Label)
	m.syncBackendForModel()
	m.slashRespond("/model", "Model set to "+entry.Label)
	return nil
}

// submitModelCompletion runs the Enter semantics of the "/model <query>"
// popup. It reports whether it consumed the key.
func (m *runMode) submitModelCompletion() (bool, tea.Cmd) {
	if !m.picker.IsModelMode() {
		return false, nil
	}
	arg := strings.TrimSpace(m.picker.Query())
	if entry, ok := modelargpopup.Resolve(arg); ok {
		m.picker.Reset()
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		if m.model == entry.Label {
			m.slashRespond("/model", "Model already set to "+entry.Label)
			return true, nil
		}
		_ = settings.SetField("model", entry.ID)
		m.model = entry.Label
		m.statusModel = tuilib.StripTier(entry.Label)
		m.syncBackendForModel()
		m.slashRespond("/model", "Model set to "+entry.Label)
		return true, nil
	}
	// The argument names no model: the highlighted row is completed into
	// the composer instead of running the command.
	if sel := m.picker.Selected(); sel != nil {
		m.input.SetValue("/model " + sel.Name)
		m.picker.Reset()
		m.promptHistory.Reset()
		return true, nil
	}
	// Nothing matches at all: the command runs and reports the argument.
	m.picker.Reset()
	m.input.Reset()
	m.store.ResetInputSelection()
	m.promptHistory.Reset()
	m.slashRespond("/model", modelargpopup.InvalidMessage(arg))
	return true, nil
}
