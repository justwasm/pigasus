package viewport

// TranscriptCellAt maps a terminal coordinate onto the transcript cell
// under it.
//
// avail is the number of rows the transcript viewport occupies and width
// is its width in columns. A coordinate outside the viewport is not a
// cell; a viewport with no height at all has no bounds to check
// against, so every coordinate is taken at face value.
func TranscriptCellAt(x, y, avail, width int) (row, col int, ok bool) {
	if y < 0 || x < 0 {
		return 0, 0, false
	}
	if avail <= 0 {
		return y, x, true
	}
	if y >= avail || x >= width {
		return 0, 0, false
	}
	return y, x, true
}
