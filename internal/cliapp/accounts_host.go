// accounts_host.go — the host's side of the /accounts panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/accountspanel: it owns the
// account list, the pool policy, the tab bar, the keys and the writes.
// The host supplies the five things the panel cannot answer itself — the
// session's account identity, the sign-in modal, the plan-tier fetch,
// the quota fetch and the way out of the panel — and nothing else.
//
// The panel is rebuilt every time it opens: the host's slashPanel struct
// is reset when a panel closes, so a stale copy of the account list, the
// cursor or the policy is never shown.
//
// # What the host keeps
//
// The session's account identity is the host's: the banner's email, the
// transport (the CloudCode client caches the keychain token it read, so
// a switch has to replace the client) and the tier the session runs at.

package cliapp

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/accountspanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
)

// newAccountsPanel builds the /accounts panel with the host's seams.
func (m *runMode) newAccountsPanel() *accountspanel.Panel {
	return accountspanel.New(accountspanel.Seams{
		// The session's account identity is the host's: the banner, the
		// transport and the tier all follow the switch.
		ApplyAccount: m.applyActiveAccount,
		// A sign-out is the host's too: the session keeps running on
		// nothing until another account is activated.
		SignOut: m.signOutActiveAccount,
		// The sign-in modal is the session's: the /accounts panel opens
		// the same modal /login opens.
		StartLogin: m.startAccountLogin,
		// The plan tier is the server's answer, and the request is the
		// session's.
		RefreshTier: m.fetchPlanTierCmd,
		// The quota fetch is the host's: the answer lands in the
		// session's cache, which the /usage panel reuses.
		QuotaCmd: fetchAccountQuotaCmd,
		// The tier the session read belongs to the account it was read
		// for, so the panel asks which account it belongs to.
		SessionTier: m.sessionTierFor,
		// Esc leaves the panel.
		Close: m.closePanel,
	})
}

// accountsPanel returns the open /accounts panel.
//
// The panel is built when the /accounts panel opens — the host's
// slashPanel struct is reset whenever a panel closes — so the accessor
// is here so that a caller which finds the panel missing still gets one
// that answers.
func (m *runMode) accountsPanel() *accountspanel.Panel {
	if m.panel.acct == nil {
		m.panel.acct = m.newAccountsPanel()
	}
	return m.panel.acct
}

// sessionTierFor returns the tier the session read, and whether it was
// read for that account at all.
//
// The tier belongs to the account it was read for: a session that signed
// in as another account must not show the previous account's tier
// against the new row.
func (m *runMode) sessionTierFor(email string) (string, bool) {
	if m == nil || !m.tierFetched {
		return "", false
	}
	if !strings.EqualFold(m.tierAccount, email) {
		return "", false
	}
	return m.tier, true
}

// startAccountLogin opens the sign-in modal so another account can be
// added. The account is registered when the sign-in completes (see
// updateLogin).
func (m *runMode) startAccountLogin() tea.Cmd {
	lm := loginui.New()
	lm.Width = m.width
	lm.Height = m.height
	m.login = lm
	return lm.Init()
}

// applyActiveAccount updates the session state after a switch: the
// banner's account, the transport and the quota tier.
//
// The session's tier belongs to the account it was read for, so a
// switch forgets it: until the new account's tier arrives the banner
// shows the email alone, and the panel shows the new row's cached tier
// (or a dash), never the previous account's tier.
func (m *runMode) applyActiveAccount(email string) {
	m.email = email
	m.forgetSessionTier()
	if m.backendFor != nil {
		if b := m.backendFor(m.model); b != nil {
			m.backend = b
		}
	}
}

// signOutActiveAccount forgets the session's account.
//
// The shared credential was cleared, so the session has no account any
// more: the banner shows the email line alone.
func (m *runMode) signOutActiveAccount() {
	m.email = ""
}

// forgetSessionTier drops the tier the session read for the previous
// account.
//
// The tier is an account's, not the session's: a switch, a sign-in or
// an outside change of the credential all invalidate it. The next
// answer fills it in again.
//
// The tier is emptied, not replaced by a placeholder: the banner
// shows the email alone until the answer lands, so the line does not
// flash a value that is not the account's.
func (m *runMode) forgetSessionTier() {
	m.tier = ""
	m.tierAccount = ""
	m.tierFetched = false
}
