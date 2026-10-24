package tuiviewport

import (
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// SelHighlightOpen and SelHighlightClose wrap the selected cells in reverse video.
//
// Cycle 278: reverse video contrasts on every theme, and the selected slice is
// stripped of its inner SGRs before it is drawn, so no reset can punch through
// the cue.
const (
	SelHighlightOpen  = "\x1b[0m\x1b[7m"
	SelHighlightClose = "\x1b[0m"
)

// SelectionRange returns the normalized range (start <= end by row,
// ties broken by col) or (zero, zero, zero, zero, false) when no
// selection is active.
func SelectionRange(sr, sc, er, ec int, dragging bool) (int, int, int, int, bool) {
	if sr == 0 && sc == 0 && er == 0 && ec == 0 {
		return 0, 0, 0, 0, false
	}
	if !dragging && sr == er && sc == ec {
		// A click without a drag — no range to highlight.
		return 0, 0, 0, 0, false
	}
	if sr > er || (sr == er && sc > ec) {
		sr, sc, er, ec = er, ec, sr, sc
	}
	return sr, sc, er, ec, true
}

// SelectionText walks the visible transcript rows and emits the plain-text
// content of the highlighted range.
//
// ANSI escapes are stripped via ansi.Strip because the rows carry embedded colour
// codes that won't survive a raw byte copy to the clipboard. Newlines are kept as \n.
func SelectionText(rows []string, sr, sc, er, ec int) string {
	if sr < 0 || sr >= len(rows) || er < 0 || er >= len(rows) {
		return ""
	}
	var out []byte
	for r := sr; r <= er; r++ {
		plain := ansi.Strip(rows[r])
		width := ansi.StringWidth(plain)
		from, to := 0, width
		if r == sr {
			from = tuilib.ClampInt(sc, 0, width)
		}
		if r == er {
			to = tuilib.ClampInt(ec+1, 0, width)
		}
		if from > to {
			from = to
		}
		out = append(out, ansi.Cut(plain, from, to)...)
		if r != er {
			out = append(out, '\n')
		}
	}
	return string(out)
}

// ApplySelectionHighlight returns the given transcript row with selection
// cells re-coloured. Rows outside the selection range are returned untouched.
func ApplySelectionHighlight(row string, sr, sc, er, ec, rowIdx int) string {
	if rowIdx < sr || rowIdx > er {
		return row
	}
	rowWidth := ansi.StringWidth(row)
	if rowWidth == 0 {
		return row
	}
	from := 0
	to := rowWidth
	if rowIdx == sr {
		from = sc
	}
	if rowIdx == er {
		to = ec + 1
	}
	if from > rowWidth {
		from = rowWidth
	}
	if to > rowWidth {
		to = rowWidth
	}
	if from >= to {
		return row
	}
	return ApplySelectionHighlightToRow(row, from, to)
}

// ApplySelectionHighlightToRow highlights the cell range [fromCell, toCell] in reverse video.
func ApplySelectionHighlightToRow(row string, fromCell, toCell int) string {
	rowWidth := ansi.StringWidth(row)
	if rowWidth == 0 || fromCell >= toCell {
		return row
	}
	if fromCell > rowWidth {
		fromCell = rowWidth
	}
	if toCell > rowWidth {
		toCell = rowWidth
	}
	if fromCell >= toCell {
		return row
	}
	prefix := ansi.Cut(row, 0, fromCell)
	selected := ansi.Strip(ansi.Cut(row, fromCell, toCell))
	suffix := ansi.Cut(row, toCell, rowWidth)
	return prefix + SelHighlightOpen + selected + SelHighlightClose + suffix
}

// SelectionPass applies the highlight to every row in the visible transcript.
func SelectionPass(rows []string, sr, sc, er, ec int, active bool) []string {
	if !active {
		return rows
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = ApplySelectionHighlight(r, sr, sc, er, ec, i)
	}
	return out
}
