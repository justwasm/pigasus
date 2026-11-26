package tui

import (
	"flag"
	"os"
	"regexp"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	modelcatalog "github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/ui/foundations/history"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	uirows "github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/logspanel"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// ---- tui_prompt_submit.go ----

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

func (m *RootModel) dispatchSlashLine(buf string) (bool, tea.Cmd) {
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
		if m.prompt.vimEng != nil {
			m.prompt.input.Reset()
			m.prompt.vimEng.SetText("")
		} else {
			// Retain slash command with trailing space in composer for panel view parity with AGY 1.5.14
			text := "/" + cmdName + " "
			if cmdArgs != "" {
				text = "/" + cmdName + " " + cmdArgs + " "
			}
			m.prompt.input.SetValue(text)
			m.prompt.SetInputCursorOffset(len(text))
			m.prompt.input.CursorEnd()
		}
		if m.prompt.undoMgr != nil {
			m.prompt.undoMgr.Clear()
		}
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		if m.overlays.panel.kind == panelregistry.Usage && m.viewport.height > 0 {
			m.display.quotaFrame = 0
			return true, tea.Batch(syncCmd, usagepanel.QuotaTick(), m.fetchQuotaSummaryCmd())
		}
		if m.overlays.panel.kind == panelregistry.Accounts {
			// The panel queries every account's quota, so the user
			// does not have to open /usage for the numbers.
			return true, tea.Batch(syncCmd, m.accountsPanel().QueueQuota())
		}
		if m.overlays.panel.kind == panelregistry.CodeSearch && m.viewport.height > 0 {
			// Cycle 381: the panel owns the tick; the host only arms it.
			return true, tea.Batch(syncCmd, m.codesearchPanel().Tick())
		}
		if m.overlays.panel.kind == panelregistry.Logs {
			return true, tea.Batch(syncCmd, logspanel.Tick())
		}
		return true, syncCmd
	}
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, tea.Batch(syncCmd, cmd)
	}
	return false, nil
}

func (m *RootModel) submitBashMode(cmd string) (bool, tea.Cmd) {
	m.session.store.BashMode = false
	m.prompt.input.Reset()
	if m.prompt.undoMgr != nil {
		m.prompt.undoMgr.Clear()
	}
	m.ResetInputSelection()
	m.prompt.promptHistory.Reset()
	m.prompt.picker.Reset()
	m.prompt.atFilePicker.Reset()
	m.prompt.modeHintDismissed = true
	if history.IsBlank(cmd) {
		return true, nil
	}
	m.pushHistory("!" + cmd)
	if m.conversation.streaming {
		// A running *shell* command is preempted: the reference stops it,
		// keeps what it printed, and runs the new one immediately (【一手】
		// `!top` then `!pwd`). Only a model turn queues — there the command
		// waits behind the turn and shows as `▸ <cmd>`.
		if m.session.store.BashRunning() {
			return true, m.preemptBashCommand(cmd)
		}
		m.session.store.BashQueue = append(m.session.store.BashQueue, cmd)
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
func (m *RootModel) submitVimBashMode(prompt string) (tea.Cmd, bool) {
	if !m.vimEnabled() || !strings.HasPrefix(prompt, "!") {
		return nil, false
	}
	_, cmd := m.submitBashMode(strings.TrimPrefix(prompt, "!"))
	return cmd, true
}

func (m *RootModel) popActiveSteer() (string, bool) {
	if m.turn.activeBridge != nil {
		if msg, ok := m.turn.activeBridge.PopSteer(); ok {
			return msg, true
		}
	}
	if sc, ok := m.turnEngine().(agentturn.SteerController); ok {
		return sc.PopSteer()
	}
	return "", false
}

func (m *RootModel) steerActiveTurn(instruction string) bool {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return false
	}
	steered := false
	if m.turn.activeBridge != nil {
		if err := m.turn.activeBridge.Steer(instruction); err == nil {
			steered = true
		}
	}
	if !steered {
		if s, ok := m.turnEngine().(agentturn.Steerer); ok {
			if err := s.Steer(instruction); err == nil {
				steered = true
			}
		}
	}
	if !steered {
		return false
	}

	// In agy (0x102a5e230 addSendingMessage):
	// The active step continues generating cleanly without interruption.
	// Record in SendingMessages to render the "▸ <instruction> (Sending message...)" row.
	m.session.store.SendingMessages = append(m.session.store.SendingMessages, instruction)
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++
	return true
}

func (m *RootModel) applySteerToTranscript(instruction string) {
	instruction = strings.TrimSpace(instruction)
	if instruction == "" {
		return
	}

	// Reconcile SendingMessages: remove the applied instruction
	if len(m.session.store.SendingMessages) > 0 {
		idx := -1
		for i, s := range m.session.store.SendingMessages {
			if s == instruction {
				idx = i
				break
			}
		}
		if idx >= 0 {
			m.session.store.SendingMessages = append(m.session.store.SendingMessages[:idx], m.session.store.SendingMessages[idx+1:]...)
		} else {
			m.session.store.SendingMessages = m.session.store.SendingMessages[1:]
		}
	}

	// Deduplicate: do not commit the same instruction if already committed as the latest user block
	if len(m.conversation.userBlocks) > 0 && m.conversation.userBlocks[len(m.conversation.userBlocks)-1].Raw == instruction {
		return
	}

	// Finalize live thinking & tool on preceding step
	if m.conversation.thinking {
		m.finishLiveThinking()
	}
	// Remember the answer streamed before this steer. TurnDone's Reply is
	// the whole turn, but the transcript region below the prompt is only
	// the suffix; finalising with the whole reply would repeat the
	// pre-steer answer (AGY 1.5.3).
	m.conversation.steerReplyPrefix += m.conversation.liveReplyBuf
	m.conversation.activeTool = ""
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""

	// Commit user prompt bubble into transcript after the finished step
	for len(m.conversation.response) > 0 && strings.TrimSpace(m.conversation.response[len(m.conversation.response)-1]) == "" {
		m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
	}
	m.conversation.response = append(m.conversation.response, "")
	start := len(m.conversation.response)
	promptLines := transcript.RenderUserPrompt(instruction, m.viewport.width)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   instruction,
	})
	m.appendUserTurn(instruction)
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.liveBatch++
	m.conversation.batchBreak = true
	m.viewport.rowsCacheDirty = true
	m.viewport.rowsGen++
	m.saveSession()
}

