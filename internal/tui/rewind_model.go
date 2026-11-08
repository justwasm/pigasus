package tui

// rewind_model.go — the /rewind sub-model.
//
// RewindModel owns the rewind panel. The panel itself lives in
// internal/ui/views/rewindpanel and owns its cursor, preview and staged
// choice; this model is the root's owner for it, so the panel is not a
// loose field on the slash-panel bucket.
//
// Upstream: google3/third_party/jetski/cli/model/model.RewindModel.

import (
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/views/rewindpanel"
)

// RewindModel owns the rewind panel.
type RewindModel struct {
	panel *rewindpanel.Panel
}

// Panel returns the rewind panel, building it on first use.
func (r *RewindModel) Panel(host panels.RewindHost) *rewindpanel.Panel {
	if r.panel == nil {
		r.panel = panels.NewRewindPanel(host)
	}
	return r.panel
}

// Reopen replaces the panel with a fresh one, so /rewind always starts from
// the conversation's current checkpoints.
func (r *RewindModel) Reopen(host panels.RewindHost) {
	r.panel = panels.NewRewindPanel(host)
}
