// panel_search.go — the /plugin panel's search box.
//
// The box is the panel's own: space (or any printable key) focuses it,
// its text filters the list live, and the cursor blinks at the end of
// the query.
//
// On the Installed tab the filter is *not* live: the rows below stay
// unfiltered until Enter commits the query, and the commit puts the
// cursor to sleep — the reference hides the reverse-video block until the
// user types again.

package pluginsui

import (
	tea "charm.land/bubbletea/v2"
)

// enterSearch focuses the search box.
//
// The query starts empty and the cursor is parked on the first result.
func (p *Panel) enterSearch() {
	p.search = true
	p.query = ""
	p.queryCommitted = false
	p.queryAsleep = false
	p.cursor = 0
	p.blinkOn = true
}

// exitSearch leaves the search box, clears the query and resets the
// cursor to the first row.
//
// An expanded detail block stays open — the reference keeps it.
func (p *Panel) exitSearch() {
	p.search = false
	p.query = ""
	p.queryCommitted = false
	p.queryAsleep = false
	p.cursor = 0
}

// handleSearchKey handles every key while the search box has focus.
//
// It reports whether it consumed the key. The caller swallows consumed
// keys and lets the keys it did not take fall through to the global
// bindings.
func (p *Panel) handleSearchKey(key tea.Key) bool {
	if key.Mod.Contains(tea.ModCtrl) && key.Code == 'u' {
		p.exitSearch()
		return true
	}
	if key.Code == tea.KeyEsc {
		p.exitSearch()
		return true
	}
	if key.Code == tea.KeyEnter {
		return p.handleSearchEnter()
	}
	if p.handleSearchNavKey(key.Code) {
		return true
	}
	return p.handleSearchEditKey(key)
}

// handleSearchEnter answers Enter while the box has focus.
//
// On the Installed tab an un-committed query commits on Enter; a
// committed query expands the detail block like the Discover tab does.
// The Discover tab's filter is always live, so there is nothing to
// commit there.
//
// Pressing Enter also puts the cursor to sleep — the reference hides the
// reverse-video block until the user types again.
func (p *Panel) handleSearchEnter() bool {
	if p.tab == TabInstalled && !p.queryCommitted {
		p.queryCommitted = true
		p.queryAsleep = true
		p.cursor = 0
		return true
	}
	if p.tab == TabInstalled {
		// A second Enter (already committed) re-awakens the cursor.
		p.queryAsleep = false
		p.toggleExpanded()
		return true
	}
	p.toggleDiscoverExpanded()
	return true
}

// handleSearchNavKey answers the navigation keys while the box has focus.
//
// Tab keeps the box focused while cycling Installed↔Discover and carries
// the typed query across; ←/→ jump to the first and the last filtered
// row.
func (p *Panel) handleSearchNavKey(code rune) bool {
	switch code {
	case tea.KeyTab:
		p.cycleTab()
		return true
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true
	case tea.KeyDown:
		if p.cursor < p.searchCount()-1 {
			p.cursor++
		}
		return true
	case tea.KeyLeft:
		p.cursor = 0
		return true
	case tea.KeyRight:
		if n := p.searchCount(); n > 0 {
			p.cursor = n - 1
		}
		return true
	default:
		return false
	}
}

// handleSearchEditKey answers the editing keys while the box has focus.
//
// A printable key appends to the query; backspace drops the last rune
// and, on an empty query, leaves search mode.
func (p *Panel) handleSearchEditKey(key tea.Key) bool {
	switch key.Code {
	case tea.KeyBackspace:
		if p.query == "" {
			p.exitSearch()
			return true
		}
		r := []rune(p.query)
		p.query = string(r[:len(r)-1])
		p.queryAsleep = false
		if p.tab == TabDiscover {
			p.cursor = 0
		}
		return true
	case tea.KeySpace:
		if p.tab == TabInstalled && p.queryCommitted {
			p.toggleEnabled()
			return true
		}
		p.query += " "
		p.queryAsleep = false
		if p.tab == TabDiscover {
			p.cursor = 0
		}
		return true
	}
	if key.Mod == 0 {
		if key.Text != "" {
			p.query += key.Text
			p.queryAsleep = false
			if p.tab == TabDiscover {
				p.cursor = 0
			}
		}
		return true
	}
	return false
}
