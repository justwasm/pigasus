package tui

import (
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

	tea "charm.land/bubbletea/v2"
	_ "github.com/justwasm/pigasus/internal/app/agentturn/plugins/all"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/tools/bashmode"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/programstatus"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/palette"
)

// ---- tui_view.go ----

func (m *RootModel) clipViewBody(body string, cursorY int) (string, int) {
	if m.viewport.height <= 0 {
		return body, cursorY
	}
	rows := tuilib.ViewRows(body)
	drop := rows - m.viewport.height
	if drop < 0 {
		drop = 0
	}
	if drop > 0 {
		lines := strings.Split(body, "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		if drop < len(lines) {
			body = strings.Join(lines[drop:], "\n")
		} else {
			body = ""
		}
		cursorY -= drop
		if cursorY < 0 {
			cursorY = -1
		}
	}
	m.prompt.picker.ClipBody(drop)
	if m.prompt.promptBodyStart >= drop {
		m.prompt.promptScreenY = m.prompt.promptBodyStart - drop
	}
	if m.overlays.btwBodyStart >= 0 {
		m.overlays.btwScreenY = m.overlays.btwBodyStart - drop
	} else {
		m.overlays.btwScreenY = -1
	}
	if m.viewport.scrollIndBodyStart >= 0 && m.viewport.scrollIndBodyStart >= drop {
		m.viewport.scrollIndScreenY = m.viewport.scrollIndBodyStart - drop
	} else {
		m.viewport.scrollIndScreenY = -1
	}
	return body, cursorY
}

// isFullViewportPanel reports whether the active panel occupies the full terminal viewport,
// replacing the conversation transcript, live rows, and prompt box.
func (m *RootModel) isFullViewportPanel() bool {
	kind := m.overlays.panel.kind
	if kind == panelregistry.None {
		return false
	}
	if kind == panelregistry.Changelog || kind == panelregistry.Diff || kind == panelregistry.FeatDiff || kind == panelregistry.Terminal || kind == panelregistry.Logs {
		return true
	}
	if kind == panelregistry.Tasks && m.overlays.panel.tp != nil && m.overlays.panel.tp.Detail() {
		return true
	}
	if kind == panelregistry.Agents && m.overlays.panel.ag != nil && m.overlays.panel.ag.DetailMode() {
		return true
	}
	if kind == panelregistry.Skills && m.overlays.panel.sk != nil && m.overlays.panel.sk.Detail() {
		return true
	}
	return false
}

// requiresAltScreen reports whether the current view/panel demands alternate screen mode.
func (m *RootModel) requiresAltScreen() bool {
	if m.isAltScreenDisabled() {
		return false
	}
	if m.isFullViewportPanel() {
		return true
	}
	kind := m.overlays.panel.kind
	if kind == panelregistry.CodeSearch || kind == panelregistry.Rewind {
		return true
	}
	return false
}

// isAltScreenDisabled reports whether altScreenMode is explicitly disabled in settings.
func (m *RootModel) isAltScreenDisabled() bool {
	stored, err := settings.CachedSnapshot().Field("altScreenMode")
	if err == nil && stored != nil {
		if s, ok := stored.(string); ok {
			s = strings.ToLower(strings.TrimSpace(s))
			if s == "never" || s == "off" || s == "false" || s == "0" {
				return true
			}
		}
	}
	return false
}

// effectiveAltScreen returns the actual alternate screen state for the frame:
// true if session altScreen is enabled, or if the current active view demands altScreen.
func (m *RootModel) effectiveAltScreen() bool {
	if m.isAltScreenDisabled() {
		return false
	}
	return m.viewport.altScreen || m.requiresAltScreen()
}

// applyTerminalSettings applies the session's terminal settings to a
// view.
//
// Every frame the program renders has to carry them: bubbletea reads
// AltScreen off the view, so a view that forgets it makes the
// renderer leave the alternate screen in the middle of the session.
// The login screen renders its own view (it is a whole-screen modal),
// but it lives inside this program — it must not drop the terminal out
// of the alternate screen that the session started in.
func (m *RootModel) applyTerminalSettings(v tea.View) tea.View {
	altScreen := m.effectiveAltScreen()
	v.AltScreen = altScreen
	if altScreen {
		v.MouseMode = tea.MouseModeCellMotion
		if m.overlays.panel.kind == panelregistry.Terminal && m.overlays.panel.term != nil && m.overlays.panel.term.MouseAllMotion() {
			v.MouseMode = tea.MouseModeAllMotion
		}
	}
	return v
}

