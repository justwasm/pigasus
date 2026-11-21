package actor

import (
	"context"
	"strings"
	"testing"
)

// TestBudgetManager_StrategyA_Checkpoint verifies that when a checkpoint is recorded,
// historical messages before IncludedThroughStep are replaced with ResumeText.
func TestBudgetManager_StrategyA_Checkpoint(t *testing.T) {
	traj := NewTrajectory()
	traj.AddTurn(&TurnRecord{
		ID:        "turn-1",
		StartStep: 0,
		Input:     "initial request",
		Steps: []Step{
			{GlobalIndex: 0, Delta: "step 0 answer"},
			{GlobalIndex: 1, Delta: "step 1 answer"},
		},
	})
	traj.AddTurn(&TurnRecord{
		ID:        "turn-2",
		StartStep: 2,
		Input:     "followup question",
		Steps: []Step{
			{GlobalIndex: 2, Delta: "step 2 answer"},
		},
	})

	// Checkpoint folds up to step 1
	traj.recordCheckpoint(Checkpoint{
		TurnID:              "turn-1",
		IncludedThroughStep: 1,
		ResumeText:          "# Resuming from a compaction: earlier context summarized",
		KeepInput:           false,
	})

	activeTurn := &TurnRecord{
		ID:        "turn-3",
		StartStep: 3,
		Input:     "latest request",
	}

	bm := DefaultBudgetManager()
	msgs, err := bm.MaterializeContext(context.Background(), ContextRequest{
		Trajectory: traj,
		TurnRecord: activeTurn,
		Input:      "latest request",
	})
	if err != nil {
		t.Fatalf("MaterializeContext failed: %v", err)
	}

	if len(msgs) < 2 {
		t.Fatalf("expected at least 2 messages, got %d", len(msgs))
	}
	if !strings.HasPrefix(msgs[0].Content, "# Resuming from a compaction") {
		t.Errorf("expected first message to be resume block, got %q", msgs[0].Content)
	}
	// step 0 and step 1 should NOT be present
	for _, m := range msgs {
		if strings.Contains(m.Content, "step 0 answer") || strings.Contains(m.Content, "step 1 answer") {
			t.Errorf("step 0/1 should have been compacted away, found in message: %q", m.Content)
		}
	}
	// step 2 and latest request should be present
	foundStep2 := false
	foundLatest := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "step 2 answer") {
			foundStep2 = true
		}
		if strings.Contains(m.Content, "latest request") {
			foundLatest = true
		}
	}
	if !foundStep2 {
		t.Errorf("expected step 2 to be preserved after checkpoint")
	}
	if !foundLatest {
		t.Errorf("expected latest request to be present in context")
	}
}

// TestBudgetManager_StrategyB_TokenSlidingWindow verifies that older historical messages
// slide out when the token budget is tight.
func TestBudgetManager_StrategyB_TokenSlidingWindow(t *testing.T) {
	traj := NewTrajectory()
	traj.AddTurn(&TurnRecord{
		ID:        "turn-1",
		StartStep: 0,
		Input:     "first question: " + strings.Repeat("a", 200),
		Steps: []Step{
			{GlobalIndex: 0, Delta: "first answer: " + strings.Repeat("b", 200)},
		},
	})
	traj.AddTurn(&TurnRecord{
		ID:        "turn-2",
		StartStep: 1,
		Input:     "second question: " + strings.Repeat("c", 200),
		Steps: []Step{
			{GlobalIndex: 1, Delta: "second answer: " + strings.Repeat("d", 200)},
		},
	})

	activeTurn := &TurnRecord{
		ID:        "turn-3",
		StartStep: 2,
		Input:     "current question",
	}

	bm := NewStandardBudgetManager(WithMaxInputTokens(150)) // Budget is limited to ~150 tokens
	msgs, err := bm.MaterializeContext(context.Background(), ContextRequest{
		Trajectory: traj,
		TurnRecord: activeTurn,
		Input:      "current question",
	})
	if err != nil {
		t.Fatalf("MaterializeContext failed: %v", err)
	}

	// Turn 1 should slide out, while Turn 2 and active turn remain
	foundTurn1 := false
	foundTurn2 := false
	foundCurrent := false
	for _, m := range msgs {
		if strings.Contains(m.Content, "first question") || strings.Contains(m.Content, "first answer") {
			foundTurn1 = true
		}
		if strings.Contains(m.Content, "second question") || strings.Contains(m.Content, "second answer") {
			foundTurn2 = true
		}
		if strings.Contains(m.Content, "current question") {
			foundCurrent = true
		}
	}

	if foundTurn1 {
		t.Errorf("turn 1 should have slid out under tight token budget")
	}
	if !foundTurn2 {
		t.Errorf("turn 2 should have been retained in sliding window")
	}
	if !foundCurrent {
		t.Errorf("current turn should always be retained")
	}
}

