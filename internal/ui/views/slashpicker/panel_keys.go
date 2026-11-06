// panel_keys.go — the popup's keys.
//
// The popup owns the navigation arrows and Tab. Every other key is
// left to the host: the composer still has to receive the letters the
// user types, and the host's global handlers (the /help overlay, the
// panels) still have to see their own keys.
//
// The popup is not modal in the way the /help overlay is: it is a
// completion popup over a live composer, so the keys it does not own
// reach the composer.

package slashpicker

import (
	tea "charm.land/bubbletea/v2"
)

// UpdateKey handles a key while the popup is up in Elm single-direction
// message passing style. It reports whether the key was handled and returns an optional tea.Cmd.
func (p *Panel) UpdateKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	switch key.Code {
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
		if p.Selected() == nil {
			return false, nil
		}
		return p.CommitSelectionCmd()
	}
	return false, nil
}

// HandleKey handles a key while the popup is up.
//
// It reports whether the key was handled. A hidden popup handles none.
func (p *Panel) HandleKey(key tea.Key) bool {
	handled, _ := p.UpdateKey(key)
	return handled
}

// Cancel closes the popup. It reports whether the popup was open.
//
// The host's Esc handler asks the popup first: an open popup is the
// modal that owns the key.
func (p *Panel) Cancel() bool {
	handled, _ := p.CancelCmd()
	return handled
}

// CancelCmd closes the popup and returns a tea.Cmd emitting DismissedMsg.
func (p *Panel) CancelCmd() (bool, tea.Cmd) {
	if p == nil || !p.Visible() {
		return false, nil
	}
	p.Reset()
	return true, DismissCmd()
}

// CommitSelectionCmd completes the highlighted row into the composer and returns
// a tea.Cmd emitting CompletedMsg.
func (p *Panel) CommitSelectionCmd() (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	sel := p.Selected()
	if sel == nil {
		return false, nil
	}
	text := sel.Name
	if p.modelMode {
		text = "/model " + sel.Name
		p.setBuffer(text)
		p.Reset()
		return true, CompletedCmd(text, true, sel.Name)
	}
	p.setBuffer(text)
	p.Update([]rune(text))
	return true, CompletedCmd(text, false, sel.Name)
}

// CommitSelectionToBuffer completes the highlighted row into the
// composer.
//
// In the command mode the composer takes the command's name; in the
// model mode it keeps the command and gains the display label
// (【一手】 agy 1.4.6, live: "/model oss" + Tab → "/model GPT-OSS
// 120B (Medium)", and the popup closes).
func (p *Panel) CommitSelectionToBuffer() bool {
	handled, _ := p.CommitSelectionCmd()
	return handled
}
