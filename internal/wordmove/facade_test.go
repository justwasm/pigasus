package wordmove_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/wordmove"
)

func TestWordmoveFacade(t *testing.T) {
	got := wordmove.BoundaryFwd("hello world", 0)
	if got != 5 {
		t.Fatalf("expected 5, got %d", got)
	}
}
