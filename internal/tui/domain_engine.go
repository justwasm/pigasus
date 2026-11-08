package tui

// IsStreamActive reports whether an LLM turn stream is actively running.
func (c *ConversationModel) IsStreamActive() bool {
	return c.streaming
}

// SetStreamActive updates whether streaming is active.
func (c *ConversationModel) SetStreamActive(active bool) {
	c.streaming = active
}

// IsThinkingActive reports whether extended thinking is in progress.
func (c *ConversationModel) IsThinkingActive() bool {
	return c.thinking
}

// SetThinkingActive updates whether thinking mode is active.
func (c *ConversationModel) SetThinkingActive(active bool) {
	c.thinking = active
}

// WasTurnCancelled reports whether the most recent turn was cancelled by the user.
func (c *ConversationModel) WasTurnCancelled() bool {
	return c.turnInterrupted
}

// SetTurnCancelled records whether the turn was cancelled by the user.
func (c *ConversationModel) SetTurnCancelled(cancelled bool) {
	c.turnInterrupted = cancelled
}

// ActiveTaskCount returns the number of active background tasks.
func (t *TasksModel) ActiveTaskCount() int {
	if t.reg != nil {
		return t.reg.CountRunning()
	}
	return 0
}

// Delegators on RootModel for backward compatibility and clean domain access:
func (m *RootModel) IsStreamActive() bool          { return m.conversation.IsStreamActive() }
func (m *RootModel) SetStreamActive(active bool)   { m.conversation.SetStreamActive(active) }
func (m *RootModel) IsThinkingActive() bool        { return m.conversation.IsThinkingActive() }
func (m *RootModel) SetThinkingActive(active bool) { m.conversation.SetThinkingActive(active) }
func (m *RootModel) WasTurnCancelled() bool        { return m.conversation.WasTurnCancelled() }
func (m *RootModel) SetTurnCancelled(c bool)       { m.conversation.SetTurnCancelled(c) }
func (m *RootModel) IsQuitting() bool              { return m.display.IsQuitting() }
func (m *RootModel) SetQuitting(quitting bool)     { m.display.SetQuitting(quitting) }
func (m *RootModel) ActiveTaskCount() int          { return m.tasks.ActiveTaskCount() }
