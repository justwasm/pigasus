package subagentrouter_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/subagentrouter"
)

func TestSubagentRouterFacade(t *testing.T) {
	mgr := subagentrouter.NewManager()
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}

	name, msg, ok := subagentrouter.ParsePrompt("@helper do this")
	if !ok || name != "helper" || msg != "do this" {
		t.Fatalf("unexpected parse: ok=%v name=%q msg=%q", ok, name, msg)
	}
}
