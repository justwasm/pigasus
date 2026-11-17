package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	tas "charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/app/tools/web"
	"github.com/justwasm/pigasus/internal/core/widthx"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/editor/composer"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	inputselection "github.com/justwasm/pigasus/internal/ui/foundations/inputselection"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/views/slashpicker"
	"github.com/justwasm/pigasus/internal/voice"
)

// performWebSearch is the pluggable web search function, defaulting to web.PerformWebSearch.
var performWebSearch = web.PerformWebSearch

// playVoicePreview is the pluggable recorded-sample player, defaulting to
// voice.PlayVoicePreview (which shells out to afplay / paplay / ffplay).
var playVoicePreview = voice.PlayVoicePreview

// ANSI Styling Helpers
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiCyan    = "\x1b[36m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiRed     = "\x1b[31m"
	ansiMagenta = "\x1b[35m"
	ansiBlue    = "\x1b[34m"
	ansiGray    = "\x1b[38;5;244m"
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

func liveChatCommands() []slashcommands.Command {
	return []slashcommands.Command{
		{Name: "/search", Description: "Search the web and feed results to agent (e.g. /search golang releases)", Aliases: []string{"web", "google"}},
		{Name: "/voice", Description: "Switch, view or audition voice personas (e.g. /voice cleo, /voice preview tari 2)", Aliases: []string{"speaker"}},
		{Name: "/resume", Description: "List and resume saved LiveChat sessions (e.g. /resume latest)", Aliases: []string{"sessions"}},
		{Name: "/retry", Description: "Force immediate reconnection to live voice session", Aliases: []string{"reconnect"}},
		{Name: "/mute", Description: "Toggle microphone mute state (or press F6)", Aliases: []string{"m"}},
		{Name: "/duplex", Description: "Toggle full-duplex barge-in mode (/duplex on, /duplex off)", Aliases: []string{"d"}},
		{Name: "/proactive", Description: "Control workspace file watcher (/p on, /p off, /p)", Aliases: []string{"p"}},
		{Name: "/quit", Description: "Exit LiveChat session (or Esc)", Aliases: []string{"q", "exit"}},
	}
}

func (m *liveChatModel) LoadSession(data *voice.LiveSessionData) {
	if data == nil {
		return
	}
	m.sessionID = data.ID
	m.sessionCreatedAt = data.CreatedAt
	if data.Voice != "" {
		m.voiceName = data.Voice
	}
	m.logs = append(data.Logs, fmt.Sprintf("%s✨ [Session Resumed]: Restored context from %s (%d turns)%s",
		ansiGreen+ansiBold, data.ID, len(data.Turns), ansiReset))
	if m.ready {
		m.viewport.SetContent(m.renderLogs(m.viewport.Width()))
		m.viewport.GotoBottom()
	}
	for _, t := range data.Turns {
		if strings.EqualFold(t.Role, "User") {
			m.history.Push(t.Content)
		}
	}
	if m.sess != nil {
		m.sess.SetConversationHistory(data.Turns)
	}
}

func (m *liveChatModel) saveSession() {
	if m.sessionID == "" {
		m.sessionID = voice.GenerateLiveSessionID()
	}
	var turns []voice.LiveTurnRecord
	if m.sess != nil {
		turns = m.sess.ConversationHistory()
	}
	data := &voice.LiveSessionData{
		ID:        m.sessionID,
		Workspace: m.workspace,
		Model:     m.modelName,
		Voice:     m.voiceName,
		CreatedAt: m.sessionCreatedAt,
		Turns:     turns,
		Logs:      m.logs,
	}
	_ = voice.SaveLiveSession(data)
}

func (m *liveChatModel) handleResumeList() (tea.Model, tea.Cmd) {
	summaries, err := voice.ListLiveSessions(m.workspace)
	if err != nil {
		m.appendLog(fmt.Sprintf("%s⚠️ Failed to list sessions: %v%s", ansiRed, err, ansiReset))
		return m, nil
	}
	if len(summaries) == 0 {
		all, _ := voice.ListLiveSessions()
		if len(all) == 0 {
			m.appendLog(fmt.Sprintf("%sℹ️ No saved LiveChat sessions found.%s", ansiYellow, ansiReset))
			return m, nil
		}
		summaries = all
	}

	m.appendLog(fmt.Sprintf("%s📜 [Saved LiveChat Sessions]:%s", ansiCyan+ansiBold, ansiReset))
	for i, s := range summaries {
		currentTag := ""
		if s.ID == m.sessionID {
			currentTag = fmt.Sprintf(" %s[CURRENT]%s", ansiGreen+ansiBold, ansiReset)
		}
		m.appendLog(fmt.Sprintf("  #%d [%s]%s · %s · %d turns · %q",
			i+1, s.ID, currentTag, s.UpdatedAt.Format("2006-01-02 15:04"), s.TurnCount, s.Title))
	}
	m.appendLog(fmt.Sprintf("%s💡 Tip: Type '/resume <id>' or '/resume <#>' to restore conversation history.%s", ansiDim, ansiReset))
	return m, nil
}

func (m *liveChatModel) handleResumeSession(target string) (tea.Model, tea.Cmd) {
	saved, err := voice.FindLiveSession(target, m.workspace)
	if err != nil {
		m.appendLog(fmt.Sprintf("%s⚠️ Cannot resume session %q: %v%s", ansiRed, target, err, ansiReset))
		return m, nil
	}

	if saved.ID == m.sessionID {
		m.appendLog(fmt.Sprintf("%sℹ️ Session %s is already active.%s", ansiDim, saved.ID, ansiReset))
		return m, nil
	}

	// 1. Auto-save current session before switching
	m.saveSession()

	// 2. Switch session ID and timestamp
	m.sessionID = saved.ID
	m.sessionCreatedAt = saved.CreatedAt
	if saved.Voice != "" {
		m.voiceName = saved.Voice
	}

	// 3. Rehydrate logs into viewport
	m.logs = append(saved.Logs, fmt.Sprintf("%s✨ [Session Resumed]: Switched to session %s (%d turns)%s",
		ansiGreen+ansiBold, saved.ID, len(saved.Turns), ansiReset))
	if m.ready {
		m.viewport.SetContent(m.renderLogs(m.viewport.Width()))
		m.viewport.GotoBottom()
	}

	// 4. Populate composer prompt history with past user turns
	for _, t := range saved.Turns {
		if strings.EqualFold(t.Role, "User") {
			m.history.Push(t.Content)
		}
	}

	// 5. Rehydrate speech context in NativeLiveSession and push to Gemini Live
	if m.sess != nil {
		m.sess.SetConversationHistory(saved.Turns)
		_ = m.sess.RehydrateResumeContext()
	}

	m.appendLog(fmt.Sprintf("%s🔄 Live voice context rehydrated with %d previous turns.%s", ansiGreen, len(saved.Turns), ansiReset))
	m.syncLayout()
	return m, nil
}

func (m *liveChatModel) handleSearch(query string) (tea.Model, tea.Cmd) {
	if query == "" {
		m.appendLog(fmt.Sprintf("%s💡 Usage: /search <query>%s", ansiDim, ansiReset))
		return m, nil
	}
	m.appendLog(fmt.Sprintf("%s🔍 [Web Search]: %s%s", ansiCyan+ansiBold, query, ansiReset))
	return m, searchCmd(query, m.sess)
}

func searchCmd(query string, sess *voice.NativeLiveSession) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		result, err := performWebSearch(ctx, query, "")
		if err == nil && sess != nil {
			_ = sess.SendText(fmt.Sprintf("[SYSTEM NOTE: Web search results for %q:\n%s\nAcknowledge and summarize for the user.]", query, result))
		}
		return searchDoneMsg{query: query, result: result, err: err}
	}
}

