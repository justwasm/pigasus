package difffold_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/difffold"
)

func TestDifffoldFacade(t *testing.T) {
	h := difffold.Hunk{
		Header: "@@ -1,3 +1,3 @@",
		Body:   []string{"+line 1", "-line 2"},
	}
	if difffold.Count(h) != 2 {
		t.Fatalf("expected count 2, got %d", difffold.Count(h))
	}
}
