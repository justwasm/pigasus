// panel_keys.go — the /config panel's key handling.
//
// The panel owns every key it receives, so the generic list navigation
// never sees them: the search box takes the letters, and the arrow keys
// change the setting's value rather than moving a cursor.

package configpanel

import (
	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// HandleKey owns every key the /config panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.submenu {
		return p.handleSubmenuKey(key)
	}

	rows := p.Rows()

	switch key.Code {
	case tea.KeyLeft:
		return true, p.cycleValue(rows, -1)
	case tea.KeyRight:
		return true, p.cycleValue(rows, +1)
	case tea.KeyUp:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.MoveUp(1, len(rows), 0)
		p.cursor = nav.Cursor
		return true, nil
	case tea.KeyDown:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.MoveDown(1, len(rows), 0)
		p.cursor = nav.Cursor
		return true, nil
	case tea.KeyEnter:
		p.openSubmenu(rows)
		return true, nil
	case tea.KeyBackspace:
		p.backspaceQuery()
		return true, nil
	case tea.KeyEsc:
		if p.query != "" {
			p.clearQuery()
			return true, nil
		}
		return true, p.close()
	case tea.KeySpace:
		p.appendQuery(" ")
		return true, nil
	}

	// A printable key types into the search box. This is the reference's
	// own shape: /config is a search-first picker, so the letters never
	// reach the list navigation.
	if key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		p.appendQuery(key.Text)
		return true, nil
	}

	// The rest of the list navigation: page up / page down and the two
	// ends of the list. The host's generic navigation used to answer
	// these; the panel owns them now, because the cursor is the panel's.
	switch tuilib.ClassifyListNavKey(key) {
	case tuilib.NavPageUp:
		p.pageCursor(-10)
		return true, nil
	case tuilib.NavPageDown:
		p.pageCursor(+10)
		return true, nil
	case tuilib.NavTop:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.Home(len(rows), 0)
		p.cursor = nav.Cursor
		return true, nil
	case tuilib.NavBottom:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.End(len(rows), 0)
		p.cursor = nav.Cursor
		return true, nil
	}

	return false, nil
}

// handleSubmenuKey handles the keys while the inline option submenu is
// open on top of the settings list.
//
// The submenu owns every key: the list underneath must not see them.
func (p *Panel) handleSubmenuKey(key tea.Key) (bool, tea.Cmd) {
	rows := p.Rows()
	if p.cursor < 0 || p.cursor >= len(rows) {
		p.submenu = false
		return true, nil
	}
	row := rows[p.cursor]
	valCount := len(row.Values)
	if valCount <= 0 {
		p.submenu = false
		return true, nil
	}
	var cmd tea.Cmd
	switch key.Code {
	case tea.KeyUp:
		if p.submenuCursor > 0 {
			p.submenuCursor--
		} else {
			p.submenuCursor = valCount - 1
		}
		cmd = p.previewTheme(row, valCount)
		return true, cmd
	case tea.KeyDown:
		if p.submenuCursor < valCount-1 {
			p.submenuCursor++
		} else {
			p.submenuCursor = 0
		}
		cmd = p.previewTheme(row, valCount)
		return true, cmd
	case tea.KeyEnter:
		if p.submenuCursor >= 0 && p.submenuCursor < valCount {
			p.applyValue(row, row.Values[p.submenuCursor])
		}
		p.submenu = false
		if row.Name == "Color Scheme" {
			cmd = tea.ClearScreen
		}
		return true, cmd
	case tea.KeyEsc:
		if row.Name == "Color Scheme" && p.submenuOrigVal != "" {
			cmd = p.applyTheme(p.submenuOrigVal)
		}
		p.submenu = false
		return true, cmd
	}
	return true, nil
}

// openSubmenu opens the inline option selection submenu for the row
// under the cursor, seeding the cursor with the row's current value.
func (p *Panel) openSubmenu(rows []Row) {
	if len(rows) == 0 || p.cursor < 0 || p.cursor >= len(rows) || len(rows[p.cursor].Values) == 0 {
		return
	}
	row := rows[p.cursor]
	p.submenu = true
	currentVal := p.rawDisplayValue(row)
	p.submenuOrigVal = currentVal
	p.submenuCursor = 0
	for i, v := range row.Values {
		if v.Display == currentVal {
			p.submenuCursor = i
			break
		}
	}
}

// previewThemeForRow applies the live theme preview when the highlighted
// row is the Color Scheme setting; otherwise it returns a nil command.
func (p *Panel) previewTheme(row Row, valCount int) tea.Cmd {
	if row.Name != "Color Scheme" {
		return nil
	}
	if p.submenuCursor < 0 || p.submenuCursor >= valCount {
		return nil
	}
	return p.applyTheme(row.Values[p.submenuCursor].Display)
}

// ---- the search box ----------------------------------------------------

// backspaceQuery removes the last rune from the search query and resets
// the scroll position.
func (p *Panel) backspaceQuery() {
	if p.query == "" {
		return
	}
	r := []rune(p.query)
	p.query = string(r[:len(r)-1])
	p.cursor = 0
	p.window = 0
}

// clearQuery empties the search query and resets the scroll position.
func (p *Panel) clearQuery() {
	p.query = ""
	p.cursor = 0
	p.window = 0
}

// appendQuery appends text to the search query and resets the scroll
// position.
func (p *Panel) appendQuery(text string) {
	p.query += text
	p.cursor = 0
	p.window = 0
}

// pageCursor moves the cursor by delta rows, clamped to the list.
func (p *Panel) pageCursor(delta int) {
	count := len(p.Rows())
	nav := tuiviewport.ListNav{Cursor: p.cursor}
	if delta < 0 {
		nav.MoveUp(-delta, count, 0)
	} else {
		nav.MoveDown(delta, count, 0)
	}
	p.cursor = nav.Cursor
}
