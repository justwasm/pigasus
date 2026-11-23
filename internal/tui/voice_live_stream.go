package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	voicebackend "github.com/justwasm/pigasus/internal/voice"
	"github.com/justwasm/pigasus/palette"
)

type voiceUserSpeechMsg struct {
	text string
}

type voiceUserSpeechInterimMsg struct {
	text string
}

type voiceAgentDeltaMsg struct {
	delta string
}

type voiceAgentDoneMsg struct {
	text string
}

type voiceInterruptedMsg struct{}

type voiceThinkerHandoffMsg struct {
	invocationID string
	task         string
}

type voiceThinkerDoneMsg struct {
	invocationID string
	answer       string
	err          error
}

func (m *RootModel) handleVoiceUserSpeechInterim(text string) {
	if m.audio.Surface != nil {
		m.audio.Surface.SetPreview(text)
	}
	m.viewport.rowsCacheDirty = true
}

func (m *RootModel) handleVoiceUserSpeech(text string) tea.Cmd {
	if m.audio.Surface != nil {
		m.audio.Surface.SetPreview("")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	sep := transcript.TurnSeparator(m.viewport.width)
	if len(m.conversation.response) == 0 {
		m.conversation.response = append(m.conversation.response, sep)
	} else {
		m.conversation.response = append(m.conversation.response, "", sep)
	}
	promptLines := transcript.RenderUserPrompt(text, m.viewport.width)
	start := len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, promptLines...)
	m.conversation.userBlocks = append(m.conversation.userBlocks, transcript.UserBlock{
		Start: start,
		Lines: len(promptLines),
		Raw:   text,
	})
	m.appendUserTurn(text)
	m.saveSession()
	wasWorking := m.conversation.streaming || m.audio.thinking
	m.conversation.streaming = true
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()
	if wasWorking {
		return nil
	}
	m.conversation.spinFrame = 0
	return func() tea.Msg { return m.conversation.spinner.Tick() }
}

func (m *RootModel) handleVoiceAgentDelta(delta string) tea.Cmd {
	if delta == "" {
		return nil
	}
	wasWorking := m.conversation.streaming || m.audio.thinking
	m.conversation.streaming = true
	var tickCmd tea.Cmd
	if !wasWorking {
		tickCmd = func() tea.Msg { return m.conversation.spinner.Tick() }
	}
	if m.conversation.voiceReplyStart < 0 {
		m.conversation.response = transcript.BlankBefore(m.conversation.response)
		m.conversation.voiceReplyStart = len(m.conversation.response)
		m.conversation.voiceReplyBuf = ""
		m.conversation.voiceTextLines = 0
	}
	m.conversation.voiceReplyBuf += delta
	styled := mdrender.RenderMarkdown(m.conversation.voiceReplyBuf, m.viewport.width)
	lines := strings.Split(styled, "\n")
	m.conversation.response = transcript.ReplaceRange(m.conversation.response, m.conversation.voiceReplyStart, m.conversation.voiceReplyStart+m.conversation.voiceTextLines, lines)
	m.conversation.voiceTextLines = len(lines)
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()
	return tickCmd
}

func (m *RootModel) handleVoiceAgentDone(text string) {
	if text == "" && m.conversation.voiceReplyBuf != "" {
		text = m.conversation.voiceReplyBuf
	}
	if m.conversation.voiceReplyStart >= 0 {
		styled := mdrender.RenderMarkdown(text, m.viewport.width)
		lines := strings.Split(styled, "\n")
		m.conversation.response = transcript.ReplaceRange(m.conversation.response, m.conversation.voiceReplyStart, m.conversation.voiceReplyStart+m.conversation.voiceTextLines, lines)
		m.conversation.voiceReplyStart = -1
		m.conversation.voiceReplyBuf = ""
		m.conversation.voiceTextLines = 0
	} else if text != "" {
		styled := mdrender.RenderMarkdown(text, m.viewport.width)
		lines := strings.Split(styled, "\n")
		m.conversation.response = transcript.BlankBefore(m.conversation.response)
		m.conversation.response = append(m.conversation.response, lines...)
	}
	if text != "" {
		m.session.appendMessage(backend.Message{
			Role:    "assistant",
			Content: text,
		})
		m.saveSession()
	}
	if !m.audio.thinking {
		m.conversation.streaming = false
	}
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()
}