// TestBudgetManager_StrategyC_ToolPruning verifies that large tool outputs in turns
// older than ToolPruningTurns are contracted to summary stubs.
func TestBudgetManager_StrategyC_ToolPruning(t *testing.T) {
	traj := NewTrajectory()
	largeOutput := strings.Repeat("X", 2000)

	// Turn 1: 2 turns ago -> should be pruned
	traj.AddTurn(&TurnRecord{
		ID:        "turn-1",
		StartStep: 0,
		Input:     "read file 1",
		Steps: []Step{
			{
				GlobalIndex: 0,
				Delta:       "calling tool",
				ToolCalls:   []ToolCall{{ID: "tc-1", Name: "read_file"}},
				ToolResults: []ToolResult{{ToolCallID: "tc-1", Output: largeOutput}},
			},
		},
	})

	// Turn 2: 1 turn ago -> should NOT be pruned (within 1 turn)
	traj.AddTurn(&TurnRecord{
		ID:        "turn-2",
		StartStep: 1,
		Input:     "read file 2",
		Steps: []Step{
			{
				GlobalIndex: 1,
				Delta:       "calling tool 2",
				ToolCalls:   []ToolCall{{ID: "tc-2", Name: "read_file"}},
				ToolResults: []ToolResult{{ToolCallID: "tc-2", Output: largeOutput}},
			},
		},
	})

	// Active turn (turn 3)
	activeTurn := &TurnRecord{
		ID:        "turn-3",
		StartStep: 2,
		Input:     "what were the files?",
	}

	// ToolPruningTurns = 1: turns older than 1 turn ago get pruned
	bm := NewStandardBudgetManager(WithToolPruning(1))
	msgs, err := bm.MaterializeContext(context.Background(), ContextRequest{
		Trajectory: traj,
		TurnRecord: activeTurn,
		Input:      "what were the files?",
	})
	if err != nil {
		t.Fatalf("MaterializeContext failed: %v", err)
	}

	// Find the tool result messages
	var tc1Result, tc2Result string
	for _, m := range msgs {
		if m.Role == RoleTool {
			if m.ToolCallID == "tc-1" {
				tc1Result = m.Content
			} else if m.ToolCallID == "tc-2" {
				tc2Result = m.Content
			}
		}
	}

	if !strings.Contains(tc1Result, "[Tool output pruned: 2000 characters omitted]") {
		t.Errorf("tc-1 output should be pruned, got: %q", tc1Result)
	}
	if tc2Result != largeOutput {
		t.Errorf("tc-2 output should NOT be pruned, got length %d, want %d", len(tc2Result), len(largeOutput))
	}
}

// mockBudgetManager tracks MaterializeContext calls.
type mockBudgetManager struct {
	called bool
}

func (m *mockBudgetManager) MaterializeContext(ctx context.Context, req ContextRequest) ([]ConversationMessage, error) {
	m.called = true
	return []ConversationMessage{
		{Role: RoleUser, Content: "custom materialized input: " + req.Input},
	}, nil
}

// TestActor_CustomBudgetManager verifies that an Actor can receive a custom
// ContextBudgetManager and delegates context generation to it.
func TestActor_CustomBudgetManager(t *testing.T) {
	mockBudget := &mockBudgetManager{}
	provider := NewMockProvider()
	provider.EnqueueChunks([]StreamChunk{
		{Delta: "custom response", FinishReason: "stop"},
	})

	act, err := Spawn(Config{
		Default: TurnConfig{
			Provider: provider,
			Budget:   mockBudget,
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	defer act.Close()

	handle, err := act.Prompt(context.Background(), "hello world")
	if err != nil {
		t.Fatalf("Prompt failed: %v", err)
	}

	for range handle.Events() {
	}

	if !mockBudget.called {
		t.Errorf("expected custom BudgetManager to be called")
	}
	if handle.Err() != nil {
		t.Errorf("turn failed: %v", handle.Err())
	}
}
