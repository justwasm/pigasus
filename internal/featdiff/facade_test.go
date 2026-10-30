package featdiff_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/featdiff"
)

func TestFeatdiffFacade(t *testing.T) {
	if featdiff.Aligned != "aligned" {
		t.Fatalf("unexpected Aligned: %s", featdiff.Aligned)
	}
	p := featdiff.PanelTitle()
	if p == "" {
		t.Fatal("expected non-empty PanelTitle")
	}
}
