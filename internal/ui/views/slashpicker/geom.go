package slashpicker

// layout is the popup's internal structure, filled in by Rows() as a
// side effect. The offsets are 0-based row indices within the popup
// overlay's own row sequence (i.e. before placement in the body, and
// before any top-trim).
type layout struct {
	HasList    bool // at least one list row was rendered (false in the "No matches" branch)
	ListTop    int  // row offset of the first LIST row (skips the ↑ N more tail)
	ListBottom int  // row offset of the last  LIST row
	UpMore     bool // ↑ N more tail was emitted
	DownMore   bool // ↓ N more tail was emitted
	HintRow    int  // row offset of the keyboard hint row (= Rows - 1)
	Rows       int  // total rows the popup wrote, including tails and hint
}

// geom is the popup's on-screen rectangle. Rows are 0-based terminal
// rows. Valid is false when the popup is hidden or the terminal size
// is unknown.
//
// TopY and BottomY bracket every row the popup emitted, including the
// optional ↑ N more / ↓ N more tail rows, the blank separator, and the
// "↑/↓ Navigate · enter Select · tab Complete" hint row. ListTopY and
// ListBottomY point at the LIST rows a click should select — the
// scroll-tail rows and the hint row are excluded. HasList mirrors
// layout.HasList so a click dispatched against an empty ("No
// matches") popup is dropped instead of picking a non-existent
// command.
type geom struct {
	Valid       bool
	HasList     bool
	TopY        int
	BottomY     int
	ListTopY    int
	ListBottomY int
	HintY       int
}

// ListRowOffset returns (offset, true) if (x, y) lands on a
// selectable list row.
func (g geom) ListRowOffset(x, y, width int) (int, bool) {
	if !g.Valid || !g.HasList {
		return 0, false
	}
	if y < g.ListTopY || y > g.ListBottomY {
		return 0, false
	}
	if x < 0 || (width > 0 && x >= width) {
		return 0, false
	}
	return y - g.ListTopY, true
}

// ContainsWheel reports whether a mouse wheel coordinate lands in the
// popup's bounds.
func (g geom) ContainsWheel(y int) bool {
	if !g.Valid || !g.HasList {
		return false
	}
	return y >= g.TopY && y <= g.BottomY
}
