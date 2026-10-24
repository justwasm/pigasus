package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/programstatus"
	"github.com/justwasm/pigasus/internal/tuilib"
)

func (m *runMode) clipViewBody(body string, cursorY int) (string, int) {
	if m.height <= 0 {
		return body, cursorY
	}
	if maxRows := m.height + viewOverflowRows; viewRows(body) > maxRows {
		lines := strings.Split(body, "\n")
		keep := maxRows
		if keep < 1 {
			keep = 1
		}
		if keep < len(lines) {
			body = strings.Join(lines[:keep], "\n")
		}
	}
	drop := viewRows(body) - m.height
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
			cursorY -= drop
			if cursorY < 0 {
				cursorY = 0
			}
		}
	}
	if m.pickerLayoutLocal.Rows > 0 && m.picker.Visible() {
		start := m.pickerBodyStart - drop
		m.pickerGeom = pickerGeom{
			Valid:       true,
			HasList:     m.pickerLayoutLocal.HasList,
			TopY:        start,
			BottomY:     start + m.pickerLayoutLocal.Rows - 1,
			ListTopY:    start + m.pickerLayoutLocal.ListTop,
			ListBottomY: start + m.pickerLayoutLocal.ListBottom,
			HintY:       start + m.pickerLayoutLocal.HintRow,
		}
	}
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
	if m.title == "on" {
		v.WindowTitle = m.conversationTitle()
	}
	v.ProgramStatus = m.programStatus()
	if m.helpMode {
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

func (m *runMode) View() tea.View {
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
	var transcript []string
	if m.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(0)
	}
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	if m.width > 0 && m.panel.kind != panelChangelog {
		body = fitLines(body, m.width)
	}
	body, cursorY = m.clipViewBody(body, cursorY)
	cursorX := m.inputCursorX()
	return m.buildTeaView(body, cursorX, cursorY)
}

func (m *runMode) renderBodyQueuesAndCard(b *strings.Builder) {
	if !m.picker.Visible() && !m.resumeMode && !m.helpMode {
		if row := m.announcementRow(); row != "" {
			b.WriteString(row)
		}
		if row := m.eligibilityNoticeRow(); row != "" {
			b.WriteString(row)
		}
	}
	if len(m.streamQueue) > 0 || len(m.bashQueue) > 0 {
		for _, queued := range m.streamQueue {
			b.WriteString(cDimGrayOpen + "\u25b8 " + queued +
				" (alt+m to send immediately)" + cDimGrayClose + "\n")
		}
		for _, queued := range m.bashQueue {
			b.WriteString(cDimGrayOpen + "\u25b8 " + queued + cDimGrayClose + "\n")
		}
		b.WriteString(m.separator() + "\n")
	}
	if row := m.aboveInputCard(); row != "" {
		b.WriteString(row + "\n")
	}
}

func (m *runMode) syncInputPlaceholder() {
	switch {
	case m.picker.Visible() || m.panel.kind != panelNone || m.helpMode || m.resumeMode || m.voiceMode:
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
	composerValue := firstLine(m.input.Value())
	inputView = tuilib.StyleComposerCommandToken(inputView, composerValue)
	if hint, ok := tuilib.ComposerArgumentHint(composerValue); ok {
		inputView = tuilib.AppendComposerHint(inputView, cDimGrayOpen+hint+cDimGrayClose, composerValue)
	}
	if m.bashMode {
		inputView = bashPromptGlyph(inputView)
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
	b.WriteString(m.separator() + "\n")
	m.renderBodyQueuesAndCard(b)

	rowsAbovePrompt := viewRows(b.String())
	m.promptBodyStart = rowsAbovePrompt

	inputView := m.formatInputView()
	b.WriteString(inputView)

	cursorY := rowsAbovePrompt
	if c := m.input.Cursor(); c != nil {
		cursorY += c.Y
	}
	b.WriteString(m.separator() + "\n")
	return cursorY
}

func (m *runMode) renderBodyOverlays(b *strings.Builder) {
	m.pickerBodyStart = viewRows(b.String())
	if pickerRows := m.pickerView(); pickerRows != "" {
		b.WriteString(pickerRows)
	}
	if atFileRows := m.atFileView(); atFileRows != "" {
		b.WriteString(atFileRows)
	}
	if resumeRows := m.resumeView(); resumeRows != "" {
		b.WriteString(resumeRows)
	}
	if panelRows := m.panelView(); panelRows != "" {
		b.WriteString(panelRows)
	}
	if voiceRows := m.voiceView(); voiceRows != "" {
		b.WriteString(voiceRows)
	}
	if m.helpMode {
		b.WriteString(renderHelp(m.helpTab, m.model, m.tier, m.cwd, m.email, m.conversationID, m))
	}
}

func (m *runMode) renderBodyStatusLine(b *strings.Builder) {
	if m.statusLine != "off" && !(m.panel.kind == panelChangelog && m.height > 0) {
		if rows := m.subAgentQuestionAlertRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		if rows := m.subAgentQuestionPickerRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row + "\n")
			}
		}
		b.WriteString(m.statusRow())
	}
}

func (m *runMode) viewBody(transcript []string) (string, int) {
	var b strings.Builder
	m.pickerGeom = pickerGeom{}
	m.promptScreenY = -1

	if (m.panel.kind == panelChangelog || m.panel.kind == panelDiff) && m.height > 0 {
		b.WriteString(m.panelView())
		if m.statusLine != "off" {
			b.WriteString(m.statusRow())
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

// programStatusApp is the stable program name reported to the terminal.
const programStatusApp = programstatus.App

// Cycle 239 — env-var names lifted verbatim from the AGY 1.4.0 changelog.
const (
	envOSC7501         = programstatus.EnvOSC7501
	envOSC7501Messages = programstatus.EnvOSC7501Messages
)

// programStatus reports Wagyu's current state to terminals that support the
// Program Status Protocol.
func (m *runMode) programStatus() *tea.ProgramStatus {
	return programstatus.Resolve(programstatus.Inputs{
		PendingPermission:  m.pendingPermission,
		HasPendingQuestion: m.hasPendingQuestion(),
		PendingAuth:        m.pendingAuth,
		IsAgentBusy:        m.isAgentBusy(),
		LastTurnOutcome:    m.lastTurnOutcome,
	})
}

// hasPendingQuestion is true while the sub-agent question picker is on
// screen OR the answer textarea is focused on a queued question.
func (m *runMode) hasPendingQuestion() bool {
	if m.subAgentQuestionPickerOpen {
		return true
	}
	if m.subAgentQuestionAnswerTarget != "" {
		return true
	}
	return false
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

