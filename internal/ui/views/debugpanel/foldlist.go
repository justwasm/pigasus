// foldlist.go — the foldable row list the Wire and Trajectory views share.
//
// # What it is
//
// A tree of rows with one fold state per row and a cursor that moves
// over the rows the user can act on. A row that carries detail lines
// shows them under itself when open; a row that carries children shows
// them, indented, when open.
//
// The list is the one place that answers "which line is the cursor on"
// and "how far may the window scroll", so the two views cannot disagree
// about either. A fold is per-row and keyed by the row's stable ID, so
// re-opening the panel on a rebuilt row set keeps the folds the user set
// where the IDs still match.
//
// # Why a tree and not two lists
//
// The wire view is literally three sections of items, and the trajectory
// is a run of steps with a body each. Both are "a row, and what is under
// it", so one type serves both and the key handling exists once.
package debugpanel

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/palette"
)

// stripStyles removes the SGR sequences from a styled row so the copy key
// hands over text a terminal pastes rather than the escapes that drew it.
func stripStyles(s string) string { return color.StripSGR(s) }

// FoldRow is one entry of a foldable list.
type FoldRow struct {
	// ID keys the row's fold state. It must be stable across rebuilds of
	// the same conversation, and unique within the list.
	ID string
	// Text is the row's one-line form, already styled by the caller.
	Text string
	// Detail is the body printed under the row when it is open. Every
	// entry is one line; the caller does the wrapping, so a long tool
	// result is cut where the caller knows the width.
	Detail []string
	// Children are the rows shown under this one, indented, when it is
	// open.
	Children []FoldRow

	// Cold marks a row whose body lives outside memory. Enter on it asks
	// the host to load the body rather than opening a note, so the panel
	// has to know which rows are cold without reading the row's text.
	Cold bool

	// Flat renders the row at the list's left edge even when it is a
	// child, so a table of numbers keeps one column per measure.
	//
	// The tree's indentation is the right vocabulary for a hierarchy —
	// the Trajectory's steps sit under their turn — but the wrong one for
	// a table: indenting the Wire's members under their group moves their
	// numbers two columns right, and a numeric column with a step in it
	// reads as two unrelated lists. The group's own ▾ marker still says
	// what the members belong to.
	Flat bool

	// metaBytes and metaTokens are the row's contribution to its parent
	// group's total. They are unexported because a caller sets them
	// through the constructors in this package and never reads them from
	// outside: the group heading is what prints the sum.
	metaBytes  int
	metaTokens int
}

// Foldable reports whether the row has anything to open.
//
// A cold row is not foldable: its Detail is the note that stands in for a
// body the host has not loaded yet, and that note is on screen from the
// first frame. Enter on such a row is the load, not a fold (see
// Panel.foldKey).
func (r FoldRow) Foldable() bool {
	if r.Cold {
		return len(r.Children) > 0
	}
	return len(r.Detail) > 0 || len(r.Children) > 0
}

// PlainText is the row's one-line form without its styling.
//
// It exists for the reader: a group's body is its members written out,
// and a body carrying the row's escape sequences would paste garbage into
// whatever the user does with it.
func (r FoldRow) PlainText() string { return stripStyles(r.Text) }

// listLine is one rendered line of the list: either a row, or a detail
// line belonging to the row above it.
type listLine struct {
	// row is the index into the flattened row sequence. Detail lines
	// carry their owner's index so the cursor can never land on them.
	row int
	// text is the rendered line.
	text string
	// detail marks a body line: it is not selectable, and the cursor
	// steps over it.
	detail bool
}

// foldList is the fold state, the cursor and the window of one view.
type foldList struct {
	rows []FoldRow
	// open is the fold state: a row is open when its ID is in the set.
	// Containers start closed, so the first frame of a long trajectory
	// is its outline.
	open   map[string]bool
	cursor int
	window int
	// lastRows is how many rows the last render had, so a key that arrives
	// before the first draw still clamps against something.
	lastRows int
	// synced reports whether the view has set its default fold state.
	// Without it a view that opens its groups by default would re-open
	// them on every frame, undoing the user's folds.
	synced bool
	// plain keeps each row's unstyled text, keyed by ID, so the copy key
	// hands the clipboard text a user can paste rather than the escapes.
	// plainSrc is the styled text it was stripped from, so a rebuild can
	// tell "same row, re-stripped" from "same row, unchanged".
	plain    map[string]string
	plainSrc map[string]string

	// rowsSet says whether rows holds a row set whose bookkeeping is
	// already built. A reset handed the same slice again has nothing to
	// rebuild — see sameFoldRowSlice.
	rowsSet bool

	// foldGen versions everything the rendered lines depend on: the rows,
	// the fold state and the cursor. lines() caches against it, because a
	// frame asks for the list several times (the wheel wants the cursor's
	// line, the window wants the slice, the counter wants the total).
	foldGen     uint64
	linesGen    uint64
	cachedLines []listLine
	linesSet    bool
}

