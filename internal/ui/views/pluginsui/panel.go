// panel.go — the /plugin picker's interactive half.
//
// The package's other files hold the *decisions* (the row model, the
// filters, the shelf rules, the rendering primitives); this file and its
// companions hold the interaction: the panel's state, its rows, its keys
// and its writes.
//
// The host keeps only the seam it alone owns — the way out of the panel.
// Everything else — the two tabs, the search box, the shelves, the inline
// local-install prompt, the cursor, and the enable/disable and uninstall
// writes — is the panel's.

package pluginsui

import (
	tea "charm.land/bubbletea/v2"
)

// The two /plugin tabs.
//
// The reference's Installed tab lists the plugins the user has; the
// Discover tab lists the marketplace feed behind the "+ Install from
// local directory..." row.
const (
	TabInstalled = iota
	TabDiscover
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Close leaves the panel.
	//
	// The panel's Esc — and the 'q' the list navigation answers with —
	// close it, and the panel does not own the transcript block the
	// close writes, so the host's own closer is the seam.
	Close func() tea.Cmd
}

// Panel is the /plugin picker.
//
// The panel owns the whole surface: the two tabs, the search box, the
// Discover shelves, the inline local-install prompt and the cursor.
// Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// cursor is the selected row.
	cursor int
	// tab is the active tab (TabInstalled / TabDiscover).
	tab int
	// expanded holds the plugins whose detail block is open, keyed by
	// plugin name. The reference keys the block on the plugin, not on
	// the cursor, so it survives cursor movement.
	expanded map[string]bool
	// shelfExpanded holds the Discover shelves whose full list is shown,
	// keyed by shelf name. A collapsed shelf shows its first plugins
	// plus a "⋯ N more — enter to show" row.
	shelfExpanded map[string]bool
	// search is the search box's focus and query its text.
	search bool
	query  string
	// queryCommitted is the Installed tab's focused-mode commit flag:
	// while the flag is false the rows below stay unfiltered, and Enter
	// commits the query. The flag is reset whenever the box is left.
	queryCommitted bool
	// queryAsleep tracks the cursor-asleep state the reference shows
	// immediately after Enter commits on the Installed tab: the reverse
	// video block stays hidden until the user types again.
	queryAsleep bool
	// enabledOnly is the Installed tab's "enabled only" filter. It lives
	// in the panel's state, so closing the panel drops it — the next
	// open reads the install tree fresh.
	enabledOnly bool
	// local is the inline local-install prompt.
	local localInstall
	// localError records the last submit failure; the panel's title
	// shows it in red.
	localError string
	// blinkOn is the blink phase of the search / install cursor.
	blinkOn bool
}

// New builds the /plugin panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the index of the selected row.
//
// The cursor is the panel's own: the host's generic list navigation does
// not touch it, because the panel owns every key it receives.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index.
//
// The index is clamped into the list, so a stale index cannot strand the
// cursor on a row that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	if idx < 0 || (p.RowCount() > 0 && idx >= p.RowCount()) {
		idx = 0
	}
	p.cursor = idx
}

// Tab is the active tab (TabInstalled / TabDiscover).
func (p *Panel) Tab() int {
	if p == nil {
		return TabInstalled
	}
	return p.tab
}

// Query is the search box's text.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// SearchFocused reports whether the search box has the focus.
func (p *Panel) SearchFocused() bool {
	if p == nil {
		return false
	}
	return p.search
}

// EnabledOnly reports whether the Installed tab's "enabled only" filter
// is on.
//
// The filter is the panel's own state: the host rebuilds the panel every
// time it opens, so the filter never survives a close.
func (p *Panel) EnabledOnly() bool {
	if p == nil {
		return false
	}
	return p.enabledOnly
}

// LocalInstallActive reports whether the inline local-install prompt is
// open and owns every keypress.
func (p *Panel) LocalInstallActive() bool {
	if p == nil {
		return false
	}
	return p.local.active
}

// Blinking reports whether a blinking cursor is on screen — the search
// box's or the local-install prompt's.
//
// The host's tick handler flips the phase while this is true and stops
// arming the next tick when it is not.
func (p *Panel) Blinking() bool {
	if p == nil {
		return false
	}
	return p.search || p.local.active
}

// ToggleBlink flips the blink phase of the search / install cursor.
func (p *Panel) ToggleBlink() {
	if p == nil {
		return
	}
	p.blinkOn = !p.blinkOn
}

// SetBlink parks the blink phase.
func (p *Panel) SetBlink(on bool) {
	if p == nil {
		return
	}
	p.blinkOn = on
}

// ---- the seams ---------------------------------------------------------

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
