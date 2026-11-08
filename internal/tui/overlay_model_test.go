package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
)

type fakeOverlayHost struct {
	panelEsc   bool
	closed     bool
	btwDismiss bool
	height     int
}

func (f *fakeOverlayHost) HandlePanelKey(key tea.Key) (bool, tea.Cmd) {
	return f.panelEsc, nil
}
func (f *fakeOverlayHost) ClosePanel() tea.Cmd { f.closed = true; return nil }
func (f *fakeOverlayHost) DismissBtwResponse() { f.btwDismiss = true }
func (f *fakeOverlayHost) ViewportHeight() int { return f.height }

// TestOverlayModel_DismissChain pins the Esc priority for overlays: the
// modal panel first, then the side-question card. The login dialog is the
// AuthModel's and is dismissed by the root before this chain runs.
func TestOverlayModel_DismissChain(t *testing.T) {
	host := &fakeOverlayHost{}

	// A panel that does not claim Esc is closed by the host.
	o := OverlayModel{panel: slashPanel{kind: panelregistry.Tasks}}
	if _, handled := o.Dismiss(host); !handled || !host.closed {
		t.Fatalf("panel dismissal: handled=%v closed=%v", handled, host.closed)
	}
	// A panel that claims Esc keeps it (the panel owns the key).
	host.closed = false
	host.panelEsc = true
	o = OverlayModel{panel: slashPanel{kind: panelregistry.Tasks}}
	if _, handled := o.Dismiss(host); !handled || host.closed {
		t.Fatalf("claimed panel: handled=%v closed=%v", handled, host.closed)
	}

	// A side-question card is the last overlay in the chain.
	host.panelEsc = false
	o = OverlayModel{btwCards: []*sidequestion.Card{{}}, activeBtwIndex: 0}
	if _, handled := o.Dismiss(host); !handled || !host.btwDismiss {
		t.Fatalf("btw dismissal: handled=%v dismissed=%v", handled, host.btwDismiss)
	}

	// With no overlay the key falls through to the caller (turn cancel,
	// Esc-Esc, the composer).
	o = OverlayModel{}
	if _, handled := o.Dismiss(host); handled {
		t.Fatal("an empty overlay stack claimed Esc")
	}
}

// TestOverlayModel_ActiveModal covers the keyboard-claim predicate.
func TestOverlayModel_ActiveModal(t *testing.T) {
	if (&OverlayModel{}).HasActiveModal() {
		t.Fatal("empty overlay stack claims a modal")
	}
	if !(&OverlayModel{panel: slashPanel{kind: panelregistry.Tasks}}).HasActiveModal() {
		t.Fatal("panel did not claim a modal")
	}
}