// bump marks the rendered lines stale.
//
// It is called from every mutator of the rows, the fold state and the
// cursor. A missed call shows a stale frame rather than corrupting
// anything, so the rule is "bump where the drawing inputs change".
func (l *foldList) bump() { l.foldGen++ }

// sameFoldRowSlice reports whether two row slices are the same set.
//
// It compares the slice header and the first row's identity, which is
// enough for the only caller that matters: the panel's own memo hands
// back the slice it built, so an unchanged set is literally the same
// slice. A rebuilt set that happens to reuse the same backing array is
// the one case this could miss, and the caller's memo cannot produce it
// (it rebuilds into a fresh slice).
func sameFoldRowSlice(a, b []FoldRow) bool {
	if len(a) != len(b) {
		return false
	}
	if len(a) == 0 {
		return true
	}
	return &a[0] == &b[0]
}

// reset replaces the rows and clamps the cursor. The folds the user set
// are kept when their IDs survive, because a rebuilt row set for the same
// conversation differs only in the values.
//
// The cursor is clamped rather than reset: a background turn can add a
// row between two draws, and jumping the user back to the top on every
// frame is worse than keeping them where they were.
func (l *foldList) reset(rows []FoldRow) {
	// The rows arrive rebuilt on every draw, so the fold bookkeeping has
	// to run each time — but the *stripping* does not. stripStyles walks
	// every escape sequence of every row, and a scrolling frame resets
	// the same rows repeatedly; at 80 steps that was ~190 µs per frame,
	// more than the rest of the draw put together.
	//
	// The rows are the same values whenever the caller's cache hit, so
	// the plain-text map is kept and only re-stripped for IDs whose text
	// actually changed.
	// The overwhelmingly common case is the same row set as the last
	// draw: the caller's memo handed back the very slice it built
	// before. Comparing the slice header is free, and it is enough
	// because the caller's cache only reuses a slice it did not rebuild.
	if l.rowsSet && sameFoldRowSlice(l.rows, rows) {
		return
	}
	if l.open == nil {
		l.open = make(map[string]bool)
	}
	if l.plain == nil {
		l.plain = make(map[string]string, len(rows))
	}
	if l.plainSrc == nil {
		l.plainSrc = make(map[string]string, len(rows))
	}
	live := make(map[string]bool, len(l.open))
	seen := make(map[string]bool, len(l.plain))
	walkFoldRows(rows, func(r FoldRow) {
		if l.open[r.ID] {
			live[r.ID] = true
		}
		seen[r.ID] = true
		// plainSrc is the styled text the plain entry was stripped from:
		// comparing the plain text to r.Text would always differ, since
		// one has escapes and the other does not.
		if prev, ok := l.plainSrc[r.ID]; !ok || prev != r.Text {
			l.plain[r.ID] = stripStyles(r.Text)
			l.plainSrc[r.ID] = r.Text
		}
	})
	// Drop the entries of rows that are gone, so a conversation that
	// shrinks does not keep its old text in memory.
	for id := range l.plain {
		if !seen[id] {
			delete(l.plain, id)
			delete(l.plainSrc, id)
		}
	}
	l.rowsSet = true
	l.rows = rows
	l.bump()
	l.open = live
	if n := len(l.flattened()); l.cursor > n-1 {
		if n == 0 {
			l.cursor = 0
		} else {
			l.cursor = n - 1
		}
	}
}

// openAll opens every foldable row: the default for a group-oriented
// view, and the `o` key.
func (l *foldList) openAll() {
	if l.open == nil {
		l.open = make(map[string]bool)
	}
	walkFoldRows(l.rows, func(r FoldRow) {
		if r.Foldable() {
			l.open[r.ID] = true
		}
	})
	l.bump()
}

// cursorClipboardText returns the row under the cursor as plain text,
// with its open body under it, so a user can paste what they are looking
// at into an issue.
func (l *foldList) cursorClipboardText() string {
	rows := l.flattened()
	if l.cursor < 0 || l.cursor >= len(rows) {
		return ""
	}
	r := rows[l.cursor]
	out := r.ID
	if plain, ok := l.plain[r.ID]; ok {
		out = plain
	}
	if l.open[r.ID] {
		for _, d := range r.Detail {
			out += "\n  " + stripStyles(d)
		}
	}
	return out
}

