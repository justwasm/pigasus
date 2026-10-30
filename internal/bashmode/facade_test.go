package bashmode_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/bashmode"
)

func TestBashModeFacade(t *testing.T) {
	row := bashmode.HeadRow("ls", false, false)
	if row == "" {
		t.Fatal("expected non-empty head row")
	}
}
