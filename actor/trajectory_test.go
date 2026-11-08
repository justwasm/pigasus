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

// waitFor polls cond until it is true or the deadline passes.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// stagingProvider emits one delta, then blocks until release or cancellation.
type stagingProvider struct {
	release chan struct{}
	started chan struct{}
	once    sync.Once
}

func newStagingProvider() *stagingProvider {
	return &stagingProvider{release: make(chan struct{}), started: make(chan struct{})}
}

func (p *stagingProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	return &stagingStream{ctx: ctx, release: p.release, started: p.started, once: &p.once}, nil
}

type stagingStream struct {
	ctx     context.Context
	release chan struct{}
	started chan struct{}
	once    *sync.Once
	sent    bool
}

func (s *stagingStream) Recv() (actor.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		s.once.Do(func() { close(s.started) })
		return actor.StreamChunk{Delta: "half"}, nil
	}
	select {
	case <-s.release:
		return actor.StreamChunk{}, io.EOF
	case <-s.ctx.Done():
		return actor.StreamChunk{}, s.ctx.Err()
	}
}

func (s *stagingStream) Close() error { return nil }

// TestTrajectory_StagedRunningTurn pins that a turn is visible while it runs:
// running status, model, and a running step with a start time.
func TestTrajectory_StagedRunningTurn(t *testing.T) {
	provider := newStagingProvider()
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{Provider: provider, Model: "model-x"},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	<-provider.started

	waitFor(t, "a staged running turn", func() bool {
		last := act.Trajectory().LastTurn()
		return last != nil && last.Status == "running" && len(last.Steps) == 1 &&
			last.Steps[0].Status == actor.StepRunning
	})
	staged := act.Trajectory().LastTurn()
	if staged.Model != "model-x" {
		t.Errorf("Model = %q, want model-x", staged.Model)
	}
	if staged.Steps[0].StartedAt.IsZero() {
		t.Error("the staged step has no StartedAt")
	}

	close(provider.release)
	for range turn.Events() {
	}

	final := act.Trajectory().LastTurn()
	if final.Status != "done" {
		t.Fatalf("final status = %q, want done", final.Status)
	}
	if len(final.Steps) != 1 || final.Steps[0].Status != actor.StepDone {
		t.Fatalf("final step = %+v, want one done step", final.Steps)
	}
	if final.Steps[0].CompletedAt.IsZero() {
		t.Error("the completed step has no CompletedAt")
	}
}

// TestTrajectory_PartialStepOnCancel pins that the text the user saw survives
// a cancellation in the trajectory and in FinalOutput.
func TestTrajectory_PartialStepOnCancel(t *testing.T) {
	provider := newStagingProvider()
	act, err := actor.Spawn(actor.Config{Default: actor.TurnConfig{Provider: provider}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	turn, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	<-provider.started

	if err := turn.Cancel("stop"); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	for range turn.Events() {
	}

	waitFor(t, "the cancelled turn record", func() bool {
		last := act.Trajectory().LastTurn()
		return last != nil && last.Status == "cancelled"
	})
	last := act.Trajectory().LastTurn()
	if len(last.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(last.Steps))
	}
	if last.Steps[0].Status != actor.StepCancelled {
		t.Errorf("step status = %q, want cancelled", last.Steps[0].Status)
	}
	if last.Steps[0].Delta != "half" {
		t.Errorf("step delta = %q, want half", last.Steps[0].Delta)
	}
	if last.FinalOutput != "half" {
		t.Errorf("FinalOutput = %q, want half (what the user saw)", last.FinalOutput)
	}
}

// errorAfterDeltaStream emits a delta and then fails with err.
type errorAfterDeltaStream struct {
	delta string
	err   error
	sent  bool
}

func (s *errorAfterDeltaStream) Recv() (actor.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return actor.StreamChunk{Delta: s.delta}, nil
	}
	return actor.StreamChunk{}, s.err
}

func (s *errorAfterDeltaStream) Close() error { return nil }

// flakyProvider fails the first turn with a transient error, then succeeds and
// records the messages it was given.
type flakyProvider struct {
	mu       sync.Mutex
	calls    int
	messages [][]actor.ConversationMessage
}

func (p *flakyProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	p.mu.Lock()
	p.calls++
	call := p.calls
	p.messages = append(p.messages, req.Messages)
	p.mu.Unlock()

	if call == 1 {
		return &errorAfterDeltaStream{delta: "half", err: errors.New("connection reset by peer")}, nil
	}
	return &errorAfterDeltaStream{delta: " rest", err: io.EOF}, nil
}

