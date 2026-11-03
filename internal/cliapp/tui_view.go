package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/tools/bashmode"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/programstatus"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/palette"
)

func (m *runMode) clipViewBody(body string, cursorY int) (string, int) {
	if m.height <= 0 {
		return body, cursorY
	}
	rows := tuilib.ViewRows(body)
	maxRows := m.height + viewOverflowRows
	drop := rows - m.height
	if drop < 0 {
		drop = 0
	}
	if rows > maxRows || drop > 0 {
		lines := strings.Split(body, "\n")
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		keep := len(lines)
		if keep > maxRows {
			keep = maxRows
		}
		if drop > keep {
			drop = keep
		}
		if drop < keep {
			body = strings.Join(lines[drop:keep], "\n")
		} else {
			body = ""
		}
		cursorY -= drop
		if cursorY < 0 {
			cursorY = 0
		}
	}
	m.picker.ClipBody(drop)
	if m.promptBodyStart >= drop {
		m.promptScreenY = m.promptBodyStart - drop
	}
	return body, cursorY
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
func (m *runMode) applyTerminalSettings(v tea.View) tea.View {
	v.AltScreen = m.altScreen
	if m.altScreen {
		v.MouseMode = tea.MouseModeCellMotion
	}
	return v
}

func (m *runMode) buildTeaView(body string, cursorX, cursorY int) tea.View {
	v := tea.NewView(body)
	v = m.applyTerminalSettings(v)
	if title := m.windowTitleText(); title != "" {
		v.WindowTitle = title
	}
	v.ProgramStatus = m.programStatus()
	if m.helpPanel.Active() {
		cursorY = 0
		cursorX = 0
	}
	v.Cursor = &tea.Cursor{
		Position: tea.Position{X: cursorX, Y: cursorY},
		Shape:    tea.CursorBlock,
		Blink:    true,
	}
	return v
}

func (m *runMode) isViewCacheValid() bool {
	if !m.viewCacheValid || m.rowsCacheDirty || m.cachedView.Content == "" {
		return false
	}
	if m.cachedPickerVis != m.picker.Visible() || m.cachedPanelKind != m.panel.kind {
		return false
	}
	if m.picker.Visible() || m.panel.kind != panelregistry.None || m.helpPanel.Active() || m.resumePicker.Active() || m.hasSelection() || m.hasInputSelection() {
		return false
	}
	if m.cachedScroll != m.scroll || m.cachedAutoScroll != m.autoScroll {
		return false
	}
	if m.cachedW != m.width || m.cachedH != m.height {
		return false
	}
	if m.cachedRespLen != len(m.response) || m.cachedRowsGen != m.rowsGen {
		return false
	}
	if m.cachedHeaderKey != m.currentHeaderKey(m.width) {
		return false
	}
	return true
}

func (m *runMode) View() tea.View {
	// The frame is painted with the selected scheme's colours: the
	// palette resolves them from the scheme the session wears.
	restore := palette.PushScheme(m.colorScheme)
	defer restore()
	if !m.picker.Visible() {
		m.picker.ResetGeom()
	}
	if m.login != nil {
		// The login screen owns its own layout, but it still runs in
		// this program's terminal: keep the session's alternate-screen
		// setting, or the renderer leaves it (the /accounts → a flow
		// used to drop the session out of the alt screen).
		// Note: the login screen does not use in-app transcript mouse
		// selection, so mouse tracking is left off (MouseModeNone) so
		// that the terminal can handle native mouse text selection,
		// copying, and Cmd+click / click on OSC 8 links without
		// application interception.
		v := m.login.View()
		v.AltScreen = m.altScreen
		return v
	}
	if m.isViewCacheValid() {
		return m.cachedView
	}
	var transcript []string
	if m.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(m.width)
	}
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	if m.width > 0 && m.panel.kind != panelregistry.Changelog {
		body = tuilib.FitLines(body, m.width)
	}
	body, cursorY = m.clipViewBody(body, cursorY)
	cursorX := m.inputCursorX()
	v := m.buildTeaView(body, cursorX, cursorY)
	m.cachedView = v
	m.cachedScroll = m.scroll
	m.cachedAutoScroll = m.autoScroll
	m.cachedW = m.width
	m.cachedH = m.height
	m.cachedRespLen = len(m.response)
	m.cachedRowsGen = m.rowsGen
	m.cachedPickerVis = m.picker.Visible()
	m.cachedPanelKind = m.panel.kind
	m.viewCacheValid = true
	return v
}

