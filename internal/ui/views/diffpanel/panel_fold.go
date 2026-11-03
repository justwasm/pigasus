// panel_fold.go — the /diff panel's hunk folding.
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
// the header line plus a dim-grey `+N` line-count indicator. Pressing f
// again expands.
//
// The primitives themselves — splitting a body into hunks, rendering
// the folded body and looking a hunk up by its rendered row — live in
// internal/ui/views/diff/fold; the row styler lives in
// internal/ui/views/diff/action.

package diffpanel

import (
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
	difffold "github.com/justwasm/pigasus/internal/ui/views/diff/fold"
)

// Hunk describes one hunk of a git diff body: the "@@ -a,b +c,d @@"
// header line and the lines that follow until the next "@@" or EOF.
type Hunk = difffold.Hunk

// Layout describes the rendered footprint of one hunk.
type Layout = difffold.Layout

// splitHunks partitions a git diff body into pre-header lines and
// hunks.
func splitHunks(body []string) (pre []string, hunks []Hunk) {
	return difffold.SplitHunks(body)
}

// renderDiffBody emits the styled rendered rows for one git diff body.
//
// 【strings】 the reference's `/diff` file view carries "syntax
// highlighting ... and a background tint on added and removed lines"
// (internal/cli/testdata/changelog_reference.txt:127), so the rows are
// rendered in the reference's row model — the line number in the
// gutter, the marker column and the text — rather than as raw git
// output.
//
// The fold state is untouched: difffold.RenderBody still walks the
// hunks and drops a folded hunk's body, and the styler only sees the
// lines it hands over.
//
// The body rows carry two columns of their own (the line number and the
// marker), so the width the styler gets is the panel's width minus the
// two spaces difffold.RenderBody puts in front of every row.
func (p *Panel) renderDiffBody(pre []string, hunks []Hunk, langFile string) ([]string, []Layout) {
	styler := diffaction.NewRowStyler(langFile, p.width()-2, diffaction.MaxLineNumber(hunks))
	return difffold.RenderBody(pre, hunks, p.folded, styler.Style, dimGrayOpen, dimGrayClose)
}

// renderDiffFromBody is the top-level entry point used by the renderer.
func (p *Panel) renderDiffFromBody(body []string, langFile string) ([]string, []Layout) {
	pre, hunks := splitHunks(body)
	return p.renderDiffBody(pre, hunks, langFile)
}

// hunkIdxForRow looks up the hunk whose header occupies rendered row
// `row`.
func hunkIdxForRow(layout []Layout, row int) (int, bool) {
	return difffold.HunkIdxForRow(layout, row)
}

// ToggleFoldAtCursor toggles the fold state of the hunk at the
// cursor.
//
// It reports whether a hunk was toggled: a cursor that is not on a
// hunk header, or a body without any hunk, is a no-op.
func (p *Panel) ToggleFoldAtCursor() bool {
	if p == nil {
		return false
	}
	body := p.body(p.viewPath, p.viewKind())
	pre, hunks := splitHunks(body)
	if len(hunks) == 0 {
		return false
	}
	_, layout := p.renderDiffBody(pre, hunks, "")
	idx, ok := hunkIdxForRow(layout, p.inlineRow)
	if !ok {
		return false
	}
	if p.folded == nil {
		p.folded = make(map[int]bool)
	}
	p.folded[idx] = !p.folded[idx]
	_, layout2 := p.renderDiffBody(pre, hunks, "")
	p.inlineRow = layout2[idx].StartRow
	return true
}

// viewKind maps the active subview to "file" or "commit".
func (p *Panel) viewKind() string {
	switch p.view {
	case 2:
		return "commit"
	default:
		return "file"
	}
}

// inlineRowMax returns the last valid value for the inline subview's
// row cursor.
func (p *Panel) inlineRowMax() int {
	body := p.body(p.viewPath, p.viewKind())
	if len(body) == 0 {
		return -1
	}
	rows, _ := p.renderDiffFromBody(body, "")
	return len(rows) - 1
}

// moveInlineCursor clamps the inline subview's row cursor into
// [0, maxRow] after delta.
func (p *Panel) moveInlineCursor(delta int) bool {
	if p == nil {
		return false
	}
	maxRow := p.inlineRowMax()
	if maxRow < 0 {
		p.inlineRow = 0
		return false
	}
	next := p.inlineRow + delta
	if next < 0 {
		next = 0
	}
	if next > maxRow {
		next = maxRow
	}
	p.inlineRow = next
	return true
}

// clampInlineCursor keeps the inline subview's row cursor inside the
// rendered body.
func (p *Panel) clampInlineCursor(maxRow int) {
	if p == nil {
		return
	}
	if maxRow < 0 {
		p.inlineRow = 0
		return
	}
	if p.inlineRow < 0 {
		p.inlineRow = 0
	}
	if p.inlineRow > maxRow {
		p.inlineRow = maxRow
	}
}