func (m *RootModel) updateSteerAppliedMsg(msg steerAppliedMsg) {
	m.applySteerToTranscript(msg.instruction)
}

// steerReplySegments splits a finished turn's reply at the last applied
// steer. The transcript below the steer prompt carries only the suffix;
// the pre-steer text is already above it. Both messages fall back to the
// full reply when no steer split this turn (or the actor's reply is not a
// superset of what was streamed).
func (m *RootModel) steerReplySegments(msg toolsteps.TurnDoneMsg) (docMsg, sessionMsg toolsteps.TurnDoneMsg) {
	prefix := m.conversation.steerReplyPrefix
	if prefix == "" {
		return msg, msg
	}
	suffix, ok := strings.CutPrefix(msg.Reply, prefix)
	if !ok {
		return msg, msg
	}
	docMsg, sessionMsg = msg, msg
	docMsg.Reply = suffix
	sessionMsg.Reply = suffix
	return docMsg, sessionMsg
}

// clearPendingTurnSteers drops steering instructions the actor still
// holds. They were accepted by the engine but the turn ended before a
// step boundary applied them; the TUI is about to send them as the next
// turn's prompt instead.
func (m *RootModel) clearPendingTurnSteers() {
	if m.turn.activeBridge != nil {
		m.turn.activeBridge.ClearSteer()
	}
	if clearer, ok := m.turnEngine().(interface{ ClearSteer() []string }); ok {
		clearer.ClearSteer()
	}
}

