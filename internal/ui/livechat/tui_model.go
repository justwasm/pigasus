package livechat

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tas "charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/widthx"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/voice"
)

// Bubbletea messages sent from background audio / watcher routines
type (
	userSpeechMsg struct {
		text    string
		interim bool
	}
	agentSpeechDeltaMsg struct {
		delta string
	}
	agentSpeechMsg struct {
		text string
	}
	agentTurnCompleteMsg struct{}
	interruptedMsg       struct{}
	thinkerStartMsg      struct {
		id   string
		task string
	}
	thinkerDoneMsg struct {
		task   string
		result string
		err    error
	}
	searchDoneMsg struct {
		query  string
		result string
		err    error
	}
	voicePreviewMsg struct {
		sample voice.VoicePreviewSample
		err    error
	}
	proactiveEventMsg struct {
		files []string
	}
	logEventMsg struct {
		text string
	}
	errorMsg struct {
		err error
	}
	reconnectingMsg struct {
		attempt     int
		maxAttempts int
		err         error
	}
	reconnectedMsg struct{}
	tickMsg        time.Time
)

type liveChatModel struct {
	sess      *voice.NativeLiveSession
	watcher   *ProactiveWatcher
	workspace string
	voiceName string
	modelName string

	viewport  viewport.Model
	vpSel     tuiviewport.TextSelection
	input     tas.Model
	picker    *slashpicker.Panel
	undoMgr   *composer.UndoManager
	history   history.List
	selection composer.Range

	logs        []string
	interimUser string

	sessionID        string
	sessionCreatedAt time.Time

	agentSpeaking   bool
	agentTurnText   strings.Builder
	dictating       bool
	dictationPrefix string

	reconnecting     bool
	disconnected     bool
	reconnectAttempt int
	reconnectMax     int
	pendingText      string

	width     int
	height    int
	ready     bool
	debugWire bool
	thinking  bool
	tickCount int

	// voicePreview gates the recorded persona clip that plays whenever a
	// voice switch succeeds. The CLI turns it on from --no-voice-preview;
	// models built by tests keep it off so no sound card is touched.
	voicePreview bool
}

func newLiveChatModel(sess *voice.NativeLiveSession, watcher *ProactiveWatcher, ws, voiceName, modelName string, debugWire ...bool) *liveChatModel {
	isWire := false
	if len(debugWire) > 0 {
		isWire = debugWire[0]
	}
	ta := tas.New()
	composer.ConfigureStandardTextArea(&ta, composer.StandardOptions{
		Prompt:      ansiCyan + ansiBold + "> " + ansiReset,
		ContPrompt:  "  ",
		Placeholder: "Type a message, /mute, /p (proactive check), or speak into your mic...",
		MinHeight:   1,
		MaxHeight:   6,
	})
	ta.Focus()

	sid := voice.GenerateLiveSessionID()
	now := time.Now()

	initLogs := []string{
		fmt.Sprintf("%s%s=== LiveChat · Proactive Voice Chat Agent ===%s", ansiBold, ansiBlue, ansiReset),
		fmt.Sprintf("%sSession:%s %s", ansiDim, ansiReset, sid),
		fmt.Sprintf("%sWorkspace:%s %s", ansiDim, ansiReset, ws),
		fmt.Sprintf("%sModel:%s %s · %sVoice:%s %s", ansiDim, ansiReset, modelName, ansiDim, ansiReset, voiceName),
		fmt.Sprintf("%sControls:%s [F6] mute · [/resume] sessions · [Enter] send · [Esc] clear/quit", ansiDim, ansiReset),
	}
	if isWire {
		initLogs = append(initLogs, fmt.Sprintf("%sDiagnostics:%s --debug-wire enabled (real-time frame metrics on)", ansiYellow, ansiReset))
	}
	initLogs = append(initLogs, "")

	m := &liveChatModel{
		sessionID:        sid,
		sessionCreatedAt: now,
		sess:             sess,
		watcher:          watcher,
		workspace:        ws,
		voiceName:        voiceName,
		modelName:        modelName,
		debugWire:        isWire,
		input:            ta,
		undoMgr:          composer.NewUndoManager(100),
		logs:             initLogs,
	}

	m.picker = slashpicker.New(slashpicker.Seams{
		AltScreen: func() bool { return true },
		Width:     func() int { return m.width },
		Buffer:    func() string { return m.input.Value() },
		SetBuffer: func(s string) {
			m.input.SetValue(s)
			m.input.CursorEnd()
		},
		CustomCommands: func() []slashcommands.Command {
			return liveChatCommands()
		},
	})
	m.picker.SetCommandSource(func(prefix string) []slashcommands.Command {
		var out []slashcommands.Command
		for _, c := range liveChatCommands() {
			if prefix == "" || strings.Contains(strings.ToLower(c.Name), strings.ToLower(prefix)) {
				out = append(out, c)
			}
		}
		return out
	})
	m.picker.SetResumeSource(func() []slashcommands.Command {
		var out []slashcommands.Command
		out = append(out, slashcommands.Command{
			Name:        "latest",
			Description: "Resume the most recent saved LiveChat session",
		})
		sessions, err := voice.ListLiveSessions(m.workspace)
		if err == nil {
			for _, s := range sessions {
				desc := fmt.Sprintf("%s · %d turns · %s", s.UpdatedAt.Format("2006-01-02 15:04"), s.TurnCount, s.Title)
				out = append(out, slashcommands.Command{
					Name:        s.ID,
					Description: desc,
				})
			}
		}
		return out
	})

	return m
}

