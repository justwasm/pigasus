package cliapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

func (m *runMode) resetDiscardedTurn() {
	m.discardTurn = false
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	m.activeTip = ""
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
}

func (m *runMode) dequeueNextPrompt() string {
	nextQueued := ""
	if m.store.QueuedPrompt != "" {
		nextQueued = m.store.QueuedPrompt
		m.store.QueuedPrompt = ""
	} else if len(m.store.StreamQueue) > 0 {
		nextQueued = m.store.StreamQueue[0]
		m.store.StreamQueue = m.store.StreamQueue[1:]
	}
	return nextQueued
}

func (m *runMode) recordTurnCompletion(msg toolsteps.TurnDoneMsg) {
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.liveThinking = ""
	m.liveThinkingSnippet = ""
	m.liveThoughtRunIdx = -1
	m.activeTip = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	// The turn's outcome is a fact the session rests in: the status
	// line's `agent_state` member reports it ("error" after a failed
	// turn) and the terminal's own program status reads the same fact.
	//
	// A turn the user cancelled with Esc is not a failure: the
	// cancellation is the turn's own outcome.
	if msg.Err != nil && !m.turnInterrupted {
		m.recordTurnOutcome(tea.ProgramStateError)
	} else {
		m.recordTurnOutcome(tea.ProgramStateDone)
	}

	if !agentturn.IsEmptyAssistantTurn(msg.Reply, nil, msg.Reasoning) {
		if m.liveReplyIdx >= 0 && m.liveReplyIdx < len(m.session) {
			m.session[m.liveReplyIdx].Content = msg.Reply
		} else {
			m.session = append(m.session, backend.Message{Role: "assistant", Content: msg.Reply})
		}
	}
	// The turn's tool calls join the trajectory before the save: the
	// calls are what the rehydration pass reads back.
	m.recordTrajectoryCalls(msg.Steps)
	// A turn the user cancelled with Esc leaves no error row behind. The
	// reference answers a cancelled turn with the composer's interrupted
	// hint instead (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`),
	// and the turn's own failure is the cancellation itself.
	if msg.Err != nil && !m.turnInterrupted {
		if message, ok := agent.TurnErrorMessage(msg.Err); ok {
			m.response = append(m.response, toolsteps.ErrorRow(message))
		}
	}
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.saveSession()
	if len(m.response) > 0 {
		m.response = append(m.response, "")
	}
}

func (m *runMode) flushNextQueued(nextQueued string) tea.Cmd {
	if nextQueued == "" {
		if len(m.store.BashQueue) > 0 {
			next := m.store.BashQueue[0]
			m.store.BashQueue = m.store.BashQueue[1:]
			return m.startBashCommand(next)
		}
		// A background task that finished while the turn was running has
		// a notification to deliver. The reference's agent picks it up
		// and carries on without waiting for the user.
		if m.taskInbox != nil && m.taskInbox.Len() > 0 {
			return m.startNotificationTurn()
		}
		return nil
	}
	if handled, cmd := m.dispatchSlashLine(nextQueued); handled {
		return cmd
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
	promptLines := transcript.RenderUserPrompt(nextQueued, m.width)
	m.response = append(m.response, promptLines...)
	m.userBlocks = append(m.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   nextQueued,
	})
	m.liveStart = len(m.response)
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.lastStreamSave = time.Time{}
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.appendUserTurn(nextQueued)
	m.saveSession()
	m.streaming = true
	m.turnInterrupted = false
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(nextQueued), tickCmd)
}

