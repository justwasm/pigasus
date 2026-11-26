package tui

import (
	"os"
	"strings"

	sp "charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
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
	"github.com/justwasm/pigasus/internal/ui/views/mentionpicker"
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
	"github.com/justwasm/pigasus/internal/ui/views/viewerpanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// ---- tui_update_mouse.go ----

const (
	maxTerminalWidth  = 2000
	maxTerminalHeight = 1000
)

// isPlausibleWindowSize guards against negative or implausibly huge resize
// events (e.g. from stray escape sequences or corrupted terminal reports)
// that would otherwise cause freezing, division by zero, or unbounded allocations.
func isPlausibleWindowSize(msg tea.WindowSizeMsg) bool {
	return msg.Width > 0 && msg.Height > 0 && msg.Width <= maxTerminalWidth && msg.Height <= maxTerminalHeight
}

func (m *RootModel) updateWindowSize(msg tea.WindowSizeMsg) (tea.Model, tea.Cmd) {
	if !isPlausibleWindowSize(msg) {
		return m, nil
	}
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
	m.resizeAllTerminalPanels()
	return m, nil
}

func (m *RootModel) updateMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind == panelregistry.Terminal {
		if tp := m.terminalPanel(); tp != nil {
			relEv := ev
			relEv.Y -= m.overlays.panelScreenY
			if handled, cmd := tp.HandleMouse(relEv, false, false); handled {
				return m, cmd
			}
		}
	}
	if (!m.viewport.altScreen && m.overlays.panel.kind == panelregistry.None) || ev.Button != tea.MouseLeft {
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
	if m.IsScrollIndicatorCell(ev.X, ev.Y) {
		m.gotoBottom()
		return m, nil
	}
	if m.prompt.IsInputY(ev.Y) {
		m.ResetSelection()
		m.ResetBtwSelection()
		offset := m.prompt.InputOffsetAt(ev.X, ev.Y)
		// Vim mode: a press outside Insert mode places the engine's
		// cursor, and the drag that follows hands its range to the
		// engine as a characterwise Visual selection (agy 1.5.17).
		//
		// The anchor drag starts first and the engine's click second, so
		// the engine's Synced caret (clamped to the last character in
		// Normal mode) is the one left on the textarea: a raw offset
		// would show a phantom caret one past the text.
		if m.vimEnabled() && m.vimBridge().MousePending() {
			m.prompt.BeginSelection(offset)
			m.vimBridge().MouseClick(offset)
			return m, nil
		}
		if m.vimEnabled() {
			m.vimBridge().ClearMouseSelection()
		}
		// The press is counted before it begins the drag: a second
		// press on the same offset inside DoubleClickWindow is a
		// double-click, and a double-click selects the whole word.
		if m.prompt.ClickAt(offset) >= 2 {
			m.prompt.BeginWordSelection(offset)
			return m, nil
		}
		m.prompt.BeginSelection(offset)
		return m, nil
	}
	if m.IsBtwCardY(ev.Y) {
		m.ResetSelection()
		m.ResetInputSelection()
		m.prompt.ResetClickTracker()
		cardRelY := ev.Y - m.overlays.btwScreenY
		m.overlays.btwSelection.SelDragging = true
		m.overlays.btwSelection.SelStartRow = cardRelY
		m.overlays.btwSelection.SelStartCol = ev.X
		m.overlays.btwSelection.SelEndRow = cardRelY
		m.overlays.btwSelection.SelEndCol = ev.X
		return m, nil
	}
	if m.IsPanelY(ev.Y) {
		m.ResetSelection()
		m.ResetInputSelection()
		m.ResetBtwSelection()
		m.prompt.ResetClickTracker()
		panelRelY := ev.Y - m.overlays.panelScreenY
		m.overlays.panelSelection.SelDragging = true
		m.overlays.panelSelection.SelStartRow = panelRelY
		m.overlays.panelSelection.SelStartCol = ev.X
		m.overlays.panelSelection.SelEndRow = panelRelY
		m.overlays.panelSelection.SelEndCol = ev.X
		return m, nil
	}
	m.ResetInputSelection()
	m.ResetBtwSelection()
	m.ResetPanelSelection()
	m.prompt.ResetClickTracker()
	avail := m.viewportHeight()
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, avail, m.viewport.width)
	if !ok {
		return m, nil
	}
	m.viewport.autoScroll = false
	stickyOffset := 0
	if m.viewport.pinnedPromptText != "" || m.viewport.stickyBarRow != "" {
		stickyOffset = 2
	}
	docRow := m.viewport.scroll + row - stickyOffset
	if docRow < 0 {
		docRow = 0
	}
	m.viewport.SelDragging = true
	m.viewport.SelStartRow, m.viewport.SelStartCol = row, col
	m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
	m.viewport.selStartDocRow = docRow
	m.viewport.selEndDocRow = docRow
	m.viewport.viewCacheValid = false
	return m, nil
}