func (m *RootModel) checkStreamingQueue(prompt string) (bool, tea.Cmd) {
	if !m.conversation.streaming {
		return false, nil
	}
	if history.IsBlank(prompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return true, nil
	}

	// Move pending steer to queue: bare /queue or /unsteer
	if prompt == "/queue" || prompt == "/unsteer" {
		if msg, ok := m.popActiveSteer(); ok && msg != "" {
			m.session.store.StreamQueue = append(m.session.store.StreamQueue, msg)
			m.saveSession()
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	// Explicit /queue <instruction> to force queueing even when QueueMode is off
	if strings.HasPrefix(prompt, "/queue ") {
		inst := strings.TrimSpace(strings.TrimPrefix(prompt, "/queue "))
		if inst != "" {
			m.pushHistory(prompt)
			m.session.store.StreamQueue = append(m.session.store.StreamQueue, inst)
			m.saveSession()
		}
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		if m.prompt.vimEng != nil {
			m.prompt.vimEng.SetText("")
		}
		m.prompt.modeHintDismissed = true
		return true, nil
	}

	// Promote queued prompt to steer if user inputs bare /steer
	if prompt == "/steer" {
		var inst string
		if m.session.store.QueuedPrompt != "" {
			inst = m.session.store.QueuedPrompt
			m.session.store.QueuedPrompt = ""
		} else if len(m.session.store.StreamQueue) > 0 {
			inst = m.session.store.StreamQueue[0]
			m.session.store.StreamQueue = m.session.store.StreamQueue[1:]
		}
		if inst != "" {
			m.saveSession()
			m.steerActiveTurn(inst)
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	// Explicit /steer <instruction> command
	if strings.HasPrefix(prompt, "/steer ") {
		inst := strings.TrimSpace(strings.TrimPrefix(prompt, "/steer "))
		if inst != "" {
			m.pushHistory(prompt)
			m.steerActiveTurn(inst)
		}
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		if m.prompt.vimEng != nil {
			m.prompt.vimEng.SetText("")
		}
		m.prompt.modeHintDismissed = true
		return true, nil
	}

	// In Send Immediately mode (QueueMode is off): submitting plain text steers the active turn
	// unless the turn is tearing down / interrupted (AGY 1.5.14), where follow-up messages queue instead.
	if !m.session.store.QueueMode && !strings.HasPrefix(prompt, "/") && !m.conversation.turnInterrupted {
		m.pushHistory(prompt)
		if m.steerActiveTurn(prompt) {
			m.prompt.input.Reset()
			m.ResetInputSelection()
			m.prompt.promptHistory.Reset()
			m.prompt.picker.Reset()
			m.prompt.atFilePicker.Reset()
			m.prompt.modeHintDismissed = true
			return true, nil
		}
	}

	if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
		m.pushHistory(prompt)
		m.session.store.StreamQueue = append(m.session.store.StreamQueue, prompt)
		m.viewport.rowsCacheDirty = true
		m.viewport.rowsGen++
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		m.prompt.atFilePicker.Reset()
		if m.prompt.vimEng != nil {
			m.prompt.vimEng.SetText("")
		}
		m.prompt.modeHintDismissed = true
		return true, nil
	}
	return false, nil
}

func (m *RootModel) expandPastedPlaceholders(prompt string) string {
	if len(m.prompt.pastedBlocks) == 0 {
		return prompt
	}
	for placeholder, content := range m.prompt.pastedBlocks {
		if strings.Contains(prompt, placeholder) {
			prompt = strings.ReplaceAll(prompt, placeholder, content)
		}
	}
	return prompt
}

func (m *RootModel) resolveSubmittedPrompt() (string, bool) {
	rawPrompt := m.prompt.input.Value()
	if history.IsBlank(rawPrompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return "", false
	}
	m.overlays.announceQueue.HideForSession()
	prompt := rawPrompt
	if m.prompt.picker.Visible() {
		if sel := m.prompt.picker.Selected(); sel != nil && strings.IndexByte(prompt, ' ') < 0 && strings.IndexByte(prompt, '\t') < 0 {
			prompt = sel.Name
		}
	}
	// Expand folded [Pasted text #N ...] placeholders into the full pasted text for model submission.
	prompt = m.expandPastedPlaceholders(prompt)
	m.pushHistory(prompt)
	return prompt, true
}

func (m *RootModel) dispatchPromptPrefix(prompt string) (bool, tea.Cmd) {
	if handled, cmd := m.handlePromptAtPrefix(prompt); handled {
		return true, cmd
	}
	if handled, cmd := m.dispatchSlashLine(prompt); handled {
		return true, cmd
	}
	// The credential question is the session's, not the dispatch's: this is
	// the composer's submit path, and on macOS the read is a keychain
	// syscall. m.hasCredential answers it from the read the session already
	// did this turn.
	if flag.Lookup("test.v") == nil && !m.hasCredential() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := loginui.New()
		lm.Width = m.viewport.width
		lm.Height = m.viewport.height
		m.auth.login = lm
		m.prompt.input.Reset()
		m.ResetInputSelection()
		return true, lm.Init()
	}
	return false, nil
}

func (m *RootModel) resetLiveTurnState(prompt string) {
	m.prompt.input.Reset()
	if m.prompt.undoMgr != nil {
		m.prompt.undoMgr.Clear()
	}
	m.prompt.pastedBlocks = nil
	m.prompt.nextPasteID = 0
	m.ResetInputSelection()
	m.prompt.atFilePicker.Reset()
	if m.prompt.vimEng != nil {
		m.prompt.vimEng.SetTextKeepingPending("")
	}
	m.clearEligibilityNotice()
	m.conversation.streaming = true
	// A new turn replaces the interrupted one: the reference's composer
	// hint belongs to the turn that was cancelled, not to the next one.
	m.conversation.turnInterrupted = false
	m.prompt.promptHistory.Reset()
	m.clearLastTurnOutcome()
	if m.session.autoTitle == "" {
		m.session.autoTitle = convstore.GenerateConversationTitle(prompt)
	}
	for _, row := range uirows.AttachmentRows(m.prompt.pendingMedia, m.prompt.mediaExpanded) {
		m.conversation.response = append(m.conversation.response, row)
	}
	for len(m.conversation.response) > 0 && strings.TrimSpace(m.conversation.response[len(m.conversation.response)-1]) == "" {
		m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
	}
	sep := transcript.TurnSeparator(m.viewport.width)
	if len(m.conversation.response) == 0 {
		m.conversation.response = append(m.conversation.response, sep)
	} else {
		m.conversation.response = append(m.conversation.response, "", sep)
	}
	start := len(m.conversation.response)
	promptLines := transcript.RenderUserPrompt(prompt, m.viewport.width)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   prompt,
	})
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.thinkingSince = time.Time{}
	m.conversation.thinkingElapsed = 0
	m.conversation.toolSeq = 0
	m.conversation.phaseTimer.reset()
	m.conversation.turnStartedAt = time.Now()
	m.conversation.lastQuotaRefreshAt = time.Time{}
	m.conversation.quotaRefreshInFlight = false
	m.conversation.stepTray.reset()
	m.conversation.liveThinking = ""
	m.conversation.liveThinkingSnippet = ""
	m.conversation.liveThoughtRunIdx = -1
	showTips := true
	if v, ok := settings.Bool("showTips"); ok {
		showTips = v
	}
	if showTips {
		m.viewport.activeTip = tuitools.RandomTip()
	} else {
		m.viewport.activeTip = ""
	}
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.voiceReplyStart = -1
	m.conversation.voiceReplyBuf = ""
	m.conversation.voiceTextLines = 0
	m.conversation.steerReplyPrefix = ""
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.conversation.turnStartedAt = time.Now()
	m.conversation.lastQuotaRefreshAt = time.Time{}
	m.conversation.quotaRefreshInFlight = false
	m.conversation.stepTray.reset()
	m.prompt.picker.Reset()
	m.session.store.ResetCompaction()
}