func (m *liveChatModel) Init() tea.Cmd {
	return tea.Batch(
		tas.Blink,
		tickEvery(500*time.Millisecond),
	)
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m *liveChatModel) composerSeams() composer.Seams {
	return composer.Seams{
		Editor:    composer.NewTextArea(&m.input),
		History:   &m.history,
		Selection: m.selection,
		SelectAll: func() (tea.Cmd, bool) {
			val := m.input.Value()
			if len(val) == 0 {
				return nil, false
			}
			m.selection = composer.Range{Start: 0, End: len(val)}
			return nil, true
		},
		ClearSelection: func() {
			m.selection = composer.Range{}
		},
		DeleteSelection: func() {
			if m.selection.Empty() {
				return
			}
			val := m.input.Value()
			start, end := m.selection.Start, m.selection.End
			if start < 0 {
				start = 0
			}
			if end > len(val) {
				end = len(val)
			}
			if start < end {
				newVal := val[:start] + val[end:]
				m.input.SetValue(newVal)
				m.selection = composer.Range{}
			}
		},
		ReplaceSelection: func(s string) {
			val := m.input.Value()
			start, end := m.selection.Start, m.selection.End
			if start < 0 {
				start = 0
			}
			if end > len(val) {
				end = len(val)
			}
			newVal := val[:start] + s + val[end:]
			m.input.SetValue(newVal)
			m.selection = composer.Range{}
		},
		Copy: func(text string) tea.Cmd {
			_ = clipboard.Write(text)
			return nil
		},
		ClearEligibilityNotice: func() {},
		DismissModeHint:        func() {},
		UndoMgr:                m.undoMgr,
	}
}

func (m *liveChatModel) statusHeight() int {
	if m.debugWire {
		return 2
	}
	return 1
}

func (m *liveChatModel) syncLayout() {
	if !m.ready || m.height <= 0 {
		return
	}
	inputHeight := m.input.Height()
	statusHeight := m.statusHeight()
	pickerHeight := 0
	if m.picker != nil && m.picker.Visible() {
		rows := m.picker.Rows()
		if rows != "" {
			pickerHeight = strings.Count(rows, "\n")
		}
	}
	vpHeight := composer.CalculateViewportHeight(m.height, inputHeight+pickerHeight, statusHeight)
	if m.viewport.Height() != vpHeight {
		m.viewport.SetHeight(vpHeight)
		m.viewport.GotoBottom()
	}
}

func formatThinkingInLogs(line string) string {
	if !strings.Contains(line, "<think>") && !strings.Contains(line, "</think>") {
		return line
	}
	line = strings.ReplaceAll(line, "<think>", "\n  "+ansiDim+ansiCyan+"💭 ─── Extended Thinking Process ───"+ansiReset+"\n  "+ansiDim+ansiGray)
	line = strings.ReplaceAll(line, "</think>", ansiReset+"\n  "+ansiDim+ansiCyan+"─────────────────────────────────────"+ansiReset+"\n")
	return line
}

func (m *liveChatModel) renderLogs(width int) string {
	if len(m.logs) == 0 {
		return ""
	}
	if width <= 0 {
		width = 80
	}
	var wrapped []string
	for _, l := range m.logs {
		formatted := formatThinkingInLogs(l)
		lines := widthx.WrapTextWithAnsi(formatted, width)
		wrapped = append(wrapped, lines...)
	}
	return strings.Join(wrapped, "\n")
}

func (m *liveChatModel) appendLog(line string) {
	m.logs = append(m.logs, line)
	if m.ready {
		wasAtBottom := m.viewport.AtBottom()
		m.viewport.SetContent(m.renderLogs(m.viewport.Width()))
		if wasAtBottom {
			m.viewport.GotoBottom()
			if !m.vpSel.SelDragging {
				m.resetVpSelection()
			}
		}
	}
}

func (m *liveChatModel) updateLastLog(line string) {
	if len(m.logs) == 0 {
		m.appendLog(line)
		return
	}
	m.logs[len(m.logs)-1] = line
	if m.ready {
		wasAtBottom := m.viewport.AtBottom()
		m.viewport.SetContent(m.renderLogs(m.viewport.Width()))
		if wasAtBottom {
			m.viewport.GotoBottom()
			if !m.vpSel.SelDragging {
				m.resetVpSelection()
			}
		}
	}
}

func (m *liveChatModel) resetVpSelection() {
	m.vpSel.ResetSelection()
}

func (m *liveChatModel) hasVpSelection() bool {
	_, _, _, _, ok := tuiviewport.SelectionRange(
		m.vpSel.SelStartRow, m.vpSel.SelStartCol,
		m.vpSel.SelEndRow, m.vpSel.SelEndCol,
		m.vpSel.SelDragging,
	)
	return ok || m.vpSel.SelDragging
}

