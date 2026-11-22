package convstore

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestTieredTrajectoryStore_AppendAndGetHot(t *testing.T) {
	cfg := TieredStoreConfig{
		HotCapacity: 10,
		AutoEvict:   true,
	}
	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-hot-test"
	step := Step{
		Idx:      0,
		Type:     StepUserInput,
		Status:   3,
		Text:     "Hello OpenAGY",
		ToolName: "",
	}

	if err := store.AppendStep(convID, step); err != nil {
		t.Fatalf("AppendStep failed: %v", err)
	}

	retrieved, tier, err := store.GetStep(convID, 0)
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if tier != TierHot {
		t.Errorf("tier = %s, want %s", tier, TierHot)
	}
	if retrieved.Text != "Hello OpenAGY" {
		t.Errorf("retrieved text = %q, want %q", retrieved.Text, "Hello OpenAGY")
	}

	stats := store.Stats(convID)
	if stats.HotCount != 1 || stats.WarmCount != 0 || stats.ColdCount != 0 {
		t.Errorf("unexpected stats: %+v", stats)
	}
}

func TestTieredTrajectoryStore_CompleteTurn(t *testing.T) {
	cfg := TieredStoreConfig{
		HotCapacity: 20,
		AutoEvict:   true,
	}
	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-turn-test"

	// Append 8 steps representing an active turn
	for i := int64(0); i < 8; i++ {
		_ = store.AppendStep(convID, Step{
			Idx:      i,
			Type:     StepToolCall,
			Status:   3,
			Text:     fmt.Sprintf("Step %d", i),
			ToolName: "run_command",
		})
	}

	statsBefore := store.Stats(convID)
	if statsBefore.HotCount != 8 {
		t.Errorf("HotCount before complete = %d, want 8", statsBefore.HotCount)
	}

	// Complete turn
	evicted, err := store.CompleteTurn(convID)
	if err != nil {
		t.Fatalf("CompleteTurn failed: %v", err)
	}
	if evicted != 3 { // 8 total - 5 kept in hot RAM = 3 evicted to warm
		t.Errorf("evicted = %d, want 3", evicted)
	}

	statsAfter := store.Stats(convID)
	if statsAfter.HotCount != 5 {
		t.Errorf("HotCount after complete = %d, want 5", statsAfter.HotCount)
	}
	if statsAfter.WarmCount != 3 {
		t.Errorf("WarmCount after complete = %d, want 3", statsAfter.WarmCount)
	}
}

func TestTieredTrajectoryStore_HotEviction(t *testing.T) {
	cfg := TieredStoreConfig{
		HotCapacity: 4,
		AutoEvict:   true,
	}
	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-evict-test"

	// Append 10 steps (indices 0..9)
	for i := int64(0); i < 10; i++ {
		st := Step{
			Idx:    i,
			Type:   StepToolCall,
			Status: 3,
			Text:   fmt.Sprintf("tool invocation %d", i),
		}
		if err := store.AppendStep(convID, st); err != nil {
			t.Fatalf("AppendStep %d failed: %v", i, err)
		}
	}

	stats := store.Stats(convID)
	if stats.HotCount != 4 {
		t.Errorf("HotCount = %d, want 4", stats.HotCount)
	}
	if stats.WarmCount != 6 {
		t.Errorf("WarmCount = %d, want 6", stats.WarmCount)
	}

	// Step 0 was evicted to Warm tier
	st0, tier0, err := store.GetStep(convID, 0)
	if err != nil {
		t.Fatalf("GetStep(0) failed: %v", err)
	}
	if tier0 != TierWarm {
		t.Errorf("step 0 tier = %s, want %s", tier0, TierWarm)
	}
	if st0.Text != "tool invocation 0" {
		t.Errorf("st0.Text = %q", st0.Text)
	}

	// Step 9 is still in Hot tier
	st9, tier9, err := store.GetStep(convID, 9)
	if err != nil {
		t.Fatalf("GetStep(9) failed: %v", err)
	}
	if tier9 != TierHot {
		t.Errorf("step 9 tier = %s, want %s", tier9, TierHot)
	}
	if st9.Text != "tool invocation 9" {
		t.Errorf("st9.Text = %q", st9.Text)
	}

	// Load full trajectory should return all 10 in index order
	traj, err := store.LoadTrajectory(convID)
	if err != nil {
		t.Fatalf("LoadTrajectory failed: %v", err)
	}
	if len(traj) != 10 {
		t.Fatalf("trajectory length = %d, want 10", len(traj))
	}
	for i, st := range traj {
		if st.Idx != int64(i) {
			t.Errorf("traj[%d].Idx = %d, want %d", i, st.Idx, i)
		}
	}
}

