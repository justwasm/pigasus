package actor_test

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

type sliceStream struct {
	chunks []actor.StreamChunk
	i      int
}

func (s *sliceStream) Recv() (actor.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return actor.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return c, nil
}

func (s *sliceStream) Close() error { return nil }

func drain(t *testing.T, h *actor.TurnHandle) []actor.Event {
	t.Helper()
	var events []actor.Event
	for ev := range h.Events() {
		events = append(events, ev)
	}
	return events
}

func waitDone(t *testing.T, h *actor.TurnHandle) {
	t.Helper()
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not finish")
	}
}

// fixedProvider always returns the same chunks.
type fixedProvider struct {
	chunks []actor.StreamChunk
}

func (p *fixedProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	return &sliceStream{chunks: p.chunks}, nil
}

// TestTurnHandle_LosslessUnderBackpressure pins the difference between the
// turn channel and the broadcast: a slow reader loses nothing.
func TestTurnHandle_LosslessUnderBackpressure(t *testing.T) {
	const deltas = 40
	chunks := make([]actor.StreamChunk, 0, deltas+1)
	for i := 0; i < deltas; i++ {
		chunks = append(chunks, actor.StreamChunk{Delta: "x"})
	}
	chunks = append(chunks, actor.StreamChunk{FinishReason: "stop"})

	act, err := actor.Spawn(actor.Config{
		Default:         actor.TurnConfig{Provider: &fixedProvider{chunks: chunks}},
		EventBufferSize: 1,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	h, err := act.Prompt(context.Background(), "stream slowly")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	got := 0
	var completed bool
	for ev := range h.Events() {
		if ev.Type == actor.EventDelta {
			got++
			// Read much slower than the producer so the one-slot channel
			// has to apply backpressure.
			time.Sleep(time.Millisecond)
		}
		if ev.Type == actor.EventTurnCompleted {
			completed = true
		}
	}
	waitDone(t, h)

	if got != deltas {
		t.Fatalf("deltas = %d, want %d (the turn channel must not drop)", got, deltas)
	}
	if !completed {
		t.Fatal("turn completion event was dropped")
	}
	if err := h.Err(); err != nil {
		t.Fatalf("Err() = %v, want nil", err)
	}
}

// blockingProvider holds the first Generate call until release is closed and
// records the model of every call.
type blockingProvider struct {
	started chan struct{}
	release chan struct{}
	once    sync.Once

	mu     sync.Mutex
	n      int
	models []string
}

func newBlockingProvider() *blockingProvider {
	return &blockingProvider{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (p *blockingProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	p.mu.Lock()
	p.n++
	first := p.n == 1
	p.models = append(p.models, req.Model)
	p.mu.Unlock()

	if first {
		p.once.Do(func() { close(p.started) })
		select {
		case <-p.release:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return &sliceStream{chunks: []actor.StreamChunk{{Delta: "done", FinishReason: "stop"}}}, nil
}

func (p *blockingProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.n
}

func (p *blockingProvider) Models() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(p.models))
	copy(out, p.models)
	return out
}

// TestTurnHandle_CancelQueuedTurn pins turn-scoped cancellation: cancelling a
// queued turn leaves the running one alone, and the queued turn never reaches
// the provider.
func TestTurnHandle_CancelQueuedTurn(t *testing.T) {
	provider := newBlockingProvider()
	act, err := actor.Spawn(actor.Config{Default: actor.TurnConfig{Provider: provider}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	first, err := act.Prompt(context.Background(), "first")
	if err != nil {
		t.Fatalf("first prompt: %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn never started")
	}

	second, err := act.Prompt(context.Background(), "second")
	if err != nil {
		t.Fatalf("second prompt: %v", err)
	}
	if err := second.Cancel("not needed"); err != nil {
		t.Fatalf("cancel queued turn: %v", err)
	}

	// The running turn must still be running.
	select {
	case <-first.Done():
		t.Fatal("cancelling the queued turn finished the running one")
	default:
	}

	close(provider.release)
	drain(t, first)
	waitDone(t, first)
	if err := first.Err(); err != nil {
		t.Fatalf("first turn Err() = %v", err)
	}

	waitDone(t, second)
	if !errors.Is(second.Err(), actor.ErrTurnCancelled) {
		t.Fatalf("second turn Err() = %v, want ErrTurnCancelled", second.Err())
	}
	if got := provider.calls(); got != 1 {
		t.Fatalf("provider calls = %d, want 1 (the cancelled turn must not run)", got)
	}
	if err := second.Cancel("again"); err != nil {
		t.Fatalf("cancelling a finished turn must be a no-op, got %v", err)
	}
}

// TestTurnRequest_PerTurnConfiguration pins that a queued turn's model cannot
// be rewritten by the next submission. Before the configuration travelled with
// the message, the second turn's model was applied to the first one mid-flight.
func TestTurnRequest_PerTurnConfiguration(t *testing.T) {
	provider := newBlockingProvider()
	act, err := actor.Spawn(actor.Config{Default: actor.TurnConfig{Provider: provider, Model: "default"}})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	first, err := act.StartTurn(context.Background(), actor.TurnRequest{
		Prompt: "one",
		Model:  "model-a",
	})
	if err != nil {
		t.Fatalf("first turn: %v", err)
	}
	select {
	case <-provider.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first turn never started")
	}

	// Submitted while the first turn is blocked.
	second, err := act.StartTurn(context.Background(), actor.TurnRequest{
		Prompt: "two",
		Model:  "model-b",
	})
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}

	// Let the first turn's step continue, then drain both.
	close(provider.release)
	drain(t, first)
	drain(t, second)

	got := provider.Models()
	if len(got) != 2 || got[0] != "model-a" || got[1] != "model-b" {
		t.Fatalf("models seen by the provider = %v, want [model-a model-b]", got)
	}
}

// queueProvider answers each Generate call with the next queued response.
type queueProvider struct {
	mu        sync.Mutex
	responses [][]actor.StreamChunk
}

func (p *queueProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(p.responses) == 0 {
		return &sliceStream{chunks: []actor.StreamChunk{{Delta: "done", FinishReason: "stop"}}}, nil
	}
	chunks := p.responses[0]
	p.responses = p.responses[1:]
	return &sliceStream{chunks: chunks}, nil
}

// TestTurnRequest_ToolsFollowTheTurn pins that a turn's tool set is the one it
// was submitted with, and that it does not mutate the actor's default registry.
func TestTurnRequest_ToolsFollowTheTurn(t *testing.T) {
	defaultTool := actor.NewFuncTool("alpha", "the actor default", func(ctx context.Context, input string) (string, error) {
		return "alpha ran", nil
	})
	// The turn is submitted with an empty tool set; the model nevertheless
	// calls the actor's default tool. The turn must not see it.
	provider := &queueProvider{responses: [][]actor.StreamChunk{
		{{ToolCalls: []actor.ToolCall{{ID: "call-1", Name: "alpha", Input: "{}"}}}},
		{{Delta: "finished", FinishReason: "stop"}},
	}}

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{Provider: provider, Tools: []actor.Tool{defaultTool}},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	h, err := act.StartTurn(context.Background(), actor.TurnRequest{
		Prompt: "hi",
		Tools:  []actor.Tool{},
	})
	if err != nil {
		t.Fatalf("turn: %v", err)
	}

	sawUnregistered := false
	for _, ev := range drain(t, h) {
		if ev.Type == actor.EventToolCallFinished && ev.ToolResult != nil {
			if ev.ToolResult.Error == "" || ev.ToolResult.Output == "alpha ran" {
				t.Fatalf("tool result = %+v, want an unregistered-tool error", *ev.ToolResult)
			}
			sawUnregistered = true
		}
	}
	if !sawUnregistered {
		t.Fatal("the turn ran the actor's default tool; tools are not per-turn")
	}

	// The default is still there: a turn without a tools override runs alpha.
	provider.mu.Lock()
	provider.responses = append(provider.responses,
		[]actor.StreamChunk{{ToolCalls: []actor.ToolCall{{ID: "call-2", Name: "alpha", Input: "{}"}}}},
		[]actor.StreamChunk{{Delta: "second", FinishReason: "stop"}},
	)
	provider.mu.Unlock()

	h2, err := act.Prompt(context.Background(), "again")
	if err != nil {
		t.Fatalf("second turn: %v", err)
	}
	sawDefault := false
	for _, ev := range drain(t, h2) {
		if ev.Type == actor.EventToolCallFinished && ev.ToolResult != nil && ev.ToolResult.Output == "alpha ran" {
			sawDefault = true
		}
	}
	if !sawDefault {
		t.Fatal("the default tool set was lost after a per-turn override")
	}
}

// TestHistoryRevision_TracksMutation pins the counter a host uses to notice a
// reset it did not perform.
func TestHistoryRevision_TracksMutation(t *testing.T) {
	provider := &fixedProvider{chunks: []actor.StreamChunk{
		{Delta: "hi", FinishReason: "stop"},
	}}
	act, err := actor.Spawn(actor.Config{
		Default:        actor.TurnConfig{Provider: provider},
		InitialHistory: []actor.ConversationMessage{{Role: actor.RoleUser, Content: "seed"}},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	seeded := act.Trajectory().Revision()
	if seeded == 0 {
		t.Fatal("seeding did not bump the revision")
	}
	h, err := act.Prompt(context.Background(), "hello")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	drain(t, h)
	afterTurn := act.Trajectory().Revision()
	if afterTurn <= seeded {
		t.Fatalf("revision after turn = %d, want > %d", afterTurn, seeded)
	}

	act.ResetHistory([]actor.ConversationMessage{{Role: actor.RoleUser, Content: "cleared"}})
	if reset := act.Trajectory().Revision(); reset <= afterTurn {
		t.Fatalf("revision after reset = %d, want > %d", reset, afterTurn)
	}
}

// TestTimeNow_StampsActorFacts pins that Config.TimeNow reaches everything the
// actor stamps itself.
func TestTimeNow_StampsActorFacts(t *testing.T) {
	fixed := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{Provider: &fixedProvider{chunks: []actor.StreamChunk{
			{Delta: "hi", FinishReason: "stop"},
		}}},
		TimeNow: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	h, err := act.Prompt(context.Background(), "clock")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}
	for _, ev := range drain(t, h) {
		if !ev.Timestamp.Equal(fixed) {
			t.Fatalf("event %s timestamp = %v, want %v", ev.Type, ev.Timestamp, fixed)
		}
	}
	last := act.Trajectory().LastTurn()
	if last == nil || !last.CreatedAt.Equal(fixed) || !last.CompletedAt.Equal(fixed) {
		t.Fatalf("turn timestamps = %v/%v, want %v", last.CreatedAt, last.CompletedAt, fixed)
	}
	if st := act.State(); !st.UpdatedAt.Equal(fixed) {
		t.Fatalf("state UpdatedAt = %v, want %v", st.UpdatedAt, fixed)
	}
}

// gatedRecordingTool requires approval and records each call it runs.
type gatedRecordingTool struct {
	mu  *sync.Mutex
	ran *[]string
}

func (t *gatedRecordingTool) Name() string        { return "gate" }
func (t *gatedRecordingTool) Description() string { return "needs approval" }
func (t *gatedRecordingTool) Definition() actor.ToolDefinition {
	return actor.ToolDefinition{Name: "gate"}
}
func (t *gatedRecordingTool) RequiresApproval(string) bool { return true }
func (t *gatedRecordingTool) Execute(ctx context.Context, input string) (actor.ToolResult, error) {
	t.mu.Lock()
	*t.ran = append(*t.ran, input)
	t.mu.Unlock()
	return actor.ToolResult{Output: "ran"}, nil
}

// TestApprove_DuplicateDoesNotPoisonTheNextWait pins the single-slot approval:
// a repeated approval for one call must not survive its wait and be consumed
// by the next call (which used to be denied with "approval id mismatch").
func TestApprove_DuplicateDoesNotPoisonTheNextWait(t *testing.T) {
	var mu sync.Mutex
	var ran []string
	provider := &queueProvider{responses: [][]actor.StreamChunk{
		{{ToolCalls: []actor.ToolCall{{ID: "call-1", Name: "gate", Input: "first"}}}},
		{{ToolCalls: []actor.ToolCall{{ID: "call-2", Name: "gate", Input: "second"}}}},
		{{Delta: "done", FinishReason: "stop"}},
	}}

	act, err := actor.Spawn(actor.Config{
		Default: actor.TurnConfig{
			Provider: provider,
			Tools:    []actor.Tool{&gatedRecordingTool{mu: &mu, ran: &ran}},
		},
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	defer act.Close()

	h, err := act.Prompt(context.Background(), "run both")
	if err != nil {
		t.Fatalf("prompt: %v", err)
	}

	for ev := range h.Events() {
		switch ev.Type {
		case actor.EventApprovalRequired:
			if ev.ToolCall == nil {
				continue
			}
			if ev.ToolCall.ID == "call-1" {
				// Double-click / retry: every extra decision must be
				// harmless, not queued for the next call.
				for i := 0; i < 5; i++ {
					_ = act.Approve("call-1", true, "yes")
				}
				continue
			}
			_ = act.Approve(ev.ToolCall.ID, true, "yes")
		case actor.EventToolCallFinished:
			if ev.ToolResult != nil && ev.ToolResult.Error != "" {
				t.Errorf("tool %s finished with %q", ev.ToolResult.ToolCallID, ev.ToolResult.Error)
			}
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if len(ran) != 2 || ran[0] != "first" || ran[1] != "second" {
		t.Fatalf("ran = %v, want [first second]", ran)
	}
}
