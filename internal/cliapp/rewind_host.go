// rewind_host.go — the host's side of the /rewind picker.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/rewindpanel: it owns the
// cursor, the visible window, the rows and the keys. The host supplies
// the seams — the conversation's steps, the terminal's height, the
// rewind itself and the way out of the panel.
//
// # The rewind
//
// The policy is the session layer's (internal/app/session/rewind): it
// decides which messages a picked step owns, how many are removed and
// what the transcript's echo line says. The host applies it to the
// live session — the truncation reaches the session the next turn
// reads — and writes the echo.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
)

// newRewindPanel builds the /rewind picker with the host's seams.
func (m *runMode) newRewindPanel() *rewindpanel.Panel {
	return rewindpanel.New(rewindpanel.Seams{
		// The conversation's steps, newest first. The steps are the
		// session layer's — the panel names a step, the session layer
		// says what a step owns.
		Steps: func() []rewind.Step { return rewind.Steps(m.session) },
		// The terminal's height sizes the visible window.
		Height: func() int { return m.height },
		// The rewind itself: the session truncation and the echo.
		Pick: m.rewindPickStep,
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// rewindPanel returns the /rewind picker, building it if the host has
// not opened one yet.
//
// The panel is built when the /rewind panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that the renderer and the key router reach the same one.
func (m *runMode) rewindPanel() *rewindpanel.Panel {
	if m.panel.rw == nil {
		m.panel.rw = m.newRewindPanel()
	}
	return m.panel.rw
}

// rewindPickStep rewinds the conversation to the step the user picked.
//
// The policy is the session layer's: internal/app/session/rewind says
// which messages the picked step owns, how many are removed and what
// the echo line says. The host applies it to the live session, so the
// truncation really reaches the session a follow-up prompt reads, and
// writes the echo through slashRespond's REF-style echo format. The
// panel closes silently (same exit-notice=false set as /mcp
// /feedback).
//
// The picker owns the cursor: the index is the step the user picked,
// not the host's shared list cursor.
//
// Returns the trimmed count for the echo line.
func (m *runMode) rewindPickStep(index int) int {
	r := rewind.ToStep(m.session, index)
	if r.Removed <= 0 {
		// Nothing to remove: an empty conversation, or the cursor on the
		// current turn (the default). No truncation, no echo.
		return 0
	}
	m.session = r.Session
	m.slashRespond("/rewind", rewind.StepEcho(r))
	return r.Removed
}
