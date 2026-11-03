package viewport

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

// TestPanelCursor pins the cursor arithmetic of a vertical list panel.
//
// The rules are the ones the other list panels use: ↑/↓ move one row, a
// page key moves ten rows, and the home/end keys park the cursor on the
// first or the last row. The cursor is clamped into the panel's rows.
func TestPanelCursor(t *testing.T) {
	cases := []struct {
		name   string
		cursor int
		action tuilib.NavAction
		total  int
		want   int
	}{
		{"up", 2, tuilib.NavUp, 5, 1},
		{"up at the top", 0, tuilib.NavUp, 5, 0},
		{"down", 2, tuilib.NavDown, 5, 3},
		{"down at the bottom", 4, tuilib.NavDown, 5, 4},
		{"page up", 12, tuilib.NavPageUp, 20, 2},
		{"page up at the top", 3, tuilib.NavPageUp, 20, 0},
		{"page down", 3, tuilib.NavPageDown, 20, 13},
		{"page down at the bottom", 18, tuilib.NavPageDown, 20, 19},
		{"top", 3, tuilib.NavTop, 5, 0},
		{"bottom", 1, tuilib.NavBottom, 5, 4},
		{"a single row", 0, tuilib.NavDown, 1, 0},
		{"an empty list keeps the cursor", 3, tuilib.NavDown, 0, 3},
		{"an empty list keeps the cursor on a page key", 3, tuilib.NavPageDown, 0, 3},
		{"an empty list keeps the cursor on the bottom key", 3, tuilib.NavBottom, 0, 3},
		{"an unknown action keeps the cursor", 2, tuilib.NavNone, 5, 2},
		{"the cancel action is not the cursor's", 2, tuilib.NavCancel, 5, 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := PanelCursor(c.cursor, c.action, c.total); got != c.want {
				t.Errorf("PanelCursor(%d, %v, %d) = %d, want %d",
					c.cursor, c.action, c.total, got, c.want)
			}
		})
	}
}

// TestPanelPageRows pins the page size: a page key moves ten rows.
func TestPanelPageRows(t *testing.T) {
	if PanelPageRows != 10 {
		t.Errorf("PanelPageRows = %d, want 10", PanelPageRows)
	}
}
