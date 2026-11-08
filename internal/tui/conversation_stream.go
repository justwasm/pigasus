package tui

// conversation_stream.go — the turn's update surface.
//
// The handlers here take the messages a turn produces — streamed text,
// tool running/finished, thoughts and completion — and drive the
// ConversationModel's lifecycle while the host performs the I/O: session
// appends, persistence and transcript-document edits. They used to sit in
// tui_stream.go beside the transport wiring; keeping them with the
// conversation model's state makes the turn's update path one module.
//
// Upstream: google3/third_party/jetski/cli/model/model.ConversationModel.Update.

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

func (m *RootModel) updateTurnDoneMsg(msg toolsteps.TurnDoneMsg) (tea.Model, tea.Cmd) {
	m.turn.activeBridge = nil
	// A remote turn just learned its cascade id; the save below names the
	// conversation after it so `/continue` can resume the remote cascade.
	m.adoptRemoteCascadeID()
	m.viewport.inputNotice = ""
	if m.conversation.discardTurn {
		m.resetDiscardedTurn()
		return m, tea.ClearScreen
	}
	// Promote any unapplied steer messages that arrived before turn completion into the queue
	// so they are executed in the next turn as promised by the architecture design.
	promotedSteers := 0
	if len(m.session.store.SendingMessages) > 0 {
		pending := append([]string(nil), m.session.store.SendingMessages...)
		m.session.store.SendingMessages = nil
		promotedSteers = len(pending)
		for _, instr := range pending {
			m.session.store.StreamQueue = append(m.session.store.StreamQueue, instr)
		}
		m.saveSession()
	}
	// A steer accepted just as the turn ended was never applied to the
	// actor. It is about to become the next turn's prompt, so drop the
	// actor's pending copy; otherwise the next turn applies it twice and
	// the transcript rewrites the message (AGY 1.5.3).
	if promotedSteers > 0 {
		m.clearPendingTurnSteers()
	}
	nextQueued := m.dequeueNextPrompt()
	docMsg, sessionMsg := m.steerReplySegments(msg)
	doc := m.transcriptDocument()
	opts := m.transcriptOptions(msg)
	if docMsg.Reply != msg.Reply {
		opts.EmptyTurn = agentturn.IsEmptyAssistantTurn(docMsg.Reply, nil, docMsg.Reasoning)
	}
	if doc.FinaliseTurnReply(docMsg, opts) {
		m.viewport.rowsCacheDirty = true
	}
	m.applyTranscriptDocument(doc)
	m.recordTurnCompletion(sessionMsg)

	// If /goal mode is active, check the outcome of this turn.
	if m.session.activeGoal != nil {
		completed, cancelled := m.session.activeGoal.CheckOutcome(msg.Reply)
		if completed || cancelled {
			m.session.activeGoal = nil
		} else {
			// If the user manually queued a prompt, that takes priority and pauses the goal loop.
			if nextQueued == "" {
				m.session.activeGoal.ContinuationCount++
				reminderPrompt := m.session.activeGoal.BuildReminderMessage()
				return m, tea.Batch(m.startModelTurn(reminderPrompt), tea.ClearScreen)
			}
		}
	}

	// In AGY 1.5.2: Stop rapid spinning retries when an error occurs while
	// there are queued messages; don't auto-flush queued prompts if the turn failed with error.
	if msg.Err != nil {
		if nextQueued != "" {
			// Put back the queued message at the head of the queue so it is not lost
			m.session.store.StreamQueue = append([]string{nextQueued}, m.session.store.StreamQueue...)
			m.saveSession()
		}
		return m, tea.ClearScreen
	}

	queueCmd := m.flushNextQueued(nextQueued)
	return m, tea.Batch(queueCmd, tea.ClearScreen)
}

func (m *RootModel) updateToolRunningMsg(msg toolsteps.ToolRunningMsg) {
	if m.conversation.thinking {
		m.finishLiveThinking()
	}
	m.conversation.BeginToolRun(tuitools.RunningSpinnerText(msg.Step.Name))

	// The transcript owns the batch arithmetic: the running call either
	// joins the current batch or starts a new one, and the rows of the
	// batch are repainted. The host keeps the batch bookkeeping.
	step := msg.Step
	doc := m.transcriptDocument()
	batch, batchBreak := doc.AppendRunningStep(&step, m.conversation.liveBatch, m.conversation.batchBreak, m.repaintOptions())
	m.applyTranscriptDocument(doc)
	m.conversation.liveBatch = batch
	m.conversation.batchBreak = batchBreak
	m.viewport.rowsCacheDirty = true
}

func (m *RootModel) updateToolStepMsg(msg toolsteps.ToolStepMsg) {
	if m.conversation.thinking {
		m.finishLiveThinking()
	}
	m.conversation.BeginToolResult()

	// The finished call takes the running call's place in the batch,
	// and the batch's rows are repainted with the block the batch now
	// holds. A call that had no running predecessor is appended as a
	// run of its own.
	step := msg.Step
	doc := m.transcriptDocument()
	opts := m.repaintOptions()
	if !doc.FinishLiveStep(&step, opts) {
		batch, batchBreak := doc.AppendLiveStep(&step, m.conversation.liveBatch, m.conversation.batchBreak, opts)
		m.conversation.liveBatch = batch
		m.conversation.batchBreak = batchBreak
	}
	m.applyTranscriptDocument(doc)
	m.conversation.liveSteps = append(m.conversation.liveSteps, step)
	m.viewport.rowsCacheDirty = true

	// If steer instructions were sent while this tool step was executing,
	// apply them now to the transcript at this tool step completion boundary!
	if len(m.session.store.SendingMessages) > 0 {
		pending := append([]string(nil), m.session.store.SendingMessages...)
		m.session.store.SendingMessages = nil
		for _, instr := range pending {
			m.applySteerToTranscript(instr)
		}
	}
}

