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

func TestTrajectory_200Turns_SSTChunkTiering(t *testing.T) {
	traj := NewTrajectory()
	coldDir := filepath.Join(t.TempDir(), "cold_sst")
	traj.SetColdDir(coldDir)

	const totalTurns = 220
	// 1. Populate 220 turns
	for i := 0; i < totalTurns; i++ {
		turnID := fmt.Sprintf("turn_%d", i)
		tr := &TurnRecord{
			ID:        turnID,
			Input:     fmt.Sprintf("User prompt for turn %d", i),
			CreatedAt: time.Now(),
			Steps: []Step{
				{
					Index:       0,
					GlobalIndex: i * 2,
					Delta:       fmt.Sprintf("Assistant thinking turn %d", i),
					ToolCalls: []ToolCall{
						{ID: fmt.Sprintf("call_%d", i), Name: "run_command", Input: fmt.Sprintf(`{"cmd":"task_%d"}`, i)},
					},
					ToolResults: []ToolResult{
						{
							ToolCallID: fmt.Sprintf("call_%d", i),
							Output:     fmt.Sprintf("Output of task_%d: done with exit code 0", i),
						},
					},
				},
				{
					Index:       1,
					GlobalIndex: i*2 + 1,
					Delta:       fmt.Sprintf("Assistant concluding turn %d", i),
				},
			},
			FinalOutput: fmt.Sprintf("Final conclusion for turn %d", i),
			Status:      "done",
			Usage: TokenUsage{
				PromptTokens: 500 + i*10,
				OutputTokens: 100,
				TotalTokens:  600 + i*10,
			},
		}
		traj.AddTurn(tr)
	}

	if traj.TotalTurns() != totalTurns {
		t.Fatalf("expected %d turns, got %d", totalTurns, traj.TotalTurns())
	}
	expectedTotalSteps := totalTurns * 2
	if traj.TotalSteps() != expectedTotalSteps {
		t.Fatalf("expected %d total steps, got %d", expectedTotalSteps, traj.TotalSteps())
	}

	// 2. Add checkpoint at turn 180 (included through step 361)
	cp := Checkpoint{
		Index:               1,
		IncludedThroughStep: 180*2 + 1, // step 361
		TurnIndex:           180,
		ResumeText:          "Compaction checkpoint: successfully completed turns 0 through 180.",
		Strategy:            "compaction",
	}
	traj.recordCheckpoint(cp)

	// Save baseline conversation messages before cold tiering
	msgsBefore := traj.ToConversationMessages()

	// 3. Perform asynchronous SST cold tiering, retaining last 20 active turns in sliding window
	errCh := traj.TierColdTurnsAsync(coldDir, 20)
	if err := <-errCh; err != nil {
		t.Fatalf("TierColdTurnsAsync failed: %v", err)
	}

	// 4. Verify SST chunks created
	chunks := traj.ColdChunks()
	if len(chunks) == 0 {
		t.Fatalf("expected at least 1 cold chunk, got 0")
	}
	chunk := chunks[0]
	if chunk.TurnCount != 181 { // turns 0..180
		t.Errorf("expected 181 tiered turns in chunk, got %d", chunk.TurnCount)
	}
	if chunk.Checksum == 0 {
		t.Errorf("expected non-zero CRC32 checksum on chunk")
	}

	// 5. Verify in-memory state:
	// Turns 0..180 should be ColdStored with empty Steps
	turns := traj.Turns()
	for i := 0; i <= 180; i++ {
		if !turns[i].ColdStored {
			t.Errorf("turn %d: expected ColdStored = true", i)
		}
		if len(turns[i].Steps) != 0 {
			t.Errorf("turn %d: expected empty Steps in memory, got %d", i, len(turns[i].Steps))
		}
		if turns[i].StepCount != 2 {
			t.Errorf("turn %d: expected StepCount = 2, got %d", i, turns[i].StepCount)
		}
	}

	// Active sliding window turns (181..219) must NOT be cold stored
	for i := 181; i < totalTurns; i++ {
		if turns[i].ColdStored {
			t.Errorf("active turn %d should not be cold stored", i)
		}
		if len(turns[i].Steps) != 2 {
			t.Errorf("active turn %d should retain 2 steps in memory, got %d", i, len(turns[i].Steps))
		}
	}

	// TotalSteps() must remain 440 even though historical turns are cold
	if traj.TotalSteps() != expectedTotalSteps {
		t.Errorf("TotalSteps() = %d after tiering, want %d", traj.TotalSteps(), expectedTotalSteps)
	}

	// 6. Test lazy loading of an arbitrary cold turn on demand
	lazyTurn, err := traj.TurnAt(50)
	if err != nil {
		t.Fatalf("TurnAt(50) failed: %v", err)
	}
	if lazyTurn.ID != "turn_50" {
		t.Errorf("lazy turn ID = %q, want turn_50", lazyTurn.ID)
	}
	if len(lazyTurn.Steps) != 2 {
		t.Fatalf("lazy turn steps = %d, want 2", len(lazyTurn.Steps))
	}
	if len(lazyTurn.Steps[0].ToolResults) == 0 {
		t.Fatalf("lazy turn step 0 missing tool results")
	}
	wantOutput := "Output of task_50: done with exit code 0"
	if lazyTurn.Steps[0].ToolResults[0].Output != wantOutput {
		t.Errorf("lazy turn tool output mismatch: got %q, want %q", lazyTurn.Steps[0].ToolResults[0].Output, wantOutput)
	}

	// 7. Verify ToConversationMessages parity (100% identical before and after cold storage)
	msgsAfter := traj.ToConversationMessages()
	if len(msgsAfter) != len(msgsBefore) {
		t.Fatalf("message count changed: before=%d after=%d", len(msgsBefore), len(msgsAfter))
	}
	for i := range msgsBefore {
		if msgsBefore[i].Content != msgsAfter[i].Content || msgsBefore[i].Role != msgsAfter[i].Role {
			t.Errorf("msg %d mismatch:\n before: %+v\n  after: %+v", i, msgsBefore[i], msgsAfter[i])
		}
	}

	// 8. Test full rehydration
	if err := traj.HydrateTurns(coldDir); err != nil {
		t.Fatalf("HydrateTurns failed: %v", err)
	}
	rehydratedTurns := traj.Turns()
	for i := 0; i < totalTurns; i++ {
		if rehydratedTurns[i].ColdStored {
			t.Errorf("turn %d still marked ColdStored after HydrateTurns", i)
		}
		if len(rehydratedTurns[i].Steps) != 2 {
			t.Errorf("turn %d has %d steps, want 2", i, len(rehydratedTurns[i].Steps))
		}
	}

	// 9. Test token reconciliation
	traj.ReconcileLatestTurnUsage(2600, 120, 1800)
	lastTurn := traj.LastTurn()
	if lastTurn.Usage.PromptTokens != 2600 || lastTurn.Usage.CachedTokens != 1800 {
		t.Errorf("token reconciliation failed: %+v", lastTurn.Usage)
	}
}
