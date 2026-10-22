package main

import (
	"flag"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

func (m *runMode) dispatchSlashLine(buf string) (bool, tea.Cmd) {
	if !strings.HasPrefix(buf, "/") {
		return false, nil
	}
	if cmdName, cmdArgs := splitSlash(buf); m.openSlashPanelArgs(cmdName, cmdArgs) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		m.atFileDropdown.Reset()
		if m.panel.kind == panelUsage && m.height > 0 {
			m.quotaFrame = 0
			return true, tea.Batch(usageQuotaTick(), fetchQuotaSummaryCmd())
		}
		if m.panel.kind == panelCodeSearch && m.height > 0 {
			return true, csTick(m)
		}
		return true, nil
	}
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, cmd
	}
	return false, nil
}

func (m *runMode) submitBashMode(cmd string) (bool, tea.Cmd) {
	m.bashMode = false
	m.input.Reset()
	m.clearInputSelection()
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Reset()
	m.atFileDropdown.Reset()
	m.modeHintDismissed = true
	if isBlank(cmd) {
		return true, nil
	}
	m.pushHistory("!" + cmd)
	if m.streaming {
		m.bashQueue = append(m.bashQueue, cmd)
		return true, nil
	}
	return true, m.startBashCommand(cmd)
}

func (m *runMode) checkStreamingQueue(prompt string) (bool, tea.Cmd) {
	if !m.streaming {
		return false, nil
	}
	if isBlank(prompt) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		return true, nil
	}
	if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
		m.pushHistory(prompt)
		m.streamQueue = append(m.streamQueue, prompt)
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		m.atFileDropdown.Reset()
		m.modeHintDismissed = true
		return true, nil
	}
	return false, nil
}

func (m *runMode) resolveSubmittedPrompt() (string, bool) {
	prompt := m.input.Value()
	if isBlank(prompt) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		return "", false
	}
	m.hideCurrentAnnouncementForSession()
	if m.picker.Visible() {
		if sel := m.picker.Selected(); sel != nil && strings.IndexByte(prompt, ' ') < 0 && strings.IndexByte(prompt, '\t') < 0 {
			prompt = sel.Name
		}
	}
	m.pushHistory(prompt)
	return prompt, true
}

func (m *runMode) dispatchPromptPrefix(prompt string) (bool, tea.Cmd) {
	if handled, cmd := m.handlePromptAtPrefix(prompt); handled {
		return true, cmd
	}
	if handled, cmd := m.dispatchSlashLine(prompt); handled {
		return true, cmd
	}
	if flag.Lookup("test.v") == nil && !hasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := newLoginModel()
		lm.width = m.width
		lm.height = m.height
		m.login = lm
		m.input.Reset()
		m.clearInputSelection()
		return true, lm.Init()
	}
	return false, nil
}

func (m *runMode) resetLiveTurnState(prompt string) {
	m.input.Reset()
	m.clearInputSelection()
	m.atFileDropdown.Reset()
	m.clearEligibilityNotice()
	m.streaming = true
	m.historyIdx = 0
	m.historyEdit = ""
	m.clearLastTurnOutcome()
	if m.autoTitle == "" {
		m.autoTitle = generateConversationTitle(prompt)
	}
	m.response = append(m.response, userPromptRow(prompt))
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.picker.Reset()
	m.compactStart = time.Time{}
	m.compactEnd = time.Time{}
	m.compactSpinnerFrame = 0
	m.compactTokensBefore = 0
	m.compactTokensAfter = 0
	m.compactReason = ""
}

func (m *runMode) startModelTurn(prompt string) tea.Cmd {
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd)
}

func (m *runMode) submitPrompt() tea.Cmd {
	m.inputNotice = ""
	if m.bashMode {
		_, cmd := m.submitBashMode(m.input.Value())
		return cmd
	}
	if handled, cmd := m.checkStreamingQueue(m.input.Value()); handled {
		return cmd
	}
	if m.submitSubAgentQuestionAnswer() {
		return nil
	}
	prompt, ok := m.resolveSubmittedPrompt()
	if !ok {
		return nil
	}
	if handled, cmd := m.dispatchPromptPrefix(prompt); handled {
		return cmd
	}
	return m.startModelTurn(prompt)
}

func (m *runMode) appendUserTurn(text string) {
	m.session = append(m.session, backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, m.model, m.wireSettingsPending, time.Now()),
	})
	m.wireSettingsPending = false
}

func (m *runMode) slashQueuesWhileStreaming(buf string) bool {
	cmd, _ := splitSlash(buf)
	switch cmd {
	case "continue", "plan", "goal", "learn", "teamwork", "boost", "grill-me":
		return true
	}
	if _, ok := slashSkillLookup(cmd); ok {
		return true
	}
	return false
}

func (m *runMode) abortTurnForLocalCommand() {
	if !m.streaming {
		return
	}
	m.discardTurn = true
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.streaming = false
	m.thinking = false
}

func (m *runMode) resetSessionTokenCounters() {
	atomic.StoreInt64(&m.sessionPromptTokens, 0)
	atomic.StoreInt64(&m.sessionCompletionTokens, 0)
	atomic.StoreInt64(&m.lastTurnCompletionTokens, 0)
}

func (m *runMode) toggleQueueMode() tea.Cmd {
	prev := m.queueMode
	m.queueMode = !prev
	label := "Send Immediately"
	if m.queueMode {
		label = "Queue"
	}
	m.response = append(m.response,
		cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
			cDimGrayOpen+"alt+m \u2192 Queued Messages: "+label+cDimGrayClose)
	return nil
}

func (m *runMode) submitOrQueuePrompt() tea.Cmd {
	prompt := m.input.Value()
	if isBlank(prompt) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		return nil
	}
	if !m.queueMode {
		return m.submitPrompt()
	}
	m.pushHistory(prompt)
	m.queuedPrompt = prompt
	m.input.Reset()
	m.clearInputSelection()
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Reset()
	m.atFileDropdown.Reset()
	m.response = append(m.response,
		cDimGrayOpen+"  \u23bf  "+"\u00bb queued: "+prompt+cDimGrayClose)
	return nil
}

func (m *runMode) saveSession() {
	if m.conversationID == "" || len(m.session) == 0 {
		return
	}
	steps := sessionToSteps(m.session)
	if err := convstore.SaveConversation(m.conversationID, m.cwd, steps); err != nil {
		return
	}
	if m.autoTitle != "" {
		_ = convstore.WriteSummariesTitle(m.conversationID, m.autoTitle)
		_ = convstore.WriteCacheTitle(m.conversationID, m.autoTitle)
	}
}
