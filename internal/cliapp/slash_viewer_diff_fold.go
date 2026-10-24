// slash_viewer_diff_fold.go — hunk-folding primitives for the /diff file panel
// (cycle diff-folding / AGY 1.3.5).
package cliapp

import (
	"github.com/justwasm/pigasus/internal/difffold"
	"github.com/justwasm/pigasus/internal/diffsyntax"
)

// diffHunk describes one hunk of a git diff body: the "@@ -a,b +c,d @@"
// header line and the lines that follow until the next "@@" or EOF.
type diffHunk = difffold.Hunk

// diffHunkCount returns the number of body lines for hunk i after splitDiffHunks.
func diffHunkCount(h diffHunk) int { return difffold.Count(h) }

// splitDiffHunks partitions a git diff body into pre-header lines and hunks.
func splitDiffHunks(body []string) (pre []string, hunks []diffHunk) {
	return difffold.SplitHunks(body)
}

// diffFoldedHunkLayout describes the rendered footprint of one hunk.
type diffFoldedHunkLayout = difffold.Layout

// renderDiffBody emits the styled rendered rows for one git diff body.
func (m *runMode) renderDiffBody(pre []string, hunks []diffHunk, langFile string) ([]string, []diffFoldedHunkLayout) {
	style := func(l string) string {
		if langFile != "" {
			return diffsyntax.RenderInlineLine(langFile, l)
		}
		return diffInlineStyle(l)
	}
	return difffold.RenderBody(pre, hunks, m.panel.diffFolded, style, cDimGrayOpen, cDimGrayClose)
}

// renderDiffFromBody is the top-level entry point used by the renderer.
func (m *runMode) renderDiffFromBody(body []string, langFile string) ([]string, []diffFoldedHunkLayout) {
	pre, hunks := splitDiffHunks(body)
	return m.renderDiffBody(pre, hunks, langFile)
}

// hunkIdxForRow looks up the hunk whose header occupies rendered row `row`.
func hunkIdxForRow(layout []diffFoldedHunkLayout, row int) (int, bool) {
	return difffold.HunkIdxForRow(layout, row)
}

// toggleDiffHunkFoldAtCursor toggles m.panel.diffFolded[i] for the hunk at cursor.
func (m *runMode) toggleDiffHunkFoldAtCursor() bool {
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, diffViewKind(m))
	pre, hunks := splitDiffHunks(body)
	if len(hunks) == 0 {
		return false
	}
	_, layout := m.renderDiffBody(pre, hunks, "")
	idx, ok := hunkIdxForRow(layout, m.panel.diffInlineRow)
	if !ok {
		return false
	}
	if m.panel.diffFolded == nil {
		m.panel.diffFolded = make(map[int]bool)
	}
	m.panel.diffFolded[idx] = !m.panel.diffFolded[idx]
	_, layout2 := m.renderDiffBody(pre, hunks, "")
	m.panel.diffInlineRow = layout2[idx].StartRow
	return true
}

// diffViewKind maps m.panel.diffView to "file" or "commit".
func diffViewKind(m *runMode) string {
	switch m.panel.diffView {
	case 2:
		return "commit"
	default:
		return "file"
	}
}

// diffInlineRowMax returns the last valid value for m.panel.diffInlineRow.
func (m *runMode) diffInlineRowMax() int {
	body := gitDiffBody(m.cwd, m.panel.diffViewPath, diffViewKind(m))
	if len(body) == 0 {
		return -1
	}
	rows, _ := m.renderDiffFromBody(body, "")
	return len(rows) - 1
}

// moveDiffInlineCursor clamps m.panel.diffInlineRow into [0, maxRow] after delta.
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
