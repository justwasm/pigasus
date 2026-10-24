// Package sidequestion runs /btw side-question turns.
//
// Cycle 230 ships the AGY 1.4.0 /btw slash command. The reference fires it
// as a fully independent sub-conversation: a fresh model session whose only
// memory of the parent is the system prompt, no shared transcript, no
// hooks. The clone replicates that shape on top of PiG's coding runtime:
//
//   - each Ask call opens a brand-new coding.Services and a brand-new
//     coding.Session. The parent session is never touched;
//   - the question is sent as the lone user message; the streamed answer
//     is collected into a Card and returned when the turn settles;
//   - Card.State reports the terminal state ("done" / "failed");
//   - Card.Tokens is the output-token total PiG's assistant message
//     carried, the same value pigrt.UsageOf reads for the main turn.
//
// Manager is the live TUI's handle: it owns the Cards, exposes them by ID
// for /copy btw, and tears down every Services it created. The package is
// stdlib + PiG only; no internal/openagy imports.
package sidequestion

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding"
)

// State values a Card carries during its lifetime.
const (
	StatePending   = "pending"
	StateStreaming = "streaming"
	StateDone      = "done"
	StateFailed    = "failed"
)

// DefaultSystemPrompt is the reference agy system prompt for side questions,
// recovered verbatim from google3/third_party/jetski/cli/backend/backend.(*ServerBackend).buildBtwCascadeConfig:
// "Help explain this concept or piece of code succinctly. Do not dive too deeply into one topic. Use context about the codebase to give an accurate explanation."
const DefaultSystemPrompt = "Help explain this concept or piece of code succinctly. Do not dive too deeply into one topic. Use context about the codebase to give an accurate explanation."

// Card is one side-question + its streamed answer.
//
// The fields are mutated from the Ask goroutine (during the stream) and
// read from the TUI render goroutine after Ask returns. mu guards the
// write/read window. ID is unique within one Manager; Question is what the
// user typed; Answer accumulates text deltas; Tokens is the total token
// count PiG reported; State is one of the constants above; Err is the
// non-recoverable failure that ended the turn (nil on success).
type Card struct {
	ID       string
	Question string
	Answer   string
	Tokens   int
	State    string
	Err      error
	Expanded bool
	Scroll   int

	mu sync.Mutex
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
// TUI renderers should use this rather than touching fields directly so
// a concurrent Ask can keep appending Answer without racing the read.
func (c *Card) LockedSnapshot() (question, answer string, tokens int, state string, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Question, c.Answer, c.Tokens, c.State, c.Err
}

// Manager tracks Cards for the lifetime of one TUI session. It owns a
// fresh coding.Services pool, but per the cycle-230 contract every Ask
// call still opens its own Services + Session — the Manager-level pool is
// here only so /copy btw can reach the cards without round-tripping
// through the agent runtime.
type Manager struct {
	mu      sync.Mutex
	cards   []*Card
	nextSeq int

	model        *ai.Model
	cwd          string
	agentDir     string
	systemPrompt string
	// sessionTimeout bounds one Ask round-trip. Zero uses 2 minutes
	// (the same default the print path takes for unrelated turns).
	sessionTimeout time.Duration
}

// Options configures one Manager. Model is required; everything else has
// a sensible default.
type Options struct {
	// Model is the ai.Model the side-question Session calls. Required.
	// The parent session and the side-question Session share neither
	// provider wiring nor transcript; pick the model the user wants
	// side questions answered with (typically the same model the
	// parent is on, but independent).
	Model *ai.Model
	// CWD anchors the fresh Services. Empty falls back to ".".
	CWD string
	// AgentDir is the agy config home the resource loader reads from.
	// Empty uses PiG's default (~/.pig/agent) which is fine for tests
	// and matches PiG's own home; production callers pass
	// pigrt.DefaultAgentHome() so the side question sees the same
	// skills / settings the parent does.
	AgentDir string
	// SystemPrompt is the only prompt the side-question Session ships
	// with. Pass the parent session's system prompt so the btw answer
	// sees the same operating context, but the user message must be
	// the only conversation entry — that is the whole point of /btw.
	SystemPrompt string
	// SessionTimeout bounds one Ask round-trip. Zero uses 2 minutes.
	SessionTimeout time.Duration
}

// NewManager builds a Manager. It does not open a Services; the first
// Ask call does that. The caller owns the Manager; Close releases the
// cards but never any of the per-Ask Services (each Ask closes its own).
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
		model:          opts.Model,
		cwd:            opts.CWD,
		agentDir:       opts.AgentDir,
		systemPrompt:   opts.SystemPrompt,
		sessionTimeout: opts.SessionTimeout,
	}
}

