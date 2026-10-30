package customize_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/customize"
)

func TestCustomizeFacade(t *testing.T) {
	b := customize.DefaultBudget()
	trimmer := customize.NewTrimmer(b)
	if trimmer == nil {
		t.Fatal("expected non-nil trimmer")
	}
}
