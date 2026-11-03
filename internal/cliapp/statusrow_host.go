// statusrow_host.go — the host's side of the status row.
//
// # Where the row lives
//
// The row itself is internal/ui/views/statusrow: it owns the two
// segments, the rules that decide the hint, the padding arithmetic
// and the compaction label.
//
// The host keeps the facts the row cannot know — the model, the mode,
// the effort tier, the running tasks, the compaction state, the
// composer and the pickers — and hands them over as a State.
//
// The row is a view: it takes the facts and writes the line, so the
// host's own job is to collect them and to decide whether the
// external status-line script answers instead.

package cliapp

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/editor/vimkeys"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/statusrow"
)

// statusRowState collects the facts the status row draws.
//
// The facts are the host's: the model, the mode, the effort tier, the
// running tasks, the compaction state, the composer and the pickers.
// The row decides what they look like together.
func (m *runMode) statusRowState() statusrow.State {
	// A closed panel keeps the hint, so the row's rule is only asked
	// about an open one.
	panelOpen := m.panel.kind != panelregistry.None
	panelKeepsHint := false
	if panelOpen {
		panelKeepsHint = m.panel.kind.KeepStatusHint()
	}

	return statusrow.State{
		Width:          m.width,
		CanonicalWidth: separatorWidth,

		PickerVisible:      m.picker.Visible(),
		PickerModelMode:    m.picker.IsModelMode(),
		Streaming:          m.streaming,
		Queued:             len(m.store.StreamQueue) + len(m.store.BashQueue),
		BufferEmpty:        strings.TrimSpace(m.input.Value()) == "",
		ResumePickerActive: m.resumePicker.Active(),
		BashMode:           m.store.BashMode,
		HelpActive:         m.helpPanel.Active(),
		PanelOpen:          panelOpen,
		PanelKeepsHint:     panelKeepsHint,
		ExitConfirm:        m.exitConfirm,
		VimBadge:           vimkeys.Badge(m.vimEnabled(), m.vimMode()),
		Compact:            m.compactState(),

		Model:              m.model,
		StatusModel:        m.statusModel,
		AgentName:          m.agentName,
		Mode:               m.mode,
		EffortSuffix:       m.statusEffortSuffix(),
		TaskSuffix:         m.tasksStatusSuffix(),
		QueueMode:          m.store.QueueMode,
		AgentState:         m.agentState(),
		ContextUsedPercent: m.contextUsedPercent(),
	}
}

// compactState is the compaction the status row reports, or nil when no
// compaction is running.
//
// A compaction that has not ended yet has no End; the row's label then
// carries the elapsed time up to now.
func (m *runMode) compactState() *statusrow.Compact {
	if m.store.CompactStart.IsZero() {
		return nil
	}
	return &statusrow.Compact{
		Start:        m.store.CompactStart,
		End:          m.store.CompactEnd,
		TokensBefore: m.store.CompactTokensBefore,
		TokensAfter:  m.store.CompactTokensAfter,
		SpinnerFrame: m.store.CompactSpinnerFrame,
	}
}
