package subagenttools

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// TestCheckpointPersistsTheTrajectory pins the persistence half of the
// subagent-detail trajectory: the checkpoint records the child's steps and
// the parent conversation that owns it, so a reopened session can show both
// the "Trajectory — <name> (<N> steps)" section and the right owner.
func TestCheckpointPersistsTheTrajectory(t *testing.T) {
	dir := t.TempDir()
	subDir := filepath.Join(dir, "agent", "subagents")
	registry := NewRegistry("parent-1")
	registry.SetNotify(func(string, string) {})
	defer registry.Close()

	tasks := taskregistry.New()
	provider := actor.NewMockProvider()
	provider.EnqueueResponse("Shanghai is overcast.")

	deps := Deps{
		Model:          "mock-model",
		Provider:       provider,
		CWD:            dir,
		AgentDir:       filepath.Join(dir, "agent"),
		Registry:       registry,
		Tasks:          tasks,
		ConversationID: "parent-1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	launched, err := Launch(ctx, Invocation{Prompt: "查一下上海天气"}, deps)
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}

	meta := waitForCheckpoint(t, subDir, launched.Child.ConversationID)
	if meta.ParentID != "parent-1" {
		t.Errorf("checkpoint parentId = %q, want %q (a child the checkpoint cannot attribute is never rehydrated)", meta.ParentID, "parent-1")
	}
	if len(meta.Steps) == 0 {
		t.Fatalf("checkpoint recorded no steps for a finished child (tool list = %v)", meta.ToolsUsed)
	}
	if meta.Steps[0] != "Shanghai is overcast." {
		t.Errorf("checkpoint steps = %q, want the child's answer step", meta.Steps)
	}
	if meta.Output != "Shanghai is overcast." {
		t.Errorf("checkpoint output = %q, want the child's answer (the detail's Answer section)", meta.Output)
	}

	task := tasks.Get(launched.Child.ConversationID)
	if task == nil || len(task.Steps) == 0 {
		t.Fatalf("the live task has no trajectory: %+v", task)
	}
}

// TestRehydrateRestoresTheTrajectory — a restored child keeps the steps the
// checkpoint recorded. This is the defect the /tasks detail showed as an
// empty "Trajectory" section after a restart.
func TestRehydrateRestoresTheTrajectory(t *testing.T) {
	subDir := t.TempDir()
	meta := CheckpointMeta{
		ConversationID: "sub-trajectory",
		ParentID:       "parent-1",
		TypeName:       "research",
		Role:           "Weather Researcher",
		Prompt:         "查一下上海天气",
		State:          StateIdle,
		StartedAt:      time.Now().Add(-time.Minute),
		LastActivityAt: time.Now(),
		ToolsUsed:      []string{"read_url_content"},
		Steps:          []string{"Looking up the forecast.", "read_url_content", "Shanghai is overcast."},
		Output:         "Shanghai is overcast.",
	}
	if err := saveCheckpoint(subDir, meta); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}

	taskReg := taskregistry.New()
	if _, err := RehydrateSubagents(subDir, NewRegistry("parent-1"), taskReg, Deps{ConversationID: "parent-1"}); err != nil {
		t.Fatalf("RehydrateSubagents: %v", err)
	}
	task := taskReg.Get("sub-trajectory")
	if task == nil {
		t.Fatal("the child was not restored")
	}
	if len(task.Steps) != 3 || task.Steps[0] != "Looking up the forecast." {
		t.Fatalf("restored trajectory = %q, want the checkpoint's three steps", task.Steps)
	}
	if task.Output != "Shanghai is overcast." {
		t.Errorf("restored answer = %q, want the checkpoint's output", task.Output)
	}
}

