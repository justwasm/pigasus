package cliapp

import (
	"flag"
	"os"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/loginui"
)

func (m *runMode) dispatchSlashLine(buf string) (bool, tea.Cmd) {
	if !strings.HasPrefix(buf, "/") {
		return false, nil
	}
	// Cycle multi-account-switch: the account may have been switched
	// outside the TUI (the `agy accounts switch` command, a sign-in in
	// another terminal). Re-read the shared credential before the
	// command runs, so a panel that shows the account (/usage) shows the
	// current one.
	syncCmd := m.syncAccountIdentity()
	if cmdName, cmdArgs := splitSlash(buf); m.openSlashPanelArgs(cmdName, cmdArgs) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		m.atFileDropdown.Reset()
		if m.panel.kind == panelUsage && m.height > 0 {
			m.quotaFrame = 0
			return true, tea.Batch(syncCmd, usageQuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.panel.kind == panelAccounts {
			// The panel queries every account's quota, so the user
			// does not have to open /usage for the numbers.
			return true, tea.Batch(syncCmd, m.queueAccountsQuota())
		}
		if m.panel.kind == panelCodeSearch && m.height > 0 {
			return true, tea.Batch(syncCmd, csTick(m))
		}
		return true, syncCmd
	}
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, tea.Batch(syncCmd, cmd)
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
		// A running *shell* command is preempted: the reference stops it,
		// keeps what it printed, and runs the new one immediately (【一手】
		// `!top` then `!pwd`). Only a model turn queues — there the command
		// waits behind the turn and shows as `▸ <cmd>`.
		if m.bashCancel != nil {
			return true, m.preemptBashCommand(cmd)
		}
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
		lm := loginui.New()
		lm.Width = m.width
		lm.Height = m.height
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
	// A new turn replaces the interrupted one: the reference's composer
	// hint belongs to the turn that was cancelled, not to the next one.
	m.turnInterrupted = false
	m.historyIdx = 0
	m.historyEdit = ""
	m.clearLastTurnOutcome()
	if m.autoTitle == "" {
		m.autoTitle = convstore.GenerateConversationTitle(prompt)
	}
	for _, row := range m.mediaRows() {
		m.response = append(m.response, row)
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
	// Cycle multi-account-switch: the turn runs as the account the
	// shared credential names right now, not the one the session
	// started with. The transport is rebuilt by the sync when the
	// account changed, and the stream command runs after this Update
	// returns — so it picks the rebuilt transport up.
	syncCmd := m.syncAccountIdentity()
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd, syncCmd)
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
	// The "/model <query>" popup owns Enter while it is open: the
	// argument either names a model (the command runs) or the highlighted
	// row is completed into the composer. 【一手】 agy 1.4.6, live.
	if handled, cmd := m.submitModelCompletion(); handled {
		return cmd
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
	msg := backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, m.model, m.wireSettingsPending, time.Now()),
	}
	// The images the user attached ride along with the turn (agy 1.4.9's
	// image pasting). The transports carry them as image parts, so the
	// model receives them the way the reference sends them.
	if parts := m.mediaImageParts(); len(parts) > 0 {
		msg.Images = parts
		m.pendingMedia = nil
	}
	m.session = append(m.session, msg)
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
	steps := m.trajectorySteps()
	if len(steps) == 0 {
		return
	}
	if err := convstore.SaveConversation(m.conversationID, m.cwd, steps); err != nil {
		return
	}
	if m.autoTitle != "" {
		_ = convstore.WriteSummariesTitle(m.conversationID, m.autoTitle)
		_ = convstore.WriteCacheTitle(m.conversationID, m.autoTitle)
	}
}
