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
