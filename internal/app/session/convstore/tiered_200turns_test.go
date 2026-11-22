package convstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestTieredTrajectoryStore_200Turns_ColdStorage(t *testing.T) {
	tmpDir := t.TempDir()
	warmDir := filepath.Join(tmpDir, "warm")
	coldDir := filepath.Join(tmpDir, "cold")

	cfg := TieredStoreConfig{
		HotCapacity: 20, // Maintain sliding window of 20 active steps in RAM
		WarmDir:     warmDir,
		ColdDir:     coldDir,
		AutoEvict:   true,
	}

	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-200-turns-test"
	const totalTurns = 200
	const stepsPerTurn = 2
	const totalSteps = totalTurns * stepsPerTurn // 400 steps

	// Append 200 turns (400 steps)
	for i := int64(0); i < totalSteps; i++ {
		var st Step
		if i%2 == 0 {
			// User input
			st = Step{
				Idx:    i,
				Type:   StepUserInput,
				Status: 3,
				Text:   fmt.Sprintf("User message turn %d", i/2),
			}
		} else {
			// Model response or tool
			if i%4 == 1 {
				st = Step{
					Idx:        i,
					Type:       StepToolCall,
					Status:     3,
					ToolName:   "run_command",
					ToolArgs:   fmt.Sprintf(`{"cmd":"task_%d"}`, i),
					ToolResult: fmt.Sprintf("Command result output %d", i),
				}
			} else {
				st = Step{
					Idx:    i,
					Type:   StepModelResponse,
					Status: 3,
					Text:   fmt.Sprintf("Model conclusion turn %d", i/2),
				}
			}
		}

		if err := store.AppendStep(convID, st); err != nil {
			t.Fatalf("AppendStep %d failed: %v", i, err)
		}

		// At step 200, simulate compaction boundary: archive steps 0..160 into cold storage
		if i == 200 {
			archive, err := store.ArchiveCold(convID, 160)
			if err != nil {
				t.Fatalf("ArchiveCold at step 200 failed: %v", err)
			}
			if archive == nil || archive.StepCount == 0 {
				t.Fatalf("expected non-empty archive at step 200")
			}
		}

		// At step 350, archive steps 160..300 via SST chunk
		if i == 350 {
			sstMeta, err := store.ArchiveColdSST(convID, 300)
			if err != nil {
				t.Fatalf("ArchiveColdSST at step 350 failed: %v", err)
			}
			if sstMeta == nil || sstMeta.StepCount == 0 {
				t.Fatalf("expected non-empty sst chunk at step 350")
			}
		}
	}

	// 1. Verify TierStats: hot tier in RAM should strictly satisfy HotCapacity
	stats := store.Stats(convID)
	if stats.HotCount > 20 {
		t.Errorf("HotCount = %d, expected <= 20", stats.HotCount)
	}
	if stats.ColdCount == 0 {
		t.Errorf("expected steps in cold tier, got 0")
	}

	// 2. Point lookup verification across all tiers (Cold, Warm, Hot)
	// Step 50 is in cold gzip archive
	st50, tier50, err := store.GetStep(convID, 50)
	if err != nil {
		t.Fatalf("GetStep(50) failed: %v", err)
	}
	if tier50 != TierCold {
		t.Errorf("step 50 tier = %s, want %s", tier50, TierCold)
	}
	if st50.Idx != 50 || st50.Text != "User message turn 25" {
		t.Errorf("step 50 content mismatch: %+v", st50)
	}

	// Step 399 is in hot RAM tier
	st399, tier399, err := store.GetStep(convID, 399)
	if err != nil {
		t.Fatalf("GetStep(399) failed: %v", err)
	}
	if tier399 != TierHot {
		t.Errorf("step 399 tier = %s, want %s", tier399, TierHot)
	}
	if st399.Idx != 399 {
		t.Errorf("step 399 Idx = %d, want 399", st399.Idx)
	}

	// 3. Complete full trajectory load across all tiers
	allSteps, err := store.LoadTrajectory(convID)
	if err != nil {
		t.Fatalf("LoadTrajectory failed: %v", err)
	}

	// Verify all 400 steps are restored in strict sequential order without duplicates or gaps
	if len(allSteps) != totalSteps {
		t.Fatalf("LoadTrajectory returned %d steps, want %d", len(allSteps), totalSteps)
	}
	for idx, s := range allSteps {
		if s.Idx != int64(idx) {
			t.Fatalf("step %d index out of order: got %d", idx, s.Idx)
		}
	}
}

func TestTieredTrajectoryStore_TypeZero_ResumeParity(t *testing.T) {
	tmpDir := t.TempDir()
	warmDir := filepath.Join(tmpDir, "warm")
	coldDir := filepath.Join(tmpDir, "cold")

	cfg := TieredStoreConfig{
		HotCapacity: 5,
		WarmDir:     warmDir,
		ColdDir:     coldDir,
		AutoEvict:   true,
	}

	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-typezero-resume"

	pUser := encodeProtoField(19, 2, encodeProtoField(3, 2, encodeProtoField(1, 2, []byte("What is type zero resume?"))))
	pModel := encodeProtoField(20, 2, encodeProtoField(1, 2, []byte("Type zero resume decodes protobuf oneof tags.")))

	// Append Type-0 steps
	_ = store.AppendStep(convID, Step{
		Idx:        0,
		Type:       0, // Official agy Type 0
		Status:     0,
		RawPayload: pUser,
	})
	_ = store.AppendStep(convID, Step{
		Idx:        1,
		Type:       0, // Official agy Type 0
		Status:     0,
		RawPayload: pModel,
	})

	// Add more steps to evict Type-0 steps into warm and cold tiers
	for i := int64(2); i < 15; i++ {
		_ = store.AppendStep(convID, Step{
			Idx:    i,
			Type:   StepModelResponse,
			Status: 3,
			Text:   fmt.Sprintf("Turn %d response", i),
		})
	}

	// Archive steps 0..10 into cold storage
	archive, err := store.ArchiveCold(convID, 10)
	if err != nil {
		t.Fatalf("ArchiveCold failed: %v", err)
	}
	if archive == nil {
		t.Fatalf("archive was nil")
	}

	// Restore and verify Type-0 step parity
	st0, _, err := store.GetStep(convID, 0)
	if err != nil {
		t.Fatalf("GetStep(0) failed: %v", err)
	}
	if st0.Type != StepUserInput {
		t.Errorf("step 0 Type = %d, want StepUserInput (%d)", st0.Type, StepUserInput)
	}
	if st0.Role() != "user" {
		t.Errorf("step 0 Role = %q, want 'user'", st0.Role())
	}
	if st0.Text != "What is type zero resume?" {
		t.Errorf("step 0 Text = %q", st0.Text)
	}

	st1, _, err := store.GetStep(convID, 1)
	if err != nil {
		t.Fatalf("GetStep(1) failed: %v", err)
	}
	if st1.Type != StepModelResponse {
		t.Errorf("step 1 Type = %d, want StepModelResponse (%d)", st1.Type, StepModelResponse)
	}
	if st1.Role() != "model" {
		t.Errorf("step 1 Role = %q, want 'model'", st1.Role())
	}
	if st1.Text != "Type zero resume decodes protobuf oneof tags." {
		t.Errorf("step 1 Text = %q", st1.Text)
	}
}