func (m *RootModel) shouldShowInputCursor(cursorX, cursorY int) bool {
	if m.help.Panel.Active() {
		return false
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		return false
	}
	if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
		return false
	}
	if m.overlays.wireHUD != nil && m.overlays.wireHUD.IsVisible() {
		return false
	}
	if m.overlays.panel.kind != panelregistry.None {
		return false
	}
	if m.isFullViewportPanel() {
		return false
	}
	writeInFocused := m.askQuestion.WriteInActive() || (!m.askQuestion.Active() && m.askQuestion.panel != nil && m.askQuestion.panel.WriteInActive())
	if !writeInFocused && !m.prompt.input.Focused() {
		return false
	}
	if m.hasSelection() || m.prompt.HasInputSelection() {
		return false
	}
	if m.overlays.HasActiveModal() {
		return false
	}
	if m.overlays.resumePicker != nil && m.overlays.resumePicker.Active() {
		return false
	}
	if m.audio.Surface != nil && m.audio.Surface.Active() && m.audio.Surface.Phase() != voice.PhaseLive {
		return false
	}
	if m.activeItems.Panel != nil && m.activeItems.Panel.Focused() {
		return false
	}
	if m.askQuestion.Active() {
		if !m.askQuestion.WriteInActive() {
			return false
		}
	} else if m.askQuestion.panel != nil && m.askQuestion.panel.Active() {
		if !m.askQuestion.panel.WriteInActive() {
			return false
		}
	}
	if m.hasActivePermissionPrompt() {
		return false
	}
	if cursorY < 0 || (m.viewport.height > 0 && cursorY >= m.viewport.height) {
		return false
	}
	if cursorX < 0 || (m.viewport.width > 0 && cursorX >= m.viewport.width) {
		return false
	}
	return true
}

func (m *RootModel) buildTeaView(body string, cursorX, cursorY int) tea.View {
	if m.overlays.wireHUD != nil && m.overlays.wireHUD.IsVisible() && m.viewport.width > 0 {
		m.syncVoiceWireHUD()
		rows := strings.Split(body, "\n")
		rows = m.overlays.wireHUD.OverlayOnRows(rows, m.viewport.width)
		body = strings.Join(rows, "\n")
	}
	v := tea.NewView(body)
	v = m.applyTerminalSettings(v)
	if title := m.windowTitleText(); title != "" {
		v.WindowTitle = title
	}
	v.ProgramStatus = m.programStatus()
	if m.shouldShowInputCursor(cursorX, cursorY) {
		v.Cursor = &tea.Cursor{
			Position: tea.Position{X: cursorX, Y: cursorY},
			Shape:    tea.CursorBlock,
			Blink:    true,
		}
	}
	return v
}

func (m *RootModel) isViewCacheValid() bool {
	if !m.viewport.viewCacheValid || m.viewport.rowsCacheDirty || m.viewport.cachedView.Content == "" {
		return false
	}
	if m.viewport.cachedStreaming != m.conversation.streaming || m.conversation.streaming {
		return false
	}
	if m.conversation.thinking || m.audio.thinking || m.session.store.BashRunning() {
		return false
	}
	if m.viewport.cachedPickerVis != m.prompt.picker.Visible() || m.viewport.cachedPanelKind != m.overlays.panel.kind {
		return false
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		return false
	}
	if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
		return false
	}
	if m.overlays.wireHUD != nil && m.overlays.wireHUD.IsVisible() {
		return false
	}
	if m.prompt.picker.Visible() || m.hasActiveModal() || (m.audio.Surface != nil && m.audio.Surface.Active() && m.audio.Surface.Phase() != voice.PhaseLive) || (m.activeItems.Panel != nil && m.activeItems.Panel.Focused()) || m.askQuestion.Active() || (m.askQuestion.panel != nil && m.askQuestion.panel.Active()) || m.hasSelection() || m.prompt.HasInputSelection() || m.hasBtwSelection() || m.hasPanelSelection() {
		return false
	}
	if m.viewport.cachedScroll != m.viewport.scroll || m.viewport.cachedAutoScroll != m.viewport.autoScroll {
		return false
	}
	if m.viewport.cachedW != m.viewport.width || m.viewport.cachedH != m.viewport.height {
		return false
	}
	if m.viewport.cachedRespLen != len(m.conversation.response) || m.viewport.cachedRowsGen != m.viewport.rowsGen {
		return false
	}
	if m.viewport.cachedHeaderKey != m.currentHeaderKey(m.viewport.width) {
		return false
	}
	return true
}