func (m *liveChatModel) handleVoiceList() (tea.Model, tea.Cmd) {
	currentPersona, _ := voice.FindVoicePersona(m.voiceName)
	m.appendLog(fmt.Sprintf("%s🎙️ [Current Voice Persona]:%s %s (%s) · Gemini Live Voice: %s",
		ansiCyan+ansiBold, ansiReset, m.voiceName, currentPersona.Description, voice.MapVoiceName(m.voiceName)))
	m.appendLog(fmt.Sprintf("%s🎭 [Antigravity 9 Canonical Personas]:%s", ansiGreen+ansiBold, ansiReset))
	for _, p := range voice.CanonicalVoicePersonas {
		curr := ""
		if strings.EqualFold(p.Name, m.voiceName) {
			curr = fmt.Sprintf(" %s[CURRENT]%s", ansiGreen+ansiBold, ansiReset)
		}
		sample := "  "
		if voice.HasVoicePreview(p.Name) {
			sample = "♪ "
		}
		m.appendLog(fmt.Sprintf("  %s• %-5s · %-28s · Tone: %-6s%s -> Gemini: %s",
			sample, p.Name, p.Description, p.Tone, curr, p.GeminiVoice))
	}
	m.appendLog(fmt.Sprintf("%s✨ [Direct Gemini Live Voices]:%s %s",
		ansiDim, ansiReset, strings.Join(voice.GeminiPrebuiltVoices, ", ")))
	m.appendLog(fmt.Sprintf("%s💡 Tip: Type '/voice <name>' or '/voice speaker <name>' to switch voice persona in real time.%s", ansiDim, ansiReset))
	m.appendLog(fmt.Sprintf("%s🔊 Tip: Type '/voice preview <name> [1-%d|all]' to audition a recorded sample (♪ marks the nine recorded personas); '/voice preview off' silences the clip played on switch.%s",
		ansiDim, voice.VoicePreviewVariants, ansiReset))
	return m, nil
}

// handleVoiceArgs routes the arguments of /voice and /speaker so both spellings
// accept the same subcommands.
func (m *liveChatModel) handleVoiceArgs(arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	lower := strings.ToLower(arg)
	if strings.HasPrefix(lower, "speaker ") {
		arg = strings.TrimSpace(arg[len("speaker "):])
		lower = strings.ToLower(arg)
	}
	switch {
	case arg == "":
		return m.handleVoiceList()
	case lower == "list":
		return m.handleVoiceList()
	case lower == "preview", strings.HasPrefix(lower, "preview "):
		return m.handleVoicePreview(strings.TrimSpace(arg[len("preview"):]))
	}
	return m.handleVoiceSwitch(arg)
}