func (m *liveChatModel) viewportLines() []string {
	content := m.viewport.View()
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func (m *liveChatModel) viewportSelectionText() string {
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(
		m.vpSel.SelStartRow, m.vpSel.SelStartCol,
		m.vpSel.SelEndRow, m.vpSel.SelEndCol,
		m.vpSel.SelDragging,
	)
	if !ok {
		return ""
	}
	lines := m.viewportLines()
	if len(lines) == 0 || sr >= len(lines) {
		return ""
	}
	if sr < 0 {
		sr = 0
		sc = 0
	}
	if er >= len(lines) {
		er = len(lines) - 1
		ec = ansi.StringWidth(lines[er])
	}
	return tuiviewport.SelectionText(lines, sr, sc, er, ec)
}

func (m *liveChatModel) renderViewportView() string {
	vpView := m.viewport.View()
	if !m.hasVpSelection() {
		return vpView
	}
	lines := strings.Split(vpView, "\n")
	if len(lines) == 0 {
		return vpView
	}
	sr, sc, er, ec, ok := tuiviewport.SelectionRange(
		m.vpSel.SelStartRow, m.vpSel.SelStartCol,
		m.vpSel.SelEndRow, m.vpSel.SelEndCol,
		m.vpSel.SelDragging,
	)
	if !ok || sr >= len(lines) {
		return vpView
	}
	if sr < 0 {
		sr = 0
		sc = 0
	}
	if er >= len(lines) {
		er = len(lines) - 1
		ec = ansi.StringWidth(lines[er])
	}
	lines = tuiviewport.SelectionPass(lines, sr, sc, er, ec, true)
	return strings.Join(lines, "\n")
}

func (m *liveChatModel) handleMouseWheel(ev tea.Mouse) (tea.Model, tea.Cmd) {
	m.resetVpSelection()
	switch ev.Button {
	case tea.MouseWheelUp:
		m.viewport.ScrollUp(3)
		return m, nil
	case tea.MouseWheelDown:
		m.viewport.ScrollDown(3)
		return m, nil
	}
	return m, nil
}

func (m *liveChatModel) handleMouseClick(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.picker != nil && m.picker.Visible() {
		m.picker.SetBodyStart(m.viewport.Height())
		if m.picker.HandleClick(ev) {
			m.syncLayout()
			return m, nil
		}
	}

	// Composer input area click
	pickerHeight := 0
	if m.picker != nil && m.picker.Visible() {
		if rows := m.picker.Rows(); rows != "" {
			pickerHeight = strings.Count(rows, "\n")
		}
	}
	inputStart := m.viewport.Height() + pickerHeight + 1
	if ev.Y >= inputStart && ev.Y < inputStart+m.input.Height() {
		m.resetVpSelection()
		return m, nil
	}

	// Transcript viewport click
	if ev.Y < m.viewport.Height() && ev.Button == tea.MouseLeft {
		m.selection = composer.Range{}
		lines := m.viewportLines()
		if len(lines) == 0 {
			m.resetVpSelection()
			return m, nil
		}
		row := ev.Y
		if row >= len(lines) {
			row = len(lines) - 1
		}
		col := ev.X
		if col < 0 {
			col = 0
		}
		if col >= m.viewport.Width() {
			col = m.viewport.Width() - 1
		}

		m.vpSel.SelDragging = true
		m.vpSel.SelStartRow = row
		m.vpSel.SelStartCol = col
		m.vpSel.SelEndRow = row
		m.vpSel.SelEndCol = col
		return m, nil
	}

	m.resetVpSelection()
	return m, nil
}

func (m *liveChatModel) handleMouseMotion(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if m.picker != nil && m.picker.Visible() {
		m.picker.HandleHover(ev)
	}

	if !m.vpSel.SelDragging {
		return m, nil
	}

	lines := m.viewportLines()
	if len(lines) == 0 {
		return m, nil
	}

	row := ev.Y
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	col := ev.X
	if col < 0 {
		col = 0
	}
	if col >= m.viewport.Width() {
		col = m.viewport.Width() - 1
	}

	m.vpSel.SelEndRow = row
	m.vpSel.SelEndCol = col
	return m, nil
}

func (m *liveChatModel) handleMouseRelease(ev tea.Mouse) (tea.Model, tea.Cmd) {
	if !m.vpSel.SelDragging {
		return m, nil
	}

	m.vpSel.SelDragging = false
	if ev.Button != tea.MouseLeft {
		m.resetVpSelection()
		return m, nil
	}

	lines := m.viewportLines()
	if len(lines) == 0 {
		m.resetVpSelection()
		return m, nil
	}

	row := ev.Y
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}
	col := ev.X
	if col < 0 {
		col = 0
	}
	if col >= m.viewport.Width() {
		col = m.viewport.Width() - 1
	}

	m.vpSel.SelEndRow = row
	m.vpSel.SelEndCol = col

	text := m.viewportSelectionText()
	if text != "" {
		_ = clipboard.Write(text)
		return m, tea.SetClipboard(text)
	}
	m.resetVpSelection()
	return m, nil
}

