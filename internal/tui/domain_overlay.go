package tui

import (
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

// HasActiveOverlay reports whether any modal, panel, picker, or login overlay is visible.
func (o *OverlayModel) HasActiveOverlay() bool {
	return o.HasActiveModal() || o.ActiveCard()
}

// HasActivePanel reports whether a slash panel is currently open.
func (o *OverlayModel) HasActivePanel() bool {
	return o.panel.kind != panelregistry.None
}

// ActivePanelKind returns the kind of the currently open slash panel.
func (o *OverlayModel) ActivePanelKind() panelregistry.Kind {
	return o.panel.kind
}

// HasActiveResumePicker reports whether the conversation resume picker is active.
func (o *OverlayModel) HasActiveResumePicker() bool {
	return o.resumePicker != nil && o.resumePicker.Active()
}

// HasSideQuestions reports whether there are active side questions (/btw cards).
func (o *OverlayModel) HasSideQuestions() bool {
	return len(o.btwCards) > 0
}

// ActiveSideQuestion returns the currently active side question card, if any.
func (o *OverlayModel) ActiveSideQuestion() *sidequestion.Card {
	if o.activeBtwIndex >= 0 && o.activeBtwIndex < len(o.btwCards) {
		return o.btwCards[o.activeBtwIndex]
	}
	return nil
}

// HasAnnouncement reports whether there is an active announcement card.
func (o *OverlayModel) HasAnnouncement() bool {
	return o.announceQueue.Active() != nil
}

// Delegators on RootModel for backward compatibility and domain access:
func (m *RootModel) HasActiveOverlay() bool                 { return m.auth.Active() || m.overlays.HasActiveOverlay() }
func (m *RootModel) HasActivePanel() bool                   { return m.overlays.HasActivePanel() }
func (m *RootModel) ActivePanelKind() panelregistry.Kind    { return m.overlays.ActivePanelKind() }
func (m *RootModel) HasActiveResumePicker() bool            { return m.overlays.HasActiveResumePicker() }
func (m *RootModel) HasActiveLogin() bool                   { return m.auth.Active() }
func (m *RootModel) HasSideQuestions() bool                 { return m.overlays.HasSideQuestions() }
func (m *RootModel) ActiveSideQuestion() *sidequestion.Card { return m.overlays.ActiveSideQuestion() }
func (m *RootModel) HasAnnouncement() bool                  { return m.overlays.HasAnnouncement() }
func (m *RootModel) HasActiveModal() bool                   { return m.auth.Active() || m.overlays.HasActiveModal() }
func (m *RootModel) ActiveCard() bool                       { return m.overlays.ActiveCard() }
