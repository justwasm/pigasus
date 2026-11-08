package tui

import (
	"github.com/justwasm/pigasus/internal/app/session"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/session/supervisor"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/core/backend"
)

// CurrentModel returns the active model name for this session.
func (s *sessionState) CurrentModel() string {
	if s.domain != nil && s.domain.Model() != "" {
		return s.domain.Model()
	}
	return s.model
}

// SetCurrentModel updates the active model name for this session.
func (s *sessionState) SetCurrentModel(m string) {
	s.model = m
	if s.domain != nil {
		s.domain.SetModel(m)
	}
}

// CurrentTier returns the resolved subscription tier for this session.
func (s *sessionState) CurrentTier() string {
	return s.tier
}

// SetSessionTier records the resolved tier and account for the session.
func (s *sessionState) SetSessionTier(tier, account string) {
	s.tier = tier
	s.tierAccount = account
	s.tierFetched = true
}

// IsTierFetched reports whether the account tier has been fetched from the server.
func (s *sessionState) IsTierFetched() bool {
	return s.tierFetched
}

// CurrentConversationID returns the active conversation UUID.
func (s *sessionState) CurrentConversationID() string {
	if s.domain != nil && s.domain.ID() != "" {
		return s.domain.ID()
	}
	return s.conversationID
}

// SetCurrentConversationID updates the active conversation UUID.
func (s *sessionState) SetCurrentConversationID(id string) {
	s.conversationID = id
	if s.domain != nil {
		s.domain.SetID(id)
	}
}

// Domain returns the underlying pure-Go session domain entity.
func (s *sessionState) Domain() *session.Session {
	if s == nil {
		return nil
	}
	return s.domain
}

// EnsureDomain ensures the domain session is allocated and synchronized.
func (s *sessionState) EnsureDomain() *session.Session {
	if s == nil {
		return nil
	}
	if s.domain == nil {
		s.domain = session.New(session.Config{
			ID:    s.conversationID,
			Model: s.model,
			CWD:   s.cwd,
			Mode:  s.mode,
		})
	}
	return s.domain
}

// messages returns a copy of the active transcript.
//
// The domain session owns the messages. There is deliberately no second
// slice beside it: every read and every write in the TUI goes through
// here (or through a narrower helper below), so the rendered transcript
// and the persisted transcript cannot drift apart.
func (s *sessionState) messages() []backend.Message {
	if s == nil {
		return nil
	}
	dom := s.EnsureDomain()
	if dom == nil {
		return nil
	}
	return dom.Messages()
}

// messageCount returns the number of active messages without copying them.
func (s *sessionState) messageCount() int {
	if s == nil {
		return 0
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.Len()
	}
	return 0
}

// lastMessage returns the newest message.
func (s *sessionState) lastMessage() (backend.Message, bool) {
	if s == nil {
		return backend.Message{}, false
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.LastMessage()
	}
	return backend.Message{}, false
}

// appendMessage appends a fully-formed message to the transcript.
func (s *sessionState) appendMessage(msg backend.Message) {
	if s == nil {
		return
	}
	if dom := s.EnsureDomain(); dom != nil {
		dom.AppendMessage(msg)
	}
}

// setMessages replaces the transcript.
func (s *sessionState) setMessages(msgs []backend.Message) {
	if s == nil {
		return
	}
	if dom := s.EnsureDomain(); dom != nil {
		dom.Replace(msgs)
	}
}

// truncateMessages keeps the first n messages.
func (s *sessionState) truncateMessages(n int) {
	if s == nil {
		return
	}
	if dom := s.EnsureDomain(); dom != nil {
		dom.Truncate(n)
	}
}

// setMessageContent replaces the content of message i.
func (s *sessionState) setMessageContent(i int, content string) bool {
	if s == nil {
		return false
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.SetMessageContent(i, content)
	}
	return false
}

// appendMessageContent appends delta to message i.
func (s *sessionState) appendMessageContent(i int, delta string) bool {
	if s == nil {
		return false
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.AppendMessageContent(i, delta)
	}
	return false
}

// sup returns the transcript supervisor the domain owns.
func (s *sessionState) sup() *supervisor.Supervisor {
	if s == nil {
		return nil
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.Supervisor()
	}
	return nil
}

// ConversationTitle returns the display name or title of the current conversation.
func (s *sessionState) ConversationTitle() string {
	if s.conversationName != "" {
		return s.conversationName
	}
	return s.autoTitle
}

// SetConversationTitle sets the user-assigned or auto-generated conversation title.
func (s *sessionState) SetConversationTitle(title string) {
	s.conversationName = title
}

// IsConversationStarted reports whether the session has exchanged messages.
func (s *sessionState) IsConversationStarted() bool {
	return s.conversationStarted
}

// MarkConversationStarted records that the conversation has started.
func (s *sessionState) MarkConversationStarted() {
	s.conversationStarted = true
}

// SessionGoal returns the active /goal description, if set.
func (s *sessionState) SessionGoal() string {
	return s.goal
}

// SetSessionGoal sets the active /goal description.
func (s *sessionState) SetSessionGoal(goal string) {
	s.goal = goal
}

// SessionLearnings returns the accumulated /learn items.
func (s *sessionState) SessionLearnings() []string {
	return s.learnings
}

// AddSessionLearning appends a new learned item.
func (s *sessionState) AddSessionLearning(item string) {
	s.learnings = append(s.learnings, item)
}

