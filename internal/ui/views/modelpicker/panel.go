// panel.go — the /model picker's interactive half.
//
// The package's other files hold the *decisions* (the row model, the
// search's subsequence match and relevance order, the catalogue-id
// mapping, the effort slider's geometry); this file and its companions
// hold the interaction: the panel's state, its rendering, its keys and
// its writes.
//
// The host keeps only the seams: the label of the model the session
// runs, the way a chosen model reaches the session, and the way out of
// the panel. Everything else — the search box, the cursor, the effort
// slider's position, the settings.json write — is the panel's.

package modelpicker

import (
	tea "charm.land/bubbletea/v2"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// CurrentModelLabel is the display label of the model the session
	// runs right now.
	//
	// The `(current)` annotation and the slider's seeding read it: the
	// reference marks the running model and parks the slider on its own
	// tier, even when the persisted settings name a model the account
	// cannot use.
	CurrentModelLabel func() string
	// ApplyModel installs the chosen model into the session.
	//
	// The panel persists the catalogue slug to settings.json; the
	// session's own model, its status row and its transport are the
	// host's, so the panel hands the display label over.
	ApplyModel func(label string)
	// Close leaves the panel.
	Close func() tea.Cmd
	// CustomRows, when non-nil, overrides the static catalogue with
	// dynamic models from the active agent engine.
	CustomRows func() []Row
}

// Panel is the /model picker.
//
// The panel owns the whole surface: the search box, the row list and the
// effort slider. Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// query is the search box's text.
	query string
	// cursor is the selected row.
	cursor int
	// effortCursor is the effort slider's position: an index into the
	// cursor row's *own* tier list, so a row with three tiers and a row
	// with five never share a number.
	effortCursor int
	// window is the index of the first visible row in Rows().
	window int
}

// New builds the /model panel.
//
// The effort slider is seeded here, the way the reference seeds it when
// the picker opens: it parks on the tier the session's model runs at.
func New(seams Seams) *Panel {
	p := &Panel{seams: seams}
	p.seedEffort()
	return p
}

// Window returns the index of the first visible row.
func (p *Panel) Window() int {
	if p == nil {
		return 0
	}
	return p.window
}

// adjustWindow scrolls the visible window so the cursor stays on screen.
func (p *Panel) adjustWindow(cursor, visible int) {
	if visible <= 0 {
		return
	}
	if cursor < p.window {
		p.window = cursor
	}
	if cursor >= p.window+visible {
		p.window = cursor - visible + 1
	}
	if p.window < 0 {
		p.window = 0
	}
	rows := p.Rows()
	if p.window+visible > len(rows) && len(rows) >= visible {
		p.window = len(rows) - visible
	}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the index of the selected row in Rows().
//
// The cursor is the panel's own: the host's generic list navigation does
// not touch it, because the panel owns every key it receives.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index of Rows().
//
// The index is clamped into the list, so a stale index cannot strand the
// cursor on a row that is not on screen. The effort slider follows the
// cursor the way it follows a ↑/↓: a row that offers tiers parks the
// slider on its own tier, a row that does not leaves the position alone.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	rows := p.Rows()
	if idx < 0 || (len(rows) > 0 && idx >= len(rows)) {
		idx = 0
	}
	p.setCursor(idx)
}

// Query is the search box's text.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// EffortCursor is the effort slider's position.
func (p *Panel) EffortCursor() int {
	if p == nil {
		return 0
	}
	return p.effortCursor
}

// ---- the seams ---------------------------------------------------------

// currentModelLabel is the label of the model the session runs.
func (p *Panel) currentModelLabel() string {
	if p == nil || p.seams.CurrentModelLabel == nil {
		return ""
	}
	return p.seams.CurrentModelLabel()
}

// applyModel hands the chosen model to the session.
func (p *Panel) applyModel(label string) {
	if p == nil || p.seams.ApplyModel == nil {
		return
	}
	p.seams.ApplyModel(label)
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
