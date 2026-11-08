package tui

// active_items_model.go — the active-items sub-model.
//
// ActiveItemsModel owns the inline block that lists the session's running
// and pending tasks and subagents under the prompt box. The block itself
// lives in internal/ui/views/activeitemspanel and owns its cursor, window,
// rows and keys; this model is the root's owner for it, so the panel is not
// a loose field on the viewport bucket.
//
// Upstream: google3/third_party/jetski/cli/model/model.ActiveItemsModel.

import "github.com/justwasm/pigasus/internal/ui/views/activeitemspanel"

// ActiveItemsModel owns the active-items panel. The embedded panel is the
// block; the model is the ownership boundary.
type ActiveItemsModel struct {
	*activeitemspanel.Panel
}
