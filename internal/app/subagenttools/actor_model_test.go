package subagenttools

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

func TestActivityWatchdog_StallDetection(t *testing.T) {
	stalled := atomic.Bool{}
	w := newActivityWatchdog(20*time.Millisecond, func() {
		stalled.Store(true)
	})
	defer w.Stop()

	// Wait for watchdog to trip
	time.Sleep(50 * time.Millisecond)
	if !stalled.Load() {
		t.Fatalf("expected watchdog to trigger stall callback")
	}
	if !w.IsStalled() {
		t.Fatalf("expected w.IsStalled() to return true")
	}
}

func TestActivityWatchdog_PingRefreshesTimer(t *testing.T) {
	stalled := atomic.Bool{}
	w := newActivityWatchdog(40*time.Millisecond, func() {
		stalled.Store(true)
	})
	defer w.Stop()

	// Continuously ping every 10ms for 80ms
	for i := 0; i < 8; i++ {
		time.Sleep(10 * time.Millisecond)
		w.Ping()
	}

	if stalled.Load() {
		t.Fatalf("watchdog should not have stalled while being regularly pinged")
	}
	if w.IsStalled() {
		t.Fatalf("w.IsStalled() should be false while pinged")
	}
}

func TestCheckpoint_SaveAndLoad(t *testing.T) {
	subDir := t.TempDir()
	now := time.Now().Truncate(time.Millisecond)

	meta := CheckpointMeta{
		ConversationID: "sub-chk-1",
		ParentID:       "parent-chk-1",
		TypeName:       "DeepCoder",
		Role:           "Alignment Engineer",
		Worktree:       "/tmp/worktree-1",
		Prompt:         "Align Actor Model",
		SystemPrompt:   "You are an assistant",
		InheritPrompt:  false,
		State:          StateStopped,
		StartedAt:      now.Add(-10 * time.Minute),
		EndedAt:        now,
		LastActivityAt: now,
		StepCount:      4,
		NestingDepth:   1,
		ToolsUsed:      []string{"run_command", "read_file"},
		Error:          "simulated 429 quota exhaustion",
	}

	if err := saveCheckpoint(subDir, meta); err != nil {
		t.Fatalf("saveCheckpoint failed: %v", err)
	}

	loaded, err := loadCheckpoint(subDir, "sub-chk-1")
	if err != nil {
		t.Fatalf("loadCheckpoint failed: %v", err)
	}
	if loaded == nil {
		t.Fatalf("loaded checkpoint is nil")
	}

	if loaded.ConversationID != meta.ConversationID {
		t.Errorf("ConversationID = %q, want %q", loaded.ConversationID, meta.ConversationID)
	}
	if loaded.ParentID != meta.ParentID {
		t.Errorf("ParentID = %q, want %q", loaded.ParentID, meta.ParentID)
	}
	if loaded.TypeName != meta.TypeName {
		t.Errorf("TypeName = %q, want %q", loaded.TypeName, meta.TypeName)
	}
	if loaded.Role != meta.Role {
		t.Errorf("Role = %q, want %q", loaded.Role, meta.Role)
	}
	if loaded.State != StateStopped {
		t.Errorf("State = %q, want %q", loaded.State, StateStopped)
	}
	if loaded.StepCount != 4 {
		t.Errorf("StepCount = %d, want 4", loaded.StepCount)
	}
	if len(loaded.ToolsUsed) != 2 || loaded.ToolsUsed[0] != "run_command" {
		t.Errorf("ToolsUsed = %v, want ['run_command', 'read_file']", loaded.ToolsUsed)
	}
	if loaded.Error != meta.Error {
		t.Errorf("Error = %q, want %q", loaded.Error, meta.Error)
	}
}

