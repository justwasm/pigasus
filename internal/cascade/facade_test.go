package cascade_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/cascade"
)

func TestCascadeFacade(t *testing.T) {
	st := cascade.NewState()
	if st == nil {
		t.Fatal("expected non-nil State")
	}

	cleared := st.ClearApprovalsBySubagent("non-existent")
	if len(cleared) != 0 {
		t.Fatalf("expected empty cleared approvals, got %v", cleared)
	}
}
