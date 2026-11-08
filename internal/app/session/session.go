// Package session provides a unified domain model for conversation sessions,
// coordinating conversation identity, transcript lifecycle, and persistence boundaries
// across interactive TUI, headless CLI, and agent server runtimes.
package session

import (
	"errors"
	"sync"
	"time"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools/shell"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// DefaultModel is the reference model used when none is explicitly specified.
const DefaultModel = "gemini-3.8-flash-high"

// Config configures a new Session instance.
type Config struct {
	ID     string
	Model  string
	Effort string
	CWD    string
	Mode   string
	Home   string
}

// Session encapsulates the complete domain state and lifecycle of an active
// conversation. It separates domain logic from UI presentation frameworks
// (Bubbletea) and allows testing and orchestrating turns in isolation.
type Session struct {
	mu sync.RWMutex

	id        string
	model     string
	effort    string
	cwd       string
	mode      string
	epoch     uint64
	createdAt time.Time

	supervisor       *supervisor.Supervisor
	store            *Store
	subagentDefs     *subagenttools.Store
	subagentRegistry *subagenttools.Registry
}

// New creates and initializes a new domain Session.
func New(cfg Config) *Session {
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
	s := &Session{
		id:               cfg.ID,
		model:            cfg.Model,
		effort:           cfg.Effort,
		cwd:              cfg.CWD,
		mode:             cfg.Mode,
		epoch:            0,
		createdAt:        time.Now(),
		supervisor:       supervisor.New(cfg.CWD),
		store:            NewStore(cfg.Home),
		subagentDefs:     subagenttools.NewStore(),
		subagentRegistry: subagenttools.NewRegistry(cfg.ID),
	}
	return s
}

// ID returns the active conversation UUID.
func (s *Session) ID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.id
}

// SetID updates the conversation UUID and resets the epoch.
func (s *Session) SetID(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.id = id
	s.epoch++
}

// Model returns the active model identifier.
func (s *Session) Model() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.model == "" {
		return DefaultModel
	}
	return s.model
}

// SetModel updates the active model identifier.
func (s *Session) SetModel(model string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.model = model
}

// Effort returns the active reasoning effort tier.
func (s *Session) Effort() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.effort
}

// SetEffort sets the reasoning effort tier.
func (s *Session) SetEffort(effort string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.effort = effort
}

// CWD returns the active working directory.
func (s *Session) CWD() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cwd
}

// SetCWD updates the active working directory.
func (s *Session) SetCWD(cwd string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cwd = cwd
	if s.supervisor != nil {
		s.supervisor.SetCWD(cwd)
	}
}

// Epoch returns the current transcript epoch.
func (s *Session) Epoch() uint64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.epoch
}

// BumpEpoch increments and returns the new transcript epoch.
// An epoch change indicates to the turn engine that the conversation history
// was replaced or cleared rather than incrementally extended.
func (s *Session) BumpEpoch() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.epoch++
	return s.epoch
}

// Supervisor returns the underlying trajectory supervisor.
func (s *Session) Supervisor() *supervisor.Supervisor {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.supervisor
}

// Store returns the persistence store for this session.
func (s *Session) Store() *Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.store
}

// Messages returns a copy of active chat turn messages.
func (s *Session) Messages() []backend.Message {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return nil
	}
	return sup.Messages()
}

// WireMessages gathers implicit shell messages and active conversation messages
// into a single slice suitable for model submission.
func (s *Session) WireMessages() []backend.Message {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return nil
	}
	return sup.WireMessages()
}

// AppendUser adds a user message to the session transcript.
func (s *Session) AppendUser(content string) {
	s.AppendMessage(backend.Message{Role: "user", Content: content})
}

// AppendAssistant adds an assistant response message to the session transcript.
func (s *Session) AppendAssistant(content string) {
	s.AppendMessage(backend.Message{Role: "assistant", Content: content})
}

// AppendMessage appends a fully-formed message. It is the only write that
// can carry images, tool calls or a wire envelope; the role-specific
// helpers are conveniences over it.
func (s *Session) AppendMessage(msg backend.Message) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup != nil {
		sup.Append(msg)
	}
}

// Len returns the number of active chat turn messages.
func (s *Session) Len() int {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return 0
	}
	return sup.Len()
}

// MessageAt returns the message at index i.
func (s *Session) MessageAt(i int) (backend.Message, bool) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return backend.Message{}, false
	}
	return sup.MessageAt(i)
}

// LastMessage returns the newest message in the transcript.
func (s *Session) LastMessage() (backend.Message, bool) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return backend.Message{}, false
	}
	return sup.LastMessage()
}

