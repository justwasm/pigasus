// panel_host.go — the panel's side of the host seam.
//
// The panel reads the host's facts through the seams and writes its
// rectangle back through the two calls the host makes around the
// frame: SetBodyStart before the popup's rows are written, ClipBody
// after the body was trimmed.

package slashpicker

// SetBodyStart records the screen row the overlay starts on.
//
// The host calls it right before it writes the popup's rows: the
// popup's rectangle is relative to that row.
func (p *Panel) SetBodyStart(row int) {
	if p == nil {
		return
	}
	p.bodyStart = row
}

// ClipBody recomputes the popup's on-screen rectangle after the host
// trimmed the body. drop is the number of rows the host removed from
// the top.
//
// A popup that rendered no row has no rectangle: the mouse handlers
// then answer false for every coordinate.
func (p *Panel) ClipBody(drop int) {
	if p == nil {
		return
	}
	if p.layout.Rows <= 0 || !p.visible {
		p.geom = geom{}
		return
	}
	start := p.bodyStart - drop
	p.geom = geom{
		Valid:       true,
		HasList:     p.layout.HasList,
		TopY:        start,
		BottomY:     start + p.layout.Rows - 1,
		ListTopY:    start + p.layout.ListTop,
		ListBottomY: start + p.layout.ListBottom,
		HintY:       start + p.layout.HintRow,
	}
}

// ResetGeom clears the popup's rectangle.
func (p *Panel) ResetGeom() {
	if p == nil {
		return
	}
	p.geom = geom{}
}

// altScreen reports whether the session runs in the alternate screen.
func (p *Panel) altScreen() bool {
	return p != nil && p.seams.AltScreen != nil && p.seams.AltScreen()
}

// width returns the terminal's width in columns.
func (p *Panel) width() int {
	if p == nil || p.seams.Width == nil {
		return 0
	}
	return p.seams.Width()
}

// isInputY reports whether a screen row belongs to the composer.
func (p *Panel) isInputY(y int) bool {
	return p != nil && p.seams.IsInputY != nil && p.seams.IsInputY(y)
}

// buffer returns the composer's current text.
func (p *Panel) buffer() string {
	if p == nil || p.seams.Buffer == nil {
		return ""
	}
	return p.seams.Buffer()
}

// setBuffer replaces the composer's text.
func (p *Panel) setBuffer(s string) {
	if p == nil || p.seams.SetBuffer == nil {
		return
	}
	p.seams.SetBuffer(s)
}
