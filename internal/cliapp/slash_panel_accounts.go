// slash_panel_accounts.go — cycle multi-account-switch: the /accounts panel.
//
// # What it is
//
// The reference (`agy`) has exactly one credential and no account
// switcher, so there is nothing to match byte for byte here: this is a
// clone-only superset, like /provider. The command is invocable but
// *not* listed in the picker's catalogue, whose entry count is
// byte-pinned to the reference's 99.
//
// # What it does
//
// The panel lists the accounts the clone knows about and lets the user
//
//   - switch the active account (enter) — the token is written to the
//     shared credential locations, so the installed `agy` uses the same
//     account from its next start-up;
//   - add an account (a) — the same sign-in modal /login opens;
//   - remove an account (d) — the active one is signed out for real,
//     and the session continues on another account when one is left.
//
// # Layout
//
// The shape follows the clone's own panels: a title, the rows with a
// blue ">" cursor, a notice line, the registry's path, and the
// "Keyboard: …" legend.
package cliapp

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/loginui"
)

// The /accounts panel's tabs.
//
// The panel has two views: the accounts the clone knows about, and the
// pool policy that decides which of them serves a turn. The tab bar is
// the /resume picker's (see poolTabBar).
const (
	accountsTabAccounts = iota
	accountsTabPool
)

// accountsPanelState is the /accounts panel's state. The rows are
// re-read from the registry on every open and after every action, so
// the panel never shows a stale account list.
type accountsPanelState struct {
	// tab is the active view: the account list or the pool policy.
	tab int

	// pool holds the Pool tab's state. See slash_panel_pool.go.
	pool poolPanelState

	rows   []accounts.Account
	active string
	notice string
	// status is the panel's one-line status: the quota fetch that is
	// running, or the error the last fetch ended with.
	status string
	// quotaQueue holds the accounts whose quota still has to be
	// fetched, in order. The panel fetches them one at a time, so a
	// registry with many accounts does not fire a burst of requests.
	quotaQueue []string
	// quotaErr is the error the last quota fetch failed with.
	quotaErr string
}

// reloadAccountsRows re-reads the registry and the shared credential.
func (m *runMode) reloadAccountsRows() {
	st, err := accounts.Load()
	if err != nil {
		m.panel.acct.rows = nil
		m.panel.acct.active = ""
		m.panel.acct.notice = "error: " + err.Error()
		return
	}
	m.panel.acct.rows = st.Accounts
	m.panel.acct.active = m.currentAccountEmail(st)
	// An account that became active outside the clone — a switch made
	// from the `agy accounts` command, a sign-in in another terminal —
	// is unknown to the registry. Learning it here keeps the panel's
	// list complete and the active marker on a row.
	if email := m.panel.acct.active; email != "" {
		if _, ok := st.Find(email); !ok {
			registerCurrentAccount(email)
			if reloaded, err := accounts.Load(); err == nil {
				m.panel.acct.rows = reloaded.Accounts
			}
		}
	}
}

// ---- the quota queue ----------------------------------------------------

// queueAccountsQuota schedules a quota fetch for every account and
// returns the command that starts it.
//
// The active account goes first: its snapshot is the one the /usage
// panel reuses, so it should be the one that lands first.
func (m *runMode) queueAccountsQuota() tea.Cmd {
	queue := make([]string, 0, len(m.panel.acct.rows))
	active := m.panel.acct.active
	for _, acc := range m.panel.acct.rows {
		if strings.EqualFold(acc.Email, active) {
			continue
		}
		queue = append(queue, acc.Email)
	}
	if active != "" {
		queue = append([]string{active}, queue...)
	}
	m.panel.acct.quotaQueue = queue
	m.panel.acct.quotaErr = ""
	return m.nextAccountsQuotaCmd()
}

// nextAccountsQuotaCmd returns the command that fetches the next
// account's quota, or nil when the queue is empty.
func (m *runMode) nextAccountsQuotaCmd() tea.Cmd {
	if len(m.panel.acct.quotaQueue) == 0 {
		m.panel.acct.status = ""
		return nil
	}
	email := m.panel.acct.quotaQueue[0]
	m.panel.acct.status = fmt.Sprintf("Refreshing quota… %d queued", len(m.panel.acct.quotaQueue))
	return fetchAccountQuotaCmd(email)
}

