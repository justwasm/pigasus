// undocumented_rows_test.go — the /model picker's Sonar row.
//
// The row is the picker's view of catalog's undocumented table: present only
// while the gate is open, appended after every row the reference itself draws,
// and carrying no effort slider.
package modelpicker_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

// gate puts the undocumented-model switch in a known state.
func gate(t *testing.T, env string) {
	t.Helper()
	configtest.Isolate(t)
	t.Setenv("OPENAGY_UNDOCUMENTED_MODELS", env)
}

func TestPickerHidesTheSonarRowOnAStockRun(t *testing.T) {
	gate(t, "off")
	if rows := modelpicker.RowsUndocumented(); len(rows) != 0 {
		t.Fatalf("RowsUndocumented() = %+v, want none", rows)
	}
	for _, r := range modelpicker.RowsView() {
		if r.Slug == catalog.SonarModelID {
			t.Fatalf("RowsView() carries %s with the gate shut", catalog.SonarModelID)
		}
	}
}

func TestPickerShowsTheSonarRowWhenTheGateIsOpen(t *testing.T) {
	gate(t, "on")
	rows := modelpicker.RowsUndocumented()
	if len(rows) != 1 {
		t.Fatalf("RowsUndocumented() = %d rows, want 1", len(rows))
	}
	if rows[0].Slug != catalog.SonarModelID || rows[0].Display != catalog.SonarModelLabel {
		t.Errorf("RowsUndocumented()[0] = %+v, want %s / %s", rows[0], catalog.SonarModelID, catalog.SonarModelLabel)
	}
	if rows[0].HasEffortSlider() {
		t.Error("the Sonar row has an effort slider; the model publishes no tier")
	}
	if got := rows[0].CatalogueID(0); got != catalog.SonarModelID {
		t.Errorf("Row.CatalogueID(0) = %q, want %q", got, catalog.SonarModelID)
	}

	view := modelpicker.RowsView()
	if len(view) != len(modelpicker.Rows)+1 {
		t.Fatalf("RowsView() = %d rows, want %d", len(view), len(modelpicker.Rows)+1)
	}
	// The reference's own rows keep their indices: the conditional row is
	// appended, never interleaved.
	for i := range modelpicker.Rows {
		if view[i].Slug != modelpicker.Rows[i].Slug {
			t.Fatalf("RowsView()[%d] = %q, want the reference row %q", i, view[i].Slug, modelpicker.Rows[i].Slug)
		}
	}
	last := view[len(view)-1]
	if last.Slug != catalog.SonarModelID {
		t.Errorf("the last row is %q, want %q", last.Slug, catalog.SonarModelID)
	}
}

func TestPickerSonarRowIsSearchable(t *testing.T) {
	gate(t, "on")
	found := false
	for _, r := range modelpicker.RowsFiltered("sonar") {
		if r.Slug == catalog.SonarModelID {
			found = true
		}
	}
	if !found {
		t.Error("searching \"sonar\" does not reach the Sonar row")
	}
}
