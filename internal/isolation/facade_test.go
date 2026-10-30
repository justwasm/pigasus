package isolation_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/isolation"
)

func TestIsolationFacade(t *testing.T) {
	ws := isolation.New()
	if ws == nil {
		t.Fatal("expected non-nil workspace")
	}
}
