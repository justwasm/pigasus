// Package viewerpanel holds the viewport-backed viewer panels: /usage,
// /changelog and /feature-diff.
//
// # What it is
//
// The reference (`agy`) shows three scrollable documentary panels. Each
// one is a document with a window over it, a counter that names the
// window's first and last line, and a footer that names the keys.
//
// The three panels differ in their document and in their chrome; they
// share the scroll machinery, and that is what this package holds.
//
// # Where it lives
//
// The panel itself is here: the viewport, the pager keybindings and the
// scroll arithmetic. The host keeps only the seams — the document, the
// terminal width, the page size and the way out of the panel.
//
// # The offset
//
// The scroll offset is the host's: the host's panel struct carries one
// window offset that several panels share (/codesearch and the /tasks
// detail views scroll through it too), so the panel takes the offset as
// an argument and hands the clamped one back.
package viewerpanel

import (
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// DefaultWidth is the viewport width the panel falls back to when the
// terminal's width is unknown.
//
// The viewer panels render full-terminal-width lines, so the width only
// has to be wide enough not to clip them. The host names the same
// number when its own terminal size is unknown, so the fallback has one
// owner.
const DefaultWidth = 200

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Spec returns the document the panel scrolls: its lines, how many
	// of them are visible at once, and the document's own line count.
	//
	// The second value is false when the panel has no document at all.
	Spec func() (viewer.Spec, bool)

	// Width returns the viewport's width.
	Width func() int

	// PageRows returns how many rows a page key scrolls.
	//
	// The default is the window's own height, which is what a pager
	// does. The reference's /changelog scrolls one row less than the
	// window on the test path, so the host names the size itself.
	PageRows func(spec viewer.Spec) int

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the viewer panel.
//
// The panel owns the viewport and the keys; the scroll offset stays the
// host's, so every method that scrolls takes it and returns the clamped
// one.
type Panel struct {
	seams Seams

	// vp is the viewport: the document, the window and the scroll
	// position.
	vp viewport.Model
}

// New builds the viewer panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Open loads the document into the viewport.
//
// The panel is opened when the host opens the viewer; the document is
// read again on every key (see Sync), so a document that changes while
// the panel is up — the /usage countdown — stays current.
func (p *Panel) Open() {
	if p == nil {
		return
	}
	spec, ok := p.spec()
	if !ok {
		return
	}
	p.vp = viewport.New(
		viewport.WithWidth(p.width()),
		viewport.WithHeight(spec.Visible),
	)
	p.vp.SetContentLines(spec.Lines)
	// The mouse wheel is the TUI's own: the panel does not scroll on a
	// wheel event.
	p.vp.MouseWheelEnabled = false
}

// Sync refreshes the viewport's content and dimensions to match the
// current document, and pins the viewport's offset to the given one.
//
// It returns the clamped offset: a document that shrank leaves the
// window nearer its end than the offset asked for.
func (p *Panel) Sync(offset int) int {
	if p == nil {
		return offset
	}
	spec, ok := p.spec()
	if !ok {
		return offset
	}
	p.vp.SetHeight(spec.Visible)
	p.vp.SetWidth(p.width())
	p.vp.SetContentLines(spec.Lines)
	if p.vp.YOffset() != offset {
		p.vp.SetYOffset(offset)
	}
	return p.vp.YOffset()
}

// HandleKey routes one key to the panel.
//
// The returned bool says whether the panel owned the key; the returned
// command is the host's (the panel itself has none); the returned offset
// is the scroll offset after the key.
//
// The keys are the pager's: j/k and the arrows scroll a line, d/u scroll
// half a page, space/b and pgup/pgdown scroll a page, g/G and
// ctrl+home/ctrl+end jump to the ends, and q/esc leave the panel. Every
// other key is handed to the viewport itself, which answers the
// horizontal scroll; a plain key is swallowed by the panel, a modified
// one falls through to the host.
func (p *Panel) HandleKey(key tea.Key, offset int) (bool, tea.Cmd, int) {
	if p == nil {
		return false, nil, offset
	}
	// Refresh the document and the window first: the document may have
	// changed since the panel opened.
	offset = p.Sync(offset)

	if handled, cmd, ok := p.nav(tuilib.ClassifyViewerNavKey(key)); ok {
		return handled, cmd, p.vp.YOffset()
	}

	// Delegate the rest to the viewport (e.g. the horizontal scroll).
	updated, cmd := p.vp.Update(tea.KeyPressMsg(key))
	p.vp = updated

	if key.Mod == 0 {
		return true, cmd, p.vp.YOffset()
	}
	return false, cmd, p.vp.YOffset()
}

// nav applies one navigation action to the viewport.
//
// The third return value says whether the action was a navigation
// action at all: an action the panel does not own is left to the
// viewport.
func (p *Panel) nav(action tuilib.NavAction) (bool, tea.Cmd, bool) {
	switch action {
	case tuilib.NavTop:
		p.vp.GotoTop()
	case tuilib.NavBottom:
		p.vp.GotoBottom()
	case tuilib.NavCancel:
		return true, p.close(), true
	case tuilib.NavPageDown:
		p.pageDown()
	case tuilib.NavPageUp:
		p.pageUp()
	case tuilib.NavHalfPageDown:
		p.vp.HalfPageDown()
	case tuilib.NavHalfPageUp:
		p.vp.HalfPageUp()
	case tuilib.NavDown:
		p.vp.ScrollDown(1)
	case tuilib.NavUp:
		p.vp.ScrollUp(1)
	default:
		return false, nil, false
	}
	return true, nil, true
}

// pageDown scrolls one page down.
func (p *Panel) pageDown() {
	spec, ok := p.spec()
	if !ok {
		p.vp.PageDown()
		return
	}
	p.vp.ScrollDown(p.pageRows(spec))
}

// pageUp scrolls one page up.
func (p *Panel) pageUp() {
	spec, ok := p.spec()
	if !ok {
		p.vp.PageUp()
		return
	}
	p.vp.ScrollUp(p.pageRows(spec))
}

// spec returns the document the panel scrolls.
func (p *Panel) spec() (viewer.Spec, bool) {
	if p == nil || p.seams.Spec == nil {
		return viewer.Spec{}, false
	}
	return p.seams.Spec()
}

// width returns the viewport's width.
func (p *Panel) width() int {
	if p == nil || p.seams.Width == nil {
		return DefaultWidth
	}
	return p.seams.Width()
}

// pageRows returns how many rows a page key scrolls.
func (p *Panel) pageRows(spec viewer.Spec) int {
	if p == nil || p.seams.PageRows == nil {
		return spec.Visible
	}
	return p.seams.PageRows(spec)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
