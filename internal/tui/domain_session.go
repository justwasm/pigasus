package tui

// CurrentModel returns the active model name for this session.
func (s *sessionState) CurrentModel() string {
	return s.model
}

// SetCurrentModel updates the active model name for this session.
func (s *sessionState) SetCurrentModel(m string) {
	s.model = m
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
	return s.conversationID
}

// SetCurrentConversationID updates the active conversation UUID.
func (s *sessionState) SetCurrentConversationID(id string) {
	s.conversationID = id
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

// Delegators on runMode for backward compatibility and clean domain access:
func (m *runMode) CurrentModel() string                { return m.session.CurrentModel() }
func (m *runMode) SetCurrentModel(model string)        { m.session.SetCurrentModel(model) }
func (m *runMode) CurrentTier() string                 { return m.session.CurrentTier() }
func (m *runMode) SetSessionTier(tier, account string) { m.session.SetSessionTier(tier, account) }
func (m *runMode) IsTierFetched() bool                 { return m.session.IsTierFetched() }
func (m *runMode) CurrentConversationID() string       { return m.session.CurrentConversationID() }
func (m *runMode) SetCurrentConversationID(id string)  { m.session.SetCurrentConversationID(id) }
func (m *runMode) ConversationTitle() string           { return m.session.ConversationTitle() }
func (m *runMode) SetConversationTitle(title string)   { m.session.SetConversationTitle(title) }
func (m *runMode) IsConversationStarted() bool         { return m.session.IsConversationStarted() }
func (m *runMode) MarkConversationStarted()            { m.session.MarkConversationStarted() }
func (m *runMode) SessionGoal() string                 { return m.session.SessionGoal() }
func (m *runMode) SetSessionGoal(goal string)          { m.session.SetSessionGoal(goal) }
func (m *runMode) SessionLearnings() []string          { return m.session.SessionLearnings() }
func (m *runMode) AddSessionLearning(item string)      { m.session.AddSessionLearning(item) }

