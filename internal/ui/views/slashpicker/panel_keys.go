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

// HandleKey handles a key while the popup is up.
//
// It reports whether the key was handled. A hidden popup handles
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
		if p.Selected() == nil {
			return false
		}
		return p.CommitSelectionToBuffer()
	}
	return false
}

// Cancel closes the popup. It reports whether the popup was open.
//
// The host's Esc handler asks the popup first: an open popup is the
// modal that owns the key.
func (p *Panel) Cancel() bool {
	if p == nil || !p.Visible() {
		return false
	}
	p.Reset()
	return true
}

// CommitSelectionToBuffer completes the highlighted row into the
// composer.
//
// In the command mode the composer takes the command's name; in the
// model mode it keeps the command and gains the display label
// (【一手】 agy 1.4.6, live: "/model oss" + Tab → "/model GPT-OSS
// 120B (Medium)", and the popup closes).
func (p *Panel) CommitSelectionToBuffer() bool {
	if p == nil {
		return false
	}
	sel := p.Selected()
	if sel == nil {
		return false
	}
	if p.modelMode {
		p.setBuffer("/model " + sel.Name)
		p.Reset()
		return true
	}
	p.setBuffer(sel.Name)
	p.Update([]rune(sel.Name))
	return true
}