// TestRehydrateDoesNotClobberALiveChild — every turn rebuilds the subagent
// tool set and rehydrates the checkpoints. A child that is still live in this
// process must keep its own task row: the checkpoint beside it is the snapshot
// from its last finished turn, which would show the child as idle with its
// latest turn's answer missing.
func TestRehydrateDoesNotClobberALiveChild(t *testing.T) {
	subDir := t.TempDir()
	meta := CheckpointMeta{
		ConversationID: "sub-live",
		ParentID:       "parent-1",
		State:          StateIdle,
		Steps:          []string{"an older step"},
		Output:         "an older answer",
	}
	if err := saveCheckpoint(subDir, meta); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}

	registry := NewRegistry("parent-1")
	defer registry.Close()
	live := NewChild("sub-live", "research", "Weather Researcher", "/tmp", nil)
	live.setState(StateRunning)
	registry.Add(live)

	taskReg := taskregistry.New()
	taskReg.Register(&taskregistry.Task{
		ID: "sub-live", Kind: taskregistry.KindSubagent, State: taskregistry.StateRunning,
		Steps: []string{"the live step"}, Output: "the live answer",
	})

	restored, err := RehydrateSubagents(subDir, registry, taskReg, Deps{ConversationID: "parent-1"})
	if err != nil {
		t.Fatalf("RehydrateSubagents: %v", err)
	}
	if len(restored) != 0 {
		t.Fatalf("rehydrated %d children that are still live", len(restored))
	}
	task := taskReg.Get("sub-live")
	if task.State != taskregistry.StateRunning {
		t.Errorf("live task state = %q, want %q", task.State, taskregistry.StateRunning)
	}
	if task.Output != "the live answer" || len(task.Steps) != 1 || task.Steps[0] != "the live step" {
		t.Errorf("live task row was overwritten by the checkpoint: output=%q steps=%q", task.Output, task.Steps)
	}
}

// TestRehydrateRebuildsTheTrajectoryFromTheTranscript — a checkpoint written
// before the steps were persisted still has its transcript beside it, and the
// steps are the same derivation the live run used.
func TestRehydrateRebuildsTheTrajectoryFromTheTranscript(t *testing.T) {
	subDir := t.TempDir()
	meta := CheckpointMeta{
		ConversationID: "sub-legacy",
		ParentID:       "parent-1",
		TypeName:       "research",
		Prompt:         "查一下上海天气",
		State:          StateIdle,
		StartedAt:      time.Now().Add(-time.Minute),
		LastActivityAt: time.Now(),
	}
	if err := saveCheckpoint(subDir, meta); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}
	persistTranscript(subDir, "sub-legacy", []SubagentMessage{
		{Role: "user", Content: "查一下上海天气"},
		{Role: "assistant", Content: "Looking up the forecast."},
		{Role: "assistant", ToolName: "read_url_content", ToolCallID: "1"},
		{Role: "toolResult", ToolCallID: "1", Content: "18C, overcast"},
		{Role: "assistant", Content: "Shanghai is overcast, 18C."},
	}, nil)

	taskReg := taskregistry.New()
	if _, err := RehydrateSubagents(subDir, NewRegistry("parent-1"), taskReg, Deps{ConversationID: "parent-1"}); err != nil {
		t.Fatalf("RehydrateSubagents: %v", err)
	}
	task := taskReg.Get("sub-legacy")
	if task == nil {
		t.Fatal("the child was not restored")
	}
	want := []string{"Looking up the forecast.", "read_url_content", "Shanghai is overcast, 18C."}
	if len(task.Steps) != len(want) {
		t.Fatalf("restored trajectory = %q, want %q", task.Steps, want)
	}
	for i := range want {
		if task.Steps[i] != want[i] {
			t.Errorf("step %d = %q, want %q", i, task.Steps[i], want[i])
		}
	}
}