// currentAccountEmail returns the email of the account behind the
// shared credential. The registry's own Active field is only a hint —
// the credential is the truth, because the reference can change it too.
func (m *runMode) currentAccountEmail(st *accounts.Store) string {
	if _, email := accounts.Current(); email != "" {
		return email
	}
	if st != nil {
		return st.Active
	}
	return ""
}

// ---- rendering ---------------------------------------------------------

// panelAccountsLines renders the /accounts panel.
func (m *runMode) panelAccountsLines() []string {
	if m.panel.acct.tab == accountsTabPool {
		return m.panelPoolLines()
	}
	lines := []string{panelTitleOpen + "Accounts" + panelTitleClose}
	lines = append(lines, m.poolTabBar(), "")
	rows := m.panel.acct.rows
	if len(rows) == 0 {
		lines = append(lines, "  "+panelGray238+"No accounts. Press a to sign in."+panelGray238C)
	}
	for i, acc := range rows {
		lines = append(lines, m.accountRowLine(acc, i == m.panel.cursor))
	}
	if m.panel.acct.notice != "" {
		lines = append(lines, "", "  "+panelGray238+m.panel.acct.notice+panelGray238C)
	}
	status := m.panel.acct.status
	quotaErr := m.panel.acct.quotaErr
	if status != "" || quotaErr != "" {
		lines = append(lines, "")
		if status != "" {
			lines = append(lines, "  "+panelGray238+status+panelGray238C)
		}
		if quotaErr != "" {
			lines = append(lines, "  "+panelGray238+"quota: "+quotaErr+panelGray238C)
		}
	}
	lines = append(lines, "", "  "+panelGray238+accounts.Path()+panelGray238C)
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate", "enter", "Switch", "a", "Add account",
		"d", "Remove", "r", "Refresh", "tab", "Pool", "esc", "Exit"))
	return lines
}

// accountRowLine renders one account row. Delegated to accounts.RowLine.
func (m *runMode) accountRowLine(acc accounts.Account, cursor bool) string {
	if strings.TrimSpace(acc.PlanTier) == "" {
		if strings.EqualFold(acc.Email, m.panel.acct.active) && m.tierFetched && strings.EqualFold(m.tierAccount, acc.Email) {
			acc.PlanTier = m.tier
		}
	}
	return accounts.RowLine(acc, cursor, m.panel.acct.active, 30, cBlueOpen, cBlueClose)
}

// accountQuotaCell renders the account's cached quota. Delegated to accounts.QuotaCell.
func accountQuotaCell(acc accounts.Account) string {
	return accounts.QuotaCell(acc)
}

// quotaSnapshotAge renders how old a cached quota is. Delegated to accounts.QuotaSnapshotAge.
func quotaSnapshotAge(snap *auth.QuotaSnapshot) string {
	return accounts.QuotaSnapshotAge(snap)
}

// ---- keys --------------------------------------------------------------

// handleAccountsPanelKey routes the /accounts panel's keys. The panel
// owns every key, so the generic list navigation must not see them.
func (m *runMode) handleAccountsPanelKey(key tea.Key) (bool, tea.Cmd) {
	if m.panel.acct.tab == accountsTabPool {
		return m.handlePoolPanelKey(key)
	}
	rows := m.panel.acct.rows
	switch key.String() {
	case "tab":
		m.switchAccountsTab(accountsTabPool)
		return true, nil
	case "shift+tab":
		m.switchAccountsTab(accountsTabAccounts)
		return true, nil
	case "esc":
		return true, m.closePanel()
	case "up":
		if m.panel.cursor > 0 {
			m.panel.cursor--
		}
		return true, nil
	case "down":
		if m.panel.cursor < len(rows)-1 {
			m.panel.cursor++
		}
		return true, nil
	case "enter":
		return true, m.switchAccount(m.selectedAccountEmail())
	case "a":
		return true, m.startAccountLogin()
	case "d":
		return true, m.removeAccount(m.selectedAccountEmail())
	case "r":
		// Refresh the live numbers: every account's quota and the
		// tier of the account the session runs on. The tier belongs
		// to the active account only, so an inactive row keeps the
		// tier it learned when it was active.
		m.reloadAccountsRows()
		return true, tea.Batch(m.fetchPlanTierCmd(), m.queueAccountsQuota())
	}
	return true, nil
}