// handleVoicePreview auditions a recorded persona clip without switching voice.
func (m *liveChatModel) handleVoicePreview(args string) (tea.Model, tea.Cmd) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		m.appendLog(fmt.Sprintf("%s🔊 [Voice Preview]: %d recorded personas · %d samples each%s",
			ansiCyan+ansiBold, len(voice.VoicePreviewPersonas()), voice.VoicePreviewVariants, ansiReset))
		m.appendLog(fmt.Sprintf("  %s", strings.Join(voice.VoicePreviewPersonas(), ", ")))
		m.appendLog(fmt.Sprintf("%s💡 Usage: /voice preview <name> [1-%d|all] · /voice preview off|on (switch clips)%s",
			ansiDim, voice.VoicePreviewVariants, ansiReset))
		return m, nil
	}

	switch strings.ToLower(fields[0]) {
	case "off":
		m.voicePreview = false
		m.appendLog(fmt.Sprintf("%s🔇 [Voice Preview]: switch-time clips disabled (explicit /voice preview still plays)%s",
			ansiYellow+ansiBold, ansiReset))
		return m, nil
	case "on":
		m.voicePreview = true
		m.appendLog(fmt.Sprintf("%s🔊 [Voice Preview]: switch-time clips enabled%s", ansiGreen+ansiBold, ansiReset))
		return m, nil
	}

	target := fields[0]
	if !voice.HasVoicePreview(target) {
		persona, ok := voice.FindVoicePersona(target)
		hint := ""
		if ok {
			hint = fmt.Sprintf(" (resolves to Gemini %s)", voice.MapVoiceName(persona.GeminiVoice))
		}
		m.appendLog(fmt.Sprintf("%s🎙️ [Voice Preview]: no recording for %q%s — recorded: %s%s",
			ansiYellow+ansiBold, target, hint, strings.Join(voice.VoicePreviewPersonas(), ", "), ansiReset))
		return m, nil
	}

	variantArg := strings.Join(fields[1:], " ")
	variant, playAll, err := voice.ParseVoicePreviewVariant(variantArg)
	if err != nil {
		m.appendLog(fmt.Sprintf("%s❌ [Voice Preview]: %v%s", ansiRed, err, ansiReset))
		return m, nil
	}
	if playAll {
		m.appendLog(fmt.Sprintf("%s🔊 [Voice Preview]: playing all %d samples of %s%s",
			ansiCyan+ansiBold, voice.VoicePreviewVariants, target, ansiReset))
		return m, voicePreviewCmd(target, 0, true)
	}
	m.appendLog(fmt.Sprintf("%s🔊 [Voice Preview]: playing %s sample %d/%d%s",
		ansiCyan+ansiBold, target, variant, voice.VoicePreviewVariants, ansiReset))
	return m, voicePreviewCmd(target, variant, false)
}

// voicePreviewCmd plays one sample, or every variant in order when playAll is set.
func voicePreviewCmd(name string, variant int, playAll bool) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), voice.VoicePreviewTimeout)
		defer cancel()

		if !playAll {
			sample, err := playVoicePreview(ctx, name, variant)
			return voicePreviewMsg{sample: sample, err: err}
		}

		var last voice.VoicePreviewSample
		for v := 1; v <= voice.VoicePreviewVariants; v++ {
			sample, err := playVoicePreview(ctx, name, v)
			if err != nil {
				return voicePreviewMsg{sample: sample, err: err}
			}
			last = sample
		}
		return voicePreviewMsg{sample: last}
	}
}

