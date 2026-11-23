package actor_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

type midStreamErrProvider struct {
	mu       sync.Mutex
	calls    int
	requests []actor.GenerateRequest
	err      error
}

func (p *midStreamErrProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	p.requests = append(p.requests, req)
	if p.calls == 1 {
		return &errAfterPartialStream{err: p.err}, nil
	}
	return &okSliceStream{
		chunks: []actor.StreamChunk{
			{Delta: "Part 2.", FinishReason: "stop"},
		},
	}, nil
}

type errAfterPartialStream struct {
	step int
	err  error
}

func (s *errAfterPartialStream) Recv() (actor.StreamChunk, error) {
	s.step++
	if s.step == 1 {
		return actor.StreamChunk{Delta: "Part 1 "}, nil
	}
	return actor.StreamChunk{}, s.err
}

func (s *errAfterPartialStream) Close() error { return nil }

type okSliceStream struct {
	chunks []actor.StreamChunk
	idx    int
}

func (s *okSliceStream) Recv() (actor.StreamChunk, error) {
	if s.idx >= len(s.chunks) {
		return actor.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *okSliceStream) Close() error { return nil }

func TestActor_ShouldRecoverMidStreamPreservesTrajectory(t *testing.T) {
	errMidStream := errors.New("mid-stream failover recoverable")
	prov := &midStreamErrProvider{err: errMidStream}

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			SystemPrompt:  "You are helpful.",
			Provider:      prov,
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
	var summary *actor.TurnSummary
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
			case actor.EventError:
				t.Fatalf("unexpected turn error: %v (%s)", ev.Err, ev.Error)
			}
		}
	}

	if deltas.String() != "Part 1 Part 2." {
		t.Fatalf("deltas = %q, want 'Part 1 Part 2.'", deltas.String())
	}
	if summary == nil || summary.Output != "Part 1 Part 2." {
		t.Fatalf("summary = %+v, want Output 'Part 1 Part 2.'", summary)
	}

	// Verify the second Generate call received the preserved partial assistant step in trajectory.
	prov.mu.Lock()
	reqs := append([]actor.GenerateRequest(nil), prov.requests...)
	prov.mu.Unlock()
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