func (m *runMode) renderBodyQueuesAndCard(b *strings.Builder) {
	if !m.picker.Visible() && !m.resumePicker.Active() && !m.helpPanel.Active() {
		if row := m.announceQueue.Row(); row != "" {
			b.WriteString(row)
		}
		if row := announce.RenderNoticeRow(m.eligibilityNotice.Reason, m.eligibilityNotice.VerificationURL, m.hyperlinkCaps); row != "" {
			b.WriteString(row)
		}
	}
	if len(m.store.StreamQueue) > 0 || len(m.store.BashQueue) > 0 {
		for _, queued := range m.store.StreamQueue {
			b.WriteString(uirows.QueuedPromptRow(queued) + "\n")
		}
		for _, queued := range m.store.BashQueue {
			b.WriteString(uirows.QueuedCommandRow(queued) + "\n")
		}
		b.WriteString(m.separator() + "\n")
	}
	if row := m.aboveInputCard(); row != "" {
		b.WriteString(row + "\n")
	}
}

func (m *runMode) syncInputPlaceholder() {
	switch {
	case m.picker.Visible() || m.panel.kind != panelregistry.None || m.helpPanel.Active() || m.resumePicker.Active() || m.voiceSurface.Active():
		m.input.Placeholder = ""
	case m.inputNotice != "":
		m.input.Placeholder = m.inputNotice
	case m.modeHintDismissed:
		m.input.Placeholder = ""
	default:
		m.input.Placeholder = permissions.ModeBanner(m.mode)
	}
}

func (m *runMode) formatInputView() string {
	m.syncInputPlaceholder()
	inputView := m.input.View()
	inputView = strings.Replace(inputView, cBlueOpen+"> \x1b[m", cBlueOpen+">\x1b[m ", 1)
	if !strings.Contains(inputView, cBlueOpen+">\x1b[m ") {
		inputView = strings.Replace(inputView, "\x1b[38;5;69m> \x1b[m", "\x1b[38;5;69m>\x1b[m ", 1)
	}
	// The reference colours the command token in the composer and shows
	// the argument hint of an empty "/model " argument
	// (【一手】 agy 1.4.6, live — internal/tuilib/composer_token.go).
	composerValue := tuilib.FirstLine(m.input.Value())
	inputView = tuilib.StyleComposerCommandToken(inputView, composerValue)
	if hint, ok := tuilib.ComposerArgumentHint(composerValue); ok {
		inputView = tuilib.AppendComposerHint(inputView, cDimGrayOpen+hint+cDimGrayClose, composerValue)
	}
	if m.store.BashMode {
		inputView = bashmode.PromptGlyph(inputView, cBlueOpen)
	}
	if !strings.HasSuffix(inputView, "\n") {
		inputView += "\n"
	}
	if m.hasInputSelection() {
		inputView = m.applyInputSelectionHighlight(inputView)
	}
	return inputView
}

func (m *runMode) renderBodyPrompt(b *strings.Builder) int {
	b.WriteString(m.promptRule(true) + "\n")
	m.renderBodyQueuesAndCard(b)

	rowsAbovePrompt := tuilib.ViewRows(b.String())
	m.promptBodyStart = rowsAbovePrompt

	// The attachments of the next turn (agy 1.4.9's image pasting) sit
	// above the composer, so the user can see what will be sent.
	for _, row := range uirows.AttachmentRows(m.pendingMedia, m.mediaExpanded) {
		b.WriteString(row + "\n")
		rowsAbovePrompt++
	}

	inputView := m.formatInputView()
	b.WriteString(inputView)

	// The reference answers a turn the user cancelled with Esc with a
	// hint in the composer, not with an error row in the transcript.
	if hint := m.interruptedHintRow(); hint != "" {
		b.WriteString(hint + "\n")
	}

	cursorY := rowsAbovePrompt
	if c := m.input.Cursor(); c != nil {
		cursorY += c.Y
	}
	b.WriteString(m.promptRule(false) + "\n")
	if activeView := m.activeItems.Rows(); activeView != "" {
		b.WriteString(activeView + "\n")
		b.WriteString(m.separator() + "\n")
	}
	return cursorY
}