// walkFoldRows visits every row of the tree, depth first.
func walkFoldRows(rows []FoldRow, fn func(FoldRow)) {
	for _, r := range rows {
		fn(r)
		walkFoldRows(r.Children, fn)
	}
}

// flattened returns every selectable row in display order, with the
// index of its parent so a fold can hide a subtree.
func (l *foldList) flattened() []FoldRow {
	var out []FoldRow
	var walk func(rows []FoldRow)
	walk = func(rows []FoldRow) {
		for _, r := range rows {
			out = append(out, r)
			if r.Foldable() && l.open[r.ID] {
				walk(r.Children)
			}
		}
	}
	walk(l.rows)
	return out
}

// lines renders the list into display lines.
//
// gutter marks the row the cursor is on; a foldable row shows ▸ when
// closed and ▾ when open. A row with neither detail nor children gets a
// blank gutter, so the column stays aligned.
func (l *foldList) lines() []listLine {
	// The list is rendered more than once per frame: the wheel asks for
	// the cursor's line, the window asks for the visible slice, the
	// counter asks for the total. Each call walked the tree and
	// concatenated every row's styled text — measured as the largest
	// remaining cost in a trajectory frame.
	//
	// The cache key is the state the render depends on: the rows, the
	// fold state's version, the cursor (the gutter marks it) and the
	// window (the cache is of the whole list, so the window does not
	// enter into it — but the fold version does).
	if l.linesSet && l.linesGen == l.foldGen {
		return l.cachedLines
	}
	out := l.buildLines()
	l.cachedLines = out
	l.linesGen = l.foldGen
	l.linesSet = true
	return out
}

// buildLines renders the list from scratch.
func (l *foldList) buildLines() []listLine {
	var out []listLine
	flat := 0
	var walk func(rows []FoldRow, depth int)
	walk = func(rows []FoldRow, depth int) {
		for _, r := range rows {
			idx := flat
			flat++
			gutter := "  "
			if idx == l.cursor {
				gutter = palette.BlueOpen + "> " + palette.BlueClose
			}
			marker := "  "
			if r.Foldable() {
				if l.open[r.ID] {
					marker = palette.DimGrayOpen + "▾ " + palette.DimGrayClose
				} else {
					marker = palette.DimGrayOpen + "▸ " + palette.DimGrayClose
				}
			}
			indent := depth
			if r.Flat {
				indent = 0
			}
			out = append(out, listLine{
				row:  idx,
				text: gutter + strings.Repeat("  ", indent) + marker + r.Text,
			})
			// A cold row is never folded: its Detail is the note that
			// stands in for a body the host has not loaded, and a note a
			// fold could hide would be a cold row indistinguishable from
			// an empty one.
			open := r.Cold || (r.Foldable() && l.open[r.ID])
			if !open {
				continue
			}
			for _, d := range r.Detail {
				out = append(out, listLine{
					row:    idx,
					text:   "   " + strings.Repeat("  ", depth) + "  " + palette.DimGrayOpen + d + palette.DimGrayClose,
					detail: true,
				})
			}
			if r.Foldable() && l.open[r.ID] {
				walk(r.Children, depth+1)
			}
		}
	}
	walk(l.rows, 0)
	l.lastRows = flat
	return out
}

// toggle opens or closes the row the cursor is on and answers whether
// the row was foldable.
func (l *foldList) toggle() bool {
	rows := l.flattened()
	if l.cursor < 0 || l.cursor >= len(rows) {
		return false
	}
	r := rows[l.cursor]
	if !r.Foldable() {
		return false
	}
	l.open[r.ID] = !l.open[r.ID]
	l.bump()
	return true
}

// moveCursor steps the cursor by delta over the selectable rows.
func (l *foldList) moveCursor(delta int) {
	n := len(l.flattened())
	if n == 0 {
		l.cursor = 0
		return
	}
	before := l.cursor
	l.cursor += delta
	if l.cursor < 0 {
		l.cursor = 0
	}
	if l.cursor > n-1 {
		l.cursor = n - 1
	}
	if l.cursor != before {
		l.bump()
	}
}

// toCursor puts the cursor on the first or the last selectable row.
func (l *foldList) toCursor(last bool) {
	n := len(l.flattened())
	if n == 0 {
		l.cursor = 0
		return
	}
	if last {
		if l.cursor != n-1 {
			l.cursor = n - 1
			l.bump()
		}
		return
	}
	if l.cursor != 0 {
		l.cursor = 0
		l.bump()
	}
}

// expandAll opens every foldable row; collapseAll closes them.
func (l *foldList) expandAll() { l.openAll() }

