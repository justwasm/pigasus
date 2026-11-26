package tasks

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestUpdateSteps_RunAndParse(t *testing.T) {
	tool := &UpdateSteps{}
	if tool.Name() != "update_steps" {
		t.Fatalf("Name() = %q, want update_steps", tool.Name())
	}
	decl := UpdateStepsDecl()
	if decl.Name != "update_steps" || !strings.Contains(decl.Description, "live checklist") {
		t.Fatalf("unexpected decl: %+v", decl)
	}

	args := json.RawMessage(`{
		"steps": [
			{"title": "Inspect codebase", "status": "done"},
			{"title": "Implement feature", "status": "in_progress"},
			{"title": "Skip legacy check", "status": "skipped"},
			{"title": "Handle failure", "status": "failed"},
			{"title": "Run test suite", "status": "pending"}
		]
	}`)
	out, err := tool.Run(context.Background(), args)
	if err != nil {
		t.Fatalf("Run failed: %v", err)
	}
	md, ok := out.(string)
	if !ok {
		t.Fatalf("expected string output, got %T", out)
	}
	want := strings.Join([]string{
		"- [x] Inspect codebase",
		"- [>] Implement feature",
		"- [-] Skip legacy check",
		"- [!] Handle failure",
		"- [ ] Run test suite",
	}, "\n")
	if md != want {
		t.Fatalf("markdown mismatch:\ngot:\n%s\nwant:\n%s", md, want)
	}

	parsed := ParseChecklistMarkdown(md)
	if len(parsed) != 5 {
		t.Fatalf("parsed len = %d, want 5", len(parsed))
	}
	if parsed[0].Status != StepStatusDone || parsed[1].Status != StepStatusInProgress ||
		parsed[2].Status != StepStatusSkipped || parsed[3].Status != StepStatusFailed ||
		parsed[4].Status != StepStatusPending {
		t.Fatalf("unexpected parsed statuses: %+v", parsed)
	}
}

func TestUpdateSteps_ValidationErrors(t *testing.T) {
	tool := &UpdateSteps{}

	// Empty title
	_, err := tool.Run(context.Background(), json.RawMessage(`{"steps":[{"title":"  ","status":"pending"}]}`))
	if err == nil || !strings.Contains(err.Error(), "step 1 has an empty title") {
		t.Fatalf("expected empty title error, got %v", err)
	}

	// Invalid status
	_, err = tool.Run(context.Background(), json.RawMessage(`{"steps":[{"title":"Do work","status":"bogus"}]}`))
	if err == nil || !strings.Contains(err.Error(), `step 1 has invalid status "bogus"; use pending, in_progress, done, skipped or failed`) {
		t.Fatalf("expected invalid status error, got %v", err)
	}

	// Too many steps (> 12)
	var items []StepItem
	for i := 0; i < 13; i++ {
		items = append(items, StepItem{Title: "Step", Status: StepStatusPending})
	}
	raw, _ := json.Marshal(map[string]any{"steps": items})
	_, err = tool.Run(context.Background(), raw)
	if err == nil || !strings.Contains(err.Error(), "at most 12") {
		t.Fatalf("expected maxItems error, got %v", err)
	}
}
