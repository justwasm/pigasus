package cliapp

import (
	"fmt"
	"net"
	"os"
	"time"

	"github.com/justwasm/pigasus/internal/ui/views/voice"
)

// voice_host.go — cycle 381: the voice dictation state machine moved
// to internal/ui/views/voice, the package that already owned the
// banner, the timer and the recorder.
//
// What stayed here is the wiring the surface cannot know: how a
// transcription session is established, where a confirmed draft is
// written, and how a failure is echoed into the conversation.

// newVoiceSurface builds the voice dictation surface.
func (m *runMode) newVoiceSurface() *voice.Surface {
	return voice.New(voice.Seams{
		Connect: m.connectVoice,
		InsertDraft: func(text string) {
			if m.input.Value() == "" {
				m.input.SetValue(text)
			} else {
				m.input.InsertString(text)
			}
		},
		Respond: func(msg string, extra ...string) {
			m.slashRespond("/voice", msg, extra...)
		},
		Width: func() int { return m.width },
	})
}

// connectVoice establishes the transcription session.
//
// It runs inside the command the surface hands back, so the dial never
// blocks the render loop.
func (m *runMode) connectVoice() error {
	if m.voiceConnectFn != nil {
		return m.voiceConnectFn()
	}
	// When ANTIGRAVITY_MIC is specified (e.g. running over SSH with
	// reverse forwarded port 4713 from a local machine running
	// `wagyu mic-serve` or `agy mic-serve`), attempt to connect to the
	// remote microphone daemon over TCP.
	if micAddr := os.Getenv("ANTIGRAVITY_MIC"); micAddr != "" {
		conn, err := net.DialTimeout("tcp", micAddr, 2*time.Second)
		if err != nil {
			return fmt.Errorf("cannot reach the mic server at %s (tcp): %w", micAddr, err)
		}
		_ = conn.Close()
	}
	// In interactive mode, voice dictation connects successfully and
	// enters the active recording phase (matching AGY 1.4.3).
	return nil
}
