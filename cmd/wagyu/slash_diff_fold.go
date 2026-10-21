// Copyright (c) 2026, the openagy project authors.
//
// slash_diff_fold.go — hunk-folding primitives for the /diff file panel
// (cycle diff-folding / AGY 1.3.5).
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
// emitted by renderDiffBody — rows[0] is the first body line (a
// pre-header file header for files with no hunks, or "diff --git...")
// and rows[len-1] is the last. Folding one hunk compresses its body
// from 1+len(body) rows to a single row, so the cursor must be
// re-mapped after a toggle so it stays "on the same header".

package main

import (
	"strconv"
	"strings"
)

// diffHunk describes one hunk of a git diff body: the "@@ -a,b +c,d @@"
// header line and the lines that follow until the next "@@" or EOF.
type diffHunk struct {
	// header is the "@@" line verbatim (no leading SGR).
	header string
	// body holds every line after the header up to (but not including)
	// the next @@ line, or end of the input. May be empty (a hunk with
	// no body lines, produced by pure removals in some encodings — git
	// usually keeps at least one "- " line, but we don't enforce it).
	body []string
}

// diffHunkCount returns the number of body lines for hunk i after
// splitDiffHunks. The +N indicator on a folded header uses this value
// so the user sees roughly how many lines they have hidden.
func diffHunkCount(h diffHunk) int { return len(h.body) }

// splitDiffHunks partitions a git diff body into pre-header lines
// (the "diff --git ...", "index ...", "--- a/x", "+++ b/y" block) and
// a sequence of diffHunk entries (one per "@@" header). Lines that
// come after the last "@@" belong to that hunk's body.
//
// Diff lines have a known shape: text starting with " ", "+", "-" is
// a body line; "@@" opens a hunk; everything else is a file-level
// header. The only "@"-prefixed line is the hunk header so the "@@"
// prefix test is unambiguous here.
func splitDiffHunks(body []string) (pre []string, hunks []diffHunk) {
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
		h := diffHunk{header: body[i]}
		i++
		for i < len(body) && !strings.HasPrefix(body[i], "@@") {
			h.body = append(h.body, body[i])
			i++
		}
		hunks = append(hunks, h)
	}
	return pre, hunks
}

// diffHunkFoldHint returns the "+N" suffix shown on a folded header
// line. N is diffHunkCount(h). The helper is centralised so the
// indicator format is the single source of truth — tests and renderer
// share it.
//
// The indicator goes after the trailing "@@" in dim grey so it stays
// visually subordinate to the header. The choice of "+N" (rather than
// "×N" or "(N)") matches the changelog wording ("a + count"); the
// literal "+" is followed by the count with no separator.
func diffHunkFoldHint(h diffHunk) string {
	return cDimGrayOpen + "+" + strconv.Itoa(diffHunkCount(h)) + cDimGrayClose
}

// diffFoldedHunkLayout describes the rendered footprint of one hunk,
// used to map the cursor row back to a hunk index. startRow is the
// rendered row of the @@ header (the only row emitted when folded);
// rowCount is 1 when folded, 1+len(body) when expanded. Hunk index
// matches the slice index returned by splitDiffHunks.
type diffFoldedHunkLayout struct {
	startRow int
	rowCount int
}

// renderDiffBody emits the styled rendered rows for one git diff body,
// applying m.panel.diffFolded[i] per hunk. It also returns a
// per-hunk-index layout slice so the caller can map the cursor row
// back to a hunk index.
//
// langFile is the path the renderer is bound to (typically
// m.panel.diffViewPath for the file view, "" for the commit view).
// When non-empty, body lines pass through renderDiffInlineLine so
// keywords / strings / numbers / comments / types pick up the
// Aurora-Dark colour tokens the cycle diff-syntax-highlight wired
// up. The commit view passes "" so it stays on the original
// diffInlineStyle monochrome behaviour — no single file path to bind
// a language to.
//
// The implementation walks splitDiffHunks once. Pre-header lines emit
// verbatim (unchanged from the inline renderer before the cycle). For
// each hunk, the @@ line is always emitted (with an additional "+N"
// suffix when folded). When the hunk is folded the body lines are
// dropped entirely; when expanded they are emitted with diffInlineStyle
// per line.
//
// The cursor clamping is the caller's responsibility — the function
// here only renders. toggleDiffHunkFoldAtCursor re-clamps after a
// toggle so the cursor never lands on a missing row.
func (m *runMode) renderDiffBody(pre []string, hunks []diffHunk, langFile string) ([]string, []diffFoldedHunkLayout) {
	style := func(l string) string {
		if langFile != "" {
			return renderDiffInlineLine(langFile, l)
		}
		return diffInlineStyle(l)
	}
	var rows []string
	var layout []diffFoldedHunkLayout
	for _, l := range pre {
		rows = append(rows, "  "+style(l))
	}
	for i, h := range hunks {
		folded := false
		if m.panel.diffFolded != nil {
			folded = m.panel.diffFolded[i]
		}
		if folded {
			// One row: header + " +N" hint.
			rows = append(rows, "  "+style(h.header)+" "+diffHunkFoldHint(h))
			layout = append(layout, diffFoldedHunkLayout{startRow: len(rows) - 1, rowCount: 1})
		} else {
			headerRow := len(rows)
			rows = append(rows, "  "+style(h.header))
			for _, l := range h.body {
				rows = append(rows, "  "+style(l))
			}
			layout = append(layout, diffFoldedHunkLayout{startRow: headerRow, rowCount: 1 + len(h.body)})
		}
	}
	return rows, layout
}