func (m *liveChatModel) handleVoiceSwitch(name string) (tea.Model, tea.Cmd) {
	persona, ok := voice.FindVoicePersona(name)
	if !ok {
		geminiVoice := voice.MapVoiceName(name)
		persona = voice.VoicePersona{
			Name:        name,
			Description: "Custom voice",
			GeminiVoice: geminiVoice,
		}
	}

	m.voiceName = persona.Name
	geminiVoice := voice.MapVoiceName(persona.Name)

	if m.sess != nil {
		if err := m.sess.SwitchVoice(context.Background(), geminiVoice); err != nil {
			m.appendLog(fmt.Sprintf("%s⚠️ Failed to switch voice: %v%s", ansiRed, err, ansiReset))
			return m, nil
		}
	}

	m.appendLog(fmt.Sprintf("%s✨ [Voice Updated]: Switched to persona %s (%s, mapped to Gemini %s)%s",
		ansiGreen+ansiBold, persona.Name, persona.Description, geminiVoice, ansiReset))
	m.saveSession()

	if m.voicePreview && voice.HasVoicePreview(persona.Name) {
		m.appendLog(fmt.Sprintf("%s🔊 [Voice Preview]: playing %s sample 1/%d (silence with /voice preview off)%s",
			ansiCyan, persona.Name, voice.VoicePreviewVariants, ansiReset))
		return m, voicePreviewCmd(persona.Name, 1, false)
	}
	return m, nil
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

func (m *liveChatModel) renderLogs(width int) string {
	if len(m.logs) == 0 {
		return ""
	}
	if width <= 0 {
		width = 80
	}
	var wrapped []string
	for _, l := range m.logs {
		lines := widthx.WrapTextWithAnsi(l, width)
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
		m.appendLog(fmt.Sprintf("%s⚙️ [Thinker Task]: %s%s", ansiYellow+ansiBold, msg.task, ansiReset))

	case thinkerDoneMsg:
		if msg.err != nil {
			m.appendLog(fmt.Sprintf("%s❌ [Thinker Failed]: %s (%v)%s", ansiRed+ansiBold, msg.task, msg.err, ansiReset))
			if msg.result != "" {
				m.appendLog(fmt.Sprintf("%s%s%s", ansiDim, msg.result, ansiReset))
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
type liveChatHandlerBridge struct {
	prog      *tea.Program
	workspace string
	sess      *voice.NativeLiveSession
}

func (h *liveChatHandlerBridge) OnUserSpeech(text string, interim bool) {
	if h.prog != nil {
		h.prog.Send(userSpeechMsg{text: text, interim: interim})
	}
}

func (h *liveChatHandlerBridge) OnAgentSpeech(text string) {
	if h.prog != nil {
		h.prog.Send(agentSpeechMsg{text: text})
	}
}

func (h *liveChatHandlerBridge) OnAgentSpeechDelta(delta string) {
	if h.prog != nil {
		h.prog.Send(agentSpeechDeltaMsg{delta: delta})
	}
}

func (h *liveChatHandlerBridge) OnTurnComplete() {
	if h.prog != nil {
		h.prog.Send(agentTurnCompleteMsg{})
	}
}

func (h *liveChatHandlerBridge) OnInterrupted() {
	if h.prog != nil {
		h.prog.Send(interruptedMsg{})
	}
}

func (h *liveChatHandlerBridge) OnError(err error) {
	if h.prog != nil && err != nil {
		h.prog.Send(errorMsg{err: err})
	}
}

func (h *liveChatHandlerBridge) OnClose() {
	if h.prog != nil {
		h.prog.Send(logEventMsg{text: fmt.Sprintf("%s📞 [Live session closed]%s", ansiDim, ansiReset)})
	}
}

func (h *liveChatHandlerBridge) OnReconnecting(attempt int, maxAttempts int, err error) {
	if h.prog != nil {
		h.prog.Send(reconnectingMsg{attempt: attempt, maxAttempts: maxAttempts, err: err})
	}
}

func (h *liveChatHandlerBridge) OnReconnected() {
	if h.prog != nil {
		h.prog.Send(reconnectedMsg{})
	}
}

func (h *liveChatHandlerBridge) OnToolCall(call voice.LiveToolCall) {
	switch call.Name {
	case "search_web", "web_search":
		query, _ := call.Args["query"].(string)
		if query == "" {
			query, _ = call.Args["q"].(string)
		}
		domain, _ := call.Args["domain"].(string)

		if h.prog != nil {
			msg := fmt.Sprintf("%s🔍 [Web Search]: %s%s", ansiCyan+ansiBold, query, ansiReset)
			if domain != "" {
				msg += fmt.Sprintf(" %s(site: %s)%s", ansiDim, domain, ansiReset)
			}
			h.prog.Send(logEventMsg{text: msg})
		}

		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			result, err := performWebSearch(ctx, query, domain)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Search Failed]: %v%s", ansiRed, err, ansiReset)})
				}
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				return
			}
			if h.prog != nil {
				summary := summarizeResult(result)
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Search Results]:%s\n%s%s%s", ansiGreen, ansiReset, ansiDim, summary, ansiReset)})
			}
			_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"result": result})
		}()

	case "fetch_url":
		url, _ := call.Args["url"].(string)
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s🌐 [Fetch URL]: %s%s", ansiCyan+ansiBold, url, ansiReset)})
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			content, err := fetchURLContent(ctx, url)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Fetch Failed]: %v%s", ansiRed, err, ansiReset)})
				}
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				return
			}
			if h.prog != nil {
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Fetched]: %d bytes from %s%s", ansiGreen, len(content), url, ansiReset)})
			}
			_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"content": content})
		}()

	case "run_command":
		cmdStr, _ := call.Args["command"].(string)
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s⚙️ [Command]: %s%s", ansiYellow+ansiBold, cmdStr, ansiReset)})
		}
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			out, err := executeRawCommand(ctx, h.workspace, cmdStr)
			if err != nil {
				if h.prog != nil {
					h.prog.Send(logEventMsg{text: fmt.Sprintf("%s❌ [Command Failed]: %v%s\n%s%s%s", ansiRed, err, ansiReset, ansiDim, out, ansiReset)})
				}
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"output": out, "error": err.Error()})
				return
			}
			if h.prog != nil {
				h.prog.Send(logEventMsg{text: fmt.Sprintf("%s✅ [Command Output]:%s\n%s%s%s", ansiGreen, ansiReset, ansiDim, out, ansiReset)})
			}
			_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"output": out})
		}()

	case "read_file":
		path, _ := call.Args["path"].(string)
		if h.prog != nil {
			h.prog.Send(logEventMsg{text: fmt.Sprintf("%s📄 [Read File]: %s%s", ansiCyan, path, ansiReset)})
		}
		go func() {
			fullPath := filepath.Join(h.workspace, path)
			data, err := os.ReadFile(fullPath)
			if err != nil {
				_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"error": err.Error()})
				return
			}
			content := string(data)
			if len(content) > 4000 {
				content = content[:4000] + "\n...(truncated)"
			}
			_ = h.sess.SendToolResponse(call.ID, call.Name, map[string]any{"content": content})
		}()
	}
}

func (h *liveChatHandlerBridge) OnThinkerHandoff(invocationID string, task string) {
	if h.prog != nil {
		h.prog.Send(thinkerStartMsg{id: invocationID, task: task})
	}

	go func() {
		_ = h.sess.SendThinkerNote("Starting execution for: " + task)

		var history []voice.LiveTurnRecord
		if h.sess != nil {
			history = h.sess.ConversationHistory()
		}
		result, err := executeThinkerTask(h.workspace, task, history)

		_ = h.sess.CompleteHandoff(invocationID, result)
		_ = h.sess.SendThinkerAnswer(result)
		if h.prog != nil {
			h.prog.Send(thinkerDoneMsg{task: task, result: result, err: err})
		}
	}()
}

