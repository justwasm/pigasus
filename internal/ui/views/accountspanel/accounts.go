// accounts.go — the /accounts panel's first tab: the account list.
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
// # Where the state lives
//
// The rows are re-read from the registry on every open and after every
// action, so the panel never shows a stale account list.

package accountspanel

import (
	"context"
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	accountrouting "github.com/justwasm/pigasus/internal/features/accounts/routing"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// Reload re-reads the registry and the shared credential.
//
// An account that became active outside the clone — a switch made from
// the `agy accounts` command, a sign-in in another terminal — is unknown
// to the registry. Learning it here keeps the panel's list complete and
// the active marker on a row.
func (p *Panel) Reload() {
	if p == nil {
		return
	}
	st, err := accounts.Load()
	if err != nil {
		p.rows = nil
		p.active = ""
		p.notice = "error: " + err.Error()
		return
	}
	p.rows = st.Accounts
	p.active = currentAccountEmail(st)
	if email := p.active; email != "" {
		if _, ok := st.Find(email); !ok {
			accounts.RegisterCurrent(email)
			if reloaded, err := accounts.Load(); err == nil {
				p.rows = reloaded.Accounts
			}
		}
	}
}

// currentAccountEmail returns the email of the account behind the shared
// credential. The registry's own Active field is only a hint — the
// credential is the truth, because the reference can change it too.
func currentAccountEmail(st *accounts.Store) string {
	if _, email := accounts.Current(); email != "" {
		return email
	}
	if st != nil {
		return st.Active
	}
	return ""
}

// accountRowLine renders one account row.
func (p *Panel) accountRowLine(acc accounts.Account, cursor bool) string {
	if isQ, qReason := accountrouting.Shared().IsQuarantined(acc.Email); isQ {
		acc.Disabled = true
		if acc.DisabledReason == "" {
			acc.DisabledReason = qReason
		}
	}
	if strings.TrimSpace(acc.PlanTier) == "" {
		// The tier the session read belongs to the account it was read
		// for: a session that signed in as another account must not show
		// the previous account's tier against this row.
		if tier, ok := p.sessionTier(acc.Email); ok && strings.EqualFold(acc.Email, p.active) {
			acc.PlanTier = tier
		}
	}
	return accounts.RowLine(acc, cursor, p.active, 30, palette.BlueOpen, palette.BlueClose)
}

// selectedAccountEmail returns the email of the row under the cursor.
func (p *Panel) selectedAccountEmail() string {
	if p == nil || p.cursor < 0 || p.cursor >= len(p.rows) {
		return ""
	}
	return p.rows[p.cursor].Email
}

// switchAccount makes the named account the active one.
//
// The token lands in the shared credential locations, so the clone's
// next request uses it and the installed `agy` uses it from its next
// start-up. The transport is rebuilt afterwards: the CloudCode client
// caches the keychain token it read, so a live session would keep
// talking as the previous account until the client is replaced.
func (p *Panel) switchAccount(email string) tea.Cmd {
	email = strings.TrimSpace(email)
	if email == "" {
		p.notice = "error: no account selected."
		return nil
	}
	if strings.EqualFold(email, p.active) {
		p.notice = fmt.Sprintf("%s is already the active account.", email)
		return nil
	}
	acc, err := accounts.Activate(context.Background(), email)
	if err != nil {
		p.notice = "error: " + err.Error()
		p.Reload()
		return nil
	}
	p.notice = fmt.Sprintf("Switched to %s.", acc.Email)
	// The quota error belonged to the account that was active before the
	// switch; keeping it would label the new account with the old one's
	// failure (the panel's one error line is not per row).
	p.quotaErr = ""
	p.applyAccount(acc.Email)
	p.Reload()
	return p.refreshTier()
}

// removeAccount forgets the named account.
//
// Removing the active account signs out for real: the shared credential
// is cleared, so neither the clone nor the reference keeps using it.
// When another account is left, the session continues on it — the point
// of the whole feature is that a sign-out does not end the session.
func (p *Panel) removeAccount(email string) tea.Cmd {
	email = strings.TrimSpace(email)
	if email == "" {
		p.notice = "error: no account selected."
		return nil
	}
	acc, active, err := accounts.Remove(email)
	if err != nil {
		p.notice = "error: " + err.Error()
		return nil
	}
	if acc.Email == "" {
		p.notice = fmt.Sprintf("No account %s.", email)
		return nil
	}
	if !active {
		p.notice = fmt.Sprintf("Removed %s.", acc.Email)
		p.Reload()
		return nil
	}
	// The sign-out is real: the shared credential is cleared, so the
	// session has no account any more.
	p.signOut()
	p.notice = fmt.Sprintf("Signed out of %s.", acc.Email)
	p.Reload()
	// Continue on another account when one is left, so the session stays
	// usable without another sign-in.
	st, err := accounts.Load()
	if err == nil && len(st.Accounts) > 0 {
		next := st.Accounts[0]
		if switched, err := accounts.Activate(context.Background(), next.Email); err == nil {
			p.notice = fmt.Sprintf("Signed out of %s. Now signed in as %s.", acc.Email, switched.Email)
			p.applyAccount(switched.Email)
			p.Reload()
			return p.refreshTier()
		}
	}
	return nil
}

// ---- the quota queue ----------------------------------------------------

// QueueQuota schedules a quota fetch for every account and returns the
// command that starts it.
//
// The active account goes first: its snapshot is the one the /usage
// panel reuses, so it should be the one that lands first.
func (p *Panel) QueueQuota() tea.Cmd {
	if p == nil {
		return nil
	}
	queue := make([]string, 0, len(p.rows))
	active := p.active
	for _, acc := range p.rows {
		if strings.EqualFold(acc.Email, active) {
			continue
		}
		queue = append(queue, acc.Email)
	}
	if active != "" {
		queue = append([]string{active}, queue...)
	}
	p.quotaQueue = queue
	p.quotaErr = ""
	return p.nextQuotaCmd()
}

// nextQuotaCmd returns the command that fetches the next account's
// quota, or nil when the queue is empty.
func (p *Panel) nextQuotaCmd() tea.Cmd {
	if len(p.quotaQueue) == 0 {
		p.status = ""
		return nil
	}
	email := p.quotaQueue[0]
	p.status = fmt.Sprintf("Refreshing quota… %d queued", len(p.quotaQueue))
	return p.quotaCmd(email)
}

// QuotaDone records one account's quota fetch result and returns the
// command that fetches the next one.
//
// gotSummary says whether the fetch answered with a summary; a fetch
// that failed keeps the last error on the panel.
//
// The panel may have been closed while the request was in flight, so the
// next account in the queue is started either way — the queue is the
// panel's, but a fetch that is already running must not be orphaned.
func (p *Panel) QuotaDone(email string, gotSummary bool, err error) tea.Cmd {
	if p == nil {
		return nil
	}
	// The finished account leaves the queue, whether the panel is still
	// open or not: a queue that keeps a finished entry would stall.
	if len(p.quotaQueue) > 0 && strings.EqualFold(p.quotaQueue[0], email) {
		p.quotaQueue = p.quotaQueue[1:]
	}
	switch {
	case err != nil:
		p.quotaErr = err.Error()
	case gotSummary:
		p.quotaErr = ""
	}
	// Pick up the snapshot the fetch cached in the registry.
	p.Reload()
	return p.nextQuotaCmd()
}

// ---- the legend ---------------------------------------------------------

// accountsLegend is the account list's footer.
func accountsLegend() string {
	return tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Switch", "a", "Add account",
		"d", "Remove", "r", "Refresh", "tab", "Pool", "esc", "Exit")
}

// noticeLine renders the panel's one-line status.
//
// An error is warned about, a success is ticked, and anything else is
// said plainly.
func noticeLine(notice string) string {
	styled := "  " + palette.SubtleOpen + notice + "\x1b[39m"
	switch {
	case strings.HasPrefix(strings.ToLower(notice), "error"),
		strings.Contains(strings.ToLower(notice), "already"):
		styled = "  " + palette.WarningOpen + "⚠ " + palette.WarningClose +
			palette.SubtleOpen + notice + "\x1b[39m"
	case strings.HasPrefix(notice, "Switched "),
		strings.HasPrefix(notice, "Signed out "),
		strings.HasPrefix(notice, "Removed "):
		styled = "  " + palette.AgentFgOpen + "✓ " + palette.ToolBulletClose +
			palette.SubtleOpen + notice + "\x1b[39m"
	}
	return styled
}
