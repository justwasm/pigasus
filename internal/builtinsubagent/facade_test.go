package builtinsubagent_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/builtinsubagent"
)

func TestBuiltinSubagentFacade(t *testing.T) {
	reg := builtinsubagent.Default()
	if reg == nil {
		t.Fatal("expected non-nil default registry")
	}
}