func (m *RootModel) View() tea.View {
	// viewportHeight measures the body once to learn how many transcript rows
	// fit, then the normal body pass renders it again. Both passes need the
	// same composer bytes, but the textarea renderer is comparatively costly.
	// Keep the prepared composer view only for this frame; Update-driven state
	// changes can therefore never reuse stale selection, cursor, placeholder,
	// or style output.
	m.viewport.promptFrameActive = true
	m.viewport.preparedInputView = ""
	m.viewport.preparedInputViewValid = false
	m.viewport.promptInputViewMisses = 0
	defer func() {
		m.viewport.lastPromptInputViewMisses = m.viewport.promptInputViewMisses
		m.viewport.promptFrameActive = false
		m.viewport.preparedInputView = ""
		m.viewport.preparedInputViewValid = false
	}()
	if m.display.perf != nil && m.display.perf.Enabled() {
		start := time.Now()
		defer func() { m.display.perf.RecordView(time.Since(start)) }()
	}
	// The frame is painted with the selected scheme's colours: the
	// palette resolves them from the scheme the session wears.
	restore := palette.PushScheme(m.viewport.colorScheme)
	defer restore()
	if !m.prompt.picker.Visible() {
		m.prompt.picker.ResetGeom()
	}
	if m.auth.login != nil {
		// The login screen owns its own layout, but it still runs in
		// this program's terminal: keep the session's alternate-screen
		// setting, or the renderer leaves it (the /accounts → a flow
		// used to drop the session out of the alt screen).
		// Note: the login screen does not use in-app transcript mouse
		// selection, so mouse tracking is left off (MouseModeNone) so
		// that the terminal can handle native mouse text selection,
		// copying, and Cmd+click / click on OSC 8 links without
		// application interception.
		v := m.auth.login.View()
		v.AltScreen = m.effectiveAltScreen()
		return v
	}
	if m.swarm.Board != nil && m.swarm.Board.Active() {
		m.swarm.Board.SetSize(m.viewport.width, m.viewport.height)
		return m.applyTerminalSettings(tea.NewView(m.swarm.Board.View()))
	}
	if m.agents.battle.Panel != nil && m.agents.battle.Panel.Active() {
		m.agents.battle.Panel.SetSize(m.viewport.width, m.viewport.height)
		return m.applyTerminalSettings(tea.NewView(m.agents.battle.Panel.View()))
	}
	if m.isViewCacheValid() {
		return m.viewport.cachedView
	}
	var transcript []string
	if m.viewport.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(m.viewport.width)
	}
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	if m.viewport.width > 0 && m.overlays.panel.kind != panelregistry.Changelog && m.overlays.panel.kind != panelregistry.Logs {
		body = tuilib.FitLines(body, m.viewport.width)
	}
	body, cursorY = m.clipViewBody(body, cursorY)
	cursorX := m.activeCursorX()
	v := m.buildTeaView(body, cursorX, cursorY)
	m.viewport.cachedView = v
	m.viewport.cachedScroll = m.viewport.scroll
	m.viewport.cachedAutoScroll = m.viewport.autoScroll
	m.viewport.cachedW = m.viewport.width
	m.viewport.cachedH = m.viewport.height
	m.viewport.cachedRespLen = len(m.conversation.response)
	m.viewport.cachedRowsGen = m.viewport.rowsGen
	m.viewport.cachedPickerVis = m.prompt.picker.Visible()
	m.viewport.cachedPanelKind = m.overlays.panel.kind
	m.viewport.cachedStreaming = m.conversation.streaming
	m.viewport.viewCacheValid = !m.conversation.streaming && !m.conversation.thinking && !m.session.store.BashRunning() && m.shouldShowInputCursor(cursorX, cursorY)
	return v
}

