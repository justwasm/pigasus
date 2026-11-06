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
