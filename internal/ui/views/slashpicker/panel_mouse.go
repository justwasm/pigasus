// panel_mouse.go — the popup's mouse handling.
//
// 【一手】 agy 1.2.13, live: a click on the composer reopens the popup,
// a click on a row completes that row, a click outside dismisses the
// popup, a hover moves the highlight and a wheel over the popup
// scrolls it.

package slashpicker

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleClick handles a mouse click over the popup.
//
// It reports whether the click was consumed by the popup.
func (p *Panel) HandleClick(ev tea.Mouse) bool {
	if p == nil || !p.altScreen() {
		return false
	}
	x, y := ev.X, ev.Y

	if p.isInputY(y) {
		// The reference reopens the popup when the composer is
		// clicked and the buffer still names a command.
		p.Update([]rune(tuilib.FirstLine(p.buffer())))
		return false
	}

	if rowOffset, ok := p.geom.ListRowOffset(x, y, p.width()); ok {
		idx := p.layout.ListTop + rowOffset
		if !p.SetIdx(idx) {
			return false
		}
		p.CommitSelectionToBuffer()
		return true
	}

	return false
}

// HandleHover handles a mouse motion over the popup: the row under
// the pointer becomes the highlighted one.
//
// It reports whether the highlight moved.
func (p *Panel) HandleHover(ev tea.Mouse) bool {
	if p == nil || !p.altScreen() {
		return false
	}
	if rowOffset, ok := p.geom.ListRowOffset(ev.X, ev.Y, p.width()); ok {
		idx := p.layout.ListTop + rowOffset
		return p.SetIdx(idx)
	}
	return false
}

// HandleWheel handles a wheel event over the popup: the popup scrolls
// instead of the transcript.
//
// It reports whether the event was consumed by the popup.
func (p *Panel) HandleWheel(ev tea.Mouse) bool {
	if p == nil || !p.altScreen() || !p.geom.ContainsWheel(ev.Y) {
		return false
	}
	switch ev.Button {
	case tea.MouseWheelUp:
		return p.MoveUp()
	case tea.MouseWheelDown:
		return p.MoveDown()
	}
	return false
}
