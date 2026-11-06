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
