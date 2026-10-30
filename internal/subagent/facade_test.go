package subagent_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/subagent"
)

func TestSubagentFacade(t *testing.T) {
	mgr := subagent.NewManager()
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	s := subagent.NewSubagent("test")
	if s == nil || s.Name != "test" {
		t.Fatalf("unexpected subagent: %v", s)
	}
}
