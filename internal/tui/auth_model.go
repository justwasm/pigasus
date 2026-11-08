package tui

// auth_model.go — the in-session auth sub-model.
//
// AuthModel owns the in-session login dialog. The dialog's own state
// machine (method selection, browser wait, failure) lives in
// internal/ui/views/loginui; this model is the root's owner for it, the
// same way the reference's AuthModel owns the sign-in flow above the
// account store.
//
// Upstream: google3/third_party/jetski/cli/model/model.AuthModel.

import "github.com/justwasm/pigasus/internal/ui/views/loginui"

// AuthModel owns the in-session login dialog.
type AuthModel struct {
	login *loginui.Model
}

// Active reports whether a login dialog is open.
func (a *AuthModel) Active() bool { return a != nil && a.login != nil }

// Model returns the open dialog, or nil.
func (a *AuthModel) Model() *loginui.Model {
	if a == nil {
		return nil
	}
	return a.login
}

// Begin opens a fresh login dialog and returns it.
func (a *AuthModel) Begin() *loginui.Model {
	a.login = loginui.New()
	return a.login
}

// Set installs the dialog. Update returns a new model, so the host writes
// it back through this method.
func (a *AuthModel) Set(m *loginui.Model) { a.login = m }

// Clear closes the dialog.
func (a *AuthModel) Clear() { a.login = nil }

// DismissMenu closes the dialog when it is still choosing a sign-in
// method. A dialog that is already waiting on the browser is not
// dismissed by Esc. It reports whether it claimed the key.
func (a *AuthModel) DismissMenu() bool {
	if a == nil || a.login == nil {
		return false
	}
	if a.login.State != loginui.StateSelectMethod {
		return false
	}
	a.login = nil
	return true
}