func (m *runMode) renderBodyOverlays(b *strings.Builder) {
	m.picker.SetBodyStart(tuilib.ViewRows(b.String()))
	if pickerRows := m.picker.Rows(); pickerRows != "" {
		b.WriteString(pickerRows)
	}
	if atFileRows := m.atFilePicker.Rows(); atFileRows != "" {
		b.WriteString(atFileRows)
	}
	if resumeRows := m.resumePicker.View(m.width, m.height); resumeRows != "" {
		b.WriteString(resumeRows)
	}
	if panelRows := m.panelView(); panelRows != "" {
		b.WriteString(panelRows)
	}
	if voiceRows := m.voiceSurface.View(); voiceRows != "" {
		b.WriteString(voiceRows)
	}
	if m.helpPanel.Active() {
		b.WriteString(m.helpPanel.Rows())
	}
}

func (m *runMode) renderBodyStatusLine(b *strings.Builder) {
	if m.statusLine != "off" && !(m.panel.kind == panelregistry.Changelog && m.height > 0) {
		if rows := m.subAgentQuestionPanel.AlertRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		if rows := m.subAgentQuestionPanel.PickerRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		m.writeStatusArea(b)
	}
}

// writeStatusArea writes the status area's rows. The last row carries no
// trailing newline: it is the last thing on the screen.
func (m *runMode) writeStatusArea(b *strings.Builder) {
	rows := statusline.AreaRows(m.statusAreaState())
	for i, row := range rows {
		if i < len(rows)-1 {
			b.WriteString(row + "\n")
			continue
		}
		b.WriteString(row)
	}
}

func (m *runMode) viewBody(transcript []string) (string, int) {
	var b strings.Builder
	m.picker.ResetGeom()
	m.promptScreenY = -1

	if (m.panel.kind == panelregistry.Changelog || m.panel.kind == panelregistry.Diff) && m.height > 0 {
		b.WriteString(m.panelView())
		if m.statusLine != "off" {
			m.writeStatusArea(&b)
		}
		return b.String(), 0
	}

	for _, line := range transcript {
		b.WriteString(line + "\n")
	}

	cursorY := m.renderBodyPrompt(&b)
	m.renderBodyOverlays(&b)
	m.renderBodyStatusLine(&b)

	return b.String(), cursorY
}

func (m *runMode) inputCursorX() int {
	if c := m.input.Cursor(); c != nil {
		return c.X
	}
	return 0
}

// programStatus reports Wagyu's current state to terminals that support the
// Program Status Protocol.
func (m *runMode) programStatus() *tea.ProgramStatus {
	return programstatus.Resolve(programstatus.Inputs{
		PendingPermission:  m.pendingPermission,
		HasPendingQuestion: m.subAgentQuestionPanel.HasPendingQuestion(),
		PendingAuth:        m.pendingAuth,
		IsAgentBusy:        m.isAgentBusy(),
		LastTurnOutcome:    m.lastTurnOutcome,
	})
}

// isAgentBusy is true while a model turn is in flight.
func (m *runMode) isAgentBusy() bool {
	return m.streaming || m.turnCancel != nil || m.thinking
}

// recordTurnOutcome records how the open turn ended.
func (m *runMode) recordTurnOutcome(state tea.ProgramState) {
	m.lastTurnOutcome = state
}

// clearLastTurnOutcome clears the recorded outcome.
func (m *runMode) clearLastTurnOutcome() {
	m.lastTurnOutcome = ""
}