var (
	ghBlobRegex     = regexp.MustCompile(`^https://github\.com/([^/]+)/([^/]+)/blob/([^/]+)/(.*)$`)
	urlRegex        = regexp.MustCompile(`https?://[^\s'"<>]+`)
	htmlTagRegex    = regexp.MustCompile(`<[^>]+>`)
	htmlScriptRegex = regexp.MustCompile(`(?is)<script.*?</script>`)
	htmlStyleRegex  = regexp.MustCompile(`(?is)<style.*?</style>`)
)

func extractURLs(text string) []string {
	matches := urlRegex.FindAllString(text, -1)
	var clean []string
	for _, u := range matches {
		u = strings.Trim(u, `"'(),;<>`)
		if u != "" {
			clean = append(clean, u)
		}
	}
	return clean
}

func stripHTML(html string) string {
	s := htmlScriptRegex.ReplaceAllString(html, "")
	s = htmlStyleRegex.ReplaceAllString(s, "")
	s = htmlTagRegex.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	var clean []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" {
			clean = append(clean, t)
		}
	}
	return strings.Join(clean, "\n")
}

func fetchURLContent(ctx context.Context, rawURL string) (string, error) {
	cleanURL := strings.TrimSpace(rawURL)
	cleanURL = strings.Trim(cleanURL, `"'(),;<>`)

	fragment := ""
	if idx := strings.Index(cleanURL, "#"); idx != -1 {
		fragment = cleanURL[idx+1:]
		cleanURL = cleanURL[:idx]
	}

	// 1. GitHub blob -> raw conversion
	if m := ghBlobRegex.FindStringSubmatch(cleanURL); m != nil {
		cleanURL = fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s/%s", m[1], m[2], m[3], m[4])
	}

	req, err := http.NewRequestWithContext(ctx, "GET", cleanURL, nil)
	if err != nil {
		return "", fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; Pigasus-LiveChat/1.0)")
	req.Header.Set("Accept", "text/plain, text/markdown, text/html, application/json, */*")

	client := &http.Client{
		Timeout: 20 * time.Second,
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("http get failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("server returned HTTP %d %s", resp.StatusCode, resp.Status)
	}

	bodyBytes, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024))
	if err != nil {
		return "", fmt.Errorf("read response body: %w", err)
	}
	text := string(bodyBytes)

	// If fragment specified (e.g. "1510" or "1.5.10" or section anchor)
	if fragment != "" {
		fragNorm := strings.ToLower(strings.ReplaceAll(fragment, ".", ""))
		lines := strings.Split(text, "\n")
		var secLines []string
		inSec := false
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				headerText := strings.TrimLeft(trimmed, "# ")
				headerNorm := strings.ToLower(strings.ReplaceAll(headerText, ".", ""))
				if strings.Contains(headerNorm, fragNorm) || strings.Contains(strings.ToLower(headerText), strings.ToLower(fragment)) {
					inSec = true
					secLines = append(secLines, line)
					continue
				} else if inSec {
					break
				}
			}
			if inSec {
				secLines = append(secLines, line)
			}
		}
		if len(secLines) > 0 {
			result := strings.TrimSpace(strings.Join(secLines, "\n"))
			if len(result) > 3500 {
				result = result[:3500] + "\n...(section truncated)"
			}
			return result, nil
		}
	}

	// If HTML, strip script/style tags for cleaner reading
	if strings.Contains(resp.Header.Get("Content-Type"), "html") || strings.Contains(text, "<html") {
		text = stripHTML(text)
	}

	if len(text) > 3500 {
		return text[:3500] + "\n...(content truncated)", nil
	}
	return text, nil
}

func executeRawCommand(ctx context.Context, ws string, cmdStr string) (string, error) {
	shell := "sh"
	if _, err := exec.LookPath("zsh"); err == nil {
		shell = "zsh"
	}
	cmd := exec.CommandContext(ctx, shell, "-c", cmdStr)
	cmd.Dir = ws
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	out, err := cmd.CombinedOutput()
	outStr := strings.TrimSpace(string(out))
	if len(outStr) > 2500 {
		outStr = outStr[:2500] + "\n...(output truncated)"
	}
	if err != nil {
		if outStr != "" {
			return outStr, err
		}
		return fmt.Sprintf("Command failed: %v", err), err
	}
	if outStr == "" {
		return "Command executed successfully with empty output.", nil
	}
	return outStr, nil
}