// selectedAccountEmail returns the email of the row under the cursor.
func (m *runMode) selectedAccountEmail() string {
	rows := m.panel.acct.rows
	if m.panel.cursor < 0 || m.panel.cursor >= len(rows) {
		return ""
	}
	return rows[m.panel.cursor].Email
}

// switchAccountsTab moves the panel to the given tab.
//
// The cursor belongs to the view, not to the panel: it is reset when
// the tab changes, so the Pool tab opens on its first row and the
// account list opens on its first account.
//
// The Pool tab re-reads the policy from the configuration file when it
// is shown: the file is shared with the CLI and the wrapper, so the
// tab must show what is on disk now.
func (m *runMode) switchAccountsTab(tab int) {
	if m.panel.acct.tab == tab {
		return
	}
	m.panel.acct.tab = tab
	m.panel.cursor = 0
	m.panel.acct.notice = ""
	m.panel.acct.pool.notice = ""
	m.panel.acct.pool.editing = false
	m.panel.acct.pool.allowOpen = false
	m.panel.acct.pool.allowCursor = 0
	if tab == accountsTabPool {
		m.reloadPoolPolicy()
	}
}

// ---- actions -----------------------------------------------------------

// switchAccount makes the named account the active one.
//
// The token lands in the shared credential locations, so the clone's
// next request uses it and the installed `agy` uses it from its next
// start-up. The transport is rebuilt afterwards: the CloudCode client
// caches the keychain token it read, so a live session would keep
// talking as the previous account until the client is replaced.
func (m *runMode) switchAccount(email string) tea.Cmd {
	email = strings.TrimSpace(email)
	if email == "" {
		m.panel.acct.notice = "error: no account selected."
		return nil
	}
	if strings.EqualFold(email, m.panel.acct.active) {
		m.panel.acct.notice = fmt.Sprintf("%s is already the active account.", email)
		return nil
	}
	acc, err := accounts.Activate(context.Background(), email)
	if err != nil {
		m.panel.acct.notice = "error: " + err.Error()
		m.reloadAccountsRows()
		return nil
	}
	m.panel.acct.notice = fmt.Sprintf("Switched to %s.", acc.Email)
	m.applyActiveAccount(acc.Email)
	m.reloadAccountsRows()
	return m.fetchPlanTierCmd()
}

// removeAccount forgets the named account.
//
// Removing the active account signs out for real: the shared credential
// is cleared, so neither the clone nor the reference keeps using it.
// When another account is left, the session continues on it — the point
// of the whole feature is that a sign-out does not end the session.
func (m *runMode) removeAccount(email string) tea.Cmd {
	email = strings.TrimSpace(email)
	if email == "" {
		m.panel.acct.notice = "error: no account selected."
		return nil
	}
	acc, active, err := accounts.Remove(email)
	if err != nil {
		m.panel.acct.notice = "error: " + err.Error()
		return nil
	}
	if acc.Email == "" {
		m.panel.acct.notice = fmt.Sprintf("No account %s.", email)
		return nil
	}
	if !active {
		m.panel.acct.notice = fmt.Sprintf("Removed %s.", acc.Email)
		m.reloadAccountsRows()
		return nil
	}
	m.email = ""
	m.panel.acct.notice = fmt.Sprintf("Signed out of %s.", acc.Email)
	m.reloadAccountsRows()
	// Continue on another account when one is left, so the session stays
	// usable without another sign-in.
	st, err := accounts.Load()
	if err == nil && len(st.Accounts) > 0 {
		next := st.Accounts[0]
		if switched, err := accounts.Activate(context.Background(), next.Email); err == nil {
			m.panel.acct.notice = fmt.Sprintf("Signed out of %s. Now signed in as %s.", acc.Email, switched.Email)
			m.applyActiveAccount(switched.Email)
			m.reloadAccountsRows()
			return m.fetchPlanTierCmd()
		}
	}
	return nil
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

// registerAccount adds the account behind tok to the registry and makes
// it the active one. It is called after every sign-in, so the account
// the user just authenticated with is the one the session runs on.
func registerAccount(tok *auth.AntigravityToken) (string, error) {
	if tok == nil {
		return "", fmt.Errorf("accounts: no token")
	}
	acc, _, err := accounts.Register(tok)
	if err != nil {
		return "", err
	}
	return acc.Email, nil
}
