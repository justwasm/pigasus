// panel.go — the /rewind picker.
//
// # What it is
//
// The reference (`agy`) answers `/rewind` with a picker: one row per
// user-anchored turn, newest first, the most recent turn annotated
// `(current)`, and the cursor on the row the user picks. Enter rewinds
// the conversation to that turn.
//
// # Where it lives
//
// The panel itself is here: the cursor, the visible window, the rows
// and the keys. The host keeps only the seams — the conversation's
// steps, the terminal's height, the rewind itself and the way out of
// the panel.
//
// The picker's pure parts — the visible budget, the row rendering and
// the counter — live in rewindpanel.go, in the same package, because
// they are the picker's own vocabulary.
//
// The step list itself is the session layer's
// (internal/app/session/rewind): the panel names a step, and the
// session layer decides what a step owns.
//
// The panel is rebuilt every time it opens, so a stale cursor or a
// stale window is never shown.
package rewindpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/rewind"
	"github.com/justwasm/pigasus/palette"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves
// as if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Steps returns the conversation's steps, newest first.
	//
	// It is read on every draw, not cached: the session changes while
	// the panel is open, and the panel is still up at that moment.
	//
	// The steps themselves are the session layer's: the panel names a
	// step, it does not decide what a step owns.
	Steps func() []rewind.Step

	// Height returns the terminal's height — the budget the visible
	// window is sized from.
	Height func() int

	// Pick performs the rewind for the picked step and reports how many
	// messages it removed.
	//
	// The policy behind it is the session layer's
	// (internal/app/session/rewind); the host applies it to the live
	// session and writes the transcript's echo line.
	Pick func(index int) int

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /rewind picker.
//
// The panel owns the whole surface: the cursor, the visible window,
// the rows and the keys. Nothing of it is left in the host but the
// seams.
type Panel struct {
	seams Seams

	// cursor is the step the cursor sits on: 0 is the most recent turn.
	cursor int
	// window is the index of the first visible step.
	window int
}

// New builds the /rewind picker.
//
// The cursor starts on the most recent turn — the way the reference
// opens the picker on the current step.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// steps is the conversation's step list.
func (p *Panel) steps() []rewind.Step {
	if p == nil || p.seams.Steps == nil {
		return nil
	}
	return p.seams.Steps()
}

// height is the terminal's height.
func (p *Panel) height() int {
	if p == nil || p.seams.Height == nil {
		return 0
	}
	return p.seams.Height()
}

// Cursor is the step the cursor sits on.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the given step.
//
// The index is clamped into the step list, so a stale index cannot
// strand the cursor off the list.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	p.cursor = idx
	p.clamp()
}

// RowCount is the number of rows the picker offers.
//
// The empty state shows one row — the `(current)` line — so the count
// is never zero.
func (p *Panel) RowCount() int {
	if p == nil {
		return 1
	}
	if n := len(p.steps()); n > 0 {
		return n
	}
	return 1
}

// visible is the number of step rows the panel shows.
func (p *Panel) visible(total int) int {
	v := stepHeight(p.height())
	if v > total {
		v = total
	}
	if v < 0 {
		v = 0
	}
	return v
}

// clamp keeps the cursor and the window inside the step list.
//
// The window follows the cursor: a cursor that moves past the visible
// slice scrolls the slice, so the row the cursor is on is always the
// row the user sees.
func (p *Panel) clamp() {
	if p == nil {
		return
	}
	total := len(p.steps())
	if total == 0 {
		p.cursor = 0
		p.window = 0
		return
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor > total-1 {
		p.cursor = total - 1
	}
	visible := p.visible(total)
	if p.window < 0 {
		p.window = 0
	}
	if max := total - visible; p.window > max && max >= 0 {
		p.window = max
	}
	if p.cursor < p.window {
		p.window = p.cursor
	}
	if visible > 0 && p.cursor >= p.window+visible {
		p.window = p.cursor - visible + 1
	}
}

// moveCursor moves the cursor by delta steps.
func (p *Panel) moveCursor(delta int) {
	if p == nil {
		return
	}
	p.cursor += delta
	p.clamp()
}

// setCursor puts the cursor on an absolute step.
func (p *Panel) setCursor(idx int) {
	if p == nil {
		return
	}
	p.cursor = idx
	p.clamp()
}

// pick rewinds the conversation to the step the cursor sits on.
func (p *Panel) pick() {
	if p == nil || p.seams.Pick == nil {
		return
	}
	p.seams.Pick(p.cursor)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"

	// colorClose is the default-foreground close the panel's three
	// coloured spans end with.
	colorClose = "\x1b[39m"
)

// The scheme-dependent colours: the cursor row's blue, the "(current)"
// annotation's muted grey and the counter's dim grey.
var (
	blueOpen    = palette.BlueOpen
	gray238     = palette.SubtleOpen
	dimGrayOpen = palette.DimGrayOpen
)

func init() {
	palette.OnSchemeChange(refreshColours)
}

// refreshColours re-reads the palette after the selected colour scheme
// changed.
func refreshColours() {
	blueOpen = palette.BlueOpen
	gray238 = palette.SubtleOpen
	dimGrayOpen = palette.DimGrayOpen
}