func (m *liveChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.PasteMsg:
		cmd := composer.HandlePaste(m.composerSeams(), msg.Content)
		m.syncLayout()
		return m, cmd

	case tea.MouseWheelMsg:
		return m.handleMouseWheel(msg.Mouse())

	case tea.MouseClickMsg:
		return m.handleMouseClick(msg.Mouse())

	case tea.MouseMotionMsg:
		return m.handleMouseMotion(msg.Mouse())

	case tea.MouseReleaseMsg:
		return m.handleMouseRelease(msg.Mouse())

	case tea.MouseMsg:
		mMsg := msg.Mouse()
		switch mMsg.Button {
		case tea.MouseWheelUp, tea.MouseWheelDown:
			return m.handleMouseWheel(mMsg)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width - 1)
		if m.picker != nil {
			m.picker.Update([]rune(m.input.Value()))
		}
		pickerHeight := 0
		if m.picker != nil && m.picker.Visible() {
			if rows := m.picker.Rows(); rows != "" {
				pickerHeight = strings.Count(rows, "\n")
			}
		}

		inputHeight := m.input.Height()
		statusHeight := m.statusHeight()
		vpHeight := composer.CalculateViewportHeight(msg.Height, inputHeight+pickerHeight, statusHeight)

		if !m.ready {
			m.viewport = viewport.New(viewport.WithWidth(msg.Width), viewport.WithHeight(vpHeight))
			m.viewport.SetContent(m.renderLogs(msg.Width))
			m.viewport.GotoBottom()
			m.ready = true
		} else {
			wasAtBottom := m.viewport.AtBottom()
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(vpHeight)
			m.viewport.SetContent(m.renderLogs(msg.Width))
			if wasAtBottom {
				m.viewport.GotoBottom()
			}
		}

	case tickMsg:
		m.tickCount++
		cmds = append(cmds, tickEvery(250*time.Millisecond))

	case userSpeechMsg:
		if msg.interim {
			// Real-time speech recognition reflected directly in composer (aligns with AGY UI/UX)
			if !m.dictating {
				m.dictating = true
				m.dictationPrefix = m.input.Value()
			}
			text := msg.text
			if m.dictationPrefix != "" {
				text = strings.TrimRight(m.dictationPrefix, " ") + " " + msg.text
			}
			m.input.SetValue(text)
			m.input.CursorEnd()
			m.interimUser = msg.text
			m.syncLayout()
		} else {
			// Finalized speech recognition turn
			text := msg.text
			if m.dictationPrefix != "" {
				text = strings.TrimRight(m.dictationPrefix, " ") + " " + msg.text
			}
			m.dictating = false
			m.dictationPrefix = ""
			m.interimUser = ""
			m.thinking = true // User finished speaking; waiting for model to respond

			if m.watcher != nil {
				m.watcher.ResetPending()
			}

			m.appendLog(fmt.Sprintf("%s🎤 [User]:%s %s", ansiCyan+ansiBold, ansiReset, text))
			m.history.Push(text)
			m.input.Reset()
			m.selection = composer.Range{}
			m.syncLayout()
			m.saveSession()
		}

	case agentSpeechDeltaMsg:
		// Stream text in real-time as model outputs tokens / synthesizes voice
		m.thinking = false
		if !m.agentSpeaking {
			m.agentSpeaking = true
			m.agentTurnText.Reset()
			m.agentTurnText.WriteString(msg.delta)
			m.appendLog(fmt.Sprintf("%s🤖 [Agent]:%s %s", ansiGreen+ansiBold, ansiReset, msg.delta))
		} else {
			m.agentTurnText.WriteString(msg.delta)
			m.updateLastLog(fmt.Sprintf("%s🤖 [Agent]:%s %s", ansiGreen+ansiBold, ansiReset, m.agentTurnText.String()))
		}

	case agentSpeechMsg:
		m.thinking = false
		if m.agentSpeaking {
			m.updateLastLog(fmt.Sprintf("%s🤖 [Agent]:%s %s", ansiGreen+ansiBold, ansiReset, msg.text))
			m.agentSpeaking = false
			m.agentTurnText.Reset()
		} else {
			m.appendLog(fmt.Sprintf("%s🤖 [Agent]:%s %s", ansiGreen+ansiBold, ansiReset, msg.text))
		}
		m.saveSession()

	case agentTurnCompleteMsg:
		m.thinking = false
		m.agentSpeaking = false
		m.agentTurnText.Reset()
		m.saveSession()

	case interruptedMsg:
		m.thinking = false
		if m.agentSpeaking && m.agentTurnText.Len() > 0 {
			m.updateLastLog(fmt.Sprintf("%s🤖 [Agent]:%s %s %s[interrupted]%s",
				ansiGreen+ansiBold, ansiReset, m.agentTurnText.String(), ansiYellow, ansiReset))
		}
		m.agentSpeaking = false
		m.agentTurnText.Reset()
		m.appendLog(fmt.Sprintf("%s⚡ [Barge-in: Agent interrupted by user]%s", ansiYellow, ansiReset))

	case thinkerStartMsg:
		m.thinking = true
		m.appendLog(fmt.Sprintf("%s⚙️ [Thinker Task]: %s%s", ansiYellow+ansiBold, msg.task, ansiReset))

	case thinkerDoneMsg:
		m.thinking = false
		if msg.err != nil {
			if errors.Is(msg.err, context.Canceled) {
				m.appendLog(fmt.Sprintf("%s🛑 [Thinker Cancelled]: %s%s", ansiYellow+ansiBold, msg.task, ansiReset))
			} else {
				m.appendLog(fmt.Sprintf("%s❌ [Thinker Failed]: %s (%v)%s", ansiRed+ansiBold, msg.task, msg.err, ansiReset))
				if msg.result != "" {
					m.appendLog(fmt.Sprintf("%s%s%s", ansiDim, msg.result, ansiReset))
				}
			}
		} else {
			m.appendLog(fmt.Sprintf("%s✅ [Thinker Finished]: %s%s", ansiGreen+ansiBold, msg.task, ansiReset))
			if msg.result != "" {
				m.appendLog(fmt.Sprintf("%s%s%s", ansiDim, summarizeResult(msg.result), ansiReset))
			}
		}
		m.saveSession()

	case searchDoneMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("%s❌ [Search Failed]: %s (%v)%s", ansiRed+ansiBold, msg.query, msg.err, ansiReset))
		} else {
			summary := summarizeResult(msg.result)
			m.appendLog(fmt.Sprintf("%s✅ [Search Results]:%s\n%s%s%s", ansiGreen+ansiBold, ansiReset, ansiDim, summary, ansiReset))
		}
		m.saveSession()

	case voicePreviewMsg:
		switch {
		case msg.err == nil:
			m.appendLog(fmt.Sprintf("%s♪ Played %s%s", ansiGreen, msg.sample, ansiReset))
		case errors.Is(msg.err, voice.ErrNoAudioPlayer):
			m.appendLog(fmt.Sprintf("%s⚠️ [Voice Preview]: no audio player on PATH: %v%s", ansiYellow, msg.err, ansiReset))
		default:
			m.appendLog(fmt.Sprintf("%s❌ [Voice Preview]: %v%s", ansiRed, msg.err, ansiReset))
		}
		m.saveSession()

	case proactiveEventMsg:
		m.appendLog(fmt.Sprintf("%s🔔 [Proactive Observer]: Detected file changes in %s (%s)%s",
			ansiMagenta+ansiBold, filepath.Base(m.workspace), strings.Join(msg.files, ", "), ansiReset))

	case logEventMsg:
		m.appendLog(msg.text)

	case errorMsg:
		m.reconnecting = false
		m.disconnected = true
		m.appendLog(fmt.Sprintf("%s⚠️ [Error]: %v%s", ansiRed, msg.err, ansiReset))
		m.appendLog(fmt.Sprintf("%s💡 Tip: Type a message and press Enter, or type '/retry' to reconnect immediately.%s", ansiDim, ansiReset))

	case reconnectingMsg:
		m.reconnecting = true
		m.disconnected = false
		m.reconnectAttempt = msg.attempt
		m.reconnectMax = msg.maxAttempts
		if msg.maxAttempts > 0 {
			m.appendLog(fmt.Sprintf("%s🔄 [Network Disrupted]: Reconnecting to live session (attempt %d/%d)...%s", ansiYellow+ansiBold, msg.attempt, msg.maxAttempts, ansiReset))
		}

	case reconnectedMsg:
		m.reconnecting = false
		m.disconnected = false
		m.reconnectAttempt = 0
		m.appendLog(fmt.Sprintf("%s✨ [Reconnected]: Live voice session restored. Context rehydrated.%s", ansiGreen+ansiBold, ansiReset))
		if m.pendingText != "" {
			val := m.pendingText
			m.pendingText = ""
			m.appendLog(fmt.Sprintf("%s📤 [Dispatched Queued Message]:%s %s", ansiCyan+ansiBold, ansiReset, val))
			if m.sess != nil {
				if err := m.sess.SendText(val); err != nil {
					m.appendLog(fmt.Sprintf("%s⚠️ Failed to send queued text: %v%s", ansiRed, err, ansiReset))
				}
			}
			m.saveSession()
		}

	case tea.KeyMsg:
		key := msg.Key()

		// Copy shortcut (Cmd+C / Ctrl+C)
		if (key.Code == 'c' || key.Code == 'C') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModAlt) {
			if m.selection.Empty() && m.hasVpSelection() {
				text := m.viewportSelectionText()
				if text != "" {
					_ = clipboard.Write(text)
					return m, tea.SetClipboard(text)
				}
			}
		}

		// Slash Command Picker key intercept
		if m.picker != nil && m.picker.Visible() {
			switch key.Code {
			case tea.KeyUp:
				m.picker.HandleKey(key)
				m.syncLayout()
				return m, nil
			case tea.KeyDown:
				m.picker.HandleKey(key)
				m.syncLayout()
				return m, nil
			case tea.KeyTab:
				if handled, _ := m.picker.CommitSelectionCmd(); handled {
					m.picker.Update([]rune(m.input.Value()))
					m.syncLayout()
					return m, nil
				}
			case tea.KeyEscape:
				m.picker.Reset()
				m.syncLayout()
				return m, nil
			case tea.KeyEnter:
				if m.picker.Selected() != nil {
					handled, _ := m.picker.CommitSelectionCmd()
					if handled && m.picker.Visible() {
						// Intermediate selection like "/voice speaker " -> keep open for voice selection
						m.syncLayout()
						return m, nil
					}
					m.picker.Reset()
				}
			}
		}

		// Global Hotkeys
		switch key.Code {
		case tea.KeyF6:
			muted := m.sess.ToggleMute()
			if muted {
				m.appendLog(fmt.Sprintf("%s🎤 Microphone MUTED [Press F6 to unmute]%s", ansiYellow, ansiReset))
			} else {
				m.appendLog(fmt.Sprintf("%s🎤 Microphone UNMUTED%s", ansiGreen, ansiReset))
			}
			return m, nil

		case tea.KeyEscape:
			if m.hasVpSelection() {
				m.resetVpSelection()
				return m, nil
			}
			if m.input.Value() != "" {
				// Clear composer content
				m.input.Reset()
				if m.picker != nil {
					m.picker.Reset()
				}
				m.selection = composer.Range{}
				m.dictating = false
				m.dictationPrefix = ""
				m.interimUser = ""
				m.syncLayout()
				return m, nil
			}
			// Empty composer on Esc -> save session, hang up and quit
			m.saveSession()
			_ = m.sess.HangUp()
			return m, tea.Quit

		case tea.KeyPgUp:
			m.resetVpSelection()
			m.viewport.HalfPageUp()
			return m, nil

		case tea.KeyPgDown:
			m.resetVpSelection()
			m.viewport.HalfPageDown()
			return m, nil

		case tea.KeyHome:
			if key.Mod.Contains(tea.ModCtrl) {
				m.resetVpSelection()
				m.viewport.GotoTop()
				return m, nil
			}

		case tea.KeyEnd:
			if key.Mod.Contains(tea.ModCtrl) {
				m.resetVpSelection()
				m.viewport.GotoBottom()
				return m, nil
			}

		case tea.KeyEnter:
			if composer.BackslashNewline(m.composerSeams()) {
				m.syncLayout()
				return m, nil
			}
			// Shift+Enter / Alt+Enter inserts newline
			if key.Mod.Contains(tea.ModShift) || key.Mod.Contains(tea.ModAlt) {
				m.input.InsertRune('\n')
				m.syncLayout()
				return m, nil
			}

			val := strings.TrimSpace(m.input.Value())
			if val == "" {
				return m, nil
			}

			m.history.Push(val)
			m.input.Reset()
			if m.picker != nil {
				m.picker.Reset()
			}
			m.selection = composer.Range{}
			m.dictating = false
			m.dictationPrefix = ""
			m.interimUser = ""
			m.syncLayout()
			m.viewport.GotoBottom()
			if m.watcher != nil {
				m.watcher.ResetPending()
			}

			lowerVal := strings.ToLower(val)
			switch {
			case lowerVal == "/q" || lowerVal == "/quit" || lowerVal == "/exit":
				m.saveSession()
				_ = m.sess.HangUp()
				return m, tea.Quit
			case lowerVal == "/retry" || lowerVal == "/reconnect":
				m.appendLog(fmt.Sprintf("%s🔄 [Reconnecting]: Triggering connection retry immediately...%s", ansiYellow+ansiBold, ansiReset))
				if m.sess != nil {
					_ = m.sess.ReconnectNow()
				}
				return m, nil
			case lowerVal == "/m" || lowerVal == "/mute":
				muted := m.sess.ToggleMute()
				if muted {
					m.appendLog(fmt.Sprintf("%s🎤 Microphone MUTED%s", ansiYellow, ansiReset))
				} else {
					m.appendLog(fmt.Sprintf("%s🎤 Microphone UNMUTED%s", ansiGreen, ansiReset))
				}
				return m, nil
			case lowerVal == "/d" || lowerVal == "/duplex":
				m.sess.SetDuplex(!m.sess.IsDuplex())
				m.appendLog(fmt.Sprintf("%sDuplex mode: %v%s", ansiDim, m.sess.IsDuplex(), ansiReset))
				return m, nil
			case lowerVal == "/p off" || lowerVal == "/proactive off":
				if m.watcher != nil {
					m.watcher.SetPaused(true)
				}
				m.appendLog(fmt.Sprintf("%s🔕 [Proactive Observer]: Workspace file monitoring PAUSED%s", ansiYellow, ansiReset))
				return m, nil
			case lowerVal == "/p on" || lowerVal == "/proactive on":
				if m.watcher != nil {
					m.watcher.SetPaused(false)
					m.watcher.ResetPending()
				}
				m.appendLog(fmt.Sprintf("%s🔔 [Proactive Observer]: Workspace file monitoring RESUMED%s", ansiGreen, ansiReset))
				return m, nil
			case lowerVal == "/p" || lowerVal == "/proactive":
				m.appendLog(fmt.Sprintf("%s[Manual Proactive Check Triggered]%s", ansiMagenta, ansiReset))
				if m.sess != nil {
					status := m.watcher.checkGitStatus()
					if status == "" {
						_ = m.sess.SendText("[PROACTIVE_EVENT: Manual check triggered. Workspace is completely clean. Tell the user everything looks clean.]")
					} else {
						files := parseChangedFiles(status)
						_ = m.sess.SendText(fmt.Sprintf("[PROACTIVE_EVENT: Manual check triggered. Files changed: %s. Inform the user.]", strings.Join(files, ", ")))
					}
				}
				return m, nil
			case lowerVal == "/model" || lowerVal == "/mdl":
				return m.handleModelList()
			case strings.HasPrefix(lowerVal, "/model "):
				return m.handleModelArgs(val[len("/model "):])
			case strings.HasPrefix(lowerVal, "/mdl "):
				return m.handleModelArgs(val[len("/mdl "):])
			case lowerVal == "/voice" || lowerVal == "/speaker":
				return m.handleVoiceList()
			case strings.HasPrefix(lowerVal, "/voice "):
				return m.handleVoiceArgs(val[len("/voice "):])
			case strings.HasPrefix(lowerVal, "/speaker "):
				return m.handleVoiceArgs(val[len("/speaker "):])
			case lowerVal == "/search" || lowerVal == "/web":
				m.appendLog(fmt.Sprintf("%s💡 Usage: /search <query> (e.g. /search golang releases)%s", ansiDim, ansiReset))
				return m, nil
			case strings.HasPrefix(lowerVal, "/search "):
				query := strings.TrimSpace(val[len("/search "):])
				return m.handleSearch(query)
			case strings.HasPrefix(lowerVal, "/web "):
				query := strings.TrimSpace(val[len("/web "):])
				return m.handleSearch(query)
			case lowerVal == "/resume" || lowerVal == "/sessions":
				return m.handleResumeList()
			case strings.HasPrefix(lowerVal, "/resume "):
				arg := strings.TrimSpace(val[len("/resume "):])
				if arg == "" {
					return m.handleResumeList()
				}
				return m.handleResumeSession(arg)
			default:
				if m.disconnected || m.reconnecting || (m.sess != nil && (m.sess.IsReconnecting() || m.sess.ConnectionState() == voice.StateDisconnected)) {
					m.pendingText = val
					m.appendLog(fmt.Sprintf("%s💬 [You (Queued)]:%s %s", ansiCyan, ansiReset, val))
					m.appendLog(fmt.Sprintf("%s🔄 [Reconnecting]: Live session is currently offline/reconnecting. Triggering immediate reconnection...%s", ansiYellow+ansiBold, ansiReset))
					if m.sess != nil {
						_ = m.sess.ReconnectNow()
					}
					return m, nil
				}

				m.appendLog(fmt.Sprintf("%s💬 [You]:%s %s", ansiCyan, ansiReset, val))
				if m.sess != nil {
					if err := m.sess.SendText(val); err != nil {
						m.appendLog(fmt.Sprintf("%s⚠️ Failed to send text: %v%s", ansiRed, err, ansiReset))
					}
				}
				m.saveSession()
				return m, nil
			}

		case tea.KeyLeft:
			if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
				seams := m.composerSeams()
				composer.HandleWordLeft(seams)
				return m, nil
			}

		case tea.KeyRight:
			if (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModCtrl)) && !key.Mod.Contains(tea.ModShift) {
				seams := m.composerSeams()
				composer.HandleWordRight(seams)
				return m, nil
			}

		case tea.KeyUp:
			if key.Mod.Contains(tea.ModShift) {
				m.resetVpSelection()
				m.viewport.ScrollUp(1)
				return m, nil
			}
			// If at line 0, recall history; otherwise navigate lines upward
			if m.input.Line() == 0 {
				if recalled, ok := m.history.Up(m.input.Value()); ok {
					m.input.SetValue(recalled)
					if m.picker != nil {
						m.picker.Update([]rune(m.input.Value()))
					}
					m.syncLayout()
					return m, nil
				}
			} else {
				m.input.CursorUp()
				return m, nil
			}

		case tea.KeyDown:
			if key.Mod.Contains(tea.ModShift) {
				m.resetVpSelection()
				m.viewport.ScrollDown(1)
				return m, nil
			}
			// If at the last line, recall history; otherwise navigate lines downward
			lines := strings.Split(m.input.Value(), "\n")
			if m.input.Line() >= len(lines)-1 {
				if recalled, ok := m.history.Down(); ok {
					m.input.SetValue(recalled)
					if m.picker != nil {
						m.picker.Update([]rune(m.input.Value()))
					}
					m.syncLayout()
					return m, nil
				}
			} else {
				m.input.CursorDown()
				return m, nil
			}
		}

		// Clear viewport selection when typing or editing in composer
		if m.hasVpSelection() {
			m.resetVpSelection()
		}

		// Delegate to composer selection, word jump, undo/redo, and editing mechanics
		seams := m.composerSeams()
		if cmd, handled := composer.HandleKey(seams, key); handled {
			if m.picker != nil {
				m.picker.Update([]rune(m.input.Value()))
			}
			m.syncLayout()
			if cmd != nil {
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		// Update textarea with character insertions / deletions
		cmd := composer.UpdateTextarea(seams, msg)
		if m.picker != nil {
			m.picker.Update([]rune(m.input.Value()))
		}
		m.syncLayout()
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)
	}

	return m, tea.Batch(cmds...)
}

