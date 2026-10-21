// tui_selection.go — cycle 174: in-app text selection.
//
// The reference agy terminal binary supports BOTH wheel scroll AND
// mouse select-and-copy simultaneously (no toggle, no /mouse slash
// command). The way it does this is: with mouse tracking on, the
// terminal cannot do its own native selection; the binary implements
// selection IN-APP — mouse-down → mouse-motion → mouse-release
// tracks an in-memory range, and on release the selected text is
// copied to the clipboard via OSC 52 (the terminal escape that
// tells the host terminal to put the bytes on the system clipboard).
//
// Visual feedback for the live drag is reverse video over the
// selected cells, applied as a post-processing pass on the
// already-padded transcript rows produced by transcriptWindow().
//
// All cell math here operates in display cells (the way `ansi.Cut`
// and `ansi.StringWidth` count), not raw bytes. ANSI escape
// sequences are zero-width and don't count toward display width.

package main

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// transcriptCellAt maps a raw (msg.X, msg.Y) screen coordinate from
// bubbletea to (row, col) inside the visible transcript produced by
// View(). The mapping is 1:1 because the transcript rows live at the
// very top of the body, with the chrome (top separator + prompt +
// bottom separator + status) below.
//
// Returns ok=false when the coordinate falls outside the visible
// transcript area (clicked on the chrome, picker, or status row) —
// callers ignore the click so other UI keeps owning its region.
func (m *runMode) transcriptCellAt(x, y int) (row, col int, ok bool) {
	if y < 0 || x < 0 {
		return 0, 0, false
	}
	avail := m.viewportHeight()
	if avail <= 0 {
		// Pre-119 path: terminal size unknown. The transcript
		// fills the whole body; we still translate 1:1 but
		// can't enforce bounds.
		return y, x, true
	}
	if y >= avail {
		return 0, 0, false
	}
	if x >= m.width {
		return 0, 0, false
	}
	return y, x, true
}

// selectionRange returns the normalized range (start <= end by row,
// ties broken by col) or (zero, zero, zero, zero, false) when no
// selection is active.
//
// "Active" here means: there is a non-empty range the user is
// currently dragging OR there is a stale range that should still be
// highlighted for one more frame (so the highlight doesn't vanish
// before the OSC52 write completes). The Update handler clears the
// state on MouseReleaseMsg.
func (m *runMode) selectionRange() (sr, sc, er, ec int, active bool) {
	sr, sc = m.selStartRow, m.selStartCol
	er, ec = m.selEndRow, m.selEndCol
	if sr == 0 && sc == 0 && er == 0 && ec == 0 {
		return 0, 0, 0, 0, false
	}
	if !m.selDragging && sr == er && sc == ec {
		// A click without a drag — no range to highlight.
		return 0, 0, 0, 0, false
	}
	if sr > er || (sr == er && sc > ec) {
		sr, sc, er, ec = er, ec, sr, sc
	}
	return sr, sc, er, ec, true
}

