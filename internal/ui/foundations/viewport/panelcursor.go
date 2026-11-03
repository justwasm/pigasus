// panelcursor.go — the cursor of a vertical list panel.
//
// The panel key router moves it: ↑/↓ move one row, a page key moves ten
// rows, and the home/end keys park the cursor on the first or the last
// row. The arithmetic is ListNav's — the same one the other list panels
// use (internal/ui/views/modelpicker, internal/ui/views/taskspanel,
// internal/ui/views/configpanel).

package viewport

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// PanelPageRows is how many rows a page key moves a panel cursor.
//
// It is the clone's own page size: ten rows.
const PanelPageRows = 10

// PanelCursor moves a vertical list panel's cursor.
//
// The rules are the ones the other list panels use: ↑/↓ move one row, a
// page key moves ten rows, and the home/end keys park the cursor on the
// first or the last row. The cursor is clamped into the panel's rows, so
// a list that shrank cannot strand the cursor past its last row.
//
// An empty list has no row to move to, so the cursor stays where it is.
func PanelCursor(cursor int, action tuilib.NavAction, total int) int {
	if total <= 0 {
		return cursor
	}
	nav := ListNav{Cursor: cursor}
	switch action {
	case tuilib.NavUp:
		nav.MoveUp(1, total, 0)
	case tuilib.NavDown:
		nav.MoveDown(1, total, 0)
	case tuilib.NavPageUp:
		nav.PageUp(total, 0, PanelPageRows)
	case tuilib.NavPageDown:
		nav.PageDown(total, 0, PanelPageRows)
	case tuilib.NavTop:
		nav.Home(total, 0)
	case tuilib.NavBottom:
		nav.End(total, 0)
	default:
		return cursor
	}
	return nav.Cursor
}
