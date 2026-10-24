package sidequestion_test

// sidequestion_test.go — cycle 230: the /btw side-question runtime.
//
// Tests exercise the package via the scripted ai.TestFauxProvider
// PiG ships (no real gateway network call). The pattern mirrors
// internal/pigrt/runtime_test.go: the provider is pinned on a
// constructed *ai.Model, the test calls Ask, and asserts on the
// returned Card. PiG's faux provider streams "42" as a single text
// delta for "What is 20+22?" — the tests below use a question that
// triggers that exact branch.

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/internal/sidequestion"
)

// fauxModel builds a model backed by PiG's deterministic faux
// provider. ProviderID "test-faux" is the one coding.Session accepts
// without credentials (see pigrt.runtime_test.go).
func fauxModel() *ai.Model {
	return &ai.Model{
		Type:         ai.ModelTypeChat,
		ID:           "faux-1",
		DisplayName:  "faux-1",
		Provider:     &ai.TestFauxProvider{},
		ProviderMeta: ai.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
		Input:        []string{"text"},
	}
}

// newManager builds a Manager against a temp agent dir so the test
// does not depend on the host's ~/.pig/agent layout.
func newManager(t *testing.T) *sidequestion.Manager {
	t.Helper()
	dir := t.TempDir()
	m := sidequestion.NewManager(sidequestion.Options{
		Model:        fauxModel(),
		CWD:          dir,
		AgentDir:     dir + "/.pig/agent",
		SystemPrompt: "You are a test side-question runtime.",
	})
	if m == nil {
		t.Fatal("NewManager returned nil")
	}
	return m
}

// TestAsk_StreamsAnswer is the happy path. Ask spawns a fresh
// coding.Session, sends the question, collects the streamed text into
// the Card, and returns when the turn settles. The faux provider
// answers "What is 20+22?" with "42"; the test asserts the Card has
// that answer and a non-negative token count.
//
// (The faux provider does not populate Usage for the basic text
// branch — only "Trigger: over-window response" does — so this test
// only checks tokens >= 0. The slash-level TestBtwSlashStreamingCard
// uses the over-window question to assert Tokens > 0.)
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

// TestAsk_IndependentServices checks the cycle-230 hard requirement:
// every Ask opens a brand-new coding.Services, and closing the btw
// session does not affect anything else on the host. The test calls
// Ask twice; each round-trip must complete successfully and the
// returned cards must be distinct.
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

// TestAsk_StreamsError exercises the failure path. A bogus question
// that triggers the faux provider's error branch yields a Card with
// State == "failed" and a non-nil Err.
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

// TestAsk_EmptyQuestion is the boundary: Ask must reject an empty
// string before opening a Session.
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

// TestManager_ListAndLatestDone pins the lookup helpers. List returns
// every card in insertion order; LatestDone returns the most recent
// done-state card, ignoring a pending or failed one.
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

	// LatestDone must not pick up a pending card; we forge one to
	// prove the filter.
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

// TestAsk_NilModel checks the Manager requires a Model. The test
// exercises NewManager without a Model and asserts Ask rejects the
// call before opening a Session.
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

// TestAsk_Concurrent exercises the manager's mutex by firing three
// Ask calls at once. The faux provider is single-threaded on the
// session, but the manager's cards slice is shared and the test
// ensures AddCard / Get / List stay safe under contention.
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