// selectionText walks the visible transcript produced by
// transcriptWindow() (with the same per-row padding the renderer
// uses) and emits the plain-text content of the highlighted range.
// Used by Update() at MouseReleaseMsg to build the OSC52 payload.
//
// The text extraction strips ANSI escapes via ansi.Strip because the
// rows carry embedded colour codes that won't survive a raw byte
// copy to the clipboard. Newlines are kept as \n so a multi-row drag
// produces a multi-line clipboard value, matching what the user sees
// on screen.
func (m *runMode) selectionText() string {
	if !m.altScreen {
		return ""
	}
	sr, sc, er, ec, ok := m.selectionRange()
	if !ok {
		return ""
	}
	rows := m.transcriptWindow()
	if sr < 0 || sr >= len(rows) || er < 0 || er >= len(rows) {
		return ""
	}
	var out []byte
	for r := sr; r <= er; r++ {
		plain := ansi.Strip(rows[r])
		// Cycle 278: from/to are *display-cell* offsets; slicing the
		// stripped string by byte offset split UTF-8 on any CJK or
		// emoji row (and read past the end on ASCII rows padded to the
		// viewport width). ansi.Cut counts cells, so it is safe here.
		width := ansi.StringWidth(plain)
		from, to := 0, width
		if r == sr {
			from = clampInt(sc, 0, width)
		}
		if r == er {
			to = clampInt(ec+1, 0, width)
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

// copySelectionCmd emits OSC 52 with the given selection text.
// bubbletea encodes the string into a base64-wrapped \x1b]52;c;…
// escape that terminals honour as "set the system clipboard to the
// decoded content". The exact wire format is bubbletea-internal;
// tea.SetClipboard is the documented way to ask for it. We wrap in
// a Cmd so the OSC write happens at the next program tick and
// doesn't block the Update loop.
//
// Returns nil when text is empty so the caller doesn't have to
// guard against a degenerate "copy zero bytes" round-trip.
func (m *runMode) copySelectionCmd(text string) tea.Cmd {
	if text == "" {
		return nil
	}
	return tea.SetClipboard(text)
}

// selHighlightOpen/Close wrap the selected cells in reverse video.
//
// Cycle 278: the previous cue was a 256-colour background (48;5;240).
// It was invisible for two independent reasons, both reported by the
// user as "选中部分和没选中的时候风格是一样的":
//
//  1. ansi.Cut(row, from, to) replays every SGR sequence of the row at
//     the *head* of the returned slice (x/ansi truncate keeps writing
//     escape bytes while it skips cells). That replay lands *after* the
//     opening escape, so the first "\x1b[m" inside the selection — and
//     glamour closes every styled span with one — turns the background
//     back off before the next glyph. Selecting inside a styled run
//     therefore highlighted nothing at all.
//  2. 48;5;240 is only visible if the terminal's background differs
//     enough from grey; it is theme-dependent.
//
// Reverse video fixes both: it is a terminal-level inversion of its own
// fg/bg, so it contrasts on every theme, and the selected slice is
// stripped of its inner SGRs before it is drawn, so no reset can punch
// through the cue. That is also what the reference does: its
// model.(*selectionState).highlight calls lipgloss.StyleRanges, which
// strips the selected run and renders one uniform style over it
// (verified in the 1.4.2 pclntab: highlight → StyleRanges). The exact
// reference SGR (reverse vs. a palette slot) was not recovered, so this
// is 【推测】 on the colour but 【一手】 on the strip-then-style strategy.
const (
	selHighlightOpen  = "\x1b[0m\x1b[7m"
	selHighlightClose = "\x1b[0m"
)

// applySelectionHighlight returns the given transcript row with
// selection cells re-coloured. Rows outside the selection range are
// returned untouched.
//
// We split the row at (from, to) display-cell offsets using
// ansi.Cut, which understands embedded ANSI escape sequences and
// returns exactly the byte substring that paints those cells. The
// selected slice is stripped of its own SGRs (see the const block:
// an inner reset would otherwise clear the cue), then wrapped in
// reverse-video on/off escapes. Prefix and suffix come straight from
// the source row, so the text outside the selection keeps its colour.
func applySelectionHighlight(row string, sr, sc, er, ec, rowIdx int) string {
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
	prefix := ansi.Cut(row, 0, from)
	selected := ansi.Strip(ansi.Cut(row, from, to))
	suffix := ansi.Cut(row, to, rowWidth)
	return prefix + selHighlightOpen + selected + selHighlightClose + suffix
}

// selectionPass applies the highlight to every row in the visible
// transcript, returning a new slice. When no selection is active the
// input is returned unchanged. Used by View() between
// transcriptWindow() and viewBody().
func (m *runMode) selectionPass(rows []string) []string {
	sr, sc, er, ec, ok := m.selectionRange()
	if !ok {
		return rows
	}
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = applySelectionHighlight(r, sr, sc, er, ec, i)
	}
	return out
}