func (m *liveChatModel) renderStatusBar() string {
	mins, secs := 0, 0
	stateDot := "\x1b[38;5;82m●\x1b[0m" // Green
	muteAction := "mute"

	level := 0
	isMuted := false
	isSpeaking := m.agentSpeaking

	if m.sess != nil {
		mins = int(m.sess.Elapsed().Minutes())
		secs = int(m.sess.Elapsed().Seconds()) % 60
		level = m.sess.InputLevel()
		isMuted = m.sess.IsMuted()
		if m.sess.Speaking() {
			isSpeaking = true
		}
	}
	timeStr := fmt.Sprintf("%02d:%02d", mins, secs)

	// Gboard / Gemini Live Mode Indicators
	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinnerChar := spinners[m.tickCount%len(spinners)]

	var indicator string
	isReconnecting := m.reconnecting || (m.sess != nil && m.sess.IsActive() && m.sess.ConnectionState() == voice.StateReconnecting)
	isOffline := !isReconnecting && (m.disconnected || (m.sess != nil && m.sess.IsActive() && m.sess.ConnectionState() == voice.StateDisconnected))

	switch {
	case isReconnecting:
		stateDot = "\x1b[38;5;214m●\x1b[0m" // Amber/Orange
		if m.reconnectMax > 0 {
			indicator = fmt.Sprintf("\x1b[38;5;214mRECONNECTING (%d/%d) %s\x1b[39m", m.reconnectAttempt, m.reconnectMax, spinnerChar)
		} else {
			indicator = fmt.Sprintf("\x1b[38;5;214mRECONNECTING (attempt %d) %s\x1b[39m", m.reconnectAttempt, spinnerChar)
		}
	case isOffline:
		stateDot = "\x1b[38;5;196m●\x1b[0m" // Red
		indicator = "\x1b[38;5;196mOFFLINE ⚠️ [Press Enter to retry]\x1b[39m"
	case isMuted:
		stateDot = "\x1b[38;5;244m●\x1b[0m"
		muteAction = "unmute"
		indicator = "\x1b[38;5;244mMUTED 🔇\x1b[39m"
	case isSpeaking:
		stateDot = "\x1b[38;5;111m●\x1b[0m"
		indicator = fmt.Sprintf("\x1b[38;5;111mSPEAKING 🔊 [ \x1b[1m%s\x1b[0m\x1b[38;5;111m ]\x1b[39m", renderSpeakerWave(m.tickCount))
	case m.thinking:
		stateDot = "\x1b[38;5;221m●\x1b[0m"
		indicator = fmt.Sprintf("\x1b[38;5;221mTHINKING %s\x1b[39m", spinnerChar)
	case level > 5 || m.interimUser != "":
		stateDot = "\x1b[38;5;82m●\x1b[0m"
		indicator = fmt.Sprintf("\x1b[38;5;82mHEARING [ %s ]\x1b[39m", renderAudioWaveform(level))
	default:
		stateDot = "\x1b[38;5;71m●\x1b[0m"
		indicator = "\x1b[38;5;71mLISTENING [ ⠂⠂⠂⠂⠂ ]\x1b[39m"
	}

	statusLine := fmt.Sprintf("  %s Live [%s] \x1b[38;5;109m%s · %s · \x1b[38;5;111mF6\x1b[38;5;109m %s · \x1b[38;5;111mEsc\x1b[38;5;109m hang up · \x1b[38;5;109m%s\x1b[39m",
		stateDot, m.sessionID, timeStr, indicator, muteAction, m.voiceName)

	if m.interimUser != "" {
		statusLine += fmt.Sprintf("  \x1b[38;5;117m🎙️  %s...\x1b[39m", m.interimUser)
	}

	if m.debugWire {
		var stats voice.WireStats
		if m.sess != nil {
			stats = m.sess.WireStats()
		}
		latStr := "--"
		if stats.LastTurnLatency > 0 {
			latStr = fmt.Sprintf("%dms", stats.LastTurnLatency.Milliseconds())
		}
		wireLine := fmt.Sprintf("  \x1b[38;5;208m[WIRE]\x1b[39m TX: \x1b[1m%s\x1b[0m (%d pkts) | RX Audio: \x1b[1m%s\x1b[0m (%d pkts) | RX Text: %d | Turns: %d | Latency: %s",
			voice.FormatBytes(stats.AudioSentBytes),
			stats.AudioSentChunks,
			voice.FormatBytes(stats.AudioRecvBytes),
			stats.AudioRecvChunks,
			stats.TextChunksRecv,
			stats.TurnsCompleted,
			latStr,
		)
		statusLine += "\n" + wireLine
	}

	return statusLine
}