func TestTieredTrajectoryStore_ArchiveAndRestoreCold(t *testing.T) {
	cfg := TieredStoreConfig{
		HotCapacity: 2,
		AutoEvict:   true,
	}
	store, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("NewTieredTrajectoryStore failed: %v", err)
	}
	defer store.Close()

	convID := "conv-archive-test"

	// Append 10 steps so steps 0..7 are in Warm, 8..9 are in Hot
	for i := int64(0); i < 10; i++ {
		st := Step{
			Idx:      i,
			Type:     StepToolCall,
			Status:   3,
			Text:     fmt.Sprintf("Large payload step content for compression test %d", i),
			ToolName: "run_command",
			ToolArgs: `{"CommandLine":"ls -la"}`,
		}
		if err := store.AppendStep(convID, st); err != nil {
			t.Fatalf("AppendStep %d failed: %v", i, err)
		}
	}

	// Archive steps before index 5 to cold storage
	info, err := store.ArchiveCold(convID, 5)
	if err != nil {
		t.Fatalf("ArchiveCold failed: %v", err)
	}
	if info == nil {
		t.Fatal("expected ArchiveInfo, got nil")
	}
	if info.StepCount != 5 {
		t.Errorf("archived step count = %d, want 5", info.StepCount)
	}
	if info.StartIdx != 0 || info.EndIdx != 4 {
		t.Errorf("archived range = [%d, %d], want [0, 4]", info.StartIdx, info.EndIdx)
	}
	if info.CompressedSize >= info.RawSize {
		t.Logf("CompressedSize=%d, RawSize=%d", info.CompressedSize, info.RawSize)
	}

	stats := store.Stats(convID)
	if stats.ColdCount != 5 {
		t.Errorf("ColdCount = %d, want 5", stats.ColdCount)
	}
	if stats.WarmCount != 3 { // steps 5, 6, 7
		t.Errorf("WarmCount = %d, want 3", stats.WarmCount)
	}
	if stats.HotCount != 2 { // steps 8, 9
		t.Errorf("HotCount = %d, want 2", stats.HotCount)
	}

	// Check step 2 (resides in Cold tier)
	st2, tier2, err := store.GetStep(convID, 2)
	if err != nil {
		t.Fatalf("GetStep(2) failed: %v", err)
	}
	if tier2 != TierCold {
		t.Errorf("step 2 tier = %s, want %s", tier2, TierCold)
	}
	if st2.Idx != 2 {
		t.Errorf("st2.Idx = %d, want 2", st2.Idx)
	}

	// Check full trajectory remains intact
	traj, err := store.LoadTrajectory(convID)
	if err != nil {
		t.Fatalf("LoadTrajectory failed: %v", err)
	}
	if len(traj) != 10 {
		t.Fatalf("trajectory length = %d, want 10", len(traj))
	}

	// Restore cold archive back to warm
	restored, err := store.RestoreCold(convID, info.ArchiveID)
	if err != nil {
		t.Fatalf("RestoreCold failed: %v", err)
	}
	if restored != 5 {
		t.Errorf("restored count = %d, want 5", restored)
	}

	statsAfter := store.Stats(convID)
	if statsAfter.ColdCount != 0 {
		t.Errorf("ColdCount after restore = %d, want 0", statsAfter.ColdCount)
	}
	if statsAfter.WarmCount != 8 { // 3 + 5
		t.Errorf("WarmCount after restore = %d, want 8", statsAfter.WarmCount)
	}

	// Step 2 should now report TierWarm
	_, tier2After, err := store.GetStep(convID, 2)
	if err != nil {
		t.Fatalf("GetStep(2) after restore failed: %v", err)
	}
	if tier2After != TierWarm {
		t.Errorf("step 2 tier after restore = %s, want %s", tier2After, TierWarm)
	}
}

func TestTieredTrajectoryStore_DiskPersistence(t *testing.T) {
	tempDir := t.TempDir()
	warmDir := filepath.Join(tempDir, "warm")
	coldDir := filepath.Join(tempDir, "cold")

	cfg := TieredStoreConfig{
		HotCapacity: 2,
		WarmDir:     warmDir,
		ColdDir:     coldDir,
		AutoEvict:   true,
	}

	convID := "disk-persistence-conv"

	// 1. Write steps and archive
	func() {
		store, err := NewTieredTrajectoryStore(cfg)
		if err != nil {
			t.Fatalf("store creation failed: %v", err)
		}
		defer store.Close()

		for i := int64(0); i < 6; i++ {
			_ = store.AppendStep(convID, Step{
				Idx:  i,
				Text: fmt.Sprintf("Persistent step %d", i),
			})
		}

		// Archive steps 0..2
		_, err = store.ArchiveCold(convID, 3)
		if err != nil {
			t.Fatalf("ArchiveCold failed: %v", err)
		}
	}()

	// 2. Re-open from disk and verify persistence across process restart
	store2, err := NewTieredTrajectoryStore(cfg)
	if err != nil {
		t.Fatalf("reopening store failed: %v", err)
	}
	defer store2.Close()

	// Verify step 4 in warm tier
	st4, tier4, err := store2.GetStep(convID, 4)
	if err != nil {
		t.Fatalf("GetStep(4) failed: %v", err)
	}
	if tier4 != TierWarm {
		t.Errorf("tier4 = %s, want %s", tier4, TierWarm)
	}
	if st4.Text != "Persistent step 4" {
		t.Errorf("st4.Text = %q", st4.Text)
	}

	// Verify step 1 in cold tier
	st1, tier1, err := store2.GetStep(convID, 1)
	if err != nil {
		t.Fatalf("GetStep(1) failed: %v", err)
	}
	if tier1 != TierCold {
		t.Errorf("tier1 = %s, want %s", tier1, TierCold)
	}
	if st1.Text != "Persistent step 1" {
		t.Errorf("st1.Text = %q", st1.Text)
	}
}
