package browsersubagent_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/browsersubagent"
)

func TestBrowserSubagentFacade(t *testing.T) {
	sub := browsersubagent.Subagent()
	if sub.Name == "" {
		t.Fatal("expected non-empty subagent name")
	}
	if browsersubagent.ToolName != "browser_subagent" {
		t.Fatalf("unexpected ToolName: %q", browsersubagent.ToolName)
	}
}
