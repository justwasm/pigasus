package tui

// diff_model.go — the /diff sub-model.
//
// DiffModel owns the diff panel and, for a `--remote-agent` session, the
// turn diff the proxy returned. The panel itself lives in
// internal/ui/views/diffpanel and owns its tabs, folds and cursor; this
// model is the root's owner for it, so the panel is not a loose field on
// the slash-panel bucket.
//
// Upstream: google3/third_party/jetski/cli/model/model.DiffModel.

import (
	"github.com/justwasm/pigasus/internal/tui/panels"
	"github.com/justwasm/pigasus/internal/ui/views/diffpanel"
)

// DiffModel owns the diff panel and the remote turn-diff cache.
type DiffModel struct {
	panel  *diffpanel.Panel
	remote *remoteDiffCache
}

// Panel returns the diff panel, building it on first use.
func (d *DiffModel) Panel(host panels.DiffHost) *diffpanel.Panel {
	if d.panel == nil {
		d.panel = panels.NewDiffPanel(host)
	}
	return d.panel
}

// Reopen replaces the panel with a fresh one, discarding its tabs, folds
// and cursor. /diff opens on the turn's current state, not the state of a
// previous invocation.
func (d *DiffModel) Reopen(host panels.DiffHost) {
	d.panel = panels.NewDiffPanel(host)
}

// Remote returns the last remote turn diff, or nil.
func (d *DiffModel) Remote() *remoteDiffCache { return d.remote }

// SetRemote records the last remote turn diff.
func (d *DiffModel) SetRemote(cache *remoteDiffCache) { d.remote = cache }
