// interactive.go — the interactive /context panel.
//
// # What it is
//
// The reference (`agy`) answers `/context` with one view: the token
// breakdown, the auto-loaded files and the scroll legend, byte for byte
// what `/context` has always drawn. Keyboard navigation (↑/↓, pgup/pgdown,
// ctrl+home/ctrl+end, esc) scrolls it and mouse dragging selects its text.
//
// # What it is not
//
// The clone briefly gave this panel two more views — the request on the
// wire and the conversation's trajectory — behind a hand-rolled tab bar.
// They now live on /debug, which is already the full-viewport diagnostics
// surface and already draws tuilib's own tab bar; this panel went back to
// the single view the reference has, so it looks like every other panel
// in the tree again instead of the only one with a bar of its own.
//
// # Scrolling
//
// One arithmetic, not two. The render clips with MaxContextScroll's answer
// and the keys clamp against the same function, so the last row is
// reachable and the window never scrolls past the end. The old pair (a
// render that reserved one budget and a key handler that clamped against
// another) disagreed about how far the body could scroll, which is how a
// panel that looked scrollable could refuse to reach its own tail.
package contextpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// Seams are the host-provided facts the interactive panel needs.
//
// Every one of them is optional: a panel built without a seam renders the
// panel's empty shapes, which keeps the package usable from a test with no
// host.
type Seams struct {
	State  func() State
	Height func() int
	Width  func() int
	Close  func() tea.Cmd
}

// Panel is the interactive /context panel.
type Panel struct {
	seams Seams

	// scroll is the body's window offset.
	scroll int
}

// New returns a new interactive /context panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// RowCount returns the number of lines the document renders.
//
// The registry asks this for a list panel's cursor bound. The panel owns
// its own cursor, so the number is a courtesy to the router.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	return len(AllLines(p.viewState()))
}

// contextMaxRows is the body's own budget: the reference's layout reserves
// room for the prompt and the status line, as it always has.
func (p *Panel) contextMaxRows() int {
	h := 0
	if p.seams.Height != nil {
		h = p.seams.Height()
	}
	if h <= 0 {
		return 0
	}
	maxRows := h - 4
	if maxRows < 14 {
		maxRows = 14
	}
	return maxRows
}

// viewState is the State the render and the scroll bound both read: the
// host's facts plus the width it clips to and the height budget its window
// has. Reading it in both places is what keeps the two from disagreeing
// about how tall the head is.
func (p *Panel) viewState() State {
	st := State{}
	if p.seams.State != nil {
		st = p.seams.State()
	}
	if p.seams.Width != nil {
		st.Width = p.seams.Width()
	}
	st.MaxRows = p.contextMaxRows()
	st.ScrollOffset = p.scroll
	return st
}

// Lines returns the rendered lines.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	return Lines(p.viewState())
}

// HandleKey processes keyboard navigation for the panel.
//
// Every bound comes from MaxContextScroll, the same function the render
// clips with, so the offset can never be pushed past what is drawn.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	max := p.contextMaxScroll()
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		p.scroll = clampScroll(p.scroll-1, max)
		return true, nil
	case tuilib.NavDown:
		p.scroll = clampScroll(p.scroll+1, max)
		return true, nil
	case tuilib.NavPageUp:
		p.scroll = clampScroll(p.scroll-10, max)
		return true, nil
	case tuilib.NavPageDown:
		p.scroll = clampScroll(p.scroll+10, max)
		return true, nil
	case tuilib.NavTop:
		p.scroll = 0
		return true, nil
	case tuilib.NavBottom:
		p.scroll = max
		return true, nil
	case tuilib.NavCancel:
		if key.Code == tea.KeyEsc || key.String() == "esc" {
			if p.seams.Close != nil {
				return true, p.seams.Close()
			}
		}
		return true, nil
	}
	return false, nil
}

// contextMaxScroll is how far the body may scroll.
//
// It asks the same function that renders the body, so the two cannot
// disagree: the last row is reachable exactly when the render would clip
// it.
func (p *Panel) contextMaxScroll() int {
	return MaxContextScroll(p.viewState())
}

// clampScroll bounds v to [0, max].
func clampScroll(v, max int) int {
	if v < 0 {
		return 0
	}
	if v > max {
		return max
	}
	return v
}
