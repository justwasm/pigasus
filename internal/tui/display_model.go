package tui

// display_model.go — the TUI's display and lifecycle flags.
//
// DisplayModel owns state that only changes what the TUI shows or how it
// leaves: the quit flag, the two-step Ctrl+C confirmation, the verbosity
// override, the /usage spinner frame, the streaming-search gate, the last
// terminal program state and the attached-engine model-row cache.
//
// It is not turn transport and not transcript data, so it left the engine
// bucket.

import (
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/perfrecorder"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// DisplayModel owns the display and lifecycle flags.
type DisplayModel struct {
	quitting bool
	// exitConfirm is the first press of the two-step Ctrl+C exit
	// confirmation; exitConfirmAt records when it was armed.
	exitConfirm   bool
	exitConfirmAt time.Time
	// transientNotice is a temporary notification (e.g. "Copied...",
	// permission reloaded, etc.) that the status line temporarily yields to.
	transientNotice string
	// verbosity overrides the settings.json verbosity when non-empty.
	verbosity string
	// quotaFrame is the /usage quota spinner frame.
	quotaFrame int
	// streamingSearch gates the streaming search path.
	streamingSearch bool
	// showThinking controls whether the model's full thinking is streamed live
	// in the transcript and kept expanded on completion (AGY 1.5.14).
	showThinking bool
	// lastTurnOutcome is the last OSC 7501 (Program Status Protocol)
	// state the TUI reported.
	lastTurnOutcome tea.ProgramState
	// engineModelRowsCache caches the rows discovered from an attached
	// dynamic engine (Codex, Claude, etc.) so rendering /model does not
	// perform blocking IPC on every frame.
	engineModelRowsCache []modelpicker.Row
	// perf records Bubble Tea Update/View frame latencies when enabled.
	perf *perfrecorder.Recorder
}

// IsQuitting reports whether the TUI application is shutting down.
func (d *DisplayModel) IsQuitting() bool {
	return d != nil && d.quitting
}

// SetQuitting marks the TUI application as shutting down.
func (d *DisplayModel) SetQuitting(quitting bool) {
	if d != nil {
		d.quitting = quitting
	}
}

// ShowThinking reports whether the model's full thinking is streamed live.
func (d *DisplayModel) ShowThinking() bool {
	return d != nil && d.showThinking
}

// SetShowThinking sets whether the model's full thinking is streamed live.
func (d *DisplayModel) SetShowThinking(show bool) {
	if d != nil {
		d.showThinking = show
	}
}
