package cliapp

import (
	tea "charm.land/bubbletea/v2"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/internal/features/auth"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
)

func (m *runMode) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd, bool) {
	if m.login == nil {
		return m, nil, false
	}
	if !tuiviewport.IsScrollMsg(msg) {
		m.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.login.Width, m.login.Height = msg.Width, msg.Height
		return m, nil, true
	case tea.KeyPressMsg:
		if msg.String() == "ctrl+c" {
			m.login = nil
			return m, tea.Quit, true
		}
		if msg.String() == "esc" && m.login.State == loginui.StateSelectMethod {
			m.login = nil
			m.slashRespond("/login", "Sign in cancelled.")
			return m, nil, true
		}
	case *auth.AntigravityToken:
		return m, m.finishLogin(msg), true
	}
	newLM, cmd := m.login.Update(msg)
	if lm, ok := newLM.(*loginui.Model); ok {
		m.login = lm
		if lm.Authenticated {
			// Cycle multi-account-switch: the OAuth sign-in becomes a
			// known account (the *auth.AntigravityToken case above).
			// The ADC path carries no token, so there is nothing to
			// register here.
			return m, m.finishLogin(nil), true
		}
	}
	return m, cmd, true
}

// finishLogin closes the login panel and applies the signed-in identity.
//
// The token is the OAuth path's credential; it is nil on the ADC path,
// which carries no token to register.
//
// It returns the command that reads the new account's tier: the tier
// belongs to an account, so the session that just signed in as another
// account has to ask about *that* account — otherwise the previous
// account's tier would be shown against the new one.
func (m *runMode) finishLogin(token *auth.AntigravityToken) tea.Cmd {
	m.login = nil
	m.email = auth.EffectiveUserEmail()
	if m.backendFor != nil {
		m.backend = m.backendFor(m.model)
	}
	// The session's tier belonged to the previous account.
	m.forgetSessionTier()
	// Cycle multi-account-switch: the sign-in becomes a known
	// account, so it can be switched back to later.
	if token != nil {
		acc, _, err := accounts.Register(token)
		if err == nil && acc.Email != "" {
			m.email = acc.Email
		}
	}
	// The sign-in completed, so the shared credential changed: the
	// panel's rows have to be refreshed even when no panel is open.
	m.accountsPanel().Reload()
	m.slashRespond("/login", "Authentication successful!")
	return m.fetchPlanTierCmd()
}
