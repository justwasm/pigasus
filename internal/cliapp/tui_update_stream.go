package cliapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/tuitools"
)

func (m *runMode) resetDiscardedTurn() {
	m.discardTurn = false
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
}

func (m *runMode) dequeueNextPrompt() string {
	nextQueued := ""
	if m.queuedPrompt != "" {
		nextQueued = m.queuedPrompt
		m.queuedPrompt = ""
	} else if len(m.streamQueue) > 0 {
		nextQueued = m.streamQueue[0]
		m.streamQueue = m.streamQueue[1:]
	}
	return nextQueued
}

func (m *runMode) finaliseTurnReply(msg turnDoneMsg) {
	if finalised := m.finaliseLiveTurn(msg); !finalised {
		var replyLines []string
		if !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
			styled := renderMarkdown(msg.reply, m.width)
			replyLines = strings.Split(styled, "\n")
		}
		block := toolBlockLines(msg.steps, m.expanded, m.width)
		final := make([]string, 0, len(block)+len(replyLines)+1)
		pad := m.liveStart > 0 && strings.TrimSpace(stripANSI(m.response[m.liveStart-1])) != ""
		if pad {
			final = append(final, "")
		}
		final = append(final, block...)
		replyStart := m.liveStart + len(final)
		final = append(final, replyLines...)
		m.replaceRange(m.liveStart, len(m.response), final)
		if len(msg.steps) > 0 {
			start := m.liveStart
			if pad {
				start++
			}
			m.blocks = append(m.blocks, turnBlock{
				start: start,
				lines: len(block),
				steps: msg.steps,
			})
		}
		if len(replyLines) > 0 {
			m.assistants = append(m.assistants, assistantBlock{
				start: replyStart,
				lines: len(replyLines),
				raw:   msg.reply,
			})
		}
	} else if !m.streamedText && !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
		styled := renderMarkdown(msg.reply, m.width)
		lines := strings.Split(styled, "\n")
		start := len(m.response)
		m.response = append(m.response, lines...)
		m.assistants = append(m.assistants, assistantBlock{
			start: start,
			lines: len(lines),
			raw:   msg.reply,
		})
	} else if m.streamedText && !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
		if m.replyStart >= 0 && m.liveTextLines > 0 {
			raw := msg.reply
			if raw == "" {
				raw = m.liveReplyBuf
			}
			m.assistants = append(m.assistants, assistantBlock{
				start: m.replyStart,
				lines: m.liveTextLines,
				raw:   raw,
			})
		}
	}
}

func (m *runMode) recordTurnCompletion(msg turnDoneMsg) {
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.streaming = false
	m.thinking = false
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.recordTurnOutcome(tea.ProgramStateDone)

	if !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
		if m.liveReplyIdx >= 0 && m.liveReplyIdx < len(m.session) {
			m.session[m.liveReplyIdx].Content = msg.reply
		} else {
			m.session = append(m.session, backend.Message{Role: "assistant", Content: msg.reply})
		}
	}
	// The turn's tool calls join the trajectory before the save: the
	// calls are what the rehydration pass reads back.
	m.recordTrajectoryCalls(msg.steps)
	// A turn the user cancelled with Esc leaves no error row behind. The
	// reference answers a cancelled turn with the composer's interrupted
	// hint instead (【反编译】 agy 1.4.8's `(*PromptModel).interruptedLine`),
	// and the turn's own failure is the cancellation itself.
	if msg.err != nil && !m.turnInterrupted {
		m.response = append(m.response, turnErrorRow(msg.err))
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
		if len(m.bashQueue) > 0 {
			next := m.bashQueue[0]
			m.bashQueue = m.bashQueue[1:]
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
	m.response = append(m.response, userPromptRow(nextQueued))
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
	m.appendUserTurn(nextQueued)
	m.saveSession()
	m.streaming = true
	m.turnInterrupted = false
	m.clearLastTurnOutcome()
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(nextQueued), tickCmd)
}

func (m *runMode) updateTurnDoneMsg(msg turnDoneMsg) (tea.Model, tea.Cmd) {
	m.inputNotice = ""
	if m.discardTurn {
		m.resetDiscardedTurn()
		return m, tea.ClearScreen
	}
	nextQueued := m.dequeueNextPrompt()
	m.finaliseTurnReply(msg)
	m.recordTurnCompletion(msg)
	queueCmd := m.flushNextQueued(nextQueued)
	return m, tea.Batch(queueCmd, tea.ClearScreen)
}

func (m *runMode) updateToolRunningMsg(msg toolRunningMsg) {
	m.activeTool = tuitools.RunningSpinnerText(msg.step.name)
	m.anyToolRun = true
	m.streaming = true
	if m.batchBreak {
		m.liveBatch++
		m.batchBreak = false
	}
	m.blankBefore()
	step := msg.step
	step.batch = m.liveBatch
	lines := toolStepLines(step, m.expanded, m.width)
	m.liveStepRuns = append(m.liveStepRuns, liveStepRun{
		start: len(m.response),
		lines: len(lines),
		step:  step,
	})
	m.response = append(m.response, lines...)
}

func (m *runMode) updateToolStepMsg(msg toolStepMsg) {
	m.liveSteps = append(m.liveSteps, msg.step)
	m.activeTool = ""
	m.replyStart = -1
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	step := msg.step
	lines := toolStepLines(step, m.expanded, m.width)
	replaced := false
	if n := len(m.liveStepRuns); n > 0 && m.liveStepRuns[n-1].step.running {
		r := &m.liveStepRuns[n-1]
		oldEnd := r.start + r.lines
		if r.start >= 0 && oldEnd <= len(m.response) {
			step.batch = r.step.batch
			lines = toolStepLines(step, m.expanded, m.width)
			delta := len(lines) - r.lines
			m.replaceRange(r.start, oldEnd, lines)
			r.lines = len(lines)
			r.step = step
			if delta != 0 && m.replyStart >= oldEnd {
				m.replyStart += delta
			}
			replaced = true
		}
	}
	if !replaced {
		if m.batchBreak {
			m.liveBatch++
			m.batchBreak = false
		}
		step.batch = m.liveBatch
		lines = toolStepLines(step, m.expanded, m.width)
		m.liveStepRuns = append(m.liveStepRuns, liveStepRun{
			start: len(m.response),
			lines: len(lines),
			step:  step,
		})
		m.response = append(m.response, lines...)
	}
	m.liveSteps[len(m.liveSteps)-1] = step
}

func (m *runMode) updateBackendChunkMsg(msg backendChunkMsg) {
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
		m.blankBefore()
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
	styled := renderMarkdown(m.liveReplyBuf, m.width)
	lines := strings.Split(styled, "\n")
	m.replaceRange(m.replyStart, m.replyStart+m.liveTextLines, lines)
	m.liveTextLines = len(lines)
	m.streaming = true
	m.saveSession()
}

func (m *runMode) updateStreamingMsg(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case turnDoneMsg:
		mod, cmd := m.updateTurnDoneMsg(msg)
		return mod, cmd, true
	case toolRunningMsg:
		m.updateToolRunningMsg(msg)
		return m, nil, true
	case thoughtStartMsg:
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
		}
		m.streaming = true
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
	case toolStepMsg:
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
		return m, tea.ClearScreen, true
	}
	return m, nil, false
}