func extractShellCommand(ws string, task string) string {
	// A. Check for backticks `command`
	backtickRegex := regexp.MustCompile("`([^`]+)`")
	if m := backtickRegex.FindStringSubmatch(task); len(m) > 1 {
		return strings.TrimSpace(m[1])
	}

	lower := strings.ToLower(task)
	words := strings.Fields(lower)

	// B. Direct terminal commands
	if len(words) == 1 {
		switch words[0] {
		case "cal", "date", "pwd", "whoami", "uname", "uptime":
			return words[0]
		}
	}

	// C. Calendar
	for _, w := range words {
		if w == "cal" || w == "'cal'" || w == "\"cal\"" {
			return "cal"
		}
	}

	// D. Git status / diff / log (Avoid matching "github"!)
	if strings.Contains(lower, "git status") || (strings.Contains(lower, "status") && !strings.Contains(lower, "http")) {
		return "git status -s"
	}
	if strings.Contains(lower, "git diff") || strings.Contains(lower, "diff") {
		return "git diff --stat"
	}
	if strings.Contains(lower, "git log") {
		return "git log -n 5 --oneline"
	}

	// E. Tests
	if strings.Contains(lower, "test") {
		if _, err := os.Stat(filepath.Join(ws, "go.mod")); err == nil {
			return "go test ./..."
		} else if _, err := os.Stat(filepath.Join(ws, "package.json")); err == nil {
			return "npm test"
		} else {
			return "make test"
		}
	}

	// F. "run the [cmd] command" pattern
	runTheCmdRegex := regexp.MustCompile(`(?i)run\s+the\s+([a-zA-Z0-9_\-\./]+)\s+command`)
	if m := runTheCmdRegex.FindStringSubmatch(task); len(m) > 1 {
		return m[1]
	}

	// G. "run [cmd]" or "execute [cmd]" pattern
	runCmdRegex := regexp.MustCompile(`(?i)^(?:run|execute)\s+([a-zA-Z0-9_\-\./]+)(?:\s+(.*))?$`)
	if m := runCmdRegex.FindStringSubmatch(task); len(m) > 1 {
		cmdName := m[1]
		if _, err := exec.LookPath(cmdName); err == nil {
			if len(m) > 2 && m[2] != "" {
				return cmdName + " " + m[2]
			}
			return cmdName
		}
	}

	// H. "use curl to fetch ..." -> extract curl command
	if strings.HasPrefix(lower, "use curl") || strings.Contains(lower, "curl ") {
		urls := extractURLs(task)
		if len(urls) > 0 {
			return fmt.Sprintf("curl -sL %s", urls[0])
		}
	}

	return task
}

func extractSearchQuery(task string) (query, domain string, ok bool) {
	lower := strings.ToLower(strings.TrimSpace(task))

	patterns := []string{
		"search the web for ",
		"search the web about ",
		"search the web: ",
		"web search for ",
		"web search: ",
		"search for ",
		"search google for ",
		"google for ",
		"google ",
		"look up online ",
		"look up on the web ",
		"find information on ",
		"find information about ",
	}

	for _, p := range patterns {
		if strings.HasPrefix(lower, p) {
			q := strings.TrimSpace(task[len(p):])
			q = strings.Trim(q, `"'.,;`)
			if q != "" {
				if idx := strings.Index(strings.ToLower(q), " site:"); idx != -1 {
					domain = strings.TrimSpace(q[idx+6:])
					q = strings.TrimSpace(q[:idx])
				}
				return q, domain, true
			}
		}
	}

	re := regexp.MustCompile(`(?i)^search\s+(.+?)\s+(?:on the web|online|on google)$`)
	if m := re.FindStringSubmatch(task); len(m) > 1 {
		return strings.TrimSpace(m[1]), "", true
	}

	return "", "", false
}

func executeThinkerTask(ws string, task string, history ...[]voice.LiveTurnRecord) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// 1. Check if task is a web search
	if query, domain, ok := extractSearchQuery(task); ok {
		result, err := performWebSearch(ctx, query, domain)
		if err != nil {
			return "", fmt.Errorf("web search for %q failed: %w", query, err)
		}
		return result, nil
	}

	// 2. Check if task contains a URL
	urls := extractURLs(task)
	// If task doesn't have a URL, check if recent history contains a URL and task asks about web/changelog/link
	if len(urls) == 0 && len(history) > 0 && len(history[0]) > 0 {
		lowerTask := strings.ToLower(task)
		if strings.Contains(lowerTask, "changelog") || strings.Contains(lowerTask, "fetch") || strings.Contains(lowerTask, "url") || strings.Contains(lowerTask, "link") || strings.Contains(lowerTask, "version") || strings.Contains(lowerTask, "summarize") || strings.Contains(lowerTask, "explain") {
			for i := len(history[0]) - 1; i >= 0; i-- {
				histURLs := extractURLs(history[0][i].Content)
				if len(histURLs) > 0 {
					urls = histURLs
					break
				}
			}
		}
	}

	if len(urls) > 0 {
		targetURL := urls[0]
		if !strings.Contains(targetURL, "#") {
			verRegex := regexp.MustCompile(`\b\d+\.\d+\.\d+\b`)
			if m := verRegex.FindString(task); m != "" {
				targetURL += "#" + m
			}
		}
		content, err := fetchURLContent(ctx, targetURL)
		if err != nil {
			return "", fmt.Errorf("fetch url %s failed: %w", targetURL, err)
		}
		if len(content) > 0 {
			return content, nil
		}
	}

	// 2. Extract shell command from task
	cmdStr := extractShellCommand(ws, task)
	return executeRawCommand(ctx, ws, cmdStr)
}

func summarizeResult(res string) string {
	lines := strings.Split(strings.TrimSpace(res), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	return fmt.Sprintf("%s (%d lines)", lines[0], len(lines))
}

// ProactiveWatcher monitors workspace git changes and pushes proactive nudges.
type ProactiveWatcher struct {
	workspace       string
	sess            *voice.NativeLiveSession
	prog            *tea.Program
	lastState       string
	lastSpoken      time.Time
	cooldown        time.Duration
	paused          atomic.Bool
	pendingUserTurn atomic.Bool
	stopCh          chan struct{}
}

func NewProactiveWatcher(workspace string, sess *voice.NativeLiveSession, cooldown time.Duration) *ProactiveWatcher {
	return &ProactiveWatcher{
		workspace: workspace,
		sess:      sess,
		cooldown:  cooldown,
		stopCh:    make(chan struct{}),
	}
}

func (w *ProactiveWatcher) SetProgram(prog *tea.Program) {
	w.prog = prog
}

func (w *ProactiveWatcher) SetPaused(paused bool) {
	w.paused.Store(paused)
}

func (w *ProactiveWatcher) IsPaused() bool {
	return w.paused.Load()
}

func (w *ProactiveWatcher) ResetPending() {
	w.pendingUserTurn.Store(false)
}

func (w *ProactiveWatcher) Start() {
	w.lastState = w.checkGitStatus()
	w.lastSpoken = time.Now()

	ticker := time.NewTicker(3 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-w.stopCh:
				return
			case <-ticker.C:
				w.poll()
			}
		}
	}()
}