// SetMessageContent replaces the content of the message at index i.
func (s *Session) SetMessageContent(i int, content string) bool {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return false
	}
	return sup.SetMessageContent(i, content)
}

// AppendMessageContent appends delta to the content of the message at index i.
func (s *Session) AppendMessageContent(i int, delta string) bool {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return false
	}
	return sup.AppendMessageContent(i, delta)
}

// Truncate keeps the first n messages and drops the rest.
func (s *Session) Truncate(n int) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup != nil {
		sup.Truncate(n)
	}
}

// Replace updates the active session messages and increments the epoch.
func (s *Session) Replace(msgs []backend.Message) {
	s.mu.Lock()
	s.epoch++
	sup := s.supervisor
	s.mu.Unlock()
	if sup != nil {
		sup.Replace(msgs)
	}
}

// SetSeededConversation seeds the session with historical steps and messages.
func (s *Session) SetSeededConversation(steps []convstore.Step, seededLen int) {
	s.mu.Lock()
	s.epoch++
	sup := s.supervisor
	s.mu.Unlock()
	if sup != nil {
		sup.SetSeededConversation(steps, seededLen)
	}
}

// RewindPopTail removes the latest turn pair from the session.
func (s *Session) RewindPopTail() {
	s.mu.Lock()
	s.epoch++
	sup := s.supervisor
	s.mu.Unlock()
	if sup != nil {
		sup.RewindPopTail()
	}
}

// SubagentDefs returns the conversation's subagent definition store.
func (s *Session) SubagentDefs() *subagenttools.Store {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subagentDefs
}

// SetSubagentDefs assigns a subagent definition store.
func (s *Session) SetSubagentDefs(defs *subagenttools.Store) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subagentDefs = defs
}

// SubagentRegistry returns the live-subagent registry for this conversation.
func (s *Session) SubagentRegistry() *subagenttools.Registry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.subagentRegistry
}

// SetSubagentRegistry assigns the live-subagent registry.
func (s *Session) SetSubagentRegistry(reg *subagenttools.Registry) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subagentRegistry = reg
}

// Schedules retrieves active timer/cron schedules for this session.
func (s *Session) Schedules() []shell.PersistedSchedule {
	s.mu.RLock()
	st := s.store
	id := s.id
	s.mu.RUnlock()
	if st == nil || id == "" {
		return nil
	}
	return st.LoadSchedules(id)
}

// RemoveSchedule removes an active timer/cron schedule for this session.
func (s *Session) RemoveSchedule(taskID string) error {
	s.mu.RLock()
	st := s.store
	id := s.id
	s.mu.RUnlock()
	if st == nil || id == "" {
		return nil
	}
	return st.RemoveSchedule(id, taskID)
}

// Clear resets the active conversation messages, trajectory, subagents, and increments the epoch.
func (s *Session) Clear() {
	s.mu.Lock()
	s.epoch++
	sup := s.supervisor
	s.subagentDefs = subagenttools.NewStore()
	s.subagentRegistry = subagenttools.NewRegistry(s.id)
	s.mu.Unlock()
	if sup != nil {
		sup.Clear()
	}
}

// Reset clears conversation state, assigns a new conversation ID, and increments epoch.
func (s *Session) Reset(newID string) {
	s.mu.Lock()
	s.id = newID
	s.epoch++
	sup := s.supervisor
	s.subagentDefs = subagenttools.NewStore()
	s.subagentRegistry = subagenttools.NewRegistry(newID)
	s.mu.Unlock()
	if sup != nil {
		sup.Clear()
	}
}

// Save persists the session's active trajectory to SQLite storage.
func (s *Session) Save(autoTitle string) error {
	s.mu.RLock()
	convID := s.id
	sup := s.supervisor
	s.mu.RUnlock()

	if convID == "" {
		return errors.New("session: cannot save session with empty ID")
	}
	if sup == nil {
		return nil
	}
	return sup.Save(convID, autoTitle)
}

// TrajectoryCounts returns the loaded-step and call counts.
func (s *Session) TrajectoryCounts() (loaded, calls, seededLen int) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return 0, 0, 0
	}
	return sup.TrajectoryCounts()
}

// InsertTrajectoryCall inserts a recorded step at a position in the calls.
func (s *Session) InsertTrajectoryCall(at int, step convstore.Step) {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup != nil {
		sup.InsertTrajectoryCall(at, step)
	}
}

// TrajectoryFacts returns the current trajectory facts for checkpointing.
func (s *Session) TrajectoryFacts() convstore.Trajectory {
	s.mu.RLock()
	sup := s.supervisor
	s.mu.RUnlock()
	if sup == nil {
		return convstore.Trajectory{}
	}
	return sup.TrajectoryFacts()
}