func (m *RootModel) updateMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.prompt.picker.HandleHover(ev)
	if m.overlays.panel.kind == panelregistry.Terminal && !m.overlays.panelSelection.SelDragging {
		if tp := m.terminalPanel(); tp != nil {
			relEv := ev
			relEv.Y -= m.overlays.panelScreenY
			if handled, cmd := tp.HandleMouse(relEv, false, true); handled {
				return m, cmd
			}
		}
	}
	if m.prompt.InputSelectionDragging() && m.viewport.altScreen {
		offset := m.prompt.InputOffsetAt(ev.X, ev.Y)
		m.prompt.DragSelection(offset)
		// Vim mode: the drag's range is the engine's Visual selection,
		// so the operator that follows acts on what the user sees.
		if m.vimEnabled() && m.vimBridge().MousePending() {
			start, end := m.prompt.InputSelectionRange()
			m.vimBridge().SelectRange(start, end)
		}
		return m, nil
	}
	if m.overlays.btwSelection.SelDragging && m.viewport.altScreen {
		cardRelY := ev.Y - m.overlays.btwScreenY
		if cardRelY < 0 {
			cardRelY = 0
		}
		if cardRelY >= m.overlays.btwHeight {
			cardRelY = m.overlays.btwHeight - 1
		}
		if cardRelY != m.overlays.btwSelection.SelEndRow || ev.X != m.overlays.btwSelection.SelEndCol {
			m.overlays.btwSelection.SelEndRow = cardRelY
			m.overlays.btwSelection.SelEndCol = ev.X
		}
		return m, nil
	}
	if m.overlays.panelSelection.SelDragging && (m.viewport.altScreen || m.overlays.panel.kind != panelregistry.None) {
		panelRelY := ev.Y - m.overlays.panelScreenY
		if panelRelY < 0 {
			panelRelY = 0
		}
		if panelRelY >= m.overlays.panelHeight {
			panelRelY = m.overlays.panelHeight - 1
		}
		if panelRelY != m.overlays.panelSelection.SelEndRow || ev.X != m.overlays.panelSelection.SelEndCol {
			m.overlays.panelSelection.SelEndRow = panelRelY
			m.overlays.panelSelection.SelEndCol = ev.X
		}
		return m, nil
	}
	if !m.viewport.SelDragging || !m.viewport.altScreen {
		return m, nil
	}
	avail := m.viewportHeight()
	stickyOffset := 0
	if m.viewport.pinnedPromptText != "" || m.viewport.stickyBarRow != "" {
		stickyOffset = 2
	}
	if ev.Y < 0 {
		m.viewport.autoScroll = false
		m.scrollBy(-1)
		col := tuilib.ClampInt(ev.X, 0, m.viewport.width-1)
		m.viewport.SelEndRow = 0
		m.viewport.SelEndCol = col
		m.viewport.selEndDocRow = m.viewport.scroll
		m.viewport.viewCacheValid = false
		return m, nil
	}
	if ev.Y >= avail {
		m.viewport.autoScroll = false
		m.scrollBy(1)
		col := tuilib.ClampInt(ev.X, 0, m.viewport.width-1)
		m.viewport.SelEndRow = avail - 1
		m.viewport.SelEndCol = col
		m.viewport.selEndDocRow = m.viewport.scroll + avail - 1 - stickyOffset
		m.viewport.viewCacheValid = false
		return m, nil
	}
	row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, avail, m.viewport.width)
	if ok {
		docRow := m.viewport.scroll + row - stickyOffset
		if docRow < 0 {
			docRow = 0
		}
		if row != m.viewport.SelEndRow || col != m.viewport.SelEndCol || docRow != m.viewport.selEndDocRow {
			m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
			m.viewport.selEndDocRow = docRow
			m.viewport.viewCacheValid = false
		}
	}
	return m, nil
}

