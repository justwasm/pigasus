package tui

import (
	"strings"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// ComposerValue returns the current text inside the composer input.
func (c *composerState) ComposerValue() string {
	return c.input.Value()
}

// SetComposerValue updates the text inside the composer input.
func (c *composerState) SetComposerValue(val string) {
	c.input.SetValue(val)
}

// IsComposerEmpty reports whether the composer input is empty or only whitespace.
func (c *composerState) IsComposerEmpty() bool {
	return strings.TrimSpace(c.input.Value()) == ""
}

// ClearComposer clears the composer input and disarms any pending Esc clear state.
func (c *composerState) ClearComposer() {
	c.input.SetValue("")
	c.escArmedFor = ""
	c.pastedBlocks = nil
	c.nextPasteID = 0
}

// HasPendingMedia reports whether there are media attachments pending for the next turn.
func (c *composerState) HasPendingMedia() bool {
	return len(c.pendingMedia) > 0
}

// AddPendingMedia appends a media attachment to the pending list.
func (c *composerState) AddPendingMedia(m clipboard.Media) {
	c.pendingMedia = append(c.pendingMedia, m)
}

// ClearPendingMedia empties the pending media list.
func (c *composerState) ClearPendingMedia() {
	c.pendingMedia = nil
}

// IsMediaExpanded reports whether the attached media preview is expanded.
func (c *composerState) IsMediaExpanded() bool {
	return c.mediaExpanded
}

// ToggleMediaExpanded toggles whether the media preview is expanded.
func (c *composerState) ToggleMediaExpanded() {
	c.mediaExpanded = !c.mediaExpanded
}

// ArmEsc arms the composer for two-step Esc clear on the given text.
func (c *composerState) ArmEsc(text string) {
	c.escArmedFor = text
}

// DisarmEsc clears the two-step Esc armed state.
func (c *composerState) DisarmEsc() {
	c.escArmedFor = ""
}

// IsEscArmed reports whether two-step Esc clear is currently armed.
func (c *composerState) IsEscArmed() bool {
	return c.escArmedFor != ""
}

// VimActive reports whether Vim editing mode is enabled in the composer.
func (c *composerState) VimActive() bool {
	return c.vimOn
}

// SetVimMode sets whether Vim editing mode is active in the composer.
func (c *composerState) SetVimMode(on bool) {
	c.vimOn = on
}

// Delegators on runMode for backward compatibility and clean domain access:
func (m *runMode) ComposerValue() string               { return m.composer.ComposerValue() }
func (m *runMode) SetComposerValue(val string)         { m.composer.SetComposerValue(val) }
func (m *runMode) IsComposerEmpty() bool               { return m.composer.IsComposerEmpty() }
func (m *runMode) ClearComposer()                      { m.composer.ClearComposer() }
func (m *runMode) HasPendingMedia() bool               { return m.composer.HasPendingMedia() }
func (m *runMode) AddPendingMedia(med clipboard.Media) { m.composer.AddPendingMedia(med) }
func (m *runMode) ClearPendingMedia()                  { m.composer.ClearPendingMedia() }
func (m *runMode) IsMediaExpanded() bool               { return m.composer.IsMediaExpanded() }
func (m *runMode) ToggleMediaExpanded()                { m.composer.ToggleMediaExpanded() }
func (m *runMode) ArmEsc(text string)                  { m.composer.ArmEsc(text) }
func (m *runMode) DisarmEsc()                          { m.composer.DisarmEsc() }
func (m *runMode) IsEscArmed() bool                    { return m.composer.IsEscArmed() }
func (m *runMode) VimActive() bool                     { return m.composer.VimActive() }
func (m *runMode) SetVimMode(on bool)                  { m.composer.SetVimMode(on) }
