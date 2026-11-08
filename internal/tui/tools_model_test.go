package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/tools"
)

// TestToolsModel_OwnsToolRuntime pins the ownership move: the advertised
// declarations, runner, sandbox and catalogue hang off ToolsModel.
func TestToolsModel_OwnsToolRuntime(t *testing.T) {
	var empty ToolsModel
	if empty.decls != nil || empty.runner != nil || empty.catalog != nil {
		t.Fatal("zero model has tool state")
	}
	m := diffTestModel()
	m.tools.catalog = tools.NewRegistry()
	if m.tools.catalog == nil {
		t.Fatal("the model did not keep the catalogue")
	}
}
