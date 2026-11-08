package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/loginui"
)

// TestAuthModel_OwnsLoginDialog pins the ownership move: the login dialog
// belongs to AuthModel, not to the overlay bucket.
func TestAuthModel_OwnsLoginDialog(t *testing.T) {
	var a AuthModel
	if a.Active() || a.Model() != nil {
		t.Fatal("zero model has an open dialog")
	}
	lm := a.Begin()
	if lm == nil || !a.Active() || a.Model() != lm {
		t.Fatal("Begin did not open and keep the dialog")
	}
	a.Clear()
	if a.Active() {
		t.Fatal("Clear kept the dialog")
	}
}

// TestAuthModel_DismissMenuOnlyWhileChoosing keeps Esc from closing a
// dialog that is already waiting on the browser.
func TestAuthModel_DismissMenuOnlyWhileChoosing(t *testing.T) {
	var a AuthModel
	if a.DismissMenu() {
		t.Fatal("empty model claimed Esc")
	}
	a.Begin()
	if !a.DismissMenu() || a.Active() {
		t.Fatal("method-selection dialog did not dismiss")
	}

	a.Begin()
	a.login.State = loginui.StateOAuth
	if a.DismissMenu() || !a.Active() {
		t.Fatal("waiting dialog was dismissed by Esc")
	}
}

// TestRootModel_LoginClaimsModal keeps the root's predicates complete now
// that the dialog is not on the overlay model.
func TestRootModel_LoginClaimsModal(t *testing.T) {
	m := &RootModel{}
	if m.HasActiveModal() || m.HasActiveLogin() || m.HasActiveOverlay() {
		t.Fatal("empty root claims an overlay")
	}
	m.auth.Begin()
	if !m.HasActiveModal() || !m.HasActiveLogin() || !m.HasActiveOverlay() {
		t.Fatal("login did not claim the modal predicates")
	}
}
