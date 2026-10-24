package convstore

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

func TestNewUUIDv4(t *testing.T) {
	id, err := NewUUIDv4()
	if err != nil {
		t.Fatalf("NewUUIDv4 failed: %v", err)
	}
	if len(id) != 36 {
		t.Errorf("NewUUIDv4 returned %q (len %d, want 36)", id, len(id))
	}
	parts := strings.Split(id, "-")
	if len(parts) != 5 {
		t.Fatalf("expected 5 segments, got %d", len(parts))
	}
}

func TestSessionToSteps(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: ""}, // should be elided
		{Role: "assistant", Content: "world\n\n"},
	}
	steps := SessionToSteps(sess)
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].Text != "hello" || steps[0].Type != StepUserInput {
		t.Errorf("step 0 mismatch: %+v", steps[0])
	}
	if steps[1].Text != "world" || steps[1].Type != StepModelResponse {
		t.Errorf("step 1 mismatch: %+v", steps[1])
	}
}

func TestForkConversationEmpty(t *testing.T) {
	id, msg := ForkConversation(nil, "abc")
	if id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
	if !strings.Contains(msg, "Error: no active conversation to fork") {
		t.Errorf("unexpected message: %q", msg)
	}
}
