package tui

import (
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

// HasActiveOverlay reports whether any modal, panel, picker, or login overlay is visible.
func (o *overlayState) HasActiveOverlay() bool {
	return o.HasActiveModal() || o.ActiveCard()
}

// HasActivePanel reports whether a slash panel is currently open.
func (o *overlayState) HasActivePanel() bool {
	return o.panel.kind != panelregistry.None
}

// ActivePanelKind returns the kind of the currently open slash panel.
func (o *overlayState) ActivePanelKind() panelregistry.Kind {
	return o.panel.kind
}

// HasActiveResumePicker reports whether the conversation resume picker is active.
func (o *overlayState) HasActiveResumePicker() bool {
	return o.resumePicker != nil && o.resumePicker.Active()
}

// HasActiveLogin reports whether the in-session login dialog is active.
func (o *overlayState) HasActiveLogin() bool {
	return o.login != nil
}

// HasSideQuestions reports whether there are active side questions (/btw cards).
func (o *overlayState) HasSideQuestions() bool {
	return len(o.btwCards) > 0
}

// ActiveSideQuestion returns the currently active side question card, if any.
func (o *overlayState) ActiveSideQuestion() *sidequestion.Card {
	if o.activeBtwIndex >= 0 && o.activeBtwIndex < len(o.btwCards) {
		return o.btwCards[o.activeBtwIndex]
	}
	return nil
}

// HasAnnouncement reports whether there is an active announcement card.
func (o *overlayState) HasAnnouncement() bool {
	return o.announceQueue.Active() != nil
}

// Delegators on runMode for backward compatibility and domain access:
func (m *runMode) HasActiveOverlay() bool                 { return m.overlays.HasActiveOverlay() }
func (m *runMode) HasActivePanel() bool                   { return m.overlays.HasActivePanel() }
func (m *runMode) ActivePanelKind() panelregistry.Kind   { return m.overlays.ActivePanelKind() }
func (m *runMode) HasActiveResumePicker() bool           { return m.overlays.HasActiveResumePicker() }
func (m *runMode) HasActiveLogin() bool                  { return m.overlays.HasActiveLogin() }
func (m *runMode) HasSideQuestions() bool                { return m.overlays.HasSideQuestions() }
func (m *runMode) ActiveSideQuestion() *sidequestion.Card { return m.overlays.ActiveSideQuestion() }
func (m *runMode) HasAnnouncement() bool                 { return m.overlays.HasAnnouncement() }
func (m *runMode) HasActiveModal() bool                  { return m.overlays.HasActiveModal() }
func (m *runMode) ActiveCard() bool                      { return m.overlays.ActiveCard() }
