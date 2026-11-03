// panel_keys.go — the dropdown's keys.
//
// The dropdown owns the navigation arrows and Tab. Every other key is
// left to the host: the composer still has to receive the letters the
// user types.
//
// The dropdown is not modal in the way the /help overlay is: it is a
// completion popup over a live composer, so the keys it does not own
// reach the composer.
//
// Enter is routed by the host rather than claimed here: the host's
// Enter path carries the newline, the queue-mode and the submit
// branches, and the dropdown's Enter has to be swallowed in the middle
// of them. The host asks the panel to complete and keeps the history
// reset — the composer's text changed under the history cursor.

package mentionpicker

import (
	tea "charm.land/bubbletea/v2"
)

// HandleKey handles a key while the dropdown is up.
//
// It reports whether the key was handled. A hidden dropdown handles
// none.
func (p *Panel) HandleKey(key tea.Key) bool {
	if p == nil {
		return false
	}
	switch key.Code {
	case tea.KeyUp:
		if !p.Visible() {
			return false
		}
		p.MoveUp()
		return true
	case tea.KeyDown:
		if !p.Visible() {
			return false
		}
		p.MoveDown()
		return true
	case tea.KeyTab:
		if key.Mod.Contains(tea.ModShift) {
			return false
		}
		if !p.Visible() {
			return false
		}
		return p.Complete(true)
	}
	return false
}