func (m *RootModel) startModelTurn(prompt string) tea.Cmd {
	// Cycle multi-account-switch: the turn runs as the account the
	// shared credential names right now, not the one the session
	// started with. The transport is rebuilt by the sync when the
	// account changed, and the stream command runs after this Update
	// returns — so it picks the rebuilt transport up.
	syncCmd := m.syncAccountIdentity()
	// The cached credential read is what the check above used. Once it has
	// aged out, re-read it here — on a command goroutine, never inline: the
	// read is a keychain syscall on macOS and this is the Enter path.
	credCmd := m.refreshCredentialCmd()
	m.session.conversationStarted = true
	m.drainTaskNotifications()
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd, syncCmd, credCmd)
}

// hasInterruptedTurn reports whether the active conversation has an interrupted or uncompleted turn.
func (m *RootModel) hasInterruptedTurn() bool {
	if m == nil {
		return false
	}
	if m.conversation.turnInterrupted || m.turnFailed() {
		return true
	}
	msgs := m.SessionMessages()
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		if last.Role == "user" {
			return true
		}
		if last.Role == "assistant" && convstore.IsLeakedAssistantError(last.Content) {
			return true
		}
	}
	return false
}

// continueInterruptedTurn resumes the last interrupted turn without initiating a new user turn.
func (m *RootModel) continueInterruptedTurn() tea.Cmd {
	if m == nil {
		return nil
	}
	syncCmd := m.syncAccountIdentity()
	m.session.conversationStarted = true
	m.drainTaskNotifications()

	m.conversation.turnInterrupted = false
	m.clearLastTurnOutcome()
	m.conversation.streaming = true
	m.viewport.inputNotice = ""
	m.prompt.input.Reset()
	m.ResetInputSelection()
	m.prompt.promptHistory.Reset()

	// If the last assistant message is a leaked error from an older turn, pop it
	// so the conversation cleanly rolls back to the unfulfilled user prompt.
	msgs := m.SessionMessages()
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == "assistant" && convstore.IsLeakedAssistantError(msgs[len(msgs)-1].Content) {
		m.setSessionMessages(msgs[:len(msgs)-1])
		msgs = m.SessionMessages()
	}

	// If the assistant was interrupted while already streaming text,
	// inject the reference recovery steer instruction so backend LLM APIs
	// receive a compliant user message.
	if len(msgs) > 0 && msgs[len(msgs)-1].Role == "assistant" {
		m.appendUserTurn(actor.StreamInterruptedMessage)
	}

	const banner = "  ⎿  Continuing interrupted turn…"
	m.conversation.response = append(m.conversation.response, banner)
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.replyStart = -1
	m.conversation.liveReplyIdx = -1
	m.conversation.liveSteps = nil
	m.conversation.liveStepRuns = nil
	m.conversation.activeTool = ""
	m.conversation.thinking = false
	m.conversation.liveThinking = ""
	m.conversation.liveThinkingSnippet = ""
	m.conversation.liveThoughtRunIdx = -1
	m.conversation.liveTextLines = 0
	m.conversation.liveReplyBuf = ""
	m.conversation.voiceReplyStart = -1
	m.conversation.voiceReplyBuf = ""
	m.conversation.voiceTextLines = 0
	m.conversation.steerReplyPrefix = ""
	m.conversation.lastStreamSave = time.Time{}
	m.conversation.streamedText = false
	m.conversation.liveBatch = 0
	m.conversation.batchBreak = false
	m.conversation.anyToolRun = false
	m.conversation.spinFrame = 0
	m.prompt.picker.Reset()
	m.session.store.ResetCompaction()

	m.saveSession()
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(m.streamPrompt(""), tickCmd, syncCmd)
}

