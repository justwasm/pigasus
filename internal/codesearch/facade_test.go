package codesearch_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/codesearch"
)

func TestCodeSearchFacade(t *testing.T) {
	if codesearch.MaxHits != 1000 {
		t.Fatalf("unexpected MaxHits: %d", codesearch.MaxHits)
	}
	r := codesearch.NewRunner("/tmp")
	if r == nil {
		t.Fatal("expected non-nil runner")
	}
}
