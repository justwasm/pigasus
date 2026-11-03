// panel_keys.go — the /model panel's key handling.
//
// The panel owns every key it receives, so the generic list navigation
// never sees them: the search box takes the letters, and the arrow keys
// move the effort slider rather than a cursor.

package modelpicker

import (
	tea "charm.land/bubbletea/v2"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// HandleKey owns every key the /model panel receives.
//
// The reference's picker is a *search field* first: every printable
// character goes into "Search:" and filters the rows, and only the arrow
// keys navigate. Verified live against agy 1.4.5 — typing "j" filters
// (it does not move the cursor down) and the panel answers
// `No models match "j"`. So this handler does not use
// tuilib.ClassifyListNavKey, which would read "j"/"k"/"h"/"l"/"g"/"q" as
// navigation.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	switch key.Code {
	case tea.KeyEsc:
		// A live query makes Esc clear it and keep the panel open
		// ("esc Clear search"); an empty box closes the panel.
		if p.query != "" {
			p.query = ""
			p.setCursor(0)
			return true, nil
		}
		return true, p.close()
	case tea.KeyEnter:
		p.selectModel()
		return true, p.close()
	case tea.KeyBackspace:
		if p.query != "" {
			r := []rune(p.query)
			p.query = string(r[:len(r)-1])
			p.setCursor(0)
		}
		return true, nil
	case tea.KeyUp:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.MoveUp(1, p.RowCount(), 0)
		p.setCursor(nav.Cursor)
		return true, nil
	case tea.KeyDown:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.MoveDown(1, p.RowCount(), 0)
		p.setCursor(nav.Cursor)
		return true, nil
	case tea.KeyPgUp:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.PageUp(p.RowCount(), 0, 10)
		p.setCursor(nav.Cursor)
		return true, nil
	case tea.KeyPgDown:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.PageDown(p.RowCount(), 0, 10)
		p.setCursor(nav.Cursor)
		return true, nil
	case tea.KeyLeft:
		p.cycleEffort(-1)
		return true, nil
	case tea.KeyRight:
		p.cycleEffort(+1)
		return true, nil
	case tea.KeyHome:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.Home(p.RowCount(), 0)
		p.setCursor(nav.Cursor)
		return true, nil
	case tea.KeyEnd:
		nav := tuiviewport.ListNav{Cursor: p.cursor}
		nav.End(p.RowCount(), 0)
		p.setCursor(nav.Cursor)
		return true, nil
	}

	if key.Mod == 0 && key.Text != "" && key.Text != "\t" && key.Text != "\n" && key.Text != "\r" {
		p.query += key.Text
		p.setCursor(0)
		return true, nil
	}
	return false, nil
}
