package main

import (
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/mattn/go-runewidth"

	"github.com/justwasm/pigasus/internal/voice"
)

const (
	voicePhaseNone       = 0
	voicePhaseConnecting = 1
	voicePhaseRecording  = 2
	voicePhaseFinishing  = 3
)

// voiceConnectDoneMsg reports whether the voice transcription session
// established a stream or failed.
type voiceConnectDoneMsg struct {
	err error
}

// voiceTickMsg is sent periodically during active recording to update
// the running duration timer.
type voiceTickMsg struct{}

// startVoiceDictation initiates voice dictation mode (matching AGY AudioModel).
func (m *runMode) startVoiceDictation(trigger string) tea.Cmd {
	if m.voiceMode {
		// If already dictating, pressing F5 / invoking again confirms and inserts the draft.
		return m.confirmVoiceDictation()
	}
	m.voiceMode = true
	m.voicePhase = voicePhaseConnecting
	m.voiceTrigger = trigger
	m.voiceCancel = false

	rec, _ := voice.NewRecorder("voice-"+fmt.Sprintf("%d", time.Now().UnixNano()), voice.MaxRecordingDuration, nil)
	m.voiceRecorder = rec

	return m.connectVoiceCmd()
}

func (m *runMode) connectVoiceCmd() tea.Cmd {
	if m.voiceConnectFn != nil {
		return func() tea.Msg {
			return voiceConnectDoneMsg{err: m.voiceConnectFn()}
		}
	}
	return func() tea.Msg {
		// When ANTIGRAVITY_MIC is specified (e.g. running over SSH with
		// reverse forwarded port 4713 from a local machine running
		// `wagyu mic-serve` or `agy mic-serve`), attempt to connect to the
		// remote microphone daemon over TCP.
		if micAddr := os.Getenv("ANTIGRAVITY_MIC"); micAddr != "" {
			conn, err := net.DialTimeout("tcp", micAddr, 2*time.Second)
			if err != nil {
				return voiceConnectDoneMsg{
					err: fmt.Errorf("cannot reach the mic server at %s (tcp): %w", micAddr, err),
				}
			}
			_ = conn.Close()
		}
		// In interactive mode, voice dictation connects successfully and
		// enters the active recording phase (matching AGY 1.4.3).
		return voiceConnectDoneMsg{err: nil}
	}
}

// confirmVoiceDictation inserts any drafted speech into the prompt input
// and leaves voice mode.
func (m *runMode) confirmVoiceDictation() tea.Cmd {
	if !m.voiceMode {
		return nil
	}
	m.voiceMode = false
	if m.voiceRecorder != nil {
		_, draft, _, _ := m.voiceRecorder.Snapshot()
		if draft != "" {
			if m.input.Value() == "" {
				m.input.SetValue(draft)
			} else {
				m.input.InsertString(draft)
			}
		}
		_ = m.voiceRecorder.Stop()
	}
	m.voicePhase = voicePhaseNone
	return nil
}

// cancelVoiceDictation aborts voice mode without inserting text or printing an error.
func (m *runMode) cancelVoiceDictation() tea.Cmd {
	m.voiceCancel = true
	m.voiceMode = false
	m.voicePhase = voicePhaseNone
	if m.voiceRecorder != nil {
		m.voiceRecorder.Reset()
	}
	return nil
}

// handleVoiceKey handles keyboard input when voice mode is active.
// Returns handled = true if the key belonged to voice mode.
func (m *runMode) handleVoiceKey(key tea.Key) (bool, tea.Cmd) {
	if !m.voiceMode {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, m.cancelVoiceDictation()
	case tea.KeyEnter, tea.KeyF5:
		if m.voicePhase == voicePhaseConnecting {
			return true, m.cancelVoiceDictation()
		}
		return true, m.confirmVoiceDictation()
	}
	// While voice mode is active, consume other keys to match AGY AudioModel.HandleKey.
	return true, nil
}

// handleVoiceConnectDone processes the result of the connection attempt.
func (m *runMode) handleVoiceConnectDone(msg voiceConnectDoneMsg) (tea.Model, tea.Cmd) {
	if m.voiceCancel {
		return m, nil
	}
	if msg.err != nil {
		m.voiceMode = false
		m.voicePhase = voicePhaseNone
		if strings.Contains(msg.err.Error(), "cannot reach the mic server") {
			m.slashRespond("/voice", "Could not start transcription: "+msg.err.Error())
		} else {
			m.slashRespond("/voice",
				"Could not start transcription: "+msg.err.Error(),
				"     Voice dictation needs a permission your current sign-in does not carry. Use /logout, sign in again, and retry. (Dictation is not available for business or enterprise accounts.)")
		}
		return m, nil
	}
	m.voicePhase = voicePhaseRecording
	if m.voiceRecorder != nil {
		_ = m.voiceRecorder.Start()
	}
	return m, voiceTickCmd()
}

// handleVoiceTick advances the voice duration timer and transitions to finishing
// when max recording duration is reached.
func (m *runMode) handleVoiceTick() (tea.Model, tea.Cmd) {
	if m.voiceMode && m.voicePhase == voicePhaseRecording && m.voiceRecorder != nil {
		m.voiceRecorder.Tick(time.Second)
		state, _, _, _ := m.voiceRecorder.Snapshot()
		if state == voice.StateAutoStopped {
			m.voicePhase = voicePhaseFinishing
		}
		return m, voiceTickCmd()
	}
	return m, nil
}

func voiceTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return voiceTickMsg{}
	})
}

// voiceView renders the voice banner overlay (matching AGY 1.4.3 AudioModel.Banner).
func (m *runMode) voiceView() string {
	if !m.voiceMode {
		return ""
	}
	width := m.width
	if width <= 0 {
		width = 80
	}
	var left, right string
	switch m.voicePhase {
	case voicePhaseConnecting:
		left = "  Connecting... "
		right = panelGray238 + "esc to cancel" + panelGray238C
	case voicePhaseRecording:
		elapsed := time.Duration(0)
		if m.voiceRecorder != nil {
			_, _, _, elapsed = m.voiceRecorder.Snapshot()
		}
		mins := int(elapsed.Minutes())
		secs := int(elapsed.Seconds()) % 60
		timerStr := fmt.Sprintf("%02d:%02d", mins, secs)
		dot := "\x1b[38;5;196m●\x1b[0m"
		if int(elapsed.Seconds())%2 == 1 {
			dot = "\x1b[38;5;196m○\x1b[0m"
		}
		left = "  " + dot + " Recording " + timerStr + " "
		right = "enter or f5 to insert " + panelGray238 + "· esc to discard" + panelGray238C
	case voicePhaseFinishing:
		left = "  Finishing up...  "
		right = "enter or f5 to insert what you have " + panelGray238 + "· esc to discard" + panelGray238C
	default:
		return ""
	}

	leftLen := voiceVisibleLen(left)
	rightLen := voiceVisibleLen(right)
	pad := width - leftLen - rightLen
	if pad < 1 {
		pad = 1
	}
	return left + strings.Repeat(" ", pad) + right + "\n"
}

func voiceVisibleLen(s string) int {
	return runewidth.StringWidth(stripSGR(s))
}
