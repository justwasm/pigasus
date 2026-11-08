package tui

// TerminalDimensions returns the current width and height of the terminal.
func (u *uiState) TerminalDimensions() (int, int) {
	return u.width, u.height
}

// InvalidateViewCache marks the rendered rows and viewport caches as dirty.
func (u *uiState) InvalidateViewCache() {
	u.viewCacheValid = false
	u.extraRowsValid = false
	u.rowsCacheDirty = true
}

// ActiveColorScheme returns the active color scheme name.
func (u *uiState) ActiveColorScheme() string {
	return u.colorScheme
}

// SetActiveColorScheme updates the active color scheme and invalidates rendered caches.
func (u *uiState) SetActiveColorScheme(scheme string) {
	u.colorScheme = scheme
	u.InvalidateViewCache()
}

// IsAltScreenEnabled reports whether the alt-screen viewport is active.
func (u *uiState) IsAltScreenEnabled() bool {
	return u.altScreen
}

// SetAltScreen updates whether the alt-screen mode is enabled.
func (u *uiState) SetAltScreen(enabled bool) {
	u.altScreen = enabled
	u.InvalidateViewCache()
}

// ViewportScroll returns the current scroll offset in rows.
func (u *uiState) ViewportScroll() int {
	return u.scroll
}

// SetViewportScroll updates the scroll offset.
func (u *uiState) SetViewportScroll(offset int) {
	u.scroll = offset
	u.InvalidateViewCache()
}

// IsAutoScrollEnabled reports whether the viewport automatically scrolls to the bottom.
func (u *uiState) IsAutoScrollEnabled() bool {
	return u.autoScroll
}

// SetAutoScroll updates whether automatic scrolling is active.
func (u *uiState) SetAutoScroll(auto bool) {
	u.autoScroll = auto
}

// Delegators on RootModel for backward compatibility and clean domain access:
func (m *RootModel) TerminalDimensions() (int, int) { return m.viewport.TerminalDimensions() }
func (m *RootModel) InvalidateViewCache()           { m.viewport.InvalidateViewCache() }
func (m *RootModel) invalidateViewCache()           { m.viewport.InvalidateViewCache() }
func (m *RootModel) ActiveColorScheme() string      { return m.viewport.ActiveColorScheme() }
func (m *RootModel) SetActiveColorScheme(s string)  { m.viewport.SetActiveColorScheme(s) }
func (m *RootModel) IsAltScreenEnabled() bool       { return m.viewport.IsAltScreenEnabled() }
func (m *RootModel) SetAltScreen(enabled bool)      { m.viewport.SetAltScreen(enabled) }
func (m *RootModel) ViewportScroll() int            { return m.viewport.ViewportScroll() }
func (m *RootModel) SetViewportScroll(offset int)   { m.viewport.SetViewportScroll(offset) }
func (m *RootModel) IsAutoScrollEnabled() bool      { return m.viewport.IsAutoScrollEnabled() }
func (m *RootModel) SetAutoScroll(auto bool)        { m.viewport.SetAutoScroll(auto) }

func (m *RootModel) ResetSelection()                 { m.viewport.ResetSelection() }
func (m *RootModel) ResetInputSelection()            { m.prompt.ResetInputSelection() }
func (m *RootModel) InputSelectionRange() (int, int) { return m.prompt.InputSelectionRange() }
