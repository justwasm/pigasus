package viewport

import (
	"strings"

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
//
// The one exception is two rows that are pieces of the *same* hyperlink: they
// are one logical string, so they are joined without a newline, and the
// continuation row's indent — the layout's, not the link's — is dropped. A URL
// too long for the terminal is cut into chunks, one chunk per row, and every
// chunk carries the complete URL as its OSC 8 target (internal/urlchunk) —
// copying the rows that way is what makes the copy a URL and not a stack of
// lines.
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
		text := ansi.Cut(plain, from, to)
		if r > sr && sameLink(rows[r-1], rows[r]) {
			text = strings.TrimLeft(text, " \t")
		}
		out = append(out, text...)
		if r != er && !sameLink(rows[r], rows[r+1]) {
			out = append(out, '\n')
		}
	}
	return string(out)
}

// sameLink reports whether two rows are pieces of the same hyperlink.
//
// A row that is a piece of a hyperlink carries the *complete* link as
// its target — the URL chunk rows do (internal/urlchunk) — so the
// target names the logical string the row belongs to.
func sameLink(a, b string) bool {
	ta := linkTarget(a)
	return ta != "" && ta == linkTarget(b)
}

// linkTarget returns the OSC 8 target of a row, or "" when the row is
// not a hyperlink.
func linkTarget(row string) string {
	const open = "\x1b]8;;"
	i := strings.Index(row, open)
	if i < 0 {
		return ""
	}
	rest := row[i+len(open):]
	// The target ends at the string terminator: ST (ESC \) or BEL.
	if j := strings.Index(rest, "\x1b\\"); j >= 0 {
		return rest[:j]
	}
	if j := strings.Index(rest, "\x07"); j >= 0 {
		return rest[:j]
	}
	return ""
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
