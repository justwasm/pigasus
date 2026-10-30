package rehydration_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/rehydration"
)

func TestRehydrationFacade(t *testing.T) {
	if rehydration.StepToolCall != 132 {
		t.Fatalf("unexpected StepToolCall: %d", rehydration.StepToolCall)
	}
	d := rehydration.New()
	if d == nil {
		t.Fatal("expected non-nil dispatcher")
	}
}
