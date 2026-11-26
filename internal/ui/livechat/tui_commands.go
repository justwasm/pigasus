package livechat

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/voice"
)

func liveChatCommands() []slashcommands.Command {
	return []slashcommands.Command{
		{Name: "/search", Description: "Search the web and feed results to agent (e.g. /search golang releases)", Aliases: []string{"web", "google"}},
		{Name: "/model", Description: "Switch or view Live AI models (e.g. /model gemini-3.8-live-extended-thinking, /model think on)", Aliases: []string{"mdl"}},
		{Name: "/voice", Description: "Switch, view or audition voice personas (e.g. /voice cleo, /voice preview tari 2)", Aliases: []string{"speaker"}},
		{Name: "/resume", Description: "List and resume saved LiveChat sessions (e.g. /resume latest)", Aliases: []string{"sessions"}},
		{Name: "/retry", Description: "Force immediate reconnection to live voice session", Aliases: []string{"reconnect"}},
		{Name: "/mute", Description: "Toggle microphone mute state (or press F6)", Aliases: []string{"m"}},
		{Name: "/duplex", Description: "Toggle full-duplex barge-in mode (/duplex on, /duplex off)", Aliases: []string{"d"}},
		{Name: "/proactive", Description: "Control workspace file watcher (/p on, /p off, /p)", Aliases: []string{"p"}},
		{Name: "/quit", Description: "Exit LiveChat session (or Esc)", Aliases: []string{"q", "exit"}},
	}
}

func (m *liveChatModel) handleModelList() (tea.Model, tea.Cmd) {
	m.appendLog(fmt.Sprintf("%s🤖 [Current Live Model]:%s %s", ansiCyan+ansiBold, ansiReset, m.modelName))
	m.appendLog(fmt.Sprintf("%s✨ [Available Live Models]:%s", ansiGreen+ansiBold, ansiReset))
	models := []struct {
		ID   string
		Desc string
	}{
		{"models/gemini-3.8-live", "Gemini 3.8 Multimodal Live (Standard, Ultra-low Latency)"},
		{"models/gemini-3.8-live-extended-thinking", "Gemini 3.8 Live + Extended Thinking (2048 budget)"},
		{"models/gemini-2.0-flash", "Gemini 2.0 Flash Live"},
		{"models/gemini-2.5-flash", "Gemini 2.5 Flash Live"},
	}
	for _, mdl := range models {
		curr := ""
		if strings.EqualFold(mdl.ID, m.modelName) || strings.EqualFold(strings.TrimPrefix(mdl.ID, "models/"), strings.TrimPrefix(m.modelName, "models/")) {
			curr = fmt.Sprintf(" %s[CURRENT]%s", ansiGreen+ansiBold, ansiReset)
		}
		m.appendLog(fmt.Sprintf("  • %-40s · %s%s", mdl.ID, mdl.Desc, curr))
	}
	m.appendLog(fmt.Sprintf("%s💡 Tip: Type '/model <name>' to switch models, or '/model think on' / '/model think off' to toggle extended thinking.%s", ansiDim, ansiReset))
	return m, nil
}

func (m *liveChatModel) handleModelArgs(arg string) (tea.Model, tea.Cmd) {
	arg = strings.TrimSpace(arg)
	lower := strings.ToLower(arg)
	switch {
	case arg == "" || lower == "list":
		return m.handleModelList()
	case lower == "think on" || lower == "thinking on":
		return m.handleModelSwitch("models/gemini-3.8-live-extended-thinking")
	case lower == "think off" || lower == "thinking off":
		return m.handleModelSwitch("models/gemini-3.8-live")
	default:
		return m.handleModelSwitch(arg)
	}
}

func (m *liveChatModel) handleModelSwitch(target string) (tea.Model, tea.Cmd) {
	target = strings.TrimSpace(target)
	if !strings.HasPrefix(target, "models/") && !strings.Contains(target, "/") {
		target = "models/" + target
	}

	m.modelName = target
	if m.sess != nil {
		if err := m.sess.SwitchModel(context.Background(), target); err != nil {
			m.appendLog(fmt.Sprintf("%s⚠️ Failed to switch model: %v%s", ansiRed, err, ansiReset))
			return m, nil
		}
	}

	m.appendLog(fmt.Sprintf("%s✨ [Model Updated]: Switched to %s%s", ansiGreen+ansiBold, target, ansiReset))
	m.saveSession()
	return m, nil
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
