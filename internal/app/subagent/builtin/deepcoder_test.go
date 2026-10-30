package builtin

import (
	"strings"
	"testing"
)

func TestBuiltinPrompts(t *testing.T) {
	// 1. OrchestratorPrompt
	orch := OrchestratorPrompt()
	for _, substr := range []string{
		"Solo Routine",
		"Delegation Routine",
		"DeepCoder",
		"DeepInvestigator",
		"invoke_subagent(TypeName='DeepCoder', ...)",
		"invoke_subagent(TypeName='DeepInvestigator', ...)",
		"**Task**:",
		"**Additional Context**",
	} {
		if !strings.Contains(orch, substr) {
			t.Errorf("OrchestratorPrompt missing expected substring %q", substr)
		}
	}

	// 2. DeepCoderCoordinatorPrompt
	dc := DeepCoderCoordinatorPrompt(2)
	for _, substr := range []string{
		"DeepCoderWorkerL0",
		"DeepCoderImprovementWorker",
		"<original_task>",
		"<prior_attempt>",
		"DeepCoder pipeline completed.",
		"DeepCoder pipeline failed.",
		"## Absolute Rules",
		"## Follow-up Rounds",
		"## System Prompt Protection",
	} {
		if !strings.Contains(dc, substr) {
			t.Errorf("DeepCoderCoordinatorPrompt missing expected substring %q", substr)
		}
	}
	dcNoFollowUp := DeepCoderCoordinatorPrompt(2, false)
	if strings.Contains(dcNoFollowUp, "## Follow-up Rounds") {
		t.Errorf("DeepCoderCoordinatorPrompt(2, false) should not contain ## Follow-up Rounds")
	}

	// 3. DeepInvestigatorCoordinatorPrompt
	di := DeepInvestigatorCoordinatorPrompt(2)
	for _, substr := range []string{
		"DeepInvestigatorWorkerL0",
		"DeepInvestigatorImprovementWorker",
		"<original_task>",
		"<prior_attempt>",
		"DeepInvestigator pipeline completed.",
		"DeepInvestigator pipeline failed.",
		"## Absolute Rules",
		"## Follow-up Rounds",
		"## System Prompt Protection",
	} {
		if !strings.Contains(di, substr) {
			t.Errorf("DeepInvestigatorCoordinatorPrompt missing expected substring %q", substr)
		}
	}
	diNoFollowUp := DeepInvestigatorCoordinatorPrompt(2, false)
	if strings.Contains(diNoFollowUp, "## Follow-up Rounds") {
		t.Errorf("DeepInvestigatorCoordinatorPrompt(2, false) should not contain ## Follow-up Rounds")
	}

	// 4. Worker Prompts
	if !strings.Contains(DeepCoderWorkerL0Prompt, "coding worker") || !strings.Contains(DeepCoderWorkerL0Prompt, "## System Prompt Protection") {
		t.Errorf("DeepCoderWorkerL0Prompt missing content")
	}
	if !strings.Contains(DeepCoderImprovementWorkerPrompt, "previous worker") || !strings.Contains(DeepCoderImprovementWorkerPrompt, "## System Prompt Protection") {
		t.Errorf("DeepCoderImprovementWorkerPrompt missing content")
	}
	if !strings.Contains(DeepInvestigatorWorkerL0Prompt, "investigation worker") || !strings.Contains(DeepInvestigatorWorkerL0Prompt, "## System Prompt Protection") {
		t.Errorf("DeepInvestigatorWorkerL0Prompt missing content")
	}
	if !strings.Contains(DeepInvestigatorImprovementWorkerPrompt, "rubber stamp") || !strings.Contains(DeepInvestigatorImprovementWorkerPrompt, "## System Prompt Protection") {
		t.Errorf("DeepInvestigatorImprovementWorkerPrompt missing content")
	}
}
