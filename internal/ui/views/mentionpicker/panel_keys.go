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
	handled, _ := p.HandleKeyMsg(key)
	return handled
}

// HandleKeyMsg handles a key while the dropdown is up and returns Elm messages.
func (p *Panel) HandleKeyMsg(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
	case tea.KeyEsc:
		if !p.Visible() {
			return false, nil
		}
		p.Reset()
		return true, func() tea.Msg { return MsgDismissed{} }
	case tea.KeyUp:
		if !p.Visible() {
			return false, nil
		}
		p.MoveUp()
		return true, nil
	case tea.KeyDown:
		if !p.Visible() {
			return false, nil
		}
		p.MoveDown()
		return true, nil
	case tea.KeyTab:
		if key.Mod.Contains(tea.ModShift) {
			return false, nil
		}
		if !p.Visible() {
			return false, nil
		}
		selected := p.selectedEntry().Rel
		if p.Complete(true) {
			buf := p.buffer()
			return true, func() tea.Msg {
				return MsgCompleted{NewBuffer: buf, RelPath: selected}
			}
		}
		return true, nil
	}
	return false, nil
}
