// Package accountspanel holds the /accounts panel: the account list and
// the account-routing policy.
//
// # What it is
//
// The reference (`agy`) has exactly one credential and no account
// switcher, so there is nothing to match byte for byte here: this is a
// clone-only superset, like /provider. The panel has two views — the
// accounts the clone knows about, and the pool policy that decides which
// of them serves a turn — and the second one is the panel's other tab.
//
// # Where it lives
//
// The panel itself is here: the rows, the tab bar, the keys, the writes
// and the policy's own arithmetic. The host keeps only the seams — the
// session's account identity, the sign-in modal, the plan-tier fetch,
// the quota fetch and the way out of the panel.
//
// The panel is rebuilt every time it opens, so a stale copy of the
// account list, the cursor or the policy is never shown.
package accountspanel

import (
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
)

// The panel's two tabs.
const (
	// TabAccounts is the account list.
	TabAccounts = iota
	// TabPool is the account-routing policy.
	TabPool
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// ApplyAccount installs the account as the session's active one.
	//
	// The session's state is the host's: the banner's account, the
	// transport (the CloudCode client caches the keychain token it read)
	// and the tier the session runs at.
	ApplyAccount func(email string)
	// SignOut tells the host that the session has no account any more.
	//
	// Removing the active account signs the session out for real: the
	// shared credential is cleared, so the session keeps running on
	// nothing until another account is activated.
	SignOut func()
	// StartLogin opens the sign-in modal so another account can be added.
	StartLogin func() tea.Cmd
	// RefreshTier asks the server for the active account's plan tier.
	RefreshTier func() tea.Cmd
	// QuotaCmd starts the quota fetch for one account.
	QuotaCmd func(email string) tea.Cmd
	// SessionTier returns the tier the session read, and whether it was
	// read for that account at all.
	//
	// The tier belongs to the account it was read for, so a session that
	// signed in as another account must not show the previous account's
	// tier against the new row.
	SessionTier func(email string) (string, bool)
	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /accounts panel.
//
// The panel owns the whole surface: the account list, the pool policy,
// the tab bar, the inline edit and the allow-list submenu. Nothing of it
// is left in the host but the seams.
type Panel struct {
	seams Seams

	// tab is the active view: the account list or the pool policy.
	tab int

	// cursor is the selected row. It belongs to the view, not to the
	// panel: it is reset when the tab changes.
	cursor int

	// pool holds the Pool tab's state.
	pool poolState

	// rows are the accounts the registry knows about.
	rows []accounts.Account

	// active is the email of the account behind the shared credential.
	active string

	// notice is the panel's one-line status: the last action's answer.
	notice string

	// status is the panel's one-line status: the quota fetch that is
	// running.
	status string

	// quotaQueue holds the accounts whose quota still has to be fetched,
	// in order. The panel fetches them one at a time, so a registry with
	// many accounts does not fire a burst of requests.
	quotaQueue []string

	// quotaErr is the error the last quota fetch ended with.
	quotaErr string

	// textInput is the inline editor the Pool tab's free-text rows use.
	textInput textinput.Model
}

// New builds the /accounts panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside ------------------------------

// Tab is the active view: TabAccounts or TabPool.
func (p *Panel) Tab() int {
	if p == nil {
		return TabAccounts
	}
	return p.tab
}

// SetTab moves the panel to the given view.
//
// The cursor belongs to the view, not to the panel: it is reset when the
// view changes, so the Pool tab opens on its first row and the account
// list opens on its first account.
//
// The Pool tab re-reads the policy from the configuration file when it
// is shown: the file is shared with the CLI and the wrapper, so the tab
// must show what is on disk now.
func (p *Panel) SetTab(tab int) {
	if p == nil {
		return
	}
	if p.tab == tab {
		return
	}
	p.tab = tab
	p.cursor = 0
	p.notice = ""
	p.pool.notice = ""
	p.pool.editing = false
	p.pool.allowOpen = false
	p.pool.allowCursor = 0
	if tab == TabPool {
		p.ReloadPolicy()
	}
}

// ClearNotice drops the panel's one-line status.
//
// The panel is opened with a clean slate: the last action's answer
// belongs to the panel that showed it.
func (p *Panel) ClearNotice() {
	if p == nil {
		return
	}
	p.notice = ""
}

// Cursor is the selected row's index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	p.cursor = idx
}

// Notice is the panel's one-line status.
func (p *Panel) Notice() string {
	if p == nil {
		return ""
	}
	return p.notice
}

// Rows are the accounts the panel is showing right now.
func (p *Panel) Rows() []accounts.Account {
	if p == nil {
		return nil
	}
	return p.rows
}

// Active is the email of the account behind the shared credential.
func (p *Panel) Active() string {
	if p == nil {
		return ""
	}
	return p.active
}

// QuotaQueue is the list of accounts whose quota is still to be fetched.
func (p *Panel) QuotaQueue() []string {
	if p == nil {
		return nil
	}
	return p.quotaQueue
}

// QuotaErr is the error the last quota fetch ended with.
func (p *Panel) QuotaErr() string {
	if p == nil {
		return ""
	}
	return p.quotaErr
}

// ---- the seams ---------------------------------------------------------

func (p *Panel) applyAccount(email string) {
	if p == nil || p.seams.ApplyAccount == nil {
		return
	}
	p.seams.ApplyAccount(email)
}

func (p *Panel) signOut() {
	if p == nil || p.seams.SignOut == nil {
		return
	}
	p.seams.SignOut()
}

func (p *Panel) startLogin() tea.Cmd {
	if p == nil || p.seams.StartLogin == nil {
		return nil
	}
	return p.seams.StartLogin()
}

func (p *Panel) refreshTier() tea.Cmd {
	if p == nil || p.seams.RefreshTier == nil {
		return nil
	}
	return p.seams.RefreshTier()
}

func (p *Panel) quotaCmd(email string) tea.Cmd {
	if p == nil || p.seams.QuotaCmd == nil {
		return nil
	}
	return p.seams.QuotaCmd(email)
}

func (p *Panel) sessionTier(email string) (string, bool) {
	if p == nil || p.seams.SessionTier == nil {
		return "", false
	}
	return p.seams.SessionTier(email)
}

func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}