func (m *RootModel) renderBodyQueuesAndCard(b *strings.Builder) {
	if !m.prompt.picker.Visible() && !m.hasActiveModal() {
		if row := m.overlays.announceQueue.Row(); row != "" {
			b.WriteString(row)
		}
		if row := announce.RenderNoticeRow(m.overlays.eligibilityNotice.Reason, m.overlays.eligibilityNotice.VerificationURL, m.viewport.hyperlinkCaps); row != "" {
			b.WriteString(row)
		}
	}
	if len(m.session.store.SendingMessages) > 0 {
		for _, sending := range m.session.store.SendingMessages {
			b.WriteString(uirows.SendingMessageRow(sending) + "\n")
		}
	}
	if len(m.session.store.StreamQueue) > 0 || len(m.session.store.BashQueue) > 0 {
		for i, queued := range m.session.store.StreamQueue {
			isLast := i == len(m.session.store.StreamQueue)-1 && len(m.session.store.BashQueue) == 0
			withHint := isLast && m.conversation.streaming
			b.WriteString(uirows.QueuedPromptRowWithHint(queued, withHint) + "\n")
		}
		for _, queued := range m.session.store.BashQueue {
			b.WriteString(uirows.QueuedCommandRow(queued) + "\n")
		}
	}
	if row := m.aboveInputCard(); row != "" {
		m.overlays.btwBodyStart = tuilib.ViewRows(b.String())
		m.overlays.btwHeight = tuilib.ViewRows(row)
		b.WriteString(row + "\n")
	} else {
		m.overlays.btwBodyStart = -1
		m.overlays.btwHeight = 0
		m.overlays.btwScreenY = -1
	}
}

// IsBtwCardY reports whether the screen row y falls within the visible /btw card.
func (m *RootModel) IsBtwCardY(y int) bool {
	if !m.isBtwCardVisible() || m.overlays.btwScreenY < 0 || m.overlays.btwHeight <= 0 {
		return false
	}
	return y >= m.overlays.btwScreenY && y < m.overlays.btwScreenY+m.overlays.btwHeight
}

// IsScrollIndicatorCell reports whether (x, y) on the screen falls on the
// scroll position indicator ("↓ 42 lines · 2 turns").
func (m *RootModel) IsScrollIndicatorCell(x, y int) bool {
	if !m.viewport.altScreen || m.viewport.autoScroll || m.hasActiveModal() || m.isFullViewportPanel() {
		return false
	}
	if m.viewport.scrollIndScreenY < 0 || m.viewport.scrollIndScreenY >= m.viewport.height {
		return false
	}
	if y != m.viewport.scrollIndScreenY {
		return false
	}
	if !m.conversation.streaming {
		return true
	}
	minX := m.viewport.scrollIndStartX - 2
	if minX < 0 {
		minX = 0
	}
	return x >= minX
}

func (m *RootModel) syncInputPlaceholder() {
	switch {
	case m.prompt.picker.Visible() || m.hasActiveModal() || (m.audio.Surface != nil && m.audio.Surface.Active() && m.audio.Surface.Phase() != voice.PhaseLive):
		m.prompt.input.Placeholder = ""
	case m.viewport.inputNotice != "":
		m.prompt.input.Placeholder = m.viewport.inputNotice
	case m.prompt.modeHintDismissed:
		m.prompt.input.Placeholder = ""
	default:
		m.prompt.input.Placeholder = m.promptPlaceholder(m.session.mode)
	}
}

func (m *RootModel) formatInputView() string {
	if m.viewport.promptFrameActive && m.viewport.preparedInputViewValid {
		return m.viewport.preparedInputView
	}
	if m.viewport.promptFrameActive {
		m.viewport.promptInputViewMisses++
	}
	m.syncInputPlaceholder()
	inputView := m.prompt.input.View()
	inputView = strings.Replace(inputView, cBlueOpen+"> \x1b[m", cBlueOpen+">\x1b[m ", 1)
	if !strings.Contains(inputView, cBlueOpen+">\x1b[m ") {
		inputView = strings.Replace(inputView, "\x1b[38;5;69m> \x1b[m", "\x1b[38;5;69m>\x1b[m ", 1)
	}
	// The reference colours the command token in the composer and shows
	// the argument hint of an empty "/model " argument
	// (【一手】 agy 1.4.6, live — internal/tuilib/composer_token.go).
	composerValue := tuilib.FirstLine(m.prompt.input.Value())
	inputView = tuilib.StyleComposerCommandToken(inputView, composerValue)
	if hint, ok := tuilib.ComposerArgumentHint(composerValue); ok {
		inputView = tuilib.AppendComposerHint(inputView, cDimGrayOpen+hint+cDimGrayClose, composerValue)
	}
	if m.session.store.BashMode {
		inputView = bashmode.PromptGlyph(inputView, cBlueOpen)
	}
	if !strings.HasSuffix(inputView, "\n") {
		inputView += "\n"
	}
	if m.prompt.HasInputSelection() {
		inputView = m.prompt.ApplyInputSelectionHighlight(inputView)
	}
	if m.viewport.promptFrameActive {
		m.viewport.preparedInputView = inputView
		m.viewport.preparedInputViewValid = true
	}
	return inputView
}

