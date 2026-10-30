package termcolor_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/termcolor"
)

func TestTermcolorFacade(t *testing.T) {
	got := termcolor.StripSGR("\x1b[31mhello\x1b[0m")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}
