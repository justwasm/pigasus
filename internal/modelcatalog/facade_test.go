package modelcatalog_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/modelcatalog"
)

func TestModelCatalogFacade(t *testing.T) {
	if modelcatalog.DefaultModel != "Gemini 3.8 Flash (High)" {
		t.Fatalf("unexpected DefaultModel: %q", modelcatalog.DefaultModel)
	}
	label := modelcatalog.FormatModelLabel("gemini-3.8-flash")
	if label == "" {
		t.Fatal("expected non-empty label")
	}
}