// renderDiffFromBody is the top-level entry point used by the
// renderer: takes the raw git-diff body and returns the rendered
// rows + hunk layout in a single call. Splits once, renders once.
// langFile is forwarded to renderDiffBody — see that doc comment
// for the file-vs-commit-view split.
func (m *runMode) renderDiffFromBody(body []string, langFile string) ([]string, []diffFoldedHunkLayout) {
	pre, hunks := splitDiffHunks(body)
	return m.renderDiffBody(pre, hunks, langFile)
}

// hunkIdxForRow looks up the hunk whose header occupies rendered row
// `row`. Returns (hunkIdx, true) when row is the @@ header of some
// hunk, (-1, false) when row is a body line or out of range. Used
// by toggleDiffHunkFoldAtCursor to honour the "f on a non-hunk line
// is a no-op" rule.
func hunkIdxForRow(layout []diffFoldedHunkLayout, row int) (int, bool) {
	if row < 0 {
		return -1, false
	}
	for i, lay := range layout {
		if row == lay.startRow {
			return i, true
		}
		if row > lay.startRow && row < lay.startRow+lay.rowCount {
			// Body line of hunk i; cursor is not on a header.
			return -1, false
		}
	}
	return -1, false
}

// toggleDiffHunkFoldAtCursor toggles m.panel.diffFolded[i] for the
// hunk whose @@ header sits at the cursor's row. Re-clamps the
// cursor onto the same hunk's header after the toggle so the
// "cursor stays on the same header" rule holds both when
// fold→unfold and when unfold→fold.
//
// Returns true on a successful toggle so the caller (the f
// binding) can decide whether to re-render; false leaves state
// untouched (cursor on a non-hunk line, or no hunks at all).
//
// The function recomputes the hunk layout twice — once with the
// current fold state to find the hunk under the cursor, then again
// with the new state so the post-toggle cursor row can be derived
// from hunk i's new startRow. Both passes split the body once;
// the cost over a few-dozen-line diff is negligible.
func (m *runMode) toggleDiffHunkFoldAtCursor() bool {
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, diffViewKind(m))
	pre, hunks := splitDiffHunks(body)
	if len(hunks) == 0 {
		return false
	}
	// Pass 1: layout under the current fold state.
	_, layout := m.renderDiffBody(pre, hunks, "")
	idx, ok := hunkIdxForRow(layout, m.panel.diffInlineRow)
	if !ok {
		return false
	}
	if m.panel.diffFolded == nil {
		m.panel.diffFolded = make(map[int]bool)
	}
	m.panel.diffFolded[idx] = !m.panel.diffFolded[idx]
	// Pass 2: layout under the new fold state — set the cursor to
	// hunk idx's header row.
	_, layout2 := m.renderDiffBody(pre, hunks, "")
	m.panel.diffInlineRow = layout2[idx].startRow
	return true
}

// diffViewKind maps m.panel.diffView (1 = file, 2 = commit) to the
// "file" / "commit" literal expected by gitDiffBody. Centralising
// the mapping here keeps the f-binding free of switch statements
// and guarantees the renderer and the toggler use the same git
// command.
func diffViewKind(m *runMode) string {
	switch m.panel.diffView {
	case 2:
		return "commit"
	default:
		// 0 ought not to call panelDiffInlineLines, but the inline
		// renderer's `default` case is also "file" — both safe.
		return "file"
	}
}

// diffInlineRowMax returns the last valid value for
// m.panel.diffInlineRow given the current fold state and the
// most-recent body. Returns -1 when the body is empty. The f
// binding uses this to clamp after a toggle, and the
// j/k/Up/Down bindings to clamp after a move.
func (m *runMode) diffInlineRowMax() int {
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, diffViewKind(m))
	if len(body) == 0 {
		return -1
	}
	rows, _ := m.renderDiffFromBody(body, "")
	return len(rows) - 1
}

// moveDiffInlineCursor clamps m.panel.diffInlineRow into
// [0, maxRow] after applying delta; returns true on success.
// Returns false when the body is empty (maxRow < 0).
func (m *runMode) moveDiffInlineCursor(delta int) bool {
	maxRow := m.diffInlineRowMax()
	if maxRow < 0 {
		m.panel.diffInlineRow = 0
		return false
	}
	next := m.panel.diffInlineRow + delta
	if next < 0 {
		next = 0
	}
	if next > maxRow {
		next = maxRow
	}
	m.panel.diffInlineRow = next
	return true
}