// Delegators on RootModel for backward compatibility and clean domain access:
func (m *RootModel) CurrentModel() string                { return m.session.CurrentModel() }
func (m *RootModel) SetCurrentModel(model string)        { m.session.SetCurrentModel(model) }
func (m *RootModel) CurrentTier() string                 { return m.session.CurrentTier() }
func (m *RootModel) SetSessionTier(tier, account string) { m.session.SetSessionTier(tier, account) }
func (m *RootModel) IsTierFetched() bool                 { return m.session.IsTierFetched() }
func (m *RootModel) CurrentConversationID() string       { return m.session.CurrentConversationID() }
func (m *RootModel) SetCurrentConversationID(id string)  { m.session.SetCurrentConversationID(id) }
func (m *RootModel) ConversationTitle() string           { return m.session.ConversationTitle() }
func (m *RootModel) SetConversationTitle(title string)   { m.session.SetConversationTitle(title) }
func (m *RootModel) IsConversationStarted() bool         { return m.session.IsConversationStarted() }
func (m *RootModel) MarkConversationStarted()            { m.session.MarkConversationStarted() }
func (m *RootModel) SessionGoal() string                 { return m.session.SessionGoal() }
func (m *RootModel) SetSessionGoal(goal string)          { m.session.SetSessionGoal(goal) }
func (m *RootModel) SessionLearnings() []string          { return m.session.SessionLearnings() }
func (m *RootModel) AddSessionLearning(item string)      { m.session.AddSessionLearning(item) }
func (m *RootModel) DomainSession() *session.Session     { return m.session.EnsureDomain() }

// SessionMessages returns the active transcript.
//
// The domain session is the only owner of the messages; there is no
// second copy to reconcile.
func (m *RootModel) SessionMessages() []backend.Message {
	if m == nil {
		return nil
	}
	return m.session.messages()
}

// setSessionMessages replaces the transcript. It is the single write path
// for wholesale replacement (/resume seeding, checkpoint compaction).
func (m *RootModel) setSessionMessages(msgs []backend.Message) {
	if m == nil {
		return
	}
	m.session.setMessages(msgs)
}

// AppendUserMessage records a user prompt in the transcript.
func (m *RootModel) AppendUserMessage(content string) {
	if m == nil {
		return
	}
	m.session.appendMessage(backend.Message{Role: "user", Content: content})
}

// AppendAssistantMessage records an assistant reply in the transcript.
func (m *RootModel) AppendAssistantMessage(content string) {
	if m == nil {
		return
	}
	m.session.appendMessage(backend.Message{Role: "assistant", Content: content})
}

// ResetSessionMessages clears the transcript.
func (m *RootModel) ResetSessionMessages() {
	if m == nil {
		return
	}
	m.session.bumpEpoch()
	if dom := m.session.EnsureDomain(); dom != nil {
		dom.Clear()
	}
}

// SubagentDefs returns the conversation's subagent definition store.
func (m *RootModel) SubagentDefs() *subagenttools.Store {
	if m == nil {
		return nil
	}
	if dom := m.DomainSession(); dom != nil {
		return dom.SubagentDefs()
	}
	return m.agents.defs
}

// SubagentRegistry returns the conversation's live-subagent registry.
func (m *RootModel) SubagentRegistry() *subagenttools.Registry {
	if m == nil {
		return nil
	}
	if dom := m.DomainSession(); dom != nil {
		return dom.SubagentRegistry()
	}
	return m.agents.registry
}

// cancelActiveTurn cancels the active turn through its bridge. The
// bridge's Tier-4 runtime is the only cancellation authority; the
// conversation keeps the presentation flags, and streaming stays true
// until the turn's terminal message arrives.
func (m *RootModel) cancelActiveTurn() bool {
	if m == nil {
		return false
	}
	if m.turn.activeBridge == nil || !m.turn.activeBridge.CancelTurn() {
		return false
	}
	m.conversation.turnInterrupted = true
	// streaming is not forced false here: the turn's terminal message
	// is what finalises the block. Clearing it early would let a second
	// prompt start while the first turn is still unwinding.
	return true
}

// epoch returns the transcript epoch. The domain session owns it so the
// turn engines and the host read one counter, not two.
func (s *sessionState) epoch() uint64 {
	if s == nil {
		return 0
	}
	if dom := s.EnsureDomain(); dom != nil {
		return dom.Epoch()
	}
	return 0
}

// bumpEpoch records that the transcript was replaced rather than extended.
// When the conversation ID changed, the domain is aligned first (SetID
// already bumps the epoch).
func (s *sessionState) bumpEpoch() {
	if s == nil {
		return
	}
	dom := s.EnsureDomain()
	if dom == nil {
		return
	}
	if s.conversationID != "" && dom.ID() != s.conversationID {
		dom.SetID(s.conversationID)
		return
	}
	dom.BumpEpoch()
}

// queueCheckpoint records a checkpoint with the call count that preceded
// it. The supervisor owns the queue; the host hands it the step.
func (s *sessionState) queueCheckpoint(step convstore.Step, after int) {
	if s == nil {
		return
	}
	if sup := s.sup(); sup != nil {
		sup.QueueCheckpoint(step, after)
	}
}

// flushCheckpoints inserts the queued checkpoints into the trajectory.
func (s *sessionState) flushCheckpoints() {
	if s == nil {
		return
	}
	if sup := s.sup(); sup != nil {
		sup.FlushCheckpoints()
	}
}
