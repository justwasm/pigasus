package supervisor

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
)

func TestSupervisor_Lifecycle(t *testing.T) {
	s := New("/test/ws")
	if s.Len() != 0 {
		t.Fatalf("expected 0 messages, got %d", s.Len())
	}

	s.Append(backend.Message{Role: "user", Content: "hello"})
	s.Append(backend.Message{Role: "assistant", Content: "world"})

	if s.Len() != 2 {
		t.Fatalf("expected 2 messages, got %d", s.Len())
	}

	msgs := s.Messages()
	if len(msgs) != 2 || msgs[0].Content != "hello" || msgs[1].Content != "world" {
		t.Fatalf("unexpected messages: %+v", msgs)
	}

	// Test Rewind
	steps := s.RewindSteps()
	if len(steps) != 1 {
		t.Fatalf("expected 1 rewind step, got %d", len(steps))
	}

	r := s.RewindPopTail()
	if r.Removed == 0 {
		t.Fatalf("expected removed messages on pop tail")
	}
	if s.Len() != 0 {
		t.Fatalf("expected empty session after pop tail, got %d", s.Len())
	}

	// Test Trajectory Recording
	convSteps := []convstore.Step{
		{
			Type:       convstore.StepToolCall,
			Status:     3,
			ToolName:   "read_file",
			ToolArgs:   `{"path": "/foo"}`,
			ToolResult: "content",
		},
	}
	s.RecordTrajectoryCalls(convSteps)
	facts := s.TrajectoryFacts()
	if len(facts.Calls) != 1 || facts.Calls[0].ToolName != "read_file" {
		t.Fatalf("unexpected trajectory calls: %+v", facts.Calls)
	}

	s.Clear()
	if s.Len() != 0 {
		t.Fatalf("expected empty session after clear")
	}
	factsAfter := s.TrajectoryFacts()
	if len(factsAfter.Calls) != 0 {
		t.Fatalf("expected empty calls after clear")
	}
}

func TestSupervisor_ReplaceAndRewindClampsSeededLen(t *testing.T) {
	s := New("/test/ws")
	s.SetSeededConversation([]convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "hi"},
	}, 10)
	s.Append(backend.Message{Role: "user", Content: "hi"})

	// Replace with shorter slice
	s.Replace([]backend.Message{
		{Role: "user", Content: "# Resuming from a compaction"},
	})
	facts := s.TrajectoryFacts()
	if facts.SeededLen != 1 {
		t.Fatalf("expected SeededLen 1 after replace, got %d", facts.SeededLen)
	}

	// Rewind also clamps
	s.SetSeededConversation(nil, 5)
	s.Append(backend.Message{Role: "assistant", Content: "reply"})
	_ = s.RewindPopTail()
	factsRewound := s.TrajectoryFacts()
	if factsRewound.SeededLen > len(factsRewound.Session) {
		t.Fatalf("expected SeededLen <= len(Session), got %d > %d", factsRewound.SeededLen, len(factsRewound.Session))
	}
}

// TestSupervisor_QueuedCheckpointsKeepInsertionOrder pins the turn-local
// checkpoint queue: a checkpoint lands at the call count that preceded it,
// and flushing clears the queue.
func TestSupervisor_QueuedCheckpointsKeepInsertionOrder(t *testing.T) {
	s := New("/tmp")
	s.InsertTrajectoryCall(0, convstore.Step{Type: convstore.StepToolCall, Status: 1})
	s.InsertTrajectoryCall(1, convstore.Step{Type: convstore.StepToolCall, Status: 2})

	cp := convstore.Step{Type: convstore.StepCheckpoint, Status: 3}
	s.QueueCheckpoint(cp, 1)
	s.FlushCheckpoints()

	calls := s.TrajectoryFacts().Calls
	if len(calls) != 3 {
		t.Fatalf("calls = %d, want 3", len(calls))
	}
	if calls[1].Type != convstore.StepCheckpoint {
		t.Fatalf("checkpoint landed at %d, want 1", calls[1].Type)
	}
	if calls[0].Status != 1 || calls[2].Status != 2 {
		t.Fatalf("call order drifted: %+v", calls)
	}

	s.FlushCheckpoints()
	if got := len(s.TrajectoryFacts().Calls); got != 3 {
		t.Fatalf("a second flush inserted again: %d calls", got)
	}
}

func TestSupervisor_MultiTurnPersistenceStepOrder(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("HOME", tempHome)
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	s := New("/test/ws")
	convID := "test-multi-turn-order"

	// Turn 1
	s.Append(backend.Message{Role: "user", Content: "turn 1 request"})
	s.RecordTrajectoryCalls([]convstore.Step{
		{Type: convstore.StepToolCall, Status: 3, ToolName: "tool_t1", ToolArgs: `{"cmd":"ls"}`},
	})
	s.Append(backend.Message{Role: "assistant", Content: "turn 1 reply"})
	if err := s.Save(convID, "Test Title"); err != nil {
		t.Fatalf("Save turn 1 failed: %v", err)
	}

	// Turn 2
	s.Append(backend.Message{Role: "user", Content: "turn 2 request"})
	s.RecordTrajectoryCalls([]convstore.Step{
		{Type: convstore.StepToolCall, Status: 3, ToolName: "tool_t2", ToolArgs: `{"cmd":"pwd"}`},
	})
	s.Append(backend.Message{Role: "assistant", Content: "turn 2 reply"})
	if err := s.Save(convID, "Test Title"); err != nil {
		t.Fatalf("Save turn 2 failed: %v", err)
	}

	// Load steps directly from DB and assert step ordering
	dbPath := convstore.ConversationPath(convID)
	c, err := convstore.LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(c.Steps) != 6 {
		t.Fatalf("expected 6 steps across 2 turns, got %d: %+v", len(c.Steps), c.Steps)
	}

	// Step 0: User turn 1
	if c.Steps[0].Type != convstore.StepUserInput || c.Steps[0].Text != "turn 1 request" {
		t.Errorf("step 0 = %+v, want User 'turn 1 request'", c.Steps[0])
	}
	// Step 1: Tool turn 1
	if c.Steps[1].Type != convstore.StepToolCall || c.Steps[1].ToolName != "tool_t1" {
		t.Errorf("step 1 = %+v, want Tool 'tool_t1'", c.Steps[1])
	}
	// Step 2: Assistant turn 1
	if c.Steps[2].Type != convstore.StepModelResponse || c.Steps[2].Text != "turn 1 reply" {
		t.Errorf("step 2 = %+v, want Assistant 'turn 1 reply'", c.Steps[2])
	}
	// Step 3: User turn 2
	if c.Steps[3].Type != convstore.StepUserInput || c.Steps[3].Text != "turn 2 request" {
		t.Errorf("step 3 = %+v, want User 'turn 2 request'", c.Steps[3])
	}
	// Step 4: Tool turn 2 (MUST NOT have been dumped into turn 1!)
	if c.Steps[4].Type != convstore.StepToolCall || c.Steps[4].ToolName != "tool_t2" {
		t.Errorf("step 4 = %+v, want Tool 'tool_t2'", c.Steps[4])
	}
	// Step 5: Assistant turn 2
	if c.Steps[5].Type != convstore.StepModelResponse || c.Steps[5].Text != "turn 2 reply" {
		t.Errorf("step 5 = %+v, want Assistant 'turn 2 reply'", c.Steps[5])
	}
}