func (m *RootModel) updateMouseReleaseBtwSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.overlays.btwSelection.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.ResetBtwSelection()
		return m, nil
	}
	cardRelY := ev.Y - m.overlays.btwScreenY
	if cardRelY < 0 {
		cardRelY = 0
	}
	if cardRelY >= m.overlays.btwHeight {
		cardRelY = m.overlays.btwHeight - 1
	}
	m.overlays.btwSelection.SelEndRow = cardRelY
	m.overlays.btwSelection.SelEndCol = ev.X

	text := m.btwSelectionText()
	m.ResetBtwSelection()
	if text != "" {
		_ = clipboard.Write(text)
		return m, m.copySelectionCmd(text)
	}
	return m, nil
}

// updateMouseReleaseInputSel finishes a composer drag.
//
// The release writes nothing to the clipboard: agy 1.5.17 requires a
// mere drag to leave the user's clipboard intact, so the copy is
// Ctrl+C's job and the cut is Ctrl+X's. The highlight stays up after the
// release, which is what makes the following Backspace/Delete, typing, a
// paste or a Vim operator act on it.
func (m *RootModel) updateMouseReleaseInputSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	offset := m.prompt.InputOffsetAt(ev.X, ev.Y)
	m.prompt.EndSelection(offset, ev.Button == tea.MouseLeft)
	// Vim mode: the highlight the engine holds is the Visual selection,
	// so a press-and-drag in Normal mode leaves the engine in Visual
	// mode with the dragged range (agy 1.5.17). A single click selects
	// nothing and leaves only the caret.
	if m.vimEnabled() && m.vimBridge().MousePending() {
		start, end := m.prompt.InputSelectionRange()
		if !m.vimBridge().SelectRange(start, end) {
			m.vimBridge().MouseClick(offset)
		}
	}
	return m, nil
}

func (m *RootModel) updateMouseReleasePanelSel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.overlays.panelSelection.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.ResetPanelSelection()
		return m, nil
	}
	panelRelY := ev.Y - m.overlays.panelScreenY
	if panelRelY < 0 {
		panelRelY = 0
	}
	if panelRelY >= m.overlays.panelHeight {
		panelRelY = m.overlays.panelHeight - 1
	}
	m.overlays.panelSelection.SelEndRow = panelRelY
	m.overlays.panelSelection.SelEndCol = ev.X

	text := m.panelSelectionText()
	m.ResetPanelSelection()
	if text != "" {
		_ = clipboard.Write(text)
		return m, m.copySelectionCmd(text)
	}
	return m, nil
}

