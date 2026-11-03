// keys.go — the panel's keys.
//
// The panel owns every key it receives: the generic list navigation must
// not see them, because the panel's own keys are not navigation — "a"
// adds an account, "d" removes one, and the arrow keys on the Pool tab
// change a value instead of moving a cursor.

package accountspanel

import (
	tea "charm.land/bubbletea/v2"
)

// HandleKey routes the panel's keys.
//
// The two tabs own their keys separately: the account list navigates and
// acts, the Pool tab changes values and edits them.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.tab == TabPool {
		return p.handlePoolKey(key)
	}
	return p.handleAccountsKey(key)
}

// handleAccountsKey routes the account list's keys.
func (p *Panel) handleAccountsKey(key tea.Key) (bool, tea.Cmd) {
	switch key.String() {
	case "tab":
		p.SetTab(TabPool)
		return true, nil
	case "shift+tab":
		p.SetTab(TabAccounts)
		return true, nil
	case "esc":
		return true, p.close()
	case "up":
		if p.cursor > 0 {
			p.cursor--
		}
		return true, nil
	case "down":
		if p.cursor < len(p.rows)-1 {
			p.cursor++
		}
		return true, nil
	case "enter":
		return true, p.switchAccount(p.selectedAccountEmail())
	case "a":
		return true, p.startLogin()
	case "d":
		return true, p.removeAccount(p.selectedAccountEmail())
	case "r":
		// Refresh the live numbers: every account's quota and the
		// tier of the account the session runs on. The tier belongs to
		// the active account only, so an inactive row keeps the tier it
		// learned when it was active.
		p.Reload()
		return true, tea.Batch(p.refreshTier(), p.QueueQuota())
	}
	return true, nil
}

// handlePoolKey owns every key the Pool tab receives.
//
// The tab has two modes: the field list and the allow-list submenu.
// Both are handled here so the keys never fall through to the generic
// navigation handler — the arrow keys change a value, they do not move
// a cursor.
func (p *Panel) handlePoolKey(key tea.Key) (bool, tea.Cmd) {
	if p.pool.editing {
		return p.handlePoolEditKey(key)
	}
	if p.pool.allowOpen {
		return p.handlePoolAllowKey(key)
	}

	switch key.String() {
	case "esc":
		return true, p.close()
	case "tab", "shift+tab":
		p.SetTab(TabAccounts)
		return true, nil
	case "up":
		p.poolMoveCursor(-1)
		return true, nil
	case "down":
		p.poolMoveCursor(+1)
		return true, nil
	case "left":
		p.poolChange(-1)
		return true, nil
	case "right":
		p.poolChange(+1)
		return true, nil
	case "enter":
		return true, p.poolActivateRow()
	case "space":
		p.poolChange(+1)
		return true, nil
	}
	return true, nil
}

// handlePoolEditKey owns every key the row being typed into receives.
func (p *Panel) handlePoolEditKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		p.pool.editing = false
		return true, nil
	case tea.KeyEnter:
		return true, p.poolCommitEdit()
	}
	updated, cmd := p.textInput.Update(tea.KeyPressMsg{Code: key.Code, Mod: key.Mod, Text: key.Text})
	p.textInput = updated
	return true, cmd
}

// handlePoolAllowKey owns every key the allow-list submenu receives.
func (p *Panel) handlePoolAllowKey(key tea.Key) (bool, tea.Cmd) {
	emails := p.poolAccountEmails()
	switch key.String() {
	case "esc":
		p.pool.allowOpen = false
		p.pool.allowCursor = 0
		return true, nil
	case "up":
		if p.pool.allowCursor > 0 {
			p.pool.allowCursor--
		}
		return true, nil
	case "down":
		if p.pool.allowCursor < len(emails) {
			p.pool.allowCursor++
		}
		return true, nil
	case "space", "enter":
		p.poolToggleAllow()
		return true, nil
	}
	return true, nil
}
