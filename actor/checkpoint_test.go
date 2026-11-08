package actor_test

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/justwasm/pigasus/actor"
)

// TestCheckpoint_BetweenStepsKeepsTurn pins the core contract: a checkpoint
// after a tool step replaces the model context before the next call, but the
// turn keeps running and the trajectory keeps every step.
func TestCheckpoint_BetweenStepsKeepsTurn(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{ToolCalls: []actor.ToolCall{{ID: "c1", Name: "noop", Input: "{}"}}, FinishReason: "tool_calls"},
	})
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "done", FinishReason: "stop"},
	})

	tool := actor.NewFuncTool("noop", "no-op", func(ctx context.Context, input string) (string, error) {
		return "tool output", nil
	})

	var mu sync.Mutex
	var probes []actor.CheckpointProbe
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{tool},
			Checkpoint: func(ctx context.Context, p actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				mu.Lock()
				probes = append(probes, p)
				mu.Unlock()
				if p.StepIndex != 1 {
					return actor.CheckpointDecision{}, nil
				}
				return actor.CheckpointDecision{
					Apply:               true,
					ResumeText:          "# Resuming from a compaction\n\nsummary",
					Summary:             "<summary>summary</summary>",
					IncludedThroughStep: 0,
					KeepRecent:          0,
					KeepInput:           false,
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	before := act.Trajectory().ResetEpoch()
	turn, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	for range turn.Events() {
	}
	if err := turn.Err(); err != nil {
		t.Fatalf("turn error = %v, want nil", err)
	}

	mu.Lock()
	gotProbes := append([]actor.CheckpointProbe(nil), probes...)
	mu.Unlock()
	if len(gotProbes) != 2 {
		t.Fatalf("probes = %d, want 2 (one per model call)", len(gotProbes))
	}
	if gotProbes[0].StepIndex != 0 || gotProbes[1].StepIndex != 1 {
		t.Fatalf("probe step indexes = %d, %d; want 0, 1", gotProbes[0].StepIndex, gotProbes[1].StepIndex)
	}
	if gotProbes[1].LastStep == nil || len(gotProbes[1].LastStep.ToolResults) != 1 {
		t.Fatalf("second probe last step = %+v, want one tool result", gotProbes[1].LastStep)
	}

	if act.Trajectory().ResetEpoch() != before {
		t.Fatal("a checkpoint must not change ResetEpoch")
	}
	cps := act.Trajectory().Checkpoints()
	if len(cps) != 1 {
		t.Fatalf("checkpoints = %d, want 1", len(cps))
	}
	if cps[0].IncludedThroughStep != 0 || cps[0].StepsSummarized != 0 {
		t.Errorf("checkpoint = %+v", cps[0])
	}

	last := act.Trajectory().LastTurn()
	if last.Status != "done" {
		t.Fatalf("turn status = %q, want done", last.Status)
	}
	if len(last.Steps) != 2 {
		t.Fatalf("steps = %d, want 2 (the checkpoint must not drop them)", len(last.Steps))
	}
	if last.FinalOutput != "done" {
		t.Errorf("FinalOutput = %q, want done", last.FinalOutput)
	}
	if last.Steps[0].GlobalIndex != 0 || last.Steps[1].GlobalIndex != 1 {
		t.Errorf("global indexes = %d, %d; want 0, 1", last.Steps[0].GlobalIndex, last.Steps[1].GlobalIndex)
	}

	// The view a later checkpoint sees contains the previous resume block
	// and only the steps after the boundary.
	view := act.Trajectory().CheckpointView()
	if view.Resume == "" {
		t.Error("checkpoint view has no resume block")
	}
	if len(view.Steps) != 1 || view.Steps[0].GlobalIndex != 1 {
		t.Fatalf("checkpoint view steps = %+v, want only global step 1", view.Steps)
	}

	// The second request starts with the resume block and contains no
	// duplicated input or folded step.
	mock2 := mock.Requests
	if len(mock2) != 2 {
		t.Fatalf("provider requests = %d, want 2", len(mock2))
	}
	msgs := mock2[1].Messages
	if len(msgs) == 0 || !strings.HasPrefix(msgs[0].Content, "# Resuming from a compaction") {
		t.Fatalf("second request does not start with the resume block: %+v", msgs)
	}
	for _, m := range msgs {
		if m.Content == "hello" {
			t.Errorf("the folded input was sent again: %+v", msgs)
		}
	}
}

// TestCheckpoint_KeepRecentStep pins moving_window_size: the last completed
// step stays outside the summary and is sent with it.
func TestCheckpoint_KeepRecentStep(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "first", ToolCalls: []actor.ToolCall{{ID: "c1", Name: "noop", Input: "{}"}}},
	})
	mock.EnqueueChunks([]actor.StreamChunk{{Delta: "second", FinishReason: "stop"}})
	tool := actor.NewFuncTool("noop", "no-op", func(ctx context.Context, input string) (string, error) {
		return "tool output", nil
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Tools:    []actor.Tool{tool},
			Checkpoint: func(ctx context.Context, p actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				if p.StepIndex == 0 {
					return actor.CheckpointDecision{}, nil
				}
				// Fold nothing but the input: keep the tool step.
				return actor.CheckpointDecision{
					Apply:               true,
					ResumeText:          "# Resuming from a compaction\n\nsummary",
					IncludedThroughStep: -1,
					KeepRecent:          1,
					KeepInput:           false,
				}, nil
			},
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	for range turn.Events() {
	}
	if err := turn.Err(); err != nil {
		t.Fatalf("turn error = %v", err)
	}

	msgs := mock.Requests[1].Messages
	var sawResume, sawTool bool
	for _, m := range msgs {
		if strings.HasPrefix(m.Content, "# Resuming from a compaction") {
			sawResume = true
		}
		if m.Role == actor.RoleTool && m.Content == "tool output" {
			sawTool = true
		}
		if m.Role == actor.RoleUser && m.Content == "hello" {
			t.Errorf("the folded input was sent again: %+v", msgs)
		}
	}
	if !sawResume || !sawTool {
		t.Fatalf("second request missing resume or kept tool step: %+v", msgs)
	}
}

// TestCheckpoint_SoftErrorKeepsFullContext pins that a soft checkpoint failure
// does not fail or truncate the turn.
func TestCheckpoint_SoftErrorKeepsFullContext(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{{Delta: "answer", FinishReason: "stop"}})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: mock,
			Checkpoint: func(ctx context.Context, p actor.CheckpointProbe) (actor.CheckpointDecision, error) {
				return actor.CheckpointDecision{}, context.DeadlineExceeded
			},
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	for range turn.Events() {
	}
	if err := turn.Err(); err != nil {
		t.Fatalf("turn error = %v, want nil", err)
	}
	if len(act.Trajectory().Checkpoints()) != 0 {
		t.Fatal("a failed checkpoint must not be recorded")
	}
}