func (m *RootModel) renderBodyPrompt(b *strings.Builder) int {
	if !m.conversation.streaming && m.viewport.altScreen && !m.viewport.autoScroll {
		if ind := m.scrollPositionIndicator(); ind != "" {
			rw := ansi.StringWidth(ind)
			pad := m.viewport.width - rw
			if pad < 0 {
				pad = 0
			}
			m.viewport.scrollIndBodyStart = tuilib.ViewRows(b.String())
			m.viewport.scrollIndStartX = pad
			m.viewport.scrollIndEndX = m.viewport.width
			b.WriteString(palette.DimGrayOpen + strings.Repeat(" ", pad) + ind + palette.DimGrayClose + "\n")
		}
	}
	m.renderBodyQueuesAndCard(b)
	if m.hasActivePermissionPrompt() {
		b.WriteString("\n")
		if permissionRows := m.permissionPromptLines(m.viewport.width); len(permissionRows) > 0 {
			for _, row := range permissionRows {
				b.WriteString(row + "\n")
			}
		}
		return tuilib.ViewRows(b.String())
	}
	b.WriteString(m.promptRule(true) + "\n")

	rowsAbovePrompt := tuilib.ViewRows(b.String())
	m.prompt.promptBodyStart = rowsAbovePrompt

	// The attachments of the next turn (agy 1.4.9's image pasting) sit
	// above the composer, so the user can see what will be sent.
	for _, row := range uirows.AttachmentRows(m.prompt.pendingMedia, m.prompt.mediaExpanded) {
		b.WriteString(row + "\n")
		rowsAbovePrompt++
	}

	if m.askQuestion.Active() {
		askStartY := tuilib.ViewRows(b.String())
		b.WriteString(m.askQuestion.View())
		b.WriteString(m.promptRule(false) + "\n")
		if c := m.askQuestion.Cursor(); c != nil {
			return askStartY + c.Position.Y
		}
		return rowsAbovePrompt
	}

	inputView := m.formatInputView()
	b.WriteString(inputView)

	// The reference answers a turn the user cancelled with Esc with a
	// hint in the composer, not with an error row in the transcript.
	if hint := m.interruptedHintRow(); hint != "" {
		b.WriteString(hint + "\n")
	}

	cursorY := rowsAbovePrompt
	if c := m.prompt.input.Cursor(); c != nil {
		cursorY += c.Y
	} else {
		cursorY += m.prompt.input.Line()
	}
	b.WriteString(m.promptRule(false) + "\n")
	if m.overlays.panel.kind == panelregistry.None {
		if activeView := m.activeItems.Panel.Rows(); activeView != "" {
			b.WriteString(activeView + "\n")
			b.WriteString(m.separator() + "\n")
		}
	}
	return cursorY
}

func (m *RootModel) renderBodyOverlays(b *strings.Builder) {
	m.prompt.picker.SetBodyStart(tuilib.ViewRows(b.String()))
	if pickerRows := m.prompt.picker.Rows(); pickerRows != "" {
		b.WriteString(pickerRows)
	}
	if atFileRows := m.prompt.atFilePicker.Rows(); atFileRows != "" {
		b.WriteString(atFileRows)
	}
	if resumeRows := m.overlays.resumePicker.View(m.viewport.width, m.viewport.height); resumeRows != "" {
		b.WriteString(resumeRows)
	}
	m.overlays.panelScreenY = tuilib.ViewRows(b.String())
	if panelRows := m.panelView(); panelRows != "" {
		b.WriteString(panelRows)
	}
	if voiceRows := m.audio.Surface.View(); voiceRows != "" {
		b.WriteString(voiceRows)
	}
	if m.overlays.panel.kind != panelregistry.Help && m.help.Panel.Active() {
		b.WriteString(m.help.Panel.Rows())
	}
}

func (m *RootModel) renderBodyStatusLine(b *strings.Builder) int {
	subagentCursorY := -1
	if m.statusLine.visible != "off" && !((m.overlays.panel.kind == panelregistry.Changelog || m.overlays.panel.kind == panelregistry.Logs) && m.viewport.height > 0) {
		if rows := m.askQuestion.panel.AlertRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		if !m.askQuestion.Active() && m.askQuestion.panel != nil {
			pickerStartY := tuilib.ViewRows(b.String())
			if rows := m.askQuestion.panel.PickerRows(); len(rows) > 0 {
				for _, row := range rows {
					b.WriteString(row + "\n")
				}
				if c := m.askQuestion.panel.Cursor(); c != nil {
					subagentCursorY = pickerStartY + c.Position.Y
				}
			}
		}
		m.writeStatusArea(b)
	}
	return subagentCursorY
}

