package diffaction_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/diffaction"
)

func TestDiffactionFacade(t *testing.T) {
	if len(diffaction.Views) != 3 {
		t.Fatalf("unexpected Views count: %d", len(diffaction.Views))
	}
	s := diffaction.StripANSI("\x1b[31mhello\x1b[0m")
	if s != "hello" {
		t.Fatalf("expected 'hello', got %q", s)
	}
}
