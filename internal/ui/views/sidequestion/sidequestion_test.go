package sidequestion_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

type fauxStream struct {
	chunks []actor.StreamChunk
	pos    int
	closed bool
}

func (s *fauxStream) Recv() (actor.StreamChunk, error) {
	if s.closed || s.pos >= len(s.chunks) {
		return actor.StreamChunk{}, io.EOF
	}
	c := s.chunks[s.pos]
	s.pos++
	if c.FinishReason == "error" {
		return actor.StreamChunk{}, errors.New("upstream stream failed")
	}
	return c, nil
}

func (s *fauxStream) Close() error {
	s.closed = true
	return nil
}

type fauxProvider struct{}

func (p *fauxProvider) Generate(ctx context.Context, req actor.GenerateRequest) (actor.ResponseStream, error) {
	content := ""
	if len(req.Messages) > 0 {
		content = req.Messages[len(req.Messages)-1].Content
	}
	if strings.Contains(content, "trigger error branch") {
		return &fauxStream{
			chunks: []actor.StreamChunk{
				{FinishReason: "error"},
			},
		}, nil
	}

	return &fauxStream{
		chunks: []actor.StreamChunk{
			{Delta: "42", Usage: &actor.TokenUsage{TotalTokens: 4}},
			{FinishReason: "stop"},
		},
	}, nil
}

func newManager(t *testing.T) *sidequestion.Manager {
	t.Helper()
	dir := t.TempDir()
	m := sidequestion.NewManager(sidequestion.Options{
		Provider:     &fauxProvider{},
		Model:        "faux-1",
		CWD:          dir,
		AgentDir:     dir + "/.pig/agent",
		SystemPrompt: "You are a test side-question runtime.",
	})
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	return m
}

func TestAsk_StreamsAnswer(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	card, err := m.Ask(ctx, "What is 20+22?")
	if err != nil {
		t.Fatalf("Ask: %v", err)
	}
	q, a, tokens, state, errv := card.LockedSnapshot()
	if errv != nil {
		t.Fatalf("Card.Err: %v", errv)
	}
	if q != "What is 20+22?" {
		t.Errorf("Card.Question = %q, want %q", q, "What is 20+22?")
	}
	if a != "42" {
		t.Errorf("Card.Answer = %q, want %q", a, "42")
	}
	if tokens < 0 {
		t.Errorf("Card.Tokens = %d, want >= 0", tokens)
	}
	if state != sidequestion.StateDone {
		t.Errorf("Card.State = %q, want %q", state, sidequestion.StateDone)
	}
}

func TestAsk_IndependentServices(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	first, err := m.Ask(ctx, "What is 20+22?")
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	second, err := m.Ask(ctx, "What is 20+22?")
	if err != nil {
		t.Fatalf("second Ask: %v", err)
	}
	if first == second {
		t.Fatal("two Ask calls returned the same Card pointer")
	}
	if first.ID == second.ID {
		t.Errorf("Cards share an ID %q", first.ID)
	}
	if _, _, _, state, _ := first.LockedSnapshot(); state != sidequestion.StateDone {
		t.Errorf("first.State = %q, want done", state)
	}
	if _, _, _, state, _ := second.LockedSnapshot(); state != sidequestion.StateDone {
		t.Errorf("second.State = %q, want done", state)
	}
}

func TestAsk_StreamsError(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	card, err := m.Ask(ctx, "trigger error branch")
	if err == nil {
		t.Fatal("Ask returned a nil error for an error stream")
	}
	_, _, _, state, errv := card.LockedSnapshot()
	if state != sidequestion.StateFailed {
		t.Errorf("Card.State = %q, want %q", state, sidequestion.StateFailed)
	}
	if errv == nil {
		t.Error("Card.Err = nil, want non-nil for an error stream")
	}
}

func TestAsk_EmptyQuestion(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := m.Ask(ctx, ""); err == nil {
		t.Fatal("Ask('') returned a nil error")
	}
	if cards := m.List(); len(cards) != 0 {
		t.Errorf("Ask('') left %d cards registered", len(cards))
	}
}

func TestManager_ListAndLatestDone(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if got := m.List(); len(got) != 0 {
		t.Errorf("empty Manager.List() = %d cards, want 0", len(got))
	}
	if got := m.LatestDone(); got != nil {
		t.Errorf("empty Manager.LatestDone() = %v, want nil", got)
	}

	first, err := m.Ask(ctx, "What is 20+22?")
	if err != nil {
		t.Fatalf("first Ask: %v", err)
	}
	if got := m.LatestDone(); got != first {
		t.Errorf("LatestDone after one Ask = %p, want %p", got, first)
	}
	list := m.List()
	if len(list) != 1 || list[0] != first {
		t.Errorf("List after one Ask = %v, want [%p]", list, first)
	}

	forged := &sidequestion.Card{State: sidequestion.StatePending, ID: "forged"}
	list = append(list, forged)
	if got := m.LatestDone(); got != first {
		t.Errorf("LatestDone with a pending tail still = %p, want %p", got, first)
	}
	if got := m.Get(first.ID); got != first {
		t.Errorf("Get(%q) = %p, want %p", first.ID, got, first)
	}
	if got := m.Get(""); got != nil {
		t.Errorf("Get(\"\") = %p, want nil", got)
	}
	if got := m.Get("missing"); got != nil {
		t.Errorf("Get(\"missing\") = %p, want nil", got)
	}
}

func TestAsk_NilModel(t *testing.T) {
	m := sidequestion.NewManager(sidequestion.Options{
		CWD:      t.TempDir(),
		AgentDir: t.TempDir() + "/.pig/agent",
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	card, err := m.Ask(ctx, "anything")
	if err == nil {
		t.Fatal("Ask with nil model returned a nil error")
	}
	if card != nil {
		t.Errorf("Ask returned %v, want nil card on model-less Manager", card)
	}
}

func TestAsk_Concurrent(t *testing.T) {
	m := newManager(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := m.Ask(ctx, "What is 20+22?"); err != nil {
				t.Errorf("concurrent Ask: %v", err)
			}
		}()
	}
	wg.Wait()

	cards := m.List()
	if len(cards) != 3 {
		t.Errorf("Manager.List() after 3 concurrent Asks = %d cards, want 3", len(cards))
	}
	seen := make(map[string]bool)
	for _, c := range cards {
		if seen[c.ID] {
			t.Errorf("duplicate Card ID %q after concurrent Asks", c.ID)
		}
		seen[c.ID] = true
		if _, _, _, state, _ := c.LockedSnapshot(); state != sidequestion.StateDone {
			t.Errorf("Card %s State = %q, want done", c.ID, state)
		}
	}
}
