package tui

// IsStreamActive reports whether an LLM turn stream is actively running.
func (e *engineState) IsStreamActive() bool {
	return e.streaming
}

// SetStreamActive updates whether streaming is active.
func (e *engineState) SetStreamActive(active bool) {
	e.streaming = active
}

// IsThinkingActive reports whether extended thinking is in progress.
func (e *engineState) IsThinkingActive() bool {
	return e.thinking
}

// SetThinkingActive updates whether thinking mode is active.
func (e *engineState) SetThinkingActive(active bool) {
	e.thinking = active
}

// WasTurnCancelled reports whether the most recent turn was cancelled by the user.
func (e *engineState) WasTurnCancelled() bool {
	return e.turnInterrupted
}

// SetTurnCancelled records whether the turn was cancelled by the user.
func (e *engineState) SetTurnCancelled(cancelled bool) {
	e.turnInterrupted = cancelled
}

// IsQuitting reports whether the TUI application is shutting down.
func (e *engineState) IsQuitting() bool {
	return e.quitting
}

// SetQuitting marks the TUI application as shutting down.
func (e *engineState) SetQuitting(quitting bool) {
	e.quitting = quitting
}

// ActiveTaskCount returns the number of active background tasks.
func (e *engineState) ActiveTaskCount() int {
	if e.taskReg != nil {
		return e.taskReg.CountRunning()
	}
	return 0
}

// CancelInFlight cancels the active in-flight stream if any.
func (e *engineState) CancelInFlight() {
	if e.turnCancel != nil {
		e.turnCancel()
		e.turnCancel = nil
	}
}

// Delegators on runMode for backward compatibility and clean domain access:
func (m *runMode) IsStreamActive() bool          { return m.engine.IsStreamActive() }
func (m *runMode) SetStreamActive(active bool)   { m.engine.SetStreamActive(active) }
func (m *runMode) IsThinkingActive() bool        { return m.engine.IsThinkingActive() }
func (m *runMode) SetThinkingActive(active bool) { m.engine.SetThinkingActive(active) }
func (m *runMode) WasTurnCancelled() bool        { return m.engine.WasTurnCancelled() }
func (m *runMode) SetTurnCancelled(c bool)       { m.engine.SetTurnCancelled(c) }
func (m *runMode) IsQuitting() bool              { return m.engine.IsQuitting() }
func (m *runMode) SetQuitting(quitting bool)     { m.engine.SetQuitting(quitting) }
func (m *runMode) ActiveTaskCount() int          { return m.engine.ActiveTaskCount() }
func (m *runMode) CancelInFlight()               { m.engine.CancelInFlight() }