// writeStatusArea writes the status area's rows. The last row carries no
// trailing newline: it is the last thing on the screen.
func (m *RootModel) writeStatusArea(b *strings.Builder) {
	rows := statusline.AreaRows(m.statusAreaState())
	for i, row := range rows {
		if i < len(rows)-1 {
			b.WriteString(row + "\n")
			continue
		}
		b.WriteString(row)
	}
}

func (m *RootModel) viewBody(transcript []string) (string, int) {
	var b strings.Builder
	m.prompt.picker.ResetGeom()
	m.prompt.promptScreenY = -1
	m.viewport.scrollIndBodyStart = -1
	m.viewport.scrollIndScreenY = -1
	m.viewport.scrollIndStartX = -1
	m.viewport.scrollIndEndX = -1

	if m.isFullViewportPanel() && m.viewport.height > 0 {
		m.overlays.panelScreenY = 0
		m.overlays.panelHeight = m.viewport.height
		b.WriteString(m.panelView())
		if m.statusLine.visible != "off" {
			m.writeStatusArea(&b)
		}
		return b.String(), -1
	}

	for _, line := range transcript {
		b.WriteString(line + "\n")
	}

	transcriptRowCount := tuilib.ViewRows(b.String())
	live := m.liveRows(m.viewport.width)
	if m.conversation.streaming && m.viewport.altScreen && !m.viewport.autoScroll {
		if ind := m.scrollPositionIndicator(); ind != "" {
			rw := ansi.StringWidth(ind)
			pad := m.viewport.width - rw
			if pad < 0 {
				pad = 0
			}
			m.viewport.scrollIndStartX = pad
			m.viewport.scrollIndEndX = m.viewport.width
			if m.viewport.activeTip != "" {
				m.viewport.scrollIndBodyStart = transcriptRowCount + 1
			} else {
				m.viewport.scrollIndBodyStart = transcriptRowCount
			}
		}
	}
	for _, line := range live {
		b.WriteString(line + "\n")
	}

	cursorY := m.renderBodyPrompt(&b)
	m.renderBodyOverlays(&b)
	if subagentCursorY := m.renderBodyStatusLine(&b); subagentCursorY >= 0 {
		cursorY = subagentCursorY
	}

	return b.String(), cursorY
}

func (m *RootModel) activeCursorX() int {
	if m.askQuestion.Active() {
		if c := m.askQuestion.Cursor(); c != nil {
			return c.Position.X
		}
	} else if m.askQuestion.panel != nil && m.askQuestion.panel.Active() {
		if c := m.askQuestion.panel.Cursor(); c != nil {
			return c.Position.X
		}
	}
	return m.inputCursorX()
}

func (m *RootModel) inputCursorX() int {
	if c := m.prompt.input.Cursor(); c != nil {
		return c.X
	}
	prefixWidth := 2
	val := m.prompt.input.Value()
	line := m.prompt.input.Line()
	col := m.prompt.input.Column()
	lines := strings.Split(val, "\n")
	if line >= 0 && line < len(lines) {
		runes := []rune(lines[line])
		if col > len(runes) {
			col = len(runes)
		}
		return prefixWidth + ansi.StringWidth(string(runes[:col]))
	}
	return prefixWidth
}

// programStatus reports Wagyu's current state to terminals that support the
// Program Status Protocol.
func (m *RootModel) programStatus() *tea.ProgramStatus {
	return programstatus.Resolve(programstatus.Inputs{
		PendingPermission:  m.session.pendingPermission,
		HasPendingQuestion: m.askQuestion.panel.HasPendingQuestion(),
		PendingAuth:        m.session.pendingAuth,
		IsAgentBusy:        m.isAgentBusy(),
		LastTurnOutcome:    m.display.lastTurnOutcome,
	})
}

// isAgentBusy is true while a model turn is in flight or background work is running.
func (m *RootModel) isAgentBusy() bool {
	if m.conversation.streaming || (m.turn.activeBridge != nil && m.turn.activeBridge.IsRunning()) || m.conversation.thinking {
		return true
	}
	// AGY 1.5.10: OSC 7501 terminal status shows the session as working while
	// waiting for background subagents or non-daemon tasks to finish. Daemon tasks
	// such as dev servers don't count.
	return m.hasActiveBackgroundWork()
}

