// surface.go — the voice dictation state machine.
//
// # What it is
//
// The reference's dictation is a small state machine with four phases
// — idle, connecting, recording and finishing — and two exits from it:
// the draft is confirmed into the composer, or the session is
// discarded. The banner overlay, the running timer and the failure
// notice all follow from the phase.
//
// 【一手】 agy 1.4.3: the overlay reads "  Connecting... ", then a
// blinking dot with "Recording mm:ss", then "Finishing up... ", and
// the hint line names the keys — "enter or f5 to insert", "esc to
// discard".
//
// # Where it lives
//
// The surface owns the phase, the trigger, the recorder and the
// cancel flag. The host keeps the facts the surface cannot know: how
// a transcription session is established, where a confirmed draft is
// written, and how a failure is echoed into the conversation.
//
// The surface is safe on a nil receiver: a host that never built one
// has no dictation, and asks it nothing.
package voice

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// Seams are the host's answers the surface cannot answer itself.
//
// Every one of them is optional: a surface built without a seam
// behaves as if the seam were answered with the zero value, which keeps
// the package usable from a test without a running TUI.
type Seams struct {
	// Connect establishes the transcription session.
	//
	// It runs inside the command the surface hands back, so a slow
	// transport never blocks the render loop. A non-nil error ends
	// the session and echoes the failure.
	Connect func() error
	// InsertDraft writes a confirmed draft into the composer's input.
	InsertDraft func(text string)
	// Respond echoes the failure into the conversation.
	//
	// The first line is the failure itself; the extra lines are the
	// reference's own hint. Both are the reference's wording.
	Respond func(msg string, extra ...string)
	// Width is the terminal's width. The banner pads itself to it.
	Width func() int
}

// Surface is the voice dictation state machine.
//
// The zero value is usable: a surface without seams connects
// successfully, writes its draft nowhere and echoes its failure
// nowhere.
type Surface struct {
	seams Seams
	// active is true while a dictation session is on.
	active bool
	// phase is the session's current phase.
	phase Phase
	// trigger names what armed the dictation — "/voice", "/record" or
	// "f5".
	//
	// The reference records it; the clone reads it nowhere yet, and
	// the field is kept so the record survives the move.
	trigger string
	// rec is the session's recorder.
	rec *Recorder
	// cancel is true when the session was abandoned. A connection
	// result that arrives after a cancel is dropped.
	cancel bool
}

// New builds the surface.
func New(seams Seams) *Surface {
	return &Surface{seams: seams}
}

// Active is true while a dictation session is on.
func (s *Surface) Active() bool {
	return s != nil && s.active
}

// Phase is the session's current phase.
func (s *Surface) Phase() Phase {
	if s == nil {
		return PhaseNone
	}
	return s.phase
}

// Cancelled is true when the session was abandoned.
//
// A connection result that arrives after a cancel is dropped, so the
// failure notice never appears for a session the user walked away from.
func (s *Surface) Cancelled() bool {
	return s != nil && s.cancel
}

// Recorder is the session's recorder.
//
// It is the timing primitive the banner's timer reads and the place a
// transcription delta lands.
func (s *Surface) Recorder() *Recorder {
	if s == nil {
		return nil
	}
	return s.rec
}

// ConnectDoneMsg reports whether the voice transcription session
// established a stream or failed.
type ConnectDoneMsg struct {
	// Err is the failure. Nil means the session is up.
	Err error
}

// TickMsg is sent periodically during active recording to update the
// running duration timer.
type TickMsg struct{}

// Start arms the dictation.
//
// Pressing F5 — or invoking /voice — while a session is already on
// confirms it and inserts the draft, the way the reference's
// KeyToggleDictation does.
//
// The command it returns establishes the session; the result arrives
// as a ConnectDoneMsg.
func (s *Surface) Start(trigger string) tea.Cmd {
	if s == nil {
		return nil
	}
	if s.active {
		// If already dictating, pressing F5 / invoking again confirms and inserts the draft.
		return s.confirm()
	}
	s.active = true
	s.phase = PhaseConnecting
	s.trigger = trigger
	s.cancel = false

	rec, _ := NewRecorder("voice-"+fmt.Sprintf("%d", time.Now().UnixNano()), MaxRecordingDuration, nil)
	s.rec = rec

	return s.connectCmd()
}