func (m *RootModel) updateMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.overlays.panel.kind == panelregistry.Terminal && !m.overlays.panelSelection.SelDragging {
		if tp := m.terminalPanel(); tp != nil {
			relEv := ev
			relEv.Y -= m.overlays.panelScreenY
			if handled, cmd := tp.HandleMouse(relEv, true, false); handled {
				return m, cmd
			}
		}
	}
	if m.prompt.InputSelectionDragging() {
		return m.updateMouseReleaseInputSel(ev)
	}
	if m.overlays.btwSelection.SelDragging {
		return m.updateMouseReleaseBtwSel(ev)
	}
	if m.overlays.panelSelection.SelDragging {
		return m.updateMouseReleasePanelSel(ev)
	}
	if !m.viewport.SelDragging {
		return m, nil
	}
	m.viewport.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.ResetSelection()
		return m, nil
	}
	avail := m.viewportHeight()
	stickyOffset := 0
	if m.viewport.pinnedPromptText != "" || m.viewport.stickyBarRow != "" {
		stickyOffset = 2
	}
	if ev.Y < 0 {
		m.viewport.SelEndRow = 0
		m.viewport.SelEndCol = tuilib.ClampInt(ev.X, 0, m.viewport.width-1)
		m.viewport.selEndDocRow = m.viewport.scroll
	} else if ev.Y >= avail {
		m.viewport.SelEndRow = avail - 1
		m.viewport.SelEndCol = tuilib.ClampInt(ev.X, 0, m.viewport.width-1)
		m.viewport.selEndDocRow = m.viewport.scroll + avail - 1 - stickyOffset
	} else {
		row, col, ok := tuiviewport.TranscriptCellAt(ev.X, ev.Y, avail, m.viewport.width)
		if ok {
			docRow := m.viewport.scroll + row - stickyOffset
			if docRow < 0 {
				docRow = 0
			}
			m.viewport.SelEndRow, m.viewport.SelEndCol = row, col
			m.viewport.selEndDocRow = docRow
		}
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

func preservesTranscriptRows(msg tea.Msg) bool {
	switch msg.(type) {
	case tasksTickMsg, tasksChangedMsg,
		sp.TickMsg,
		compactSpinnerTickMsg,
		btwTickMsg,
		statusLineExternalTickMsg,
		usageQuotaTickMsg,
		pluginsui.BlinkMsg,
		resumepicker.BlinkMsg,
		keybind.EscTimeoutMsg,
		voice.TickMsg,
		codesearchpanel.TickMsg,
		tea.MouseMotionMsg:
		return true
	}
	return false
}

func (m *RootModel) isIdleMouseMotion(msg tea.Msg) bool {
	if _, ok := msg.(tea.MouseMotionMsg); !ok {
		return false
	}
	if m.overlays.panel.kind == panelregistry.Terminal && m.overlays.panel.term != nil &&
		(m.overlays.panel.term.MenuOpen() || m.overlays.panel.term.MouseTrackingEnabled()) {
		return false
	}
	return !m.viewport.SelDragging &&
		!m.prompt.InputSelectionDragging() &&
		!m.overlays.btwSelection.SelDragging &&
		!m.overlays.panelSelection.SelDragging &&
		!m.prompt.picker.Visible()
}

func (m *RootModel) updateMouseAndMiscMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case pluginReloadMsg:
		m.reloadPluginsAndMCP()
		return m, nil, true
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
			// A vim-owned Esc was already dispatched on the key press;
			// only a held-back one is released here.
			delivered := m.prompt.altEscBuf.Delivered
			m.prompt.altEscBuf.Pending = false
			m.prompt.altEscBuf.Delivered = false
			if !delivered {
				m.releaseBufferedEsc()
			}
		}
		return m, nil, true
	case voice.ConnectDoneMsg:
		return m, m.audio.Surface.HandleConnectDone(msg), true
	case voice.TickMsg:
		return m, m.audio.Surface.HandleTick(), true
	case voice.DictationDoneMsg:
		return m, m.audio.Surface.HandleDictationDone(msg), true
	case tea.PasteMsg:
		if m.overlays.panel.kind == panelregistry.Terminal {
			if tp := m.terminalPanel(); tp != nil {
				tp.HandlePaste(msg.Content)
			}
			return m, nil, true
		}
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
		m.prompt.SetInputCursorOffset(len(msg.Text))
		m.prompt.input.CursorEnd()
		if !m.prompt.picker.Visible() {
			m.prompt.picker.Reset()
		}
		if m.prompt.vimEng != nil {
			m.prompt.vimEng.SetTextKeepingPending(msg.Text)
			m.prompt.vimEng.Row = len(m.prompt.vimEng.Lines) - 1
			if m.prompt.vimEng.Row >= 0 && m.prompt.vimEng.Row < len(m.prompt.vimEng.Lines) {
				m.prompt.vimEng.Col = len([]rune(m.prompt.vimEng.Lines[m.prompt.vimEng.Row]))
			}
			m.syncVimInput()
		}
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
		return m, m.ensureTasksTick(), true
	case taskspanel.TaskKilledMsg:
		if msg.Task != nil {
			m.killSelectedTask(msg.Task)
		}
		return m, m.ensureTasksTick(), true
	case taskspanel.TaskRemovedMsg:
		return m, m.ensureTasksTick(), true
	case taskspanel.DismissedMsg:
		if m.swarm.fromBoard {
			m.swarm.fromBoard = false
			m.OpenSwarmBoard()
			return m, m.closePanel(), true
		}
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
