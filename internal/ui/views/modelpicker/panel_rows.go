// panel_rows.go — the /model panel's rows.
//
// The row model itself lives in modelpicker.go; this file holds the
// panel's own view of it: the rows the search box leaves, the row the
// cursor is on and the slider position each row shows.

package modelpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/models"
)

// Rows returns the picker rows the panel is showing right now: the full
// list while the search box is empty, the filtered list after that.
//
// Every cursor-indexed helper (the renderer, the effort slider, the
// selection) goes through it, so the cursor always points at a row that
// is on screen.
func (p *Panel) Rows() []Row {
	if p == nil {
		return nil
	}
	if p.seams.CustomRows != nil {
		if custom := p.seams.CustomRows(); custom != nil {
			return FilterRows(custom, p.query)
		}
	}
	return RowsFiltered(p.query)
}

// RowCount is the number of rows the cursor moves over.
//
// The count follows the search filter, so it shrinks with the query.
func (p *Panel) RowCount() int {
	return len(p.Rows())
}

// currentRow returns the picker row the cursor is on.
func (p *Panel) currentRow() (Row, bool) {
	rows := p.Rows()
	idx := p.cursor
	if idx < 0 || idx >= len(rows) {
		return Row{}, false
	}
	return rows[idx], true
}

// effortCursorFor returns the slider index a row shows.
//
// The running model's own tier wins when it belongs to the row: the
// reference's slider parks on the tier the session runs at. A row that
// is not the running model parks on its own default — the reference's
// default is high.
//
// The index is into *that row's* tier list, so a row with five tiers and
// a row with three never share a number.
func (p *Panel) effortCursorFor(r Row) int {
	if len(r.Tiers) <= 1 {
		return 0
	}
	current := p.currentModelLabel()
	curSlug := ResolveCatalogueID(current)
	if curSlug == "" {
		curSlug = strings.TrimSpace(current)
	}
	if curSlug != "" && (models.BaseModelID(curSlug) == models.BaseModelID(r.Slug) || curSlug == r.Slug) {
		if i := r.TierIndex(models.TierFromLabel(current)); i >= 0 {
			return i
		}
	}
	if i := r.TierIndex(r.DefaultTier); i >= 0 {
		return i
	}
	return len(r.Tiers) - 1
}

// seedEffort parks the effort slider on the tier the session's model
// runs at.
//
// The slider's position is an index into the cursor row's own tier list,
// so it is seeded from the row the cursor lands on: the running model's
// tier when the running model *is* that row, the row's own default
// otherwise.
func (p *Panel) seedEffort() {
	row, ok := p.currentRow()
	if !ok {
		return
	}
	p.effortCursor = p.effortCursorFor(row)
}

// setCursor moves the cursor and re-seeds the effort slider for the row
// it lands on.
//
// The re-seed matters because the slider's position is an index into the
// row's tier list: the cursor leaving the three-tier Gemini row for a
// configured model's five tiers has to move the slider too, or the
// marker would sit on the wrong tier.
//
// A row without a slider leaves the position alone — the slider is not
// on screen there, and the reference keeps the position so that coming
// back to the row restores it.
func (p *Panel) setCursor(idx int) {
	p.cursor = idx
	row, ok := p.currentRow()
	if !ok || !row.HasEffortSlider() {
		return
	}
	p.effortCursor = p.effortCursorFor(row)
}