func (m *RootModel) submitPrompt() tea.Cmd {
	m.viewport.inputNotice = ""
	if m.session.store.BashMode {
		_, cmd := m.submitBashMode(m.expandPastedPlaceholders(m.prompt.input.Value()))
		return cmd
	}
	// In vim mode the engine put the "!" into the buffer itself — the
	// reference's NORMAL-mode "!" inserts the character and switches to
	// INSERT mode — so the shell command reaches the submit path with
	// the "!" still in front of it.
	if cmd, ok := m.submitVimBashMode(m.expandPastedPlaceholders(m.prompt.input.Value())); ok {
		return cmd
	}
	// A staged question answer wins over the streaming queue: the turn is
	// parked on the question, so the composer's text is the answer, not a
	// message for the next turn.
	if m.askQuestion.panel.SubmitAnswer() {
		return nil
	}
	if handled, cmd := m.checkStreamingQueue(m.expandPastedPlaceholders(m.prompt.input.Value())); handled {
		return cmd
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
	prompt = PrependSlashPlanIfNeeded(prompt, m.session.mode)
	if handled, cmd := m.dispatchPromptPrefix(prompt); handled {
		return cmd
	}
	if m.isLiveVoiceActive() {
		return m.startLiveVoiceTurn(prompt)
	}
	return m.startModelTurn(prompt)
}

func (m *RootModel) isLiveVoiceActive() bool {
	return m.audio.Surface != nil &&
		m.audio.Surface.Active() &&
		m.audio.Surface.Phase() == voice.PhaseLive
}

func (m *RootModel) startLiveVoiceTurn(prompt string) tea.Cmd {
	m.session.conversationStarted = true
	m.drainTaskNotifications()
	m.resetLiveTurnState(prompt)
	m.appendUserTurn(prompt)
	m.saveSession()
	sess := m.audio.liveSession
	sendCmd := func() tea.Msg {
		if sess != nil && sess.IsActive() {
			_ = sess.SendText(prompt)
		}
		return nil
	}
	tickCmd := func() tea.Msg { return m.conversation.spinner.Tick() }
	return tea.Batch(sendCmd, tickCmd)
}

func (m *RootModel) appendUserTurn(text string) {
	msg := backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, modelcatalog.FormatModelLabel(m.session.model), m.session.wireSettingsPending, time.Now()),
	}
	// The images the user attached ride along with the turn (agy 1.4.9's
	// image pasting). The transports carry them as image parts, so the
	// model receives them the way the reference sends them.
	if parts := m.mediaImageParts(); len(parts) > 0 {
		msg.Images = parts
		m.prompt.pendingMedia = nil
		m.prompt.mediaExpanded = false
	}
	m.session.appendMessage(msg)
	m.session.wireSettingsPending = false
}

