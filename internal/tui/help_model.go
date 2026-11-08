package tui

// help_model.go — the /help sub-model.
//
// HelpModel owns the /help overlay. The panel lives in
// internal/ui/views/helppanel and owns its tabs, cursor, window and keys;
// this model is the root's owner for it, so the panel is not a loose field
// on the viewport bucket.
//
// Upstream: google3/third_party/jetski/cli/model/model.HelpModel.

import "github.com/justwasm/pigasus/internal/ui/views/helppanel"

// HelpModel owns the help overlay.
type HelpModel struct {
	*helppanel.Panel
}