func (m *RootModel) hasActiveBackgroundWork() bool {
	if m.agents.registry != nil && m.agents.registry.Busy() {
		return true
	}
	if m.session.conversationID != "" {
		for _, task := range shell.RunningTaskSnapshots(m.session.conversationID) {
			if !task.IsDaemon {
				return true
			}
		}
	}
	return false
}

// recordTurnOutcome records how the open turn ended.
func (m *RootModel) recordTurnOutcome(state tea.ProgramState) {
	m.display.lastTurnOutcome = state
}

// clearLastTurnOutcome clears the recorded outcome.
func (m *RootModel) clearLastTurnOutcome() {
	m.display.lastTurnOutcome = ""
}

// ---- renderer.go ----

// renderer.go - cycle 256: the renderer's optimization/bypass
// behaviour, exposed as a public, testable seam.
//
// AGY 1.1.19's render path emits only dirty rows on each frame
// and falls back to the full viewport only when the consumer
// explicitly disables the optimization. chasm.land/bubbletea/v2
// owns the actual diff implementation inside cursed_renderer.go;
// we don't fork it. What cycle 256 ships is:
//
//  1. A boolean escapeSequenceOptimizationsEnabled() (in
//     escape_optimizations.go) that reads
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS.
//
//  2. applyRenderer(prev, current, optimizationsEnabled) -
//     public test surface for "given two frames and a gate,
//     what does the renderer emit?". The contract:
//
//     optimizationsEnabled AND prev == current : ""    (no-op)
//     otherwise                                : current (full redraw)
//
//     When the env var bypasses optimizations,
//     optimizationsEnabled is false and applyRenderer always
//     returns `current`. The cycle-256 deliverable's
//     "What the fix does" section lists every call site
//     currently consulting this seam; the View() in tui_run.go
//     does not yet route through applyRenderer because that
//     would require either wrapping or replacing the
//     bubbletea/v2 renderer (out of scope for cycle 256's
//     env-var gap; documented in 硬约束).
//
//  3. shouldConsultRenderer(mode) - the print-mode bypass pin.
//     Print mode (-p / --print) writes plain text to stdout and
//     never reaches bubbletea's View() pipeline, so the
//     AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS env var is
//     irrelevant there. The cycle-256 spec says
//     "print mode (`-p`) doesn't trigger the renderer code path
//     (it's a TUI concern); pin in test." shouldConsultRenderer
//     is the public pin.

// applyRenderer returns the bytes the renderer should emit given
// the previous frame, the current frame, and whether the
// dirty-rectangle / diff optimizations are enabled.
//
// When optimizations are on AND the previous and current frame
// are identical (byte-for-byte), applyRenderer returns the empty
// string to signal "no draw needed". The match is exact and
// untrimmed: the renderer downstream always sees the literal
// viewport bytes, and a single trailing newline difference is
// still a redraw. The cycle 256 deliverable documents this
// decision against any future cycle that wants a trimmed
// comparison (trimming would let optimisations elide frame
// transitions bubbletea treats as different for layout reasons
// but humans view as identical - kept simple here).
//
// When optimizations are off, applyRenderer returns the full
// current view regardless of whether it differs from prev. This
// is the AGY 1.1.19 "bypass the renderer's ... optimizations"
// behaviour: full redraw every frame.
//
// When optimizations are on AND the views differ, applyRenderer
// returns the full current view (the diff itself would be
// returned in a future cycle - cycle 256 exercises the gate
// only, not the diff algorithm).
func applyRenderer(prev, current string, optimizationsEnabled bool) string {
	if optimizationsEnabled && prev == current {
		return ""
	}
	return current
}

// mode constants for shouldConsultRenderer. Held as named
// strings rather than an enum so the print-mode dispatcher in
// cmd/wagyu/main.go can pass through values verbatim without a
// conversion step.
const (
	modePrint = "print"
	modeDashP = "-p"
)