func (m *RootModel) isEngineSlashCommand(cmd string) bool {
	// /agent is a host command whose work the engine performs: it is
	// answered by the dispatcher and forwarded to the engine, so it is
	// not a plain host command.
	if cmd == "agent" {
		return true
	}
	name := cmd
	if canonical, ok := slashcommands.Canonical(cmd); ok {
		name = canonical
	}
	if m.dispatchesCommand(name) {
		return false
	}
	if _, ok := slashcommands.PanelFor(name); ok {
		return false
	}
	for _, dyn := range m.engineSlashCommands() {
		clean := strings.TrimPrefix(dyn.Name, "/")
		if strings.EqualFold(clean, cmd) {
			return true
		}
	}
	return m.turn.engine != nil
}

func (m *RootModel) slashQueuesWhileStreaming(buf string) bool {
	if cmds, _ := slashparse.ParseLeadingCommands(buf); len(cmds) > 1 {
		return true
	}
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

func (m *RootModel) abortTurnForLocalCommand() {
	if !m.conversation.streaming {
		return
	}
	m.conversation.discardTurn = true
	if m.turn.activeBridge != nil {
		m.turn.activeBridge.CancelTurn()
	}
	m.conversation.streaming = false
	m.conversation.thinking = false
}

func (m *RootModel) submitAlternate() tea.Cmd {
	if m.askQuestion.panel != nil && m.askQuestion.panel.SubmitAnswer() {
		return nil
	}
	prompt := m.prompt.input.Value()
	if history.IsBlank(prompt) {
		m.prompt.input.Reset()
		m.ResetInputSelection()
		m.prompt.promptHistory.Reset()
		m.prompt.picker.Reset()
		return nil
	}
	// Invert QueueMode temporarily for this submission
	orig := m.session.store.QueueMode
	m.session.store.QueueMode = !orig
	defer func() {
		m.session.store.QueueMode = orig
	}()
	return m.submitPrompt()
}

func (m *RootModel) submitOrQueuePrompt() tea.Cmd {
	return m.submitPrompt()
}

// saveSession persists the conversation.
//
// The split is deliberate: the part that needs the session's live state (the
// supervisor snapshot and its durable-boundary advance) runs here, in order,
// on the Update goroutine; the part that only touches SQLite runs on the
// session writer. See persist.go for why.
func (m *RootModel) saveSession() {
	job := m.prepareSaveJob()
	if job == nil {
		return
	}
	if w := m.session.writer; w != nil {
		w.enqueue(job)
		return
	}
	// No writer (a test, or a host that never installed one): keep the old
	// synchronous behaviour so the store is immediately observable.
	applyPersistJob(job)
}

// prepareSaveJob snapshots everything one save has to write.
//
// It runs on the Update goroutine and is the only part of a save that reads
// or mutates session state.
func (m *RootModel) prepareSaveJob() *persistJob {
	var queued []string
	if m.session.store.QueuedPrompt != "" {
		queued = append(queued, m.session.store.QueuedPrompt)
	}
	if len(m.session.store.StreamQueue) > 0 {
		queued = append(queued, m.session.store.StreamQueue...)
	}
	job := &persistJob{
		convID: m.session.conversationID,
		queued: queued,
	}

	if sup := m.session.sup(); sup != nil {
		if plan, ok := sup.PrepareSave(m.session.conversationID, m.session.autoTitle); ok {
			job.plan = plan
			job.hasPlan = true
		}
		return job
	}
	if m.session.conversationID == "" || m.session.messageCount() == 0 {
		return job
	}
	steps := convstore.BuildSteps(m.trajectoryFacts())
	if len(steps) == 0 {
		return job
	}
	job.steps = steps
	job.hasSteps = true
	job.workspace = m.session.cwd
	job.title = m.session.autoTitle
	return job
}