func (m *runMode) updateTurnDoneMsg(msg toolsteps.TurnDoneMsg) (tea.Model, tea.Cmd) {
	m.inputNotice = ""
	if m.discardTurn {
		m.resetDiscardedTurn()
		return m, tea.ClearScreen
	}
	nextQueued := m.dequeueNextPrompt()
	doc := m.transcriptDocument()
	if doc.FinaliseTurnReply(msg, m.transcriptOptions(msg)) {
		m.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
	m.recordTurnCompletion(msg)

	// If /goal mode is active, check the outcome of this turn.
	if m.activeGoal != nil {
		completed, cancelled := m.activeGoal.CheckOutcome(msg.Reply)
		if completed || cancelled {
			m.activeGoal = nil
		} else {
			// If the user manually queued a prompt, that takes priority and pauses the goal loop.
			if nextQueued == "" {
				m.activeGoal.ContinuationCount++
				reminderPrompt := m.activeGoal.BuildReminderMessage()
				return m, tea.Batch(m.startModelTurn(reminderPrompt), tea.ClearScreen)
			}
		}
	}

	queueCmd := m.flushNextQueued(nextQueued)
	return m, tea.Batch(queueCmd, tea.ClearScreen)
}

func (m *runMode) updateToolRunningMsg(msg toolsteps.ToolRunningMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	m.activeTool = tuitools.RunningSpinnerText(msg.Step.Name)
	m.anyToolRun = true
	m.streaming = true

	// The transcript owns the batch arithmetic: the running call either
	// joins the current batch or starts a new one, and the rows of the
	// batch are repainted. The host keeps the batch bookkeeping.
	step := msg.Step
	doc := m.transcriptDocument()
	batch, batchBreak := doc.AppendRunningStep(&step, m.liveBatch, m.batchBreak, m.repaintOptions())
	m.applyTranscriptDocument(doc)
	m.liveBatch = batch
	m.batchBreak = batchBreak
	m.rowsCacheDirty = true
}

func (m *runMode) updateToolStepMsg(msg toolsteps.ToolStepMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	m.activeTool = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""

	// The finished call takes the running call's place in the batch,
	// and the batch's rows are repainted with the block the batch now
	// holds. A call that had no running predecessor is appended as a
	// run of its own.
	step := msg.Step
	doc := m.transcriptDocument()
	opts := m.repaintOptions()
	if !doc.FinishLiveStep(&step, opts) {
		batch, batchBreak := doc.AppendLiveStep(&step, m.liveBatch, m.batchBreak, opts)
		m.liveBatch = batch
		m.batchBreak = batchBreak
	}
	m.applyTranscriptDocument(doc)
	m.liveSteps = append(m.liveSteps, step)
	m.rowsCacheDirty = true
}

func (m *runMode) updateBackendChunkMsg(msg backendChunkMsg) {
	if m.thinking {
		m.finishLiveThinking()
	}
	chunk := string(msg)
	if chunk == "" || m.discardTurn {
		return
	}
	// The cancelled turn's own stream error — the loop reports the
	// cancellation as a chunk — is not part of the answer either. The
	// reference's cancelled turn shows the composer's interrupted hint and
	// nothing else.
	if m.turnInterrupted && chunk == context.Canceled.Error() {
		return
	}
	if m.replyStart < 0 {
		m.response = transcript.BlankBefore(m.response)
		m.replyStart = len(m.response)
		m.liveTextLines = 0
		m.liveReplyBuf = ""
	}
	m.liveReplyBuf += chunk
	m.streamedText = true
	m.batchBreak = true
	if m.liveReplyIdx < 0 {
		m.session = append(m.session, backend.Message{Role: "assistant", Content: chunk})
		m.liveReplyIdx = len(m.session) - 1
	} else {
		m.session[m.liveReplyIdx].Content += chunk
	}
	styled := mdrender.RenderMarkdown(m.liveReplyBuf, m.width)
	lines := strings.Split(styled, "\n")
	m.response = transcript.ReplaceRange(m.response, m.replyStart, m.replyStart+m.liveTextLines, lines)
	m.liveTextLines = len(lines)
	m.streaming = true
	m.saveSessionStreamDebounced()
}

func (m *runMode) saveSessionStreamDebounced() {
	now := time.Now()
	// Persist the first chunk immediately so initial step is written to disk,
	// then throttle subsequent disk writes to at most once every 1.5 seconds.
	if m.lastStreamSave.IsZero() || now.Sub(m.lastStreamSave) >= 1500*time.Millisecond {
		m.saveSession()
		m.lastStreamSave = now
	}
}

func (m *runMode) updateStreamingMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case toolsteps.TurnDoneMsg:
		mod, cmd := m.updateTurnDoneMsg(msg)
		return mod, cmd, true
	case toolsteps.ToolRunningMsg:
		m.updateToolRunningMsg(msg)
		return m, nil, true
	case toolsteps.ThoughtStartMsg:
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
			m.liveThinking = ""
			m.liveThinkingSnippet = ""
			m.liveThoughtRunIdx = -1
		}
		m.streaming = true
		return m, nil, true
	case toolsteps.ThoughtChunkMsg:
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
		}
		m.streaming = true
		m.liveThinking += msg.Delta
		m.liveThinkingSnippet = toolsteps.ThinkingLiveSnippet(m.liveThinking)
		if m.isExpanded() {
			m.syncLiveThoughtStep()
		}
		return m, nil, true
	case autoContBannerMsg:
		banner := "↻  auto-continuing… (retry " + fmt.Sprintf("%d", msg.depth) + " of " + fmt.Sprintf("%d", msg.maxDepth) + ")"
		m.response = append(m.response, banner)
		if m.streaming {
			m.streaming = false
		}
		return m, nil, true
	case eligibilityNoticeMsg:
		m.setEligibilityNotice(msg.Reason, msg.VerificationURL)
		return m, nil, true
	case toolsteps.ToolStepMsg:
		m.updateToolStepMsg(msg)
		return m, nil, true
	case taskNotificationMsg:
		// A background task finished. When the agent is idle the
		// notification is delivered in a turn of its own; when a turn is
		// in flight it waits for the next one.
		return m, m.updateTaskNotificationMsg(), true
	case browserAgentDoneMsg:
		// The browser agent behind the /browser slash command answered.
		m.updateBrowserAgentDoneMsg(msg)
		return m, nil, true
	case backendChunkMsg:
		m.updateBackendChunkMsg(msg)
		return m, nil, true
	case backendDoneMsg:
		m.replyStart = -1
		m.liveReplyIdx = -1
		m.streaming = false
		m.thinking = false
		m.saveSession()
		if len(m.response) > 0 {
			m.response = append(m.response, "")
		}
		nextQueued := m.dequeueNextPrompt()
		if queueCmd := m.flushNextQueued(nextQueued); queueCmd != nil {
			return m, tea.Batch(tea.ClearScreen, queueCmd), true
		}
		return m, tea.ClearScreen, true
	}
	return m, nil, false
}
