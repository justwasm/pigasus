package tui

// slash_compact_spinner_arm_test.go — the tick that makes the spinner turn.
//
// # What this pins
//
// Three separate faults have hidden behind "the spinner is static":
//
//   - tui_compact_spinner_test.go pins the first: the command produced a
//     message *type* the update switch never matched, so the host simply
//     dropped it.
//   - compact_progress_row_test.go pins the row's shape and its repaint:
//     updateCompactSpinnerTickMsg is driven by hand and the row must
//     move, without the transcript growing a row per frame.
//   - This file pins what neither can see: that /compact ever *sends*
//     the tick in the first place. A row that animates when ticked and
//     is never ticked is a still image of a spinner, which is exactly
//     how it read.
//
// The assertion is therefore on the command the slash command returns.

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// TestSlashCompactArmsTheSpinnerTick pins the arming: starting a
// compaction must hand back the command that produces the next tick.
//
// Without it the live row is drawn once and never repainted, so the
// Braille frame never advances even though every piece of the animation
// exists and every other spinner test passes.
func TestSlashCompactArmsTheSpinnerTick(t *testing.T) {
	m := mkCompactNoProgModel(t, &fakeStreamClient{summary: "s", hang: true})
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})

	_, cmd := m.handleSlashCommand("/compact")
	if cmd == nil {
		t.Fatal("/compact returned no command: the live row is armed but nothing ticks it, so the spinner never advances")
	}
	if _, ok := cmd().(compactSpinnerTickMsg); !ok {
		t.Fatalf("/compact armed %T, want a compactSpinnerTickMsg", cmd())
	}
}

// TestCompactSpinnerReArmsWhileRunning pins the loop: the tick the
// slash command armed must re-arm itself, or the spinner advances one
// frame and stops.
func TestCompactSpinnerReArmsWhileRunning(t *testing.T) {
	m := mkCompactNoProgModel(t, &fakeStreamClient{summary: "s", hang: true})
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})

	if _, cmd := m.handleSlashCommand("/compact"); cmd == nil {
		t.Fatal("/compact returned no command")
	}

	if _, next := m.updateCompactSpinnerTickMsg(); next == nil {
		t.Error("a tick did not re-arm the next one; the spinner would stop after a single frame")
	}
}

// TestCompactSpinnerStopsAfterTheCompactionEnds pins the stop: once the
// compaction resolves the tick must not keep re-arming, or the spinner
// runs on a finished compaction for the rest of the session.
func TestCompactSpinnerStopsAfterTheCompactionEnds(t *testing.T) {
	m := mkCompactNoProgModel(t, &fakeStreamClient{summary: "s", hang: true})
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})

	if _, cmd := m.handleSlashCommand("/compact"); cmd == nil {
		t.Fatal("/compact returned no command")
	}

	m.session.store.CompactEnd = time.Now()
	if _, next := m.updateCompactSpinnerTickMsg(); next != nil {
		t.Error("the tick re-armed after the compaction ended")
	}
}
