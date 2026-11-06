package agentserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	subagent "github.com/justwasm/pigasus/internal/app/subagent/core"
	"github.com/justwasm/pigasus/internal/app/tools"
)

func TestFormatAgentsSummary(t *testing.T) {
	tmpDir := t.TempDir()

	// Case 1: Empty workspace
	summary := formatAgentsSummary(tmpDir)
	if !strings.Contains(summary, "Custom Agents") {
		t.Errorf("expected summary to contain 'Custom Agents', got: %s", summary)
	}
	if !strings.Contains(summary, "Subagents") {
		t.Errorf("expected summary to contain 'Subagents', got: %s", summary)
	}

	// Case 2: Custom agent in workspace (.agents/agents/test-agent.md)
	agentsDir := filepath.Join(tmpDir, ".agents", "agents")
	if err := os.MkdirAll(agentsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	agentContent := "---\nname: code-reviewer\ndescription: Specialized code reviewer agent\n---\nYou are a code reviewer."
	if err := os.WriteFile(filepath.Join(agentsDir, "reviewer.md"), []byte(agentContent), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// Case 3: Register a mock subagent
	mgr := tools.SharedSubagentManager()
	if mgr != nil {
		mockSub := &subagent.Subagent{
			ID:    "sub-acp-test-1",
			Name:  "worker-1",
			Title: "Refactor backend",
			State: "running",
		}
		mgr.Add(mockSub)
		defer mgr.Remove("sub-acp-test-1")
	}

	summaryWithAgents := formatAgentsSummary(tmpDir)
	if !strings.Contains(summaryWithAgents, "code-reviewer") {
		t.Errorf("expected summary to contain custom agent 'code-reviewer', got: %s", summaryWithAgents)
	}
	if !strings.Contains(summaryWithAgents, "Specialized code reviewer agent") {
		t.Errorf("expected summary to contain description, got: %s", summaryWithAgents)
	}
	if mgr != nil {
		if !strings.Contains(summaryWithAgents, "sub-acp-test-1") {
			t.Errorf("expected summary to contain 'sub-acp-test-1', got: %s", summaryWithAgents)
		}
		if !strings.Contains(summaryWithAgents, "worker-1") {
			t.Errorf("expected summary to contain 'worker-1', got: %s", summaryWithAgents)
		}
	}
}
