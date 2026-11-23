package actor_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestActor_ShouldRecoverMidStreamPreservesTrajectory(t *testing.T) {
	errMidStream := errors.New("mid-stream failover recoverable")

	mock := actor.NewMockProvider()
	// Attempt 1: emits partial text then fails mid-stream with errMidStream.
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Part 1 "},
		{Err: errMidStream},
	})
	// Attempt 2 (recovery): completes the response.
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "Part 2."},
		{FinishReason: "stop"},
	})

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			SystemPrompt:  "You are helpful.",
			Provider:      mock,
			MaxRecoveries: 2,
			ShouldRecover: func(err error) bool {
				return errors.Is(err, errMidStream)
			},
		},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "Continue cleanly")
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}

	var deltas strings.Builder
	var completed bool
	var summary actor.TurnSummary
	timeout := time.After(3 * time.Second)
	for !completed {
		select {
		case <-timeout:
			t.Fatal("timed out waiting for turn completion")
		case ev := <-turn.Events():
			switch ev.Type {
			case actor.EventDelta:
				deltas.WriteString(ev.Delta)
			case actor.EventTurnCompleted:
				completed = true
				summary = ev.TurnSummary
			case actor.EventTurnFailed:
				t.Fatalf("unexpected turn failure: %v", ev.Err)
			}
		}
	}

	if deltas.String() != "Part 1 Part 2." {
		t.Fatalf("deltas = %q, want 'Part 1 Part 2.'", deltas.String())
	}
	if summary.Output != "Part 1 Part 2." {
		t.Fatalf("summary.Output = %q, want 'Part 1 Part 2.'", summary.Output)
	}

	// Verify the second Generate call received the preserved partial assistant step in trajectory.
	reqs := mock.Requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 Generate requests, got %d", len(reqs))
	}
	var sawPartial bool
	for _, msg := range reqs[1].Messages {
		if msg.Role == actor.RoleAssistant && strings.Contains(msg.Content, "Part 1 ") {
			sawPartial = true
		}
	}
	if !sawPartial {
		t.Fatalf("expected recovery request to preserve partial assistant output in trajectory, got %+v", reqs[1].Messages)
	}
}