func (l *foldList) collapseAll() {
	l.open = make(map[string]bool)
	l.bump()
}

// windowLines returns the visible slice of the list and keeps the cursor
// on screen.
//
// Two callers move different things and both must survive this function:
//
//	keys    move the cursor; the window follows it (moveCursor)
//	wheel   moves the window; the cursor is dragged along (scrollWindow)
//
// So the window is only adjusted when the cursor has actually left it.
// The earlier version pulled the window back onto the cursor
// unconditionally, which undid every wheel scroll on the next frame: the
// window moved, the cursor did not, and the render snapped back. That is
// what made the wheel feel sticky compared with the transcript, where the
// wheel moves the viewport and nothing else.
func (l *foldList) windowLines(visible int) []string {
	all := l.lines()
	if visible < 1 {
		visible = 1
	}
	cursorLine := l.lineOfCursor(all)
	if cursorLine < l.window {
		l.window = cursorLine
	}
	if cursorLine >= l.window+visible {
		l.window = cursorLine - visible + 1
	}
	maxWindow := len(all) - visible
	if maxWindow < 0 {
		maxWindow = 0
	}
	if l.window > maxWindow {
		l.window = maxWindow
	}
	if l.window < 0 {
		l.window = 0
	}
	end := l.window + visible
	if end > len(all) {
		end = len(all)
	}
	out := make([]string, 0, end-l.window)
	for _, ln := range all[l.window:end] {
		out = append(out, ln.text)
	}
	return out
}

// lineOfCursor is the display line the cursor's row is drawn on.
//
// An open row prints detail lines under itself, so the cursor's row index
// and its line index are not the same number past the first body. A
// cursor that matches no line (a row the last fold hid) reports the
// window's top, which leaves the window where it is rather than jumping
// it somewhere arbitrary.
func (l *foldList) lineOfCursor(all []listLine) int {
	for i, ln := range all {
		if !ln.detail && ln.row == l.cursor {
			return i
		}
	}
	return l.window
}

// scrollWindow moves the viewport by delta display lines, for the wheel.
//
// This is the transcript's wheel behaviour rather than the cursor keys':
// the window moves and the cursor is dragged into it only when it would
// otherwise be left off screen. The two are separate because a wheel is a
// "show me a different part of this list" gesture, while j/k is a "pick
// the next row" gesture — and making the wheel move the selection is what
// made it feel unlike scrolling anywhere else in the app.
//
// It answers whether anything moved, so a wheel at the top or the bottom
// does not repaint for nothing.
func (l *foldList) scrollWindow(delta, visible int) bool {
	if delta == 0 {
		return false
	}
	all := l.lines()
	if visible < 1 {
		visible = 1
	}
	maxWindow := len(all) - visible
	if maxWindow < 0 {
		maxWindow = 0
	}
	old := l.window
	l.window = clampOffset(l.window+delta, maxWindow)
	l.pullCursorInto(all, visible)
	return l.window != old
}

// pullCursorInto moves the cursor to the nearest selectable row inside
// the window.
//
// The cursor is what a key acts on, so it must stay somewhere the user
// can see; dragging it along is what makes the wheel safe to use before
// pressing a key. Detail lines are skipped — they are not selectable.
func (l *foldList) pullCursorInto(all []listLine, visible int) {
	end := l.window + visible
	if end > len(all) {
		end = len(all)
	}
	line := l.lineOfCursor(all)
	if line >= l.window && line < end {
		return
	}
	// Prefer the row at the window's top edge, so scrolling up leaves the
	// cursor where the eye already is rather than at the bottom.
	for i := l.window; i < end; i++ {
		if !all[i].detail {
			if l.cursor != all[i].row {
				l.cursor = all[i].row
				l.bump()
			}
			return
		}
	}
	// A window of nothing but detail lines: walk back to their owner.
	for i := l.window - 1; i >= 0; i-- {
		if !all[i].detail {
			if l.cursor != all[i].row {
				l.cursor = all[i].row
				l.bump()
			}
			return
		}
	}
}

// position reports the 1-based window range and the list's length, for
// the counter row.
func (l *foldList) position(visible int) (start, end, total int) {
	total = len(l.lines())
	if total == 0 {
		return 0, 0, 0
	}
	if visible < 1 {
		visible = 1
	}
	start = l.window + 1
	end = l.window + visible
	if end > total {
		end = total
	}
	if start > total {
		start = total
	}
	return start, end, total
}

// rowCount is the number of selectable rows, which is what the cursor
// is bounded by.
func (l *foldList) rowCount() int { return len(l.flattened()) }