// ErrEmptyQuestion is returned by Ask when the caller passes an empty
// question — the slash handler is supposed to short-circuit that case
// before reaching Ask, but defending the boundary makes the failure
// surface explicit at the package level.
var ErrEmptyQuestion = errors.New("sidequestion: empty question")

// Ask opens a fresh coding.Services and a fresh coding.Session, sends the
// question, and blocks until the streamed answer settles. The returned
// Card is registered with the Manager; List/Get/Active return it from
// then on.
//
// The parent session is untouched: even if the user is mid-turn in the
// TUI, this call does not append any message to its transcript. The
// tradeoff is that Ask blocks the caller until the model finishes; the
// TUI routes the call through tea.Cmd so the bubbletea event loop keeps
// spinning.
//
// On error the Card's State is "failed" and Err carries the original
// failure. The non-nil error return mirrors the contract pigrt.Runtime
// exposes for its own runs.
func drainSessionEvents(session *coding.Session, card *Card) <-chan struct{} {
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		for ev := range session.Events() {
			if coding.AcknowledgeEvent(ev) {
				continue
			}
			update, ok := ev.(agent.MessageUpdateEvent)
			if !ok {
				continue
			}
			delta, ok := update.AssistantMessageEvent.(ai.TextDeltaEvent)
			if !ok || delta.Delta == "" {
				continue
			}
			card.mu.Lock()
			if card.State == StatePending {
				card.State = StateStreaming
			}
			card.Answer += delta.Delta
			card.mu.Unlock()
		}
	}()
	return drainDone
}

func finalizeCard(card *Card, msgs []agent.AgentMessage, sendErr error) error {
	card.mu.Lock()
	defer card.mu.Unlock()
	if sendErr != nil {
		card.State = StateFailed
		card.Err = sendErr
		return sendErr
	}
	if total, ok := totalTokens(msgs); ok {
		card.Tokens = total
	}
	if last := lastAssistant(msgs); last != nil {
		switch last.StopReason {
		case ai.StopReasonError, ai.StopReasonAborted:
			card.State = StateFailed
			msg := last.ErrorMessage
			if msg == "" {
				msg = "side-question: upstream stream failed"
			}
			card.Err = errors.New(msg)
			return card.Err
		}
	}
	card.State = StateDone
	return nil
}

func runSession(ctx context.Context, session *coding.Session, question string, timeout time.Duration, card *Card) ([]agent.AgentMessage, error) {
	drainDone := drainSessionEvents(session, card)
	askCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	msgs, err := session.Send(askCtx, question)
	_ = session.FlushEvents(askCtx)
	_ = session.Close()
	<-drainDone
	return msgs, err
}

func (m *Manager) Ask(ctx context.Context, question string) (*Card, error) {
	if question == "" {
		return nil, ErrEmptyQuestion
	}
	if m.model == nil {
		return nil, errors.New("sidequestion: Manager.Model is required")
	}

	card := &Card{
		ID:       m.allocID(),
		Question: question,
		State:    StatePending,
	}
	m.addCard(card)

	services, rt, session, err := m.openSession(ctx)
	if err != nil {
		card.mu.Lock()
		card.State = StateFailed
		card.Err = err
		card.mu.Unlock()
		return card, err
	}
	defer func() {
		_ = rt.Close()
		services.Close()
	}()

	msgs, sendErr := runSession(ctx, session, question, m.sessionTimeout, card)
	if err := finalizeCard(card, msgs, sendErr); err != nil {
		return card, err
	}
	return card, nil
}

