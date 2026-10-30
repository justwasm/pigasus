package tuiviewport_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/tuiviewport"
)

func TestTuiviewportFacade(t *testing.T) {
	if tuiviewport.ViewOverflowRows != 8 {
		t.Fatalf("unexpected ViewOverflowRows: %d", tuiviewport.ViewOverflowRows)
	}
	sr, sc, er, ec, active := tuiviewport.SelectionRange(0, 0, 0, 0, false)
	if active {
		t.Errorf("expected inactive for zeros, got %d,%d to %d,%d", sr, sc, er, ec)
	}
}