// connectCmd returns the command that establishes the session.
func (s *Surface) connectCmd() tea.Cmd {
	return func() tea.Msg {
		var err error
		if s.seams.Connect != nil {
			err = s.seams.Connect()
		}
		return ConnectDoneMsg{Err: err}
	}
}

// confirm inserts any drafted speech into the composer and leaves
// voice mode.
func (s *Surface) confirm() tea.Cmd {
	if s == nil || !s.active {
		return nil
	}
	s.active = false
	if s.rec != nil {
		_, draft, _, _ := s.rec.Snapshot()
		if draft != "" {
			s.insertDraft(draft)
		}
		_ = s.rec.Stop()
	}
	s.phase = PhaseNone
	return nil
}

// cancel aborts voice mode without inserting text or printing an error.
func (s *Surface) cancelSession() tea.Cmd {
	if s == nil {
		return nil
	}
	s.cancel = true
	s.active = false
	s.phase = PhaseNone
	if s.rec != nil {
		s.rec.Reset()
	}
	return nil
}

// HandleKey handles keyboard input when voice mode is active.
//
// The second return value is "handled": false means the key was not
// the surface's and the host may keep it.
func (s *Surface) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if s == nil || !s.active {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		return true, s.cancelSession()
	case tea.KeyEnter, tea.KeyF5:
		if s.phase == PhaseConnecting {
			return true, s.cancelSession()
		}
		return true, s.confirm()
	}
	// While voice mode is active, consume other keys to match AGY AudioModel.HandleKey.
	return true, nil
}

// HandleConnectDone processes the result of the connection attempt.
//
// It returns the command the host must run next: the tick that keeps
// the timer moving once the session is up.
func (s *Surface) HandleConnectDone(msg ConnectDoneMsg) tea.Cmd {
	if s == nil {
		return nil
	}
	if s.cancel {
		return nil
	}
	if msg.Err != nil {
		s.active = false
		s.phase = PhaseNone
		if strings.Contains(msg.Err.Error(), "cannot reach the mic server") {
			s.respond("Could not start transcription: " + msg.Err.Error())
		} else {
			s.respond("Could not start transcription: "+msg.Err.Error(),
				"     Voice dictation needs a permission your current sign-in does not carry. Use /logout, sign in again, and retry. (Dictation is not available for business or enterprise accounts.)")
		}
		return nil
	}
	s.phase = PhaseRecording
	if s.rec != nil {
		_ = s.rec.Start()
	}
	return tickCmd()
}

// HandleTick advances the voice duration timer and transitions to
// finishing when max recording duration is reached.
func (s *Surface) HandleTick() tea.Cmd {
	if s == nil {
		return nil
	}
	if s.active && s.phase == PhaseRecording && s.rec != nil {
		s.rec.Tick(time.Second)
		state, _, _, _ := s.rec.Snapshot()
		if state == StateAutoStopped {
			s.phase = PhaseFinishing
		}
		return tickCmd()
	}
	return nil
}

// View renders the voice banner overlay (matching AGY 1.4.3 AudioModel.Banner).
func (s *Surface) View() string {
	if s == nil || !s.active {
		return ""
	}
	elapsed := time.Duration(0)
	if s.rec != nil {
		_, _, _, elapsed = s.rec.Snapshot()
	}
	return FormatBanner(s.phase, elapsed, s.width())
}

// tickCmd returns the command that advances the timer by one second.
//
// The message and the command live together, so the two cannot drift
// apart: the host's switch names the same TickMsg the command sends.
func tickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return TickMsg{}
	})
}

// insertDraft writes a confirmed draft into the composer's input.
func (s *Surface) insertDraft(text string) {
	if s == nil || s.seams.InsertDraft == nil {
		return
	}
	s.seams.InsertDraft(text)
}

// respond echoes the failure into the conversation.
func (s *Surface) respond(msg string, extra ...string) {
	if s == nil || s.seams.Respond == nil {
		return
	}
	s.seams.Respond(msg, extra...)
}

// width is the terminal's width.
func (s *Surface) width() int {
	if s == nil || s.seams.Width == nil {
		return 0
	}
	return s.seams.Width()
}