// openSession builds a fresh Services + Runtime + Session, returning
// them in the order they must be torn down. The Session is the only one
// that owns an Events channel the drain goroutine reads; building a
// no-extension Runtime keeps the surface minimal so a btw turn cannot
// accidentally pick up MCP servers or skills the parent session did.
func (m *Manager) openSession(ctx context.Context) (*coding.AgentSessionServices, *coding.Runtime, *coding.Session, error) {
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      m.cwd,
		AgentDir: m.agentDir,
	})
	if err != nil {
		return nil, nil, nil, fmt.Errorf("sidequestion: new services: %w", err)
	}
	rt, err := coding.NewRuntime(coding.RuntimeOptions{
		Services:     services,
		AbortContext: ctx,
	})
	if err != nil {
		services.Close()
		return nil, nil, nil, fmt.Errorf("sidequestion: new runtime: %w", err)
	}
	session, err := rt.New(coding.SessionStartOptions{
		Model:            m.model,
		SystemPrompt:     m.systemPrompt,
		NoSession:        true,
		SkipBuiltinTools: true,
	})
	if err != nil {
		_ = rt.Close()
		services.Close()
		return nil, nil, nil, fmt.Errorf("sidequestion: new session: %w", err)
	}
	return services, rt, session, nil
}

// List returns every Card registered with the Manager in insertion
// order. The slice is a defensive copy so a concurrent Ask cannot
// perturb it from underneath the caller.
func (m *Manager) List() []*Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Card, len(m.cards))
	copy(out, m.cards)
	return out
}

// Get returns the Card with the given ID, or nil when no such card
// exists. Lookup is by exact ID match — the TUI remembers the active
// card's ID across keystrokes so the handler can address a single card
// even when several are open at once.
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

// Latest returns the most recent Card, regardless of state. /copy btw
// with no card in flight uses this; so does the TUI render loop when it
// needs the current card to focus.
func (m *Manager) Latest() *Card {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.cards) == 0 {
		return nil
	}
	return m.cards[len(m.cards)-1]
}

// LatestDone returns the most recent Card whose State is StateDone.
// /copy btw prefers this over Latest so a still-streaming card does not
// race the clipboard with a half-written answer.
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

// addCard appends c to the manager's list under the mutex.
func (m *Manager) addCard(c *Card) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cards = append(m.cards, c)
}

// allocID returns a fresh ID that is unique within the Manager.
func (m *Manager) allocID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextSeq++
	return fmt.Sprintf("btw-%d-%d", time.Now().UnixNano(), m.nextSeq)
}

// totalTokens sums the TotalTokens field across every assistant
// message PiG produced. ok is false when no message carried usage at
// all (the reference omits the usage block in that case, so the TUI
// footer says nothing rather than "0 tok").
func totalTokens(msgs []agent.AgentMessage) (int, bool) {
	var total int
	found := false
	for _, m := range msgs {
		if m.Assistant == nil || m.Assistant.Usage == nil {
			continue
		}
		found = true
		total += m.Assistant.Usage.TotalTokens
		if total == 0 {
			total += m.Assistant.Usage.Output
		}
	}
	return total, found
}

// lastAssistant returns the last non-nil AssistantMessage in msgs.
// Cycle 209 documents the contract — PiG folds a stream failure into a
// trailing assistant message with StopReasonError, so the last one is
// the one whose stop reason we inspect. The agent.AgentMessage union
// holds an *agent.AssistantMessage whose StopReason field is what we
// need (mirrors pigrt.Runtime.turnFailure).
func lastAssistant(msgs []agent.AgentMessage) *agent.AssistantMessage {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Assistant != nil {
			return msgs[i].Assistant
		}
	}
	return nil
}
