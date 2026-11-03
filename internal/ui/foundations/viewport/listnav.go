package viewport

import (
	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// ListNav coordinates cursor selection and viewport windowing for a list of items.
//
// Invariants guaranteed by all methods:
// 1. If total <= 0: Cursor == 0, Window == 0.
// 2. If total > 0: 0 <= Cursor < total.
// 3. If visible > 0: Window <= Cursor < Window + visible (cursor is kept on screen).
// 4. 0 <= Window <= max(0, total - visible).
type ListNav struct {
	Cursor int
	Window int
}

// Sync re-clamps Cursor and Window against total and visible elements,
// ensuring the cursor is strictly on screen and within bounds.
// It returns the visible Window.
func (n *ListNav) Sync(total, visible int) Window {
	if total <= 0 {
		n.Cursor = 0
		n.Window = 0
		return Window{total: max(0, total)}
	}
	n.Cursor = tuilib.ClampInt(n.Cursor, 0, total-1)
	if visible <= 0 || total <= visible {
		n.Window = 0
		return NewWindow(total, 0, visible)
	}
	// Keep window around cursor
	if n.Cursor < n.Window {
		n.Window = n.Cursor
	} else if n.Cursor >= n.Window+visible {
		n.Window = n.Cursor - visible + 1
	}
	maxWindow := max(0, total-visible)
	n.Window = tuilib.ClampInt(n.Window, 0, maxWindow)
	return NewWindow(total, n.Window, visible)
}

// SetCursor moves the cursor to idx and updates Window to keep Cursor visible.
func (n *ListNav) SetCursor(idx, total, visible int) Window {
	n.Cursor = idx
	return n.Sync(total, visible)
}

// MoveUp moves the cursor up by delta rows (defaults to 1 if delta <= 0).
func (n *ListNav) MoveUp(delta, total, visible int) Window {
	step := 1
	if delta > 0 {
		step = delta
	}
	n.Cursor -= step
	return n.Sync(total, visible)
}

// MoveDown moves the cursor down by delta rows (defaults to 1 if delta <= 0).
func (n *ListNav) MoveDown(delta, total, visible int) Window {
	step := 1
	if delta > 0 {
		step = delta
	}
	n.Cursor += step
	return n.Sync(total, visible)
}

// PageUp moves the cursor up by pageSize rows (or visible rows if pageSize is omitted or <= 0).
func (n *ListNav) PageUp(total, visible int, pageSize ...int) Window {
	ps := visible
	if len(pageSize) > 0 && pageSize[0] > 0 {
		ps = pageSize[0]
	}
	return n.MoveUp(max(1, ps), total, visible)
}

// PageDown moves the cursor down by pageSize rows (or visible rows if pageSize is omitted or <= 0).
func (n *ListNav) PageDown(total, visible int, pageSize ...int) Window {
	ps := visible
	if len(pageSize) > 0 && pageSize[0] > 0 {
		ps = pageSize[0]
	}
	return n.MoveDown(max(1, ps), total, visible)
}

// Home moves the cursor to the first element (index 0).
func (n *ListNav) Home(total, visible int) Window {
	return n.SetCursor(0, total, visible)
}

// End moves the cursor to the last element (index total - 1).
func (n *ListNav) End(total, visible int) Window {
	return n.SetCursor(max(0, total-1), total, visible)
}

// HandleNav applies standard list navigation actions (Up, Down, PageUp, PageDown, Top, Bottom).
// If pageSize is provided, PageUp/PageDown uses it instead of visible rows.
// Returns true if the action was a navigation action and was handled.
func (n *ListNav) HandleNav(action tuilib.NavAction, total, visible int, pageSize ...int) bool {
	switch action {
	case tuilib.NavUp:
		n.MoveUp(1, total, visible)
		return true
	case tuilib.NavDown:
		n.MoveDown(1, total, visible)
		return true
	case tuilib.NavPageUp:
		n.PageUp(total, visible, pageSize...)
		return true
	case tuilib.NavPageDown:
		n.PageDown(total, visible, pageSize...)
		return true
	case tuilib.NavTop:
		n.Home(total, visible)
		return true
	case tuilib.NavBottom:
		n.End(total, visible)
		return true
	default:
		return false
	}
}

// IsSelected reports whether idx is currently selected by the cursor.
func (n *ListNav) IsSelected(idx int) bool {
	return n.Cursor == idx
}
