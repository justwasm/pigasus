package tui

// overlay_model.go — the modal-overlay sub-model.
//
// OverlayModel owns the state of everything that sits above the primary
// frame: the modal panel, the resume picker, the side-question cards, the
// announcement queue, the eligibility notice and the in-session login
// model. It also owns the Escape dismissal chain, which is the one rule
// every overlay shares (AGENTS invariant #10).
//
// RootModel routes a key here when an overlay is active and performs the
// panel-specific work through the small OverlayDismissHost seam.
//
// Upstream: google3/third_party/jetski/cli/model/model.RootModel's overlay
// fields plus the panel models' own Dismiss handlers.

import (
	"context"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/resumepicker"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

// OverlayModel owns all non-primary screen overlays, modals, popups, cards,
// and notices (Side Questions, Modal Panels, Resume Picker, Announcements,
// Eligibility Notices, and Login Dialog).
type OverlayModel struct {
	// Modal Panels & Pickers
	panel        slashPanel
	panelReg     *panelregistry.Registry
	resumePicker *resumepicker.Picker

	// Side Question (/btw) Cards
	btwManager      *sidequestion.Manager
	btwCards        []*sidequestion.Card
	activeBtwID     string
	activeBtwIndex  int
	activeBtwCancel context.CancelFunc
	btwScroll       int
	asyncBtw        bool

	// Notice & Announcement Cards
	announceQueue     announce.Queue
	eligibilityNotice announce.Notice
}

// OverlayDismissHost is the host surface the dismissal chain needs: the
// panel owns its own Esc behaviour, the card and viewport belong to the
// root.
type OverlayDismissHost interface {
	HandlePanelKey(key tea.Key) (bool, tea.Cmd)
	ClosePanel() tea.Cmd
	DismissBtwResponse()
	ViewportHeight() int
	HelpActive() bool
	CloseHelp()
}

// Dismiss runs the Escape dismissal chain in priority order: help overlay,
// modal panel, resume picker, side-question card. It reports whether an overlay
// claimed the key and the command the dismissal scheduled.
func (o *OverlayModel) Dismiss(host OverlayDismissHost) (tea.Cmd, bool) {
	if o == nil {
		return nil, false
	}
	if host != nil && host.HelpActive() {
		host.CloseHelp()
		return nil, true
	}
	if o.panel.kind != panelregistry.None {
		if handled, cmd := host.HandlePanelKey(tea.Key{Code: tea.KeyEsc}); handled {
			return cmd, true
		}
		return host.ClosePanel(), true
	}
	if o.resumePicker != nil && o.resumePicker.Active() {
		return o.resumePicker.HandleKey(tea.Key{Code: tea.KeyEsc}, host.ViewportHeight()), true
	}
	if o.BtwCardVisible() {
		host.DismissBtwResponse()
		return nil, true
	}
	return nil, false
}

// HasActiveModal reports whether a modal overlay claims the keyboard.
func (o *OverlayModel) HasActiveModal() bool {
	return o != nil && (o.panel.kind != panelregistry.None ||
		(o.resumePicker != nil && o.resumePicker.Active()))
}

// BtwCardVisible reports whether the side-question card is showing.
func (o *OverlayModel) BtwCardVisible() bool {
	return o != nil && o.activeBtwIndex >= 0 && o.activeBtwIndex < len(o.btwCards)
}

// ActiveCard reports whether a floating card (Side Question or Announcement)
// is currently visible.
func (o *OverlayModel) ActiveCard() bool {
	return o.BtwCardVisible() || (o != nil && o.announceQueue.Active() != nil)
}
