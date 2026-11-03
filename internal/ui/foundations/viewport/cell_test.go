package viewport_test

import (
	"testing"

	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// TestTranscriptCellAt pins the coordinate mapping: a coordinate inside
// the viewport is the cell it names, a coordinate outside it is not a
// cell, and a viewport with no height has no bounds to check against.
func TestTranscriptCellAt(t *testing.T) {
	cases := []struct {
		name    string
		x, y    int
		avail   int
		width   int
		wantRow int
		wantCol int
		wantOK  bool
	}{
		{"inside", 3, 2, 10, 40, 2, 3, true},
		{"top left", 0, 0, 10, 40, 0, 0, true},
		{"last column", 39, 0, 10, 40, 0, 39, true},
		{"past the width", 40, 0, 10, 40, 0, 0, false},
		{"past the height", 0, 10, 10, 40, 0, 0, false},
		{"negative x", -1, 0, 10, 40, 0, 0, false},
		{"negative y", 0, -1, 10, 40, 0, 0, false},
		{"negative both", -1, -1, 10, 40, 0, 0, false},
		{"no height takes every cell", 100, 100, 0, 40, 100, 100, true},
		{"negative height takes every cell", 100, 100, -5, 40, 100, 100, true},
		{"no width has no cell", 0, 0, 10, 0, 0, 0, false},
	}
	for _, tc := range cases {
		row, col, ok := tuiviewport.TranscriptCellAt(tc.x, tc.y, tc.avail, tc.width)
		if row != tc.wantRow || col != tc.wantCol || ok != tc.wantOK {
			t.Errorf("%s: TranscriptCellAt(%d, %d, %d, %d) = %d, %d, %v; want %d, %d, %v",
				tc.name, tc.x, tc.y, tc.avail, tc.width, row, col, ok, tc.wantRow, tc.wantCol, tc.wantOK)
		}
	}
}