func (m *RootModel) updateBackendChunkMsg(msg backendChunkMsg) {
	if m.conversation.thinking {
		m.finishLiveThinking()
	}
	chunk := string(msg)
	if chunk == "" || m.conversation.discardTurn {
		return
	}
	// The cancelled turn's own stream error — the loop reports the
	// cancellation as a chunk — is not part of the answer either. The
	// reference's cancelled turn shows the composer's interrupted hint and
	// nothing else.
	if m.conversation.turnInterrupted && chunk == context.Canceled.Error() {
		return
	}
	if m.conversation.replyStart < 0 {
		m.conversation.response = transcript.BlankBefore(m.conversation.response)
		m.conversation.BeginReply(len(m.conversation.response))
	}
	m.conversation.AppendReplyChunk(chunk)
	if m.conversation.liveReplyIdx < 0 {
		m.session.appendMessage(backend.Message{Role: "assistant", Content: chunk})
		m.conversation.liveReplyIdx = m.session.messageCount() - 1
	} else {
		m.session.appendMessageContent(m.conversation.liveReplyIdx, chunk)
	}
	styled := mdrender.RenderMarkdown(m.conversation.liveReplyBuf, m.viewport.width)
	lines := strings.Split(styled, "\n")
	m.conversation.response = transcript.ReplaceRange(m.conversation.response, m.conversation.replyStart, m.conversation.replyStart+m.conversation.liveTextLines, lines)
	m.conversation.liveTextLines = len(lines)
	m.saveSessionStreamDebounced()
}

func (m *RootModel) saveSessionStreamDebounced() {
	now := time.Now()
	// Persist the first chunk immediately so initial step is written to disk,
	// then throttle subsequent disk writes to at most once every 1.5 seconds.
	if m.conversation.lastStreamSave.IsZero() || now.Sub(m.conversation.lastStreamSave) >= 1500*time.Millisecond {
		m.saveSession()
		m.conversation.lastStreamSave = now
	}
}

func (m *RootModel) updateStreamingMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case terminalStartedMsg:
		m.remote.terminalID = msg.Info.TerminalID
		m.conversation.response = append(m.conversation.response,
			"  ⎿  Terminal "+msg.Info.TerminalID+" created in "+msg.Info.Cwd)
		return m, nil, true
	case terminalChunkMsg:
		m.conversation.response = append(m.conversation.response, terminalOutputRows(msg.Text)...)
		return m, nil, true
	case terminalExitMsg:
		if msg.Err != nil {
			m.conversation.response = append(m.conversation.response, "  ⎿  terminal: "+msg.Err.Error())
		} else if msg.Code != 0 {
			m.conversation.response = append(m.conversation.response, "  ⎿  Process exited with code "+itoaTerminal(msg.Code)+".")
		} else {
			m.conversation.response = append(m.conversation.response, "  ⎿  Process exited with code 0.")
		}
		return m, nil, true
	case remoteInteractionRaisedMsg:
		// The remote turn parked on a question or permission; the view
		// polls the modal/store, so the repaint is the whole handler.
		return m, nil, true
	case toolsteps.TurnDoneMsg:
		mod, cmd := m.updateTurnDoneMsg(msg)
		return mod, cmd, true
	case toolsteps.ToolRunningMsg:
		m.updateToolRunningMsg(msg)
		return m, nil, true
	case toolsteps.ThoughtStartMsg:
		m.conversation.BeginThinking(time.Now())
		return m, nil, true
	case toolsteps.ThoughtChunkMsg:
		m.conversation.AppendThought(msg.Delta, time.Now())
		if m.isExpanded() {
			m.syncLiveThoughtStep()
		}
		return m, nil, true
	case autoContBannerMsg:
		banner := "↻  auto-continuing… (retry " + fmt.Sprintf("%d", msg.depth) + " of " + fmt.Sprintf("%d", msg.maxDepth) + ")"
		m.conversation.response = append(m.conversation.response, banner)
		if m.conversation.streaming {
			m.conversation.streaming = false
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
	case steerAppliedMsg:
		m.updateSteerAppliedMsg(msg)
		return m, nil, true
	case backendDoneMsg:
		m.turn.activeBridge = nil
		m.conversation.EndStream()
		m.saveSession()
		if len(m.conversation.response) > 0 {
			m.conversation.response = append(m.conversation.response, "")
		}
		nextQueued := m.dequeueNextPrompt()
		if queueCmd := m.flushNextQueued(nextQueued); queueCmd != nil {
			return m, tea.Batch(tea.ClearScreen, queueCmd), true
		}
		return m, tea.ClearScreen, true
	}
	return m, nil, false
}
