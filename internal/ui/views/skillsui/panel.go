// panel.go — the /skills picker's interactive half.
//
// The package's other files hold the *decisions* (the catalogue model,
// the row renderer, the detail document and its markdown and table
// renderers); this file and its companions hold the interaction: the
// panel's state, its rows and its keys.
//
// The host keeps only the seams it alone owns — the catalogue the panel
// reads, whether the terminal can carry OSC 8 hyperlinks, the terminal's
// size and the way out of the panel. Everything else — the grouped list,
// the cursor, the per-skill detail view and its scroll window — is the
// panel's.

package skillsui

import (
	tea "charm.land/bubbletea/v2"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Catalog returns the live skill catalogue.
	//
	// The catalogue is read fresh on every draw, so a skill installed
	// while the panel is open shows up without the panel being rebuilt.
	Catalog func() []SkillInfo
	// OSC8 reports whether the terminal can carry OSC 8 hyperlinks.
	//
	// The marketplace link under a skill row is only rendered when the
	// terminal can carry it.
	OSC8 func() bool
	// Size returns the terminal's width and height in cells.
	//
	// A zero width means no size message has arrived yet: the panel
	// falls back to the 220×30 capture the reference was measured at.
	Size func() (width, height int)
	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /skills picker.
//
// The panel owns the whole surface: the grouped list, the cursor, the
// per-skill detail view and its scroll window. Nothing of it is left in
// the host but the seams.
type Panel struct {
	seams Seams
	// cursor is the selected skill's index in the catalogue.
	//
	// The reference opens the picker with nothing selected — it shows
	// no "› " cursor until the first ↑/↓ — so the cursor starts at -1.
	cursor int
	// detail says the per-skill detail view is open.
	detail bool
	// window is the detail view's scroll offset, in logical document
	// lines.
	window int
}

// New builds the /skills panel.
//
// The picker opens with nothing selected, so the cursor starts at -1.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, cursor: -1}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the index of the selected skill in the catalogue.
//
// The cursor is the panel's own: the host's generic list navigation does
// not touch it, because the panel owns every key it receives.
func (p *Panel) Cursor() int {
	if p == nil {
		return -1
	}
	return p.cursor
}

// SetCursor puts the cursor on the skill at the given index.
//
// The index is clamped into the catalogue, so a stale index cannot strand
// the cursor on a skill that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	n := p.RowCount()
	if idx < -1 || (n > 0 && idx >= n) {
		idx = -1
	}
	p.cursor = idx
}

// Selected is the catalogue entry the cursor is on.
//
// It is the zero value when nothing is selected — the picker opens with
// no row under the cursor.
func (p *Panel) Selected() SkillInfo {
	if p == nil {
		return SkillInfo{}
	}
	return p.selected()
}

// Detail reports whether the per-skill detail view is open.
func (p *Panel) Detail() bool {
	if p == nil {
		return false
	}
	return p.detail
}

// RowCount is the number of skills the catalogue holds.
//
// The count is the *grouped* catalogue's size: the cursor indexes the
// groups' skills in the order the list renders them.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	return len(PanelCatalog(p.catalog()))
}

// ---- the seams ---------------------------------------------------------

// catalog is the live skill catalogue.
func (p *Panel) catalog() []SkillInfo {
	if p == nil || p.seams.Catalog == nil {
		return nil
	}
	return p.seams.Catalog()
}

// osc8 reports whether the terminal can carry OSC 8 hyperlinks.
func (p *Panel) osc8() bool {
	if p == nil || p.seams.OSC8 == nil {
		return false
	}
	return p.seams.OSC8()
}

// size is the terminal's width and height in cells.
func (p *Panel) size() (int, int) {
	if p == nil || p.seams.Size == nil {
		return 0, 0
	}
	return p.seams.Size()
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