func (m *RootModel) handleVoiceInterrupted() {
	m.audio.thinking = false
	m.conversation.streaming = false
	if m.conversation.voiceReplyStart >= 0 {
		if len(m.conversation.response) > 0 && m.conversation.voiceReplyStart < len(m.conversation.response) {
			lastIdx := m.conversation.voiceReplyStart + m.conversation.voiceTextLines - 1
			if lastIdx >= 0 && lastIdx < len(m.conversation.response) {
				m.conversation.response[lastIdx] += " " + palette.SubtleOpen + "[interrupted]" + palette.ToolVerbClose
			}
		}
		if m.conversation.voiceReplyBuf != "" {
			m.session.appendMessage(backend.Message{
				Role:    "assistant",
				Content: m.conversation.voiceReplyBuf + " [interrupted]",
			})
			m.saveSession()
		}
		m.conversation.voiceReplyStart = -1
		m.conversation.voiceReplyBuf = ""
		m.conversation.voiceTextLines = 0
	}
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()
}

func (m *RootModel) handleVoiceThinkerHandoff(invocationID string, task string) tea.Cmd {
	task = strings.TrimSpace(task)
	if task == "" {
		return nil
	}
	wasWorking := m.conversation.streaming || m.audio.thinking
	m.audio.thinking = true
	m.conversation.streaming = true
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()

	taskCtx, taskCancel := context.WithCancel(context.Background())
	m.audio.setThinkerCancel(invocationID, taskCancel)

	go func() {
		defer taskCancel()
		sess := m.audio.liveSession
		throttler := voicebackend.NewNoteThrottler(endpoints.CurrentVoiceNoteInterval())
		result, err := m.RunThinkerTaskWithContext(taskCtx, task, func(note string) {
			if sess != nil && sess.IsActive() && throttler.ShouldSend(time.Now(), note, false) {
				_ = sess.SendThinkerNote(note)
			}
		})
		alreadySent := m.audio.clearThinkerCancel(invocationID)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				if !alreadySent && sess != nil && sess.IsActive() {
					_ = sess.SendTaskStopped()
					if invocationID != "" {
						_ = sess.CompleteHandoff(invocationID, map[string]any{"cancelled": true})
					}
				}
			} else if sess != nil && sess.IsActive() {
				_ = sess.SendThinkerNote("Task failed: " + err.Error())
				if invocationID != "" {
					_ = sess.CompleteHandoff(invocationID, map[string]any{"error": err.Error()})
				}
			}
			if !alreadySent && m.turn.prog != nil {
				m.turn.prog.Send(voiceThinkerDoneMsg{
					invocationID: invocationID,
					err:          err,
				})
			}
			return
		}
		if sess != nil && sess.IsActive() {
			if invocationID != "" {
				_ = sess.CompleteHandoff(invocationID, map[string]any{"answer": result})
			}
			_ = sess.SendThinkerAnswer(result)
		}
		if m.turn.prog != nil {
			m.turn.prog.Send(voiceThinkerDoneMsg{
				invocationID: invocationID,
				answer:       result,
			})
		}
	}()

	if wasWorking {
		return nil
	}
	return func() tea.Msg { return m.conversation.spinner.Tick() }
}

func (m *RootModel) handleVoiceThinkerDone(msg voiceThinkerDoneMsg) {
	m.audio.thinking = false
	m.conversation.streaming = false
	m.viewport.autoScroll = true
	m.viewport.rowsCacheDirty = true
	m.syncVoiceWireHUD()
}

func (m *RootModel) syncVoiceWireHUD() {
	if m == nil || m.overlays.wireHUD == nil {
		return
	}
	sess := m.audio.liveSession
	if sess == nil {
		m.overlays.wireHUD.UpdateVoiceMetrics(false, "", 0, 0, 0, 0, 0, 0, 0, 0)
		return
	}
	stats := sess.WireStats()
	m.overlays.wireHUD.UpdateVoiceMetrics(
		sess.IsActive(),
		sess.ConnectionState().String(),
		stats.AudioSentBytes,
		stats.AudioSentChunks,
		stats.AudioRecvBytes,
		stats.AudioRecvChunks,
		stats.TextChunksRecv,
		stats.TurnsCompleted,
		stats.InterruptedCount,
		stats.LastTurnLatency,
	)
}