// shouldConsultRenderer reports whether the renderer / dirty-rect
// optimization seam applies for a given run mode. Returns false
// for print mode (and its `-p` shorthand) and true otherwise.
//
// match is case-insensitive and whitespace-trimmed so callers do
// not have to normalize mode strings before calling; the test
// fixture in renderer_test.go pins this contract for every
// spelling the print dispatcher accepts (cmd/wagyu/main.go
// normalizes "-p" and "--print" to a single print path long
// before this gate fires in production, but the gate still has
// to be robust to the literals a future cycle might emit).
//
// The default (unknown mode) returns true - "be conservative
// and apply the gate" matches the AGY 1.1.19 reading of the
// spec.
func shouldConsultRenderer(mode string) bool {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case modePrint, modeDashP:
		return false
	}
	return true
}

const envDisableEscapeSequenceOptimizations = tuilib.EnvDisableEscapeSequenceOptimizations

// The palette's colours follow the selected colour scheme, so these
// aliases are refreshed whenever the scheme changes.
var (
	cBlueOpen            = palette.BlueOpen
	cBlueBoldOpen        = palette.BlueBoldOpen
	cUserFgOpen          = palette.UserFgOpen
	cDimGrayOpen         = palette.DimGrayOpen
	cWarningOpen         = palette.WarningOpen
	cWarningBlueOpen     = palette.WarningBlueOpen
	cWarningBodyOpen     = palette.WarningBodyOpen
	cSeparatorOpen       = palette.SeparatorOpen
	cSubtleOpen          = palette.SubtleOpen
	cToolBulletErrOpen   = palette.ToolBulletErrOpen
	cDiffAddTintDark     = palette.DiffAddTintDark
	cDiffRemoveTintDark  = palette.DiffRemoveTintDark
	cDiffAddTintLight    = palette.DiffAddTintLight
	cDiffRemoveTintLight = palette.DiffRemoveTintLight
	cDiffAddTintOpen     = palette.DiffAddTintOpen
	cDiffRemoveTintOpen  = palette.DiffRemoveTintOpen
	cInlineCodeOpen      = palette.InlineCodeOpen
	cInlineCodeClose     = palette.InlineCodeClose
	cBoldOpen            = palette.BoldOpen
	cBoldClose           = palette.BoldClose
	cBlueClose           = palette.BlueClose
	cDimGrayClose        = palette.DimGrayClose
	cWarningClose        = palette.WarningClose
	cWarningBlueClose    = palette.WarningBlueClose
	cWarningBodyClose    = palette.WarningBodyClose
	cSeparatorClose      = palette.SeparatorClose
)

func init() {
	palette.OnSchemeChange(refreshPaletteAliases)
}

// refreshPaletteAliases re-reads the palette's colours after the
// selected colour scheme changed.
func refreshPaletteAliases() {
	cBlueOpen = palette.BlueOpen
	cBlueBoldOpen = palette.BlueBoldOpen
	cUserFgOpen = palette.UserFgOpen
	cDimGrayOpen = palette.DimGrayOpen
	cWarningOpen = palette.WarningOpen
	cWarningBlueOpen = palette.WarningBlueOpen
	cWarningBodyOpen = palette.WarningBodyOpen
	cSeparatorOpen = palette.SeparatorOpen
	cSubtleOpen = palette.SubtleOpen
	panelGray238 = palette.SubtleOpen
	cToolBulletErrOpen = palette.ToolBulletErrOpen
	cDiffAddTintDark = palette.DiffAddTintDark
	cDiffRemoveTintDark = palette.DiffRemoveTintDark
	cDiffAddTintLight = palette.DiffAddTintLight
	cDiffRemoveTintLight = palette.DiffRemoveTintLight
	cDiffAddTintOpen = palette.DiffAddTintOpen
	cDiffRemoveTintOpen = palette.DiffRemoveTintOpen
	cInlineCodeOpen = palette.InlineCodeOpen
	cInlineCodeClose = palette.InlineCodeClose
	cBoldOpen = palette.BoldOpen
	cBoldClose = palette.BoldClose
	cBlueClose = palette.BlueClose
	cDimGrayClose = palette.DimGrayClose
	cWarningClose = palette.WarningClose
	cWarningBlueClose = palette.WarningBlueClose
	cWarningBodyClose = palette.WarningBodyClose
	cSeparatorClose = palette.SeparatorClose

	// Cycle 381: the /help overlay's own aliases left the host with
	// the panel; the panel reads tuilib's colours directly, and tuilib
	// refreshes them when the scheme changes.

	// Cycle 381: the slash-command popup's own colours left the host
	// with the panel; the panel reads the palette's colours directly
	// and refreshes them when the scheme changes.

	// Cycle 381: the bash block's body colours left the host with the
	// block; the block reads the palette's colours directly, and the
	// palette refreshes them when the scheme changes.
}
