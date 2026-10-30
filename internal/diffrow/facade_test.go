package diffrow_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/diffrow"
)

func TestDiffrowFacade(t *testing.T) {
	if diffrow.Add != '+' {
		t.Fatalf("unexpected Add: %c", diffrow.Add)
	}
	m := diffrow.Marker(diffrow.Add)
	if m == "" {
		t.Fatal("expected non-empty Marker")
	}
}
