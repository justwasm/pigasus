package subagentquestions_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/subagentquestions"
)

func TestSubagentQuestionsFacade(t *testing.T) {
	mgr := subagentquestions.NewManager(true)
	if mgr == nil {
		t.Fatal("expected non-nil manager")
	}
	if subagentquestions.SkipAnswer != "(skipped)" {
		t.Fatalf("unexpected SkipAnswer: %q", subagentquestions.SkipAnswer)
	}
}