func (p *flakyProvider) Calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

// TestTrajectory_NetworkInterruptionContinues pins that a transient stream
// error recovers on the same turn with the recovery instruction.
func TestTrajectory_NetworkInterruptionContinues(t *testing.T) {
	provider := &flakyProvider{}
	act, err := actor.Spawn(actor.Config{Default: actor.TurnConfig{Provider: provider}})
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

	if provider.Calls() != 2 {
		t.Fatalf("provider calls = %d, want 2", provider.Calls())
	}
	last := act.Trajectory().LastTurn()
	if last.Status != "done" {
		t.Fatalf("status = %q, want done", last.Status)
	}
	if len(last.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(last.Steps))
	}
	if last.Steps[0].Status != actor.StepInterrupted {
		t.Errorf("step 0 status = %q, want interrupted", last.Steps[0].Status)
	}
	if last.Steps[0].Delta != "half" || last.Steps[1].Delta != " rest" {
		t.Errorf("deltas = %q / %q", last.Steps[0].Delta, last.Steps[1].Delta)
	}
	if last.FinalOutput != "half rest" {
		t.Errorf("FinalOutput = %q, want 'half rest'", last.FinalOutput)
	}

	// The recovery instruction rides the second request as guidance.
	provider.mu.Lock()
	second := provider.messages[1]
	provider.mu.Unlock()
	found := false
	for _, m := range second {
		if strings.Contains(m.Content, actor.StreamInterruptedMessage) {
			found = true
		}
	}
	if !found {
		t.Error("the recovery request did not carry StreamInterruptedMessage")
	}
}

// TestTrajectory_AutoContinueLimit pins the reference's 3-recovery ceiling.
func TestTrajectory_AutoContinueLimit(t *testing.T) {
	provider := &lengthProvider{}
	act, err := actor.Spawn(actor.Config{Default: actor.TurnConfig{Provider: provider}})
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
	if !errors.Is(turn.Err(), actor.ErrAutoContinueLimit) {
		t.Fatalf("turn error = %v, want ErrAutoContinueLimit", turn.Err())
	}

	last := act.Trajectory().LastTurn()
	if last.Status != "error" {
		t.Fatalf("status = %q, want error", last.Status)
	}
	if len(last.Steps) != actor.AutoContMaxDepth+1 {
		t.Fatalf("steps = %d, want %d", len(last.Steps), actor.AutoContMaxDepth+1)
	}
	for i, step := range last.Steps {
		if step.FinishReason != "length" {
			t.Errorf("step %d finish reason = %q, want length", i, step.FinishReason)
		}
	}
}

// lengthProvider always returns a step truncated by length.
type lengthProvider struct{}

func (lengthProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	return &sliceStream{chunks: []actor.StreamChunk{
		{Delta: "x", FinishReason: "length"},
	}}, nil
}

// TestTrajectory_PerStepUsage pins that each step carries its own usage, not
// the turn's running total.
func TestTrajectory_PerStepUsage(t *testing.T) {
	mock := actor.NewMockProvider()
	mock.EnqueueChunks([]actor.StreamChunk{
		{
			ToolCalls: []actor.ToolCall{{ID: "c1", Name: "noop", Input: "{}"}},
			Usage:     &actor.TokenUsage{TotalTokens: 5, PromptTokens: 3, OutputTokens: 2},
		},
	})
	mock.EnqueueChunks([]actor.StreamChunk{
		{Delta: "done", FinishReason: "stop", Usage: &actor.TokenUsage{TotalTokens: 7, PromptTokens: 4, OutputTokens: 3}},
	})

	tool := actor.NewFuncTool("noop", "no-op", func(ctx context.Context, input string) (string, error) {
		return "ok", nil
	})
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{Provider: mock, Tools: []actor.Tool{tool}},
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

	last := act.Trajectory().LastTurn()
	if len(last.Steps) != 2 {
		t.Fatalf("steps = %d, want 2", len(last.Steps))
	}
	if last.Steps[0].Usage == nil || last.Steps[0].Usage.TotalTokens != 5 {
		t.Errorf("step 0 usage = %+v, want 5", last.Steps[0].Usage)
	}
	if last.Steps[1].Usage == nil || last.Steps[1].Usage.TotalTokens != 7 {
		t.Errorf("step 1 usage = %+v, want 7", last.Steps[1].Usage)
	}
	if last.Usage.TotalTokens != 12 {
		t.Errorf("turn usage = %d, want 12", last.Usage.TotalTokens)
	}
}