func (w *ProactiveWatcher) Stop() {
	close(w.stopCh)
}

func (w *ProactiveWatcher) checkGitStatus() string {
	cmd := exec.Command("git", "status", "-s")
	cmd.Dir = w.workspace
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func (w *ProactiveWatcher) poll() {
	if w.paused.Load() {
		return
	}
	if w.pendingUserTurn.Load() {
		// Do not repeatedly nag the user if they haven't spoken or acted since previous prompt
		return
	}
	if w.sess != nil && (w.sess.Speaking() || w.sess.IsReconnecting()) {
		return
	}
	if time.Since(w.lastSpoken) < w.cooldown {
		return
	}

	current := w.checkGitStatus()
	if current == "" || current == w.lastState {
		return
	}

	w.lastState = current
	w.lastSpoken = time.Now()
	w.pendingUserTurn.Store(true)

	files := parseChangedFiles(current)
	if w.prog != nil {
		w.prog.Send(proactiveEventMsg{files: files})
	}

	if w.sess != nil {
		prompt := fmt.Sprintf(
			"[PROACTIVE_EVENT: Detected file changes in workspace: %s. In one brief, natural spoken sentence, mention to the user that you noticed these changes and ask if they'd like you to run tests or review them.]",
			strings.Join(files, ", "),
		)
		_ = w.sess.SendText(prompt)
	}
}

func parseChangedFiles(status string) []string {
	var files []string
	lines := strings.Split(status, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) > 3 {
			files = append(files, strings.TrimSpace(line[2:]))
		}
	}
	if len(files) > 4 {
		return append(files[:4], fmt.Sprintf("+%d more files", len(files)-4))
	}
	return files
}

