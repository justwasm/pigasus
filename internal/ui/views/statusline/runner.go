package statusline

// RenderStatusLine runs the status-line script once and stores its output
// in the state. It reports whether a script was configured and enabled.
//
// A failed run keeps the error for the status area's error card and counts
// one failure; the reference switches the status line off after 30 failures
// in a row, and the state itself counts them.
func RenderStatusLine(state *State, cfg Config, payload []byte) bool {
	if state == nil || !cfg.Active() {
		return false
	}
	if state.Disabled() {
		return true
	}
	res := RunScript(cfg.Command, payload)
	if res.Err != nil {
		state.SetRendered("", res.Err)
		state.RecordFailure()
		return true
	}
	state.SetRendered(res.Output, nil)
	state.RecordSuccess()
	return true
}

// RenderWindowTitle runs the window-title script once and stores the
// sanitized title.
//
// The reference sends the very same payload document to the title script
// as to the status-line script.
func RenderWindowTitle(titles *TitleStore, cfg Config, payload []byte) {
	if titles == nil || !cfg.Active() {
		return
	}
	res := RunScript(cfg.Command, payload)
	if res.Err != nil {
		return
	}
	titles.Set(SanitizeTitle(res.Output))
}
