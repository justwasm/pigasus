// panel_keys.go — the /plugin panel's key handling.
//
// The panel owns every key it receives, so the host's generic list
// navigation never sees them: the search box takes the letters, and the
// arrow keys move the cursor.
//
// The panel's own navigation (↑/↓, ←/→, the tab switch, the shelf keys)
// is the reference's. The leftover list navigation — 'j'/'k' navigate,
// 'g'/'G' jump to the ends, 'q' closes the panel, pgup/pgdown page — is
// the clone's, and the panel answers it itself now that it owns the
// cursor.

package pluginsui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey owns every key the /plugin panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.localActive() {
		handled := p.handleLocalKey(key)
		return true, p.cmdForFocus(handled)
	}
	if p.search {
		handled := p.handleSearchKey(key)
		return true, p.cmdForFocus(handled)
	}
	switch key.Code {
	case tea.KeyTab:
		p.cycleTab()
		return true, nil
	case tea.KeyUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tea.KeyDown:
		if p.cursor < p.RowCount()-1 {
			p.cursor++
		}
		return true, nil
	}
	if p.tab == TabInstalled {
		return p.handleInstalledKey(key)
	}
	return p.handleDiscoverKey(key)
}

// cycleTab switches between the Installed and the Discover tab and parks
// the cursor on the first row.
func (p *Panel) cycleTab() {
	if p.tab == TabInstalled {
		p.tab = TabDiscover
	} else {
		p.tab = TabInstalled
	}
	p.cursor = 0
}

// handleInstalledKey owns the keys of the Installed tab.
//
// Space flips the plugin's enabled flag, Enter opens the detail block,
// '/' toggles the "enabled only" filter and ctrl+s uninstalls the
// highlighted plugin.
func (p *Panel) handleInstalledKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeySpace:
		p.toggleEnabled()
		return true, nil
	case key.Code == tea.KeyEnter:
		p.toggleExpanded()
		return true, nil
	case key.Code == '/':
		p.enabledOnly = !p.enabledOnly
		p.cursor = 0
		return true, nil
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		notice, _ := p.uninstallHighlighted()
		if notice != "" {
			p.localError = notice
		}
		return true, nil
	}
	if handled, cmd := p.handleLeftoverNavKey(key); handled {
		return true, cmd
	}
	return p.swallow(key), nil
}

// handleDiscoverKey owns the keys of the Discover tab.
//
// ←/→ jump to the first and the last row, space and a printable key
// focus the search box, Enter opens the local-install prompt on the
// first row and expands the detail block elsewhere, ctrl+e expands or
// collapses the shelf under the cursor and '[' / ']' jump between the
// shelves.
func (p *Panel) handleDiscoverKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyLeft:
		p.cursor = 0
		return true, nil
	case key.Code == tea.KeyRight:
		if n := p.RowCount(); n > 0 {
			p.cursor = n - 1
		}
		return true, nil
	case key.Code == tea.KeySpace:
		p.enterSearch()
		return true, Blink()
	case key.Code == tea.KeyEnter:
		if !p.search && p.cursor == 0 {
			p.openLocalInstall()
			return true, Blink()
		}
		p.toggleDiscoverExpanded()
		return true, nil
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		if !p.search && p.cursor == 0 {
			p.openLocalInstall()
			return true, Blink()
		}
	case key.Code == 'e' && key.Mod.Contains(tea.ModCtrl):
		p.toggleShelfExpanded()
		return true, nil
	case key.Code == '[':
		p.jumpShelf(-1)
		return true, nil
	case key.Code == ']':
		p.jumpShelf(1)
		return true, nil
	}
	if keybind.IsTypedText(key) {
		p.enterSearch()
		p.query = key.Text
		return true, Blink()
	}
	if handled, cmd := p.handleLeftoverNavKey(key); handled {
		return true, cmd
	}
	return p.swallow(key), nil
}

// handleLeftoverNavKey runs the list navigation the host used to run for
// the keys the panel does not own.
//
// The bound is the install tree's size, not the row list the tab draws:
// that is the bound the host's navigation used.
func (p *Panel) handleLeftoverNavKey(key tea.Key) (bool, tea.Cmd) {
	n := p.navCount()
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavUp:
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case tuilib.NavDown:
		if p.cursor < n-1 {
			p.cursor++
		}
		return true, nil
	case tuilib.NavPageUp:
		p.pageCursor(-10, n)
		return true, nil
	case tuilib.NavPageDown:
		p.pageCursor(+10, n)
		return true, nil
	case tuilib.NavTop:
		p.cursor = 0
		return true, nil
	case tuilib.NavBottom:
		if n > 0 {
			p.cursor = n - 1
		}
		return true, nil
	case tuilib.NavCancel:
		return true, p.close()
	}
	return false, nil
}

// pageCursor moves the cursor by n rows, clamped into the list.
func (p *Panel) pageCursor(n, count int) {
	if count <= 0 {
		return
	}
	c := p.cursor + n
	if c < 0 {
		c = 0
	} else if c >= count {
		c = count - 1
	}
	p.cursor = c
}

// swallow reports whether the key is one the panel eats without acting
// on it.
//
// The host's own panel key handling ended with the same rule: Enter, Tab
// and Backspace are swallowed, and so is every printable key, so a
// stray letter never reaches the composer while a panel is open. A
// modified key is *not* swallowed — Ctrl+C has to reach the host's
// global handlers.
func (p *Panel) swallow(key tea.Key) bool {
	switch key.Code {
	case tea.KeyEnter, tea.KeyTab, tea.KeyBackspace:
		return true
	}
	return key.Mod == 0 && key.Text != ""
}