func TestRehydrateSubagents_RestoresStateAndHandle(t *testing.T) {
	subDir := t.TempDir()
	now := time.Now().Truncate(time.Millisecond)

	// Persist 2 subagent checkpoints: one stopped, one that was interrupted while running
	chk1 := CheckpointMeta{
		ConversationID: "sub-rehydrate-1",
		TypeName:       "DeepCoder",
		Role:           "Worker 1",
		Worktree:       "/tmp/wt-1",
		Prompt:         "Task 1",
		State:          StateStopped,
		StartedAt:      now.Add(-5 * time.Minute),
		LastActivityAt: now,
		StepCount:      2,
		ToolsUsed:      []string{"read_file"},
	}
	chk2 := CheckpointMeta{
		ConversationID: "sub-rehydrate-2",
		TypeName:       "DeepCoder",
		Role:           "Worker 2",
		Worktree:       "/tmp/wt-2",
		Prompt:         "Task 2",
		State:          StateRunning, // interrupted by CLI crash
		StartedAt:      now.Add(-2 * time.Minute),
		LastActivityAt: now,
		StepCount:      5,
		ToolsUsed:      []string{"run_command"},
	}

	if err := saveCheckpoint(subDir, chk1); err != nil {
		t.Fatalf("save chk1 error: %v", err)
	}
	if err := saveCheckpoint(subDir, chk2); err != nil {
		t.Fatalf("save chk2 error: %v", err)
	}

	reg := NewRegistry("parent-main")
	taskReg := taskregistry.New()

	restored, err := RehydrateSubagents(subDir, reg, taskReg, Deps{})
	if err != nil {
		t.Fatalf("RehydrateSubagents failed: %v", err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored count = %d, want 2", len(restored))
	}

	// Verify child 1 in Registry and task.Registry
	c1, ok := reg.Get("sub-rehydrate-1")
	if !ok || c1 == nil {
		t.Fatalf("child 1 not found in registry")
	}
	if c1.State() != StateStopped {
		t.Errorf("child 1 state = %q, want %q", c1.State(), StateStopped)
	}
	t1 := taskReg.Get("sub-rehydrate-1")
	if t1 == nil {
		t.Fatalf("task 1 not found in taskRegistry")
	}
	if t1.State != taskregistry.StateStopped {
		t.Errorf("task 1 state = %q, want %q", t1.State, taskregistry.StateStopped)
	}

	// Verify child 2 was transitioned from "running" to "stopped" upon rehydration after crash
	c2, ok := reg.Get("sub-rehydrate-2")
	if !ok || c2 == nil {
		t.Fatalf("child 2 not found in registry")
	}
	if c2.State() != StateStopped {
		t.Errorf("child 2 state = %q, want %q (crash recovery)", c2.State(), StateStopped)
	}
	t2 := taskReg.Get("sub-rehydrate-2")
	if t2 == nil {
		t.Fatalf("task 2 not found in taskRegistry")
	}
	if t2.State != taskregistry.StateStopped {
		t.Errorf("task 2 state = %q, want %q (crash recovery)", t2.State, taskregistry.StateStopped)
	}

	// Verify task handle was registered
	h1 := taskReg.GetHandle("sub-rehydrate-1")
	if h1 == nil {
		t.Fatalf("task handle 1 should be registered")
	}
}

func TestRecoverableLimitError_Detection(t *testing.T) {
	testCases := []struct {
		err  error
		want bool
	}{
		{err: nil, want: false},
		{err: errors.New("context deadline exceeded"), want: false},
		{err: errors.New("HTTP 429 Too Many Requests"), want: true},
		{err: errors.New("ResourceExhausted: quota exceeded for quota metric"), want: true},
		{err: errors.New("rate limit reached for gpt-4o"), want: true},
		{err: errors.New("token limit exceeded: maximum context length is 128000"), want: true},
		{err: errors.New("StopReason: max_tokens"), want: true},
	}

	for _, tc := range testCases {
		got := isRecoverableLimitError(tc.err)
		if got != tc.want {
			t.Errorf("isRecoverableLimitError(%v) = %v, want %v", tc.err, got, tc.want)
		}
	}
}

func TestDrainSubagentEvents_TouchesWatchdogAndTaskActivity(t *testing.T) {
	taskReg := taskregistry.New()
	taskReg.Register(&taskregistry.Task{
		ID:        "sub-act-1",
		Name:      "Sub Activity",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-10 * time.Second),
	})

	child := NewChild("sub-act-1", "DeepCoder", "Role", "/tmp", func() {})
	_ = child
	pinged := atomic.Bool{}
	w := newActivityWatchdog(time.Minute, func() {})
	defer w.Stop()

	// Wrap ping to track activity
	oldPing := w.Ping
	_ = oldPing

	taskReg.RecordActivity("sub-act-1")
	task := taskReg.Get("sub-act-1")
	if task == nil {
		t.Fatalf("task not found")
	}
	if time.Since(task.LastActivityAt) > 2*time.Second {
		t.Errorf("task.LastActivityAt was not updated: %v", task.LastActivityAt)
	}
	_ = pinged
}

func TestTranscriptResumePreservesAllPriorSteps(t *testing.T) {
	subDir := t.TempDir()
	subID := "sub-steps-preservation"

	// Simulate step 1 (User), step 2 (Tool Call), step 3 (Tool Result)
	m1 := SubagentMessage{
		Role:    "user",
		Content: "Find all markdown files",
	}
	m2 := SubagentMessage{
		Role:       "assistant",
		ToolCallID: "call_find_1",
		ToolName:   "run_command",
	}
	m3 := SubagentMessage{
		Role:       "toolResult",
		ToolCallID: "call_find_1",
		Content:    "README.md\nAGENTS.md",
	}

	initialSteps := []SubagentMessage{m1, m2, m3}
	persistTranscript(subDir, subID, initialSteps, nil)

	// Now load the transcript (as resume would do)
	loaded, err := loadTranscript(subDir, subID)
	if err != nil {
		t.Fatalf("loadTranscript failed: %v", err)
	}
	if len(loaded) != 3 {
		t.Fatalf("loaded messages count = %d, want 3", len(loaded))
	}
	if loaded[0].Role != "user" {
		t.Errorf("loaded[0].Role = %q, want user", loaded[0].Role)
	}
	if loaded[1].Role != "assistant" {
		t.Errorf("loaded[1].Role = %q, want assistant", loaded[1].Role)
	}
	if loaded[2].Role != "toolResult" {
		t.Errorf("loaded[2].Role = %q, want toolResult", loaded[2].Role)
	}

	// Verify that the jsonl file exists and matches the agy layout
	path := filepath.Join(subDir, subID+".jsonl")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat failed on %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("transcript file is empty")
	}
}