// TestRehydrateDoesNotAdoptAnotherConversationsChild — the subagents
// directory is shared by every conversation on the machine, so a
// conversation only restores the children its own checkpoints name. A
// checkpoint with no owner at all is not attributed by guesswork.
func TestRehydrateDoesNotAdoptAnotherConversationsChild(t *testing.T) {
	subDir := t.TempDir()
	for _, meta := range []CheckpointMeta{
		{ConversationID: "sub-foreign", ParentID: "parent-other", State: StateIdle},
		{ConversationID: "sub-unowned", State: StateIdle},
	} {
		if err := saveCheckpoint(subDir, meta); err != nil {
			t.Fatalf("saveCheckpoint: %v", err)
		}
	}

	taskReg := taskregistry.New()
	restored, err := RehydrateSubagents(subDir, NewRegistry("parent-1"), taskReg, Deps{ConversationID: "parent-1"})
	if err != nil {
		t.Fatalf("RehydrateSubagents: %v", err)
	}
	if len(restored) != 0 {
		t.Errorf("restored %d children the conversation does not own", len(restored))
	}
	if n := len(taskReg.List()); n != 0 {
		t.Errorf("/tasks holds %d rows for another conversation's children", n)
	}

	// The owner still gets its own child.
	if err := saveCheckpoint(subDir, CheckpointMeta{ConversationID: "sub-own", ParentID: "parent-1", State: StateIdle}); err != nil {
		t.Fatalf("saveCheckpoint: %v", err)
	}
	restored, err = RehydrateSubagents(subDir, NewRegistry("parent-1"), taskReg, Deps{ConversationID: "parent-1"})
	if err != nil {
		t.Fatalf("RehydrateSubagents: %v", err)
	}
	if len(restored) != 1 || restored[0].ConversationID != "sub-own" {
		t.Fatalf("restored = %d children, want only sub-own", len(restored))
	}
	if restored[0].ParentID != "parent-1" {
		t.Errorf("restored child's parentId = %q, want %q", restored[0].ParentID, "parent-1")
	}
}

// TestTrajectoryFillsWhileTheChildWorks — a detail view opened mid-run shows
// the steps taken so far instead of "No steps yet". The second provider turn
// runs after the first turn's tool call, so the assertion is deterministic.
func TestTrajectoryFillsWhileTheChildWorks(t *testing.T) {
	dir := t.TempDir()
	registry := NewRegistry("parent-1")
	registry.SetNotify(func(string, string) {})
	defer registry.Close()

	tasks := taskregistry.New()
	provider := actor.NewMockProvider()
	provider.EnqueueChunks([]actor.StreamChunk{{
		ToolCalls:    []actor.ToolCall{{ID: "1", Name: "read_url_content", Input: `{}`}},
		FinishReason: "tool_calls",
	}})
	observed := make(chan []string, 1)
	provider.EnqueueHandler(func(actor.GenerateRequest) ([]actor.StreamChunk, error) {
		// The first turn's tool call is recorded as the child works, so the
		// step is visible while this (the second) turn is still in flight.
		// The event consumer runs on its own goroutine, so give it a moment
		// to catch up — the point is that it lands before the turn ends.
		deadline := time.Now().Add(2 * time.Second)
		for {
			var ids []string
			for _, task := range tasks.List() {
				ids = append(ids, task.Steps...)
			}
			if len(ids) > 0 || time.Now().After(deadline) {
				observed <- ids
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		return []actor.StreamChunk{{Delta: "done", FinishReason: "stop"}}, nil
	})

	deps := Deps{
		Model:          "mock-model",
		Provider:       provider,
		CWD:            dir,
		AgentDir:       filepath.Join(dir, "agent"),
		Registry:       registry,
		Tasks:          tasks,
		ConversationID: "parent-1",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := Launch(ctx, Invocation{Prompt: "查一下上海天气"}, deps); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	select {
	case steps := <-observed:
		if len(steps) == 0 || steps[0] != "read_url_content" {
			t.Fatalf("mid-run trajectory = %q, want the tool call the child already made", steps)
		}
	case <-ctx.Done():
		t.Fatal("the child never reached its second turn")
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, task := range tasks.List() {
			if len(task.Steps) >= 2 {
				return
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("the trajectory never recorded the child's answer step")
}

// waitForCheckpoint waits for the child's checkpoint file and returns it.
func waitForCheckpoint(t *testing.T, subDir, id string) *CheckpointMeta {
	t.Helper()
	path := filepath.Join(subDir, id+".meta.json")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			meta, err := loadCheckpoint(subDir, id)
			if err != nil {
				t.Fatalf("loadCheckpoint: %v", err)
			}
			return meta
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("no checkpoint was written to %s", path)
	return nil
}