func main() {
	apiKeyFlag := flag.String("api-key", "", "Gemini API Key (defaults to credentials in ~/.openagy/config.json or $GEMINI_API_KEY)")
	modelFlag := flag.String("model", "models/gemini-3.8-live", "Gemini Live model name")
	voiceFlag := flag.String("voice", "Fola", "Voice persona (Antigravity: Fola, Arlo, Cleo, Finn, Gero, Lora, Neno, Olin, Tari; Gemini: Aoede, Puck, Kore, Fenrir, Charon, Zephyr, Leda, Orus)")
	workspaceFlag := flag.String("workspace", ".", "Workspace directory to monitor for changes")
	mockFlag := flag.Bool("mock", false, "Use Mock Live Backend for offline testing")
	duplexFlag := flag.Bool("duplex", false, "Enable full duplex mode (headphones only)")
	debugWireFlag := flag.Bool("debug-wire", false, "Show real-time WebSocket and audio frame diagnostics")
	reconnectFlag := flag.Bool("reconnect", true, "Enable automatic reconnection on network disruption")
	maxRetriesFlag := flag.Int("max-retries", 5, "Maximum reconnection attempts on network disruption")
	cooldownFlag := flag.Duration("cooldown", 20*time.Second, "Proactive alert minimum cooldown")
	resumeFlag := flag.String("resume", "", "Resume a saved LiveChat session by ID, prefix, index (e.g. 1), or 'latest'")
	listSessionsFlag := flag.Bool("list-sessions", false, "List all saved LiveChat sessions and exit")
	noPreviewFlag := flag.Bool("no-voice-preview", false, "Do not play a recorded persona clip when the voice switches")
	flag.Parse()

	absWs, err := filepath.Abs(*workspaceFlag)
	if err != nil {
		absWs = "."
	}

	if *listSessionsFlag {
		summaries, err := voice.ListLiveSessions(absWs)
		if err != nil {
			fmt.Printf("Failed to list sessions: %v\n", err)
			os.Exit(1)
		}
		if len(summaries) == 0 {
			summaries, _ = voice.ListLiveSessions()
		}
		if len(summaries) == 0 {
			fmt.Println("No saved LiveChat sessions found.")
			os.Exit(0)
		}
		fmt.Println("Saved LiveChat Sessions:")
		for i, s := range summaries {
			fmt.Printf("  #%d [%s] · %s · %d turns · %q (%s)\n",
				i+1, s.ID, s.UpdatedAt.Format("2006-01-02 15:04"), s.TurnCount, s.Title, s.Workspace)
		}
		os.Exit(0)
	}

	var initialSession *voice.LiveSessionData
	if *resumeFlag != "" {
		resumed, err := voice.FindLiveSession(*resumeFlag, absWs)
		if err != nil {
			fmt.Printf("%sFailed to resume session %q: %v%s\n", ansiRed, *resumeFlag, err, ansiReset)
			os.Exit(1)
		}
		initialSession = resumed
		fmt.Printf("%sResuming LiveChat session [%s] (%d turns)...%s\n", ansiGreen, resumed.ID, len(resumed.Turns), ansiReset)
	}

	// 1. Resolve Credentials (Reusing Pigasus credential priority cascade)
	apiKey := *apiKeyFlag
	if apiKey == "" {
		apiKey = endpoints.CurrentGeminiAPIKey()
	}

	var bearerToken string
	if apiKey == "" {
		if tok, err := auth.LoadTokenUnified(); err == nil && tok != nil && tok.Token.AccessToken != "" {
			bearerToken = tok.Token.AccessToken
		}
	}

	var liveBackend voice.LiveBackend
	if *mockFlag || os.Getenv("PIGASUS_MOCK_VOICE") != "" || os.Getenv("OPENAGY_MOCK_VOICE") != "" {
		mockB := voice.NewMockLiveBackend()
		mockB.SetAutoEcho(true)
		liveBackend = mockB
	} else if apiKey != "" {
		liveBackend = voice.NewGeminiLiveBackend(apiKey)
	} else if bearerToken != "" {
		liveBackend = voice.NewGeminiLiveBackendWithBearer(bearerToken)
	} else {
		fmt.Printf("%s⚠️  No Gemini API Key or active Pigasus session found.%s\n", ansiRed+ansiBold, ansiReset)
		fmt.Println("To connect to Google Gemini Multimodal Live:")
		fmt.Println("  1. Add \"gemini_api_key\" to ~/.openagy/config.json")
		fmt.Println("  2. Or set export GEMINI_API_KEY=\"...\"")
		fmt.Println("  3. Or run with --mock for an interactive offline simulation")
		os.Exit(1)
	}

	// 2. Configure System Prompt for Proactive Voice Agent with Real Tools
	sysPrompt := fmt.Sprintf(`You are LiveChat, an intelligent voice coding assistant running in workspace: %s.
Key rules:
1. Keep spoken responses short, natural, friendly, and spoken-friendly (1-2 sentences). Do not use markdown tables or raw code blocks in speech.
2. You have real executable tools:
   - 'search_web': Call this to search the web for up-to-date information, documentation, news, or general web queries.
   - 'fetch_url': Call this whenever the user gives an HTTP/HTTPS link or asks to read documentation, GitHub files, or changelogs.
   - 'run_command': Call this whenever the user asks to run terminal commands (e.g. 'cal', 'curl', 'git status', 'ls -la', 'go test'). Pass the exact command string.
   - 'read_file': Call this to inspect local repository files.
   - 'delegate_to_thinker': Call this for complex multi-step coding or analysis tasks.
3. NEVER fake running a command or hallucinate web content. Always call the corresponding tool to obtain real ground-truth output.
4. When you receive a [PROACTIVE_EVENT: ...] message, acknowledge it out loud concisely.
5. When a tool or Thinker finishes, summarize the real execution result out loud to the user naturally.`, absWs)

	selectedVoice := *voiceFlag
	if initialSession != nil && initialSession.Voice != "" {
		selectedVoice = initialSession.Voice
	}

	cfg := voice.LiveSessionConfig{
		Voice:        voice.MapVoiceName(selectedVoice),
		Model:        *modelFlag,
		SystemPrompt: sysPrompt,
		SampleRate:   16000,
	}

	bridge := &liveChatHandlerBridge{
		workspace: absWs,
	}
	sess := voice.NewNativeLiveSession(liveBackend, bridge)
	sess.SetDuplex(*duplexFlag)
	sess.SetReconnectConfig(voice.ReconnectConfig{
		Enabled:        *reconnectFlag,
		MaxAttempts:    *maxRetriesFlag,
		InitialBackoff: 500 * time.Millisecond,
		MaxBackoff:     8 * time.Second,
		Continuous:     true,
	})
	bridge.sess = sess

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := sess.Start(ctx, cfg); err != nil {
		fmt.Printf("%sFailed to start Live session: %v%s\n", ansiRed, err, ansiReset)
		os.Exit(1)
	}
	defer sess.HangUp()

	watcher := NewProactiveWatcher(absWs, sess, *cooldownFlag)
	watcher.Start()
	defer watcher.Stop()

	// Proactive Welcome Nudge
	go func() {
		time.Sleep(1 * time.Second)
		if initialSession != nil {
			_ = sess.SendText(fmt.Sprintf("[PROACTIVE_EVENT: LiveChat session [%s] resumed with %d previous turns. Concisely welcome back the user in one sentence and say you are ready to continue where we left off.]", initialSession.ID, len(initialSession.Turns)))
		} else {
			_ = sess.SendText("[PROACTIVE_EVENT: Agent session established. Introduce yourself concisely in one spoken sentence: 'Hi, I'm LiveChat, your proactive voice coding assistant. I'm actively monitoring your workspace and ready to help.']")
		}
	}()

	// 3. Launch Bubbletea TUI with Rich Composer, AltScreen and Mouse Wheel Support
	tuiModel := newLiveChatModel(sess, watcher, absWs, selectedVoice, *modelFlag, *debugWireFlag)
	tuiModel.voicePreview = !*noPreviewFlag
	if initialSession != nil {
		tuiModel.LoadSession(initialSession)
		_ = sess.RehydrateResumeContext()
	}
	defer tuiModel.saveSession()

	prog := tea.NewProgram(tuiModel)
	bridge.prog = prog
	watcher.SetProgram(prog)

	if _, err := prog.Run(); err != nil {
		fmt.Printf("Error running TUI: %v\n", err)
	}
}
