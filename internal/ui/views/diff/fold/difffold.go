// Package difffold holds the hunk-folding primitives for the /diff file
// panel (cycle diff-folding / AGY 1.3.5).
//
// AGY 1.3.5 changelog line 4:
//
//	"Added folding to the /diff file view: hunk headers can be collapsed
//	 with the f key, which hides the lines inside the hunk and shows just
//	 the @@ header with a + count; pressing f again expands the hunk and
//	 the cursor stays on the same header."
//
// The reference's side-by-side viewer (out of scope) carries the f key
// for full-file collapse; the clone's plain inline body keeps the same
// gesture — press f on an @@ header and the hunk's body collapses into
// the header line plus a dim-grey `+N` line-count indicator. Pressing
// f again expands.
//
// The model:
//
//	pre-header lines   (= everything before the first "@@" line)
//	hunk[0] = {header, body}
//	hunk[1] = {header, body}
//	...
//
// A line is a "@@" header iff it starts with "@@" (git's diff output has
// no other "@"-prefixed line in this context — `diff --git` starts with
// "d", `index` with "i", `---`/`+++` with "-"/"+", and hunk ranges with
// "@@"). Pre-header lines never participate in folding.
//
// The cursor (m.panel.diffInlineRow) indexes the rendered body rows
// emitted by RenderBody — rows[0] is the first body line (a
// pre-header file header for files with no hunks, or "diff --git...")
// and rows[len-1] is the last. Folding one hunk compresses its body
// from 1+len(body) rows to a single row, so the cursor must be
// re-mapped after a toggle so it stays "on the same header".
//
// Everything here is pure: the split, the layout, the row lookup. The
// fold *state* lives with the panel, in cmd/wagyu.
package fold

import (
	"strconv"
	"strings"
)

// Hunk describes one hunk of a git diff body: the "@@ -a,b +c,d @@"
// header line and the lines that follow until the next "@@" or EOF.
type Hunk struct {
	// Header is the "@@" line verbatim (no leading SGR).
	Header string
	// Body holds every line after the header up to (but not including)
	// the next @@ line, or end of the input. May be empty (a hunk with
	// no body lines, produced by pure removals in some encodings — git
	// usually keeps at least one "- " line, but we don't enforce it).
	Body []string
}

// Count returns the number of body lines for hunk i after
// SplitHunks. The +N indicator on a folded header uses this value
// so the user sees roughly how many lines they have hidden.
func Count(h Hunk) int { return len(h.Body) }

// FoldHint returns the "+N" suffix shown on a folded header
// line. N is Count(h). The helper is centralised so the
// indicator format is the single source of truth — tests and renderer
// share it.
//
// The indicator goes after the trailing "@@" in dim grey so it stays
// visually subordinate to the header. The choice of "+N" (rather than
// "×N" or "(N)") matches the changelog wording ("a + count"); the
// literal "+" is followed by the count with no separator.
func FoldHint(h Hunk, dimOpen, dimClose string) string {
	return dimOpen + "+" + strconv.Itoa(Count(h)) + dimClose
}

// SplitHunks partitions a git diff body into pre-header lines
// (the "diff --git ...", "index ...", "--- a/x", "+++ b/y" block) and
// a sequence of Hunk entries (one per "@@" header). Lines that
// come after the last "@@" belong to that hunk's body.
//
// Diff lines have a known shape: text starting with " ", "+", "-" is
// a body line; "@@" opens a hunk; everything else is a file-level
// header. The only "@"-prefixed line is the hunk header so the "@@"
// prefix test is unambiguous here.
func SplitHunks(body []string) (pre []string, hunks []Hunk) {
	i := 0
	// Pre-header: lines before the first "@@".
	for i < len(body) && !strings.HasPrefix(body[i], "@@") {
		pre = append(pre, body[i])
		i++
	}
	// Hunks: groups of (@@ header, body lines until next @@ or EOF).
	for i < len(body) {
		if !strings.HasPrefix(body[i], "@@") {
			// Defensive: callers pass trimmed git output, so a
			// stray non-"@@" line past the pre-header would be a
			// corrupted diff. Bail out rather than misclassify.
			break
		}
		h := Hunk{Header: body[i]}
		i++
		for i < len(body) && !strings.HasPrefix(body[i], "@@") {
			h.Body = append(h.Body, body[i])
			i++
		}
		hunks = append(hunks, h)
	}
	return pre, hunks
}

// Layout describes the rendered footprint of one hunk,
// used to map the cursor row back to a hunk index. StartRow is the
// rendered row of the @@ header (the only row emitted when folded);
// RowCount is 1 when folded, 1+len(body) when expanded. Hunk index
// matches the slice index returned by SplitHunks.
type Layout struct {
	StartRow int
	RowCount int
}

// RenderBody emits the styled rendered rows for one git diff body,
// applying the per-hunk fold state. It also returns a
// per-hunk-index layout slice so the caller can map the cursor row
// back to a hunk index.
//
// style is the line styler: the caller passes the syntax-highlighting
// renderer for the file view and the monochrome inline styler for the
// commit view. When the body is empty for a hunk the header is still
// emitted.
//
// The implementation walks SplitHunks once. Pre-header lines emit
// verbatim. For each hunk, the @@ line is always emitted (with an
// additional "+N" suffix when folded). When the hunk is folded the body
// lines are dropped entirely; when expanded they are emitted one by one.
//
// The cursor clamping is the caller's responsibility — the function
// here only renders. The toggler re-clamps after a
// toggle so the cursor never lands on a missing row.
func RenderBody(pre []string, hunks []Hunk, folded map[int]bool, style func(string) string, dimOpen, dimClose string) ([]string, []Layout) {
	var rows []string
	var layout []Layout
	for _, l := range pre {
		rows = append(rows, "  "+style(l))
	}
	for i, h := range hunks {
		if folded != nil && folded[i] {
			// One row: header + " +N" hint.
			rows = append(rows, "  "+style(h.Header)+" "+FoldHint(h, dimOpen, dimClose))
			layout = append(layout, Layout{StartRow: len(rows) - 1, RowCount: 1})
			continue
		}
		headerRow := len(rows)
		rows = append(rows, "  "+style(h.Header))
		for _, l := range h.Body {
			rows = append(rows, "  "+style(l))
		}
		layout = append(layout, Layout{StartRow: headerRow, RowCount: 1 + len(h.Body)})
	}
	return rows, layout
}

// HunkIdxForRow looks up the hunk whose header occupies rendered row
// `row`. Returns (hunkIdx, true) when row is the @@ header of some
// hunk, (-1, false) when row is a body line or out of range. Used
// by the toggler to honour the "f on a non-hunk line is a no-op" rule.
func HunkIdxForRow(layout []Layout, row int) (int, bool) {
	if row < 0 {
		return -1, false
	}
	for i, lay := range layout {
		if row == lay.StartRow {
			return i, true
		}
		if row > lay.StartRow && row < lay.StartRow+lay.RowCount {
			// Body line of hunk i; cursor is not on a header.
			return -1, false
		}
	}
	return -1, false
}
