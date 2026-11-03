package cliapp

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// slashCommandRegex matches any leading slash command (agy 1.4.12 at 0x3680ea0).
var slashCommandRegex = regexp.MustCompile(`^/([a-zA-Z0-9_-]*)(?:\s|\n|$)`)

// PrependSlashPlanIfNeeded mirrors agy's model.PrependSlashPlanIfNeeded (0x102be4660).
// When the session is in plan mode, if the user prompt does not start with any slash command,
// it prepends "/plan " to the input.
func PrependSlashPlanIfNeeded(prompt string, mode string) string {
	if mode != permissions.ModePlan {
		return prompt
	}
	trimmed := strings.TrimSpace(prompt)
	if slashCommandRegex.MatchString(trimmed) {
		return prompt
	}
	return "/plan " + prompt
}

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
	if cmdName, cmdArgs := slashparse.Split(buf); m.openSlashPanelArgs(cmdName, cmdArgs) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		if m.panel.kind == panelregistry.Usage && m.height > 0 {
			m.quotaFrame = 0
			return true, tea.Batch(syncCmd, usagepanel.QuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.panel.kind == panelregistry.Accounts {
			// The panel queries every account's quota, so the user
			// does not have to open /usage for the numbers.
			return true, tea.Batch(syncCmd, m.accountsPanel().QueueQuota())
		}
		if m.panel.kind == panelregistry.CodeSearch && m.height > 0 {
			// Cycle 381: the panel owns the tick; the host only arms it.
			return true, tea.Batch(syncCmd, m.codesearchPanel().Tick())
		}
		return true, syncCmd
	}
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, tea.Batch(syncCmd, cmd)
	}
	return false, nil
}

func (m *runMode) submitBashMode(cmd string) (bool, tea.Cmd) {
	m.store.BashMode = false
	m.input.Reset()
	m.store.ResetInputSelection()
	m.promptHistory.Reset()
	m.picker.Reset()
	m.atFilePicker.Reset()
	m.modeHintDismissed = true
	if history.IsBlank(cmd) {
		return true, nil
	}
	m.pushHistory("!" + cmd)
	if m.streaming {
		// A running *shell* command is preempted: the reference stops it,
		// keeps what it printed, and runs the new one immediately (【一手】
		// `!top` then `!pwd`). Only a model turn queues — there the command
		// waits behind the turn and shows as `▸ <cmd>`.
		if m.store.BashCancel != nil {
			return true, m.preemptBashCommand(cmd)
		}
		m.store.BashQueue = append(m.store.BashQueue, cmd)
		return true, nil
	}
	return true, m.startBashCommand(cmd)
}

// submitVimBashMode runs a prompt that begins with "!" as a shell
// command. The reference's NORMAL-mode "!" inserts the character and
// switches to INSERT mode, so the buffer carries the "!" and the submit
// path is what turns it into a shell command.
//
// The check is scoped to vim mode on purpose: outside it the "!" key
// arms the bash mode directly (and the buffer never carries the "!"),
// and a prompt that merely starts with "!" is an ordinary prompt.
func (m *runMode) submitVimBashMode(prompt string) (tea.Cmd, bool) {
	if !m.vimEnabled() || !strings.HasPrefix(prompt, "!") {
		return nil, false
	}
	_, cmd := m.submitBashMode(strings.TrimPrefix(prompt, "!"))
	return cmd, true
}

func (m *runMode) checkStreamingQueue(prompt string) (bool, tea.Cmd) {
	if !m.streaming {
		return false, nil
	}
	if history.IsBlank(prompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return true, nil
	}
	if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
		m.pushHistory(prompt)
		m.store.StreamQueue = append(m.store.StreamQueue, prompt)
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		m.atFilePicker.Reset()
		m.modeHintDismissed = true
		return true, nil
	}
	return false, nil
}

func (m *runMode) resolveSubmittedPrompt() (string, bool) {
	prompt := m.input.Value()
	if history.IsBlank(prompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return "", false
	}
	m.announceQueue.HideForSession()
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
	if flag.Lookup("test.v") == nil && !backendselect.HasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := loginui.New()
		lm.Width = m.width
		lm.Height = m.height
		m.login = lm
		m.input.Reset()
		m.store.ResetInputSelection()
		return true, lm.Init()
	}
	return false, nil
}