func renderAudioWaveform(level int) string {
	bars := []rune{' ', '▂', '▃', '▅', '▆', '▇', '█'}
	if level <= 0 {
		return "\x1b[38;5;240m⠂⠂⠂⠂⠂\x1b[39m"
	}
	idx := (level * (len(bars) - 1)) / 100
	if idx >= len(bars) {
		idx = len(bars) - 1
	}
	i1 := idx / 3
	i2 := (idx * 2) / 3
	i3 := idx
	return fmt.Sprintf("\x1b[38;5;82m%c%c%c%c%c\x1b[39m", bars[i1], bars[i2], bars[i3], bars[i2], bars[i1])
}

func renderSpeakerWave(tick int) string {
	waves := []string{"∿ ∿ ∿", "∿ ∿ ∼", "∼ ∿ ∿", "∿ ∼ ∿"}
	return waves[tick%len(waves)]
}

func (m *liveChatModel) View() tea.View {
	if !m.ready {
		return tea.NewView("Initializing LiveChat...\n")
	}

	vpView := m.renderViewportView()
	inputView := m.input.View()
	if !m.selection.Empty() {
		inputView = inputselection.ApplyHighlight(inputView, m.input.Value(), m.selection.Start, m.selection.End)
	}

	composerFrame := composer.DualRulesFrame(m.width, inputView)
	statusBar := m.renderStatusBar()

	pickerHeight := 0
	var pickerRows string
	if m.picker != nil && m.picker.Visible() {
		pickerRows = m.picker.Rows()
		if pickerRows != "" {
			pickerHeight = strings.Count(pickerRows, "\n")
		}
	}

	var body string
	if pickerRows != "" {
		body = vpView + "\n" + pickerRows + composerFrame + "\n" + statusBar
	} else {
		body = vpView + "\n" + composerFrame + "\n" + statusBar
	}
	v := tea.NewView(body)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion

	// Position hardware cursor in the composer input area
	if m.input.Focused() {
		cursorX := 2 + m.input.Column()
		if c := m.input.Cursor(); c != nil {
			cursorX = c.X
		}
		cursorY := m.viewport.Height() + pickerHeight + 1 + m.input.Line()
		v.Cursor = &tea.Cursor{X: cursorX, Y: cursorY}
	}

	return v
}

// Handler bridge that directs NativeLive events into Bubbletea messages
