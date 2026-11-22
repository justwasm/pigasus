package actor

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

func TestTrajectory_TierColdHistory(t *testing.T) {
	traj := NewTrajectory()
	coldDir := filepath.Join(t.TempDir(), "cold_storage")

	// 1. Create 3 turns with tools and outputs
	for i := 1; i <= 3; i++ {
		tr := &TurnRecord{
			ID:        fmt.Sprintf("turn_%d", i),
			Input:     fmt.Sprintf("User prompt %d", i),
			CreatedAt: time.Now(),
			Steps: []Step{
				{
					Index:       0,
					GlobalIndex: i - 1,
					Delta:       fmt.Sprintf("Assistant step %d", i),
					ToolCalls: []ToolCall{
						{ID: fmt.Sprintf("call_%d", i), Name: "read_file", Input: `{"path":"large.txt"}`},
					},
					ToolResults: []ToolResult{
						{
							ToolCallID: fmt.Sprintf("call_%d", i),
							Output:     fmt.Sprintf("Large payload data from file for turn %d: %s", i, "abcdefghijklmnopqrstuvwxyz1234567890"),
						},
					},
				},
			},
			FinalOutput: fmt.Sprintf("Final answer %d", i),
			Status:      "done",
		}
		traj.AddTurn(tr)
	}

	if traj.TotalSteps() != 3 {
		t.Fatalf("expected 3 steps, got %d", traj.TotalSteps())
	}

	// 2. Compact steps 0 and 1 via Checkpoint (IncludedThroughStep = 1)
	cp := Checkpoint{
		Index:               1,
		IncludedThroughStep: 1,
		ResumeText:          "Summary of turns 1 and 2",
		Strategy:            "compaction",
	}
	traj.recordCheckpoint(cp)

	// Save baseline conversation messages before tiering
	msgsBefore := traj.ToConversationMessages()

	// 3. Run cold tiering
	prunedSteps, freedBytes, err := traj.TierColdHistory(coldDir)
	if err != nil {
		t.Fatalf("TierColdHistory failed: %v", err)
	}
	if prunedSteps != 2 {
		t.Errorf("expected 2 pruned steps, got %d", prunedSteps)
	}
	if freedBytes <= 0 {
		t.Errorf("expected positive freed bytes, got %d", freedBytes)
	}

	// 4. Verify in-memory state:
	// Steps 0 and 1 should have empty Output and ColdStored = true
	turns := traj.Turns()
	if !turns[0].Steps[0].ToolResults[0].ColdStored || turns[0].Steps[0].ToolResults[0].Output != "" {
		t.Errorf("expected turn 0 step 0 to be cold stored with empty in-memory output")
	}
	if !turns[1].Steps[0].ToolResults[0].ColdStored || turns[1].Steps[0].ToolResults[0].Output != "" {
		t.Errorf("expected turn 1 step 0 to be cold stored with empty in-memory output")
	}

	// Step 2 (after checkpoint boundary) must NOT be cold stored and must retain Output
	if turns[2].Steps[0].ToolResults[0].ColdStored || turns[2].Steps[0].ToolResults[0].Output == "" {
		t.Errorf("expected active turn 2 to remain in memory with non-empty output")
	}

	// 5. Verify on-demand hydration via LoadColdStepResults
	recovered, err := LoadColdStepResults(coldDir, 0)
	if err != nil {
		t.Fatalf("LoadColdStepResults failed: %v", err)
	}
	if len(recovered) != 1 {
		t.Fatalf("expected 1 recovered tool result, got %d", len(recovered))
	}
	wantOutput := "Large payload data from file for turn 1: abcdefghijklmnopqrstuvwxyz1234567890"
	if recovered[0].Output != wantOutput {
		t.Errorf("recovered output mismatch:\n got: %q\nwant: %q", recovered[0].Output, wantOutput)
	}

	// 6. Verify ToConversationMessages is 100% identical before and after cold storage
	msgsAfter := traj.ToConversationMessages()
	if len(msgsAfter) != len(msgsBefore) {
		t.Fatalf("message count changed: before=%d after=%d", len(msgsBefore), len(msgsAfter))
	}
	for i := range msgsBefore {
		if msgsBefore[i].Content != msgsAfter[i].Content || msgsBefore[i].Role != msgsAfter[i].Role {
			t.Errorf("msg %d mismatch:\n before: %+v\n  after: %+v", i, msgsBefore[i], msgsAfter[i])
		}
	}
}
