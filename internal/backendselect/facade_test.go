package backendselect_test

import (
	"io"
	"testing"

	"github.com/justwasm/pigasus/internal/backendselect"
)

func TestBackendSelectFacade(t *testing.T) {
	norm := backendselect.Normalise("gateway", io.Discard)
	if norm != "gateway" {
		t.Fatalf("unexpected Normalise: %s", norm)
	}
}