func (m *runMode) resetLiveTurnState(prompt string) {
	m.input.Reset()
	m.store.ResetInputSelection()
	m.atFilePicker.Reset()
	m.clearEligibilityNotice()
	m.streaming = true
	// A new turn replaces the interrupted one: the reference's composer
	// hint belongs to the turn that was cancelled, not to the next one.
	m.turnInterrupted = false
	m.promptHistory.Reset()
	m.clearLastTurnOutcome()
	if m.autoTitle == "" {
		m.autoTitle = convstore.GenerateConversationTitle(prompt)
	}
	for _, row := range uirows.AttachmentRows(m.pendingMedia, m.mediaExpanded) {
		m.response = append(m.response, row)
	}
	for len(m.response) > 0 && strings.TrimSpace(m.response[len(m.response)-1]) == "" {
		m.response = m.response[:len(m.response)-1]
	}
	sep := transcript.TurnSeparator(m.width)
	if len(m.response) == 0 {
		m.response = append(m.response, sep)
	} else {
		m.response = append(m.response, "", sep)
	}
	start := len(m.response)
	promptLines := transcript.RenderUserPrompt(prompt, m.width)
	m.response = append(m.response, promptLines...)
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   prompt,
	})
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	showTips := true
	if v, ok := settings.Bool("showTips"); ok {
		showTips = v
	}
	if showTips {
		m.activeTip = tuitools.RandomTip()
	} else {
		m.activeTip = ""
	}
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.picker.Reset()
	m.store.ResetCompaction()
}

func (m *runMode) startModelTurn(prompt string) tea.Cmd {
	// Cycle multi-account-switch: the turn runs as the account the
	// shared credential names right now, not the one the session
	// started with. The transport is rebuilt by the sync when the
	// account changed, and the stream command runs after this Update
	// returns — so it picks the rebuilt transport up.
	syncCmd := m.syncAccountIdentity()
	m.conversationStarted = true
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd, syncCmd)
}

func (m *runMode) submitPrompt() tea.Cmd {
	m.inputNotice = ""
	if m.store.BashMode {
		_, cmd := m.submitBashMode(m.input.Value())
		return cmd
	}
	// In vim mode the engine put the "!" into the buffer itself — the
	// reference's NORMAL-mode "!" inserts the character and switches to
	// INSERT mode — so the shell command reaches the submit path with
	// the "!" still in front of it.
	if cmd, ok := m.submitVimBashMode(m.input.Value()); ok {
		return cmd
	}
	if handled, cmd := m.checkStreamingQueue(m.input.Value()); handled {
		return cmd
	}
	if m.subAgentQuestionPanel.SubmitAnswer() {
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
	prompt = PrependSlashPlanIfNeeded(prompt, m.mode)
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
		m.mediaExpanded = false
	}
	m.session = append(m.session, msg)
	m.wireSettingsPending = false
}

func (m *runMode) isEngineSlashCommand(cmd string) bool {
	if cmd == "agent" {
		return true
	}
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	if _, ok := slashHandlers[name]; ok {
		return false
	}
	for _, dyn := range m.engineSlashCommands() {
		clean := strings.TrimPrefix(dyn.Name, "/")
		if strings.EqualFold(clean, cmd) {
			return true
		}
	}
	return m.engine != nil
}

func (m *runMode) slashQueuesWhileStreaming(buf string) bool {
	cmd, _ := slashparse.Split(buf)
	// Cycle 383 (A-8): the list below names *commands*, so it is
	// stated in the canonical name. "/teamwork" is the alias of
	// "/teamwork-preview" and both queue behind the running turn.
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	switch name {
	case "continue", "plan", "goal", "learn", "teamwork-preview", "boost", "grill-me":
		return true
	}
	if _, ok := slashSkillLookup(cmd); ok {
		return true
	}
	if m.isEngineSlashCommand(cmd) {
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

func (m *runMode) toggleQueueMode() tea.Cmd {
	prev := m.store.QueueMode
	m.store.QueueMode = !prev
	label := "Send Immediately"
	if m.store.QueueMode {
		label = "Queue"
	}
	m.response = append(m.response,
		uirows.NoticeDim("alt+m \u2192 Queued Messages: "+label))
	return nil
}

func (m *runMode) submitOrQueuePrompt() tea.Cmd {
	prompt := m.input.Value()
	if history.IsBlank(prompt) {
		m.input.Reset()
		m.store.ResetInputSelection()
		m.promptHistory.Reset()
		m.picker.Reset()
		return nil
	}
	if !m.store.QueueMode {
		return m.submitPrompt()
	}
	m.pushHistory(prompt)
	m.store.QueuedPrompt = prompt
	m.input.Reset()
	m.store.ResetInputSelection()
	m.promptHistory.Reset()
	m.picker.Reset()
	m.atFilePicker.Reset()
	m.response = append(m.response,
		cDimGrayOpen+"  \u23bf  "+"\u00bb queued: "+prompt+cDimGrayClose)
	return nil
}

func (m *runMode) saveSession() {
	if m.conversationID == "" || len(m.session) == 0 {
		return
	}
	steps := convstore.BuildSteps(m.trajectoryFacts())
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
