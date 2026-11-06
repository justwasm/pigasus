package sidequestion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// State values a Card carries during its lifetime.
const (
	StatePending   = "pending"
	StateStreaming = "streaming"
	StateDone      = "done"
	StateFailed    = "failed"
)

// DefaultSystemPrompt is the reference agy system prompt for side questions.
const DefaultSystemPrompt = "Help explain this concept or piece of code succinctly. Do not dive too deeply into one topic. Use context about the codebase to give an accurate explanation."

// Card is one side-question + its streamed answer.
type Card struct {
	ID          string
	Question    string
	Answer      string
	Tokens      int
	State       string
	Err         error
	Expanded    bool
	Scroll      int
	ToolCalls   []string
	CurrentTool string

	mu sync.Mutex
}

// ToolCount returns the number of tools called by this card.
func (c *Card) ToolCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.ToolCalls)
}

// GetCurrentTool returns the tool currently executing.
func (c *Card) GetCurrentTool() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.CurrentTool
}

// SetCurrentTool sets the tool currently executing.
func (c *Card) SetCurrentTool(tool string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.CurrentTool = tool
}

// RecordToolCall records that a tool finished executing.
func (c *Card) RecordToolCall(tool string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ToolCalls = append(c.ToolCalls, tool)
	c.CurrentTool = ""
}

// IsActive reports whether the question is still in-flight.
func (c *Card) IsActive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.State == StatePending || c.State == StateStreaming
}

// IsExpanded reports whether the card is expanded.
func (c *Card) IsExpanded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Expanded
}

// SetExpanded sets the card's expansion state.
func (c *Card) SetExpanded(exp bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Expanded = exp
}

// ToggleExpanded flips the card's expansion state and returns the new value.
func (c *Card) ToggleExpanded() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Expanded = !c.Expanded
	return c.Expanded
}

// MarkCancelled marks the card as failed due to cancellation if it was in progress.
func (c *Card) MarkCancelled() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.State == StatePending || c.State == StateStreaming {
		c.State = StateFailed
		c.Err = errors.New("cancelled")
	}
}

// LockedSnapshot returns a copy of the card's mutable fields under mu.
func (c *Card) LockedSnapshot() (question, answer string, tokens int, state string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Question, c.Answer, c.Tokens, c.State, c.Err
}

// StreamFn streams response text from the model for a prompt, calling onDelta for each chunk.
type StreamFn func(ctx context.Context, prompt string, onDelta func(delta string)) (tokens int, err error)

// ToolHooks receives stream and tool lifecycle callbacks during a side question.
type ToolHooks struct {
	OnDelta     func(delta string)
	OnToolStart func(name string)
	OnToolEnd   func(name string)
}

// StreamWithToolsFn streams model response text and executes tool calls.
type StreamWithToolsFn func(ctx context.Context, prompt string, hooks ToolHooks) (tokens int, err error)

// Options configures one Manager.
type Options struct {
	StreamFn          StreamFn
	StreamWithToolsFn StreamWithToolsFn
	Model             string
	CWD               string
	AgentDir          string
	SystemPrompt      string
	SessionTimeout    time.Duration
}

// Manager tracks Cards for the lifetime of one TUI session.
type Manager struct {
	mu                sync.Mutex
	cards             []*Card
	nextSeq           int
	streamFn          StreamFn
	streamWithToolsFn StreamWithToolsFn
	model             string
	cwd               string
	agentDir          string
	systemPrompt      string
	sessionTimeout    time.Duration
}

// NewManager builds a Manager.
func NewManager(opts Options) *Manager {
	if opts.CWD == "" {
		opts.CWD = "."
	}
	if opts.SessionTimeout == 0 {
		opts.SessionTimeout = 2 * time.Minute
	}
	if opts.SystemPrompt == "" {
		opts.SystemPrompt = DefaultSystemPrompt
	}
	return &Manager{
		streamFn:          opts.StreamFn,
		streamWithToolsFn: opts.StreamWithToolsFn,
		model:             opts.Model,
		cwd:               opts.CWD,
		agentDir:          opts.AgentDir,
		systemPrompt:      opts.SystemPrompt,
		sessionTimeout:    opts.SessionTimeout,
	}
}

// ErrEmptyQuestion is returned by Ask when the caller passes an empty question.
var ErrEmptyQuestion = errors.New("sidequestion: empty question")

// StartAsk creates a Card in StatePending, registers it with the Manager,
// and returns the Card along with a runner function that executes the query in the background.
func (m *Manager) StartAsk(ctx context.Context, question string) (*Card, func() error, error) {
	if question == "" {
		return nil, nil, ErrEmptyQuestion
	}
	if m.streamFn == nil && m.streamWithToolsFn == nil {
		return nil, nil, errors.New("sidequestion: Manager requires StreamFn or StreamWithToolsFn")
	}

	card := &Card{
		ID:       m.allocID(),
		Question: question,
		State:    StatePending,
	}
	m.addCard(card)

	run := func() error {
		askCtx, cancel := context.WithTimeout(ctx, m.sessionTimeout)
		defer cancel()

		onDelta := func(delta string) {
			card.mu.Lock()
			if card.State == StatePending {
				card.State = StateStreaming
			}
			card.Answer += delta
			card.mu.Unlock()
		}

		if m.streamWithToolsFn != nil {
			hooks := ToolHooks{
				OnDelta: onDelta,
				OnToolStart: func(name string) {
					card.SetCurrentTool(name)
				},
				OnToolEnd: func(name string) {
					card.RecordToolCall(name)
				},
			}
			toks, err := m.streamWithToolsFn(askCtx, question, hooks)
			card.mu.Lock()
			defer card.mu.Unlock()
			if err != nil {
				card.State = StateFailed
				card.Err = err
				return err
			}
			card.Tokens = toks
			card.State = StateDone
			return nil
		}

		if m.streamFn != nil {
			toks, err := m.streamFn(askCtx, question, onDelta)
			card.mu.Lock()
			defer card.mu.Unlock()
			if err != nil {
				card.State = StateFailed
				card.Err = err
				return err
			}
			card.Tokens = toks
			card.State = StateDone
			return nil
		}

		card.mu.Lock()
		card.State = StateFailed
		card.Err = errors.New("sidequestion: no stream runner available")
		card.mu.Unlock()
		return card.Err
	}

	return card, run, nil
}

// Ask sends the question and blocks until the answer settles.
func (m *Manager) Ask(ctx context.Context, question string) (*Card, error) {
	card, run, err := m.StartAsk(ctx, question)
	if err != nil {
		return card, err
	}
	return card, run()
}

// List returns every Card registered with the Manager in insertion order.
func (m *Manager) List() []*Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Card, len(m.cards))
	copy(out, m.cards)
	return out
}

// Get returns the Card with the given ID, or nil when no such card exists.
func (m *Manager) Get(id string) *Card {
	if id == "" {
		return nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.cards {
		if c.ID == id {
			return c
		}
	}
	return nil
}

// Latest returns the most recent Card, regardless of state.
func (m *Manager) Latest() *Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cards) == 0 {
		return nil
	}
	return m.cards[len(m.cards)-1]
}

// LatestDone returns the most recent Card whose State is StateDone.
func (m *Manager) LatestDone() *Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := len(m.cards) - 1; i >= 0; i-- {
		if m.cards[i].State == StateDone {
			return m.cards[i]
		}
	}
	return nil
}

func (m *Manager) addCard(c *Card) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cards = append(m.cards, c)
}

func (m *Manager) allocID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeq++
	return fmt.Sprintf("btw-%d-%d", time.Now().UnixNano(), m.nextSeq)
}
