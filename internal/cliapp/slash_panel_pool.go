// slash_panel_pool.go — cycle 361: the /accounts panel's Pool tab.
//
// # What it is
//
// The account-routing policy (the "account_pool" section of
// ~/.config/openagy/config.json) had two homes before this file: the
// configuration file, and the doc. The file is the policy's home, but a
// file the user has to edit by hand is a policy they forget about. This
// file gives the policy a panel: the second tab of /accounts, next to
// the account list.
//
// The tab bar is the /resume picker's tab bar (the active tab is bold,
// underlined and blue; the inactive one is the separator grey; the row
// ends with "(tab to cycle)"), because that is the reference's own way
// of showing that a surface has more than one view.
//
// # What it does
//
// The rows are the policy's own fields:
//
//	Enabled            on / off
//	Strategy           sticky · round_robin · quota_aware · failover
//	Threshold          the quota floor, in percent
//	Refresh interval   how often a quota figure is read again
//	Cooldown           how long a spent account stays out
//	Max attempts       how many accounts one turn may try
//	Switch on quota    whether a quota error moves the next turn
//	Accounts in pool   the allow-list
//
// ←/→ change a value, `enter` edits a free-text one, and every change
// is written out at once — the policy is read per turn, so a turn
// started after the write runs on the new policy. Nothing is staged and
// nothing has to be confirmed.
//
// # One writer
//
// Every write goes through endpoints.SaveAccountPool — the same layer
// the file's reader is in. The panel holds no configuration logic of its
// own: it builds an accountpool.Config and hands it over, and the
// writer rewrites the one section that changed and leaves the rest of
// the document alone.
package cliapp

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
)

// poolPanelState is the Pool tab's state.
//
// The policy is re-read from the configuration file every time the tab
// is shown, so the panel shows what is on disk and not what was on disk
// when the panel was last used — the file is shared with the CLI and
// the wrapper.
type poolPanelState struct {
	// policy is the section the file carries, with its defaults filled
	// in.
	policy accountpool.Config

	// present reports whether the file carries the section at all. A
	// file without one is "no routing", which is the same behaviour as a
	// policy that is switched off — but the panel says which one it is.
	present bool

	// notice is the panel's one-line status: the last write's answer.
	notice string

	// editing reports whether a row is being typed into, and editRow
	// is which one.
	editing bool
	editRow int

	// allowOpen is the allow-list submenu's open flag, and allowCursor
	// its row.
	allowOpen   bool
	allowCursor int
}

// The Pool tab's rows, in the order they are shown.
const (
	poolRowEnabled = iota
	poolRowStrategy
	poolRowThreshold
	poolRowRefresh
	poolRowCooldown
	poolRowAttempts
	poolRowSwitchOnQuota
	poolRowAccounts
	poolRowCount
)

// poolRowLabels are the row names, in the order the rows are shown.
var poolRowLabels = [poolRowCount]string{
	"Enabled",
	"Strategy",
	"Threshold",
	"Refresh interval",
	"Cooldown",
	"Max attempts",
	"Switch on quota error",
	"Accounts in pool",
}

// poolDurationLadder is the set of durations ←/→ cycles through.
//
// A duration is a free-text field, so `enter` takes any Go duration
// ("90s", "2m", "1h30m"). The ladder is the quick way: the arrow keys
// walk the values a pool is actually run with.
var poolDurationLadder = []string{"30s", "1m", "2m", "5m", "15m", "1h"}

// ---- loading ------------------------------------------------------------

// reloadPoolPolicy re-reads the policy from the configuration file.
//
// A file without the section is not an error: it is a pool that is
// switched off, and the panel shows the defaults with "off".
func (m *runMode) reloadPoolPolicy() {
	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		m.panel.acct.pool.policy = accountpool.Config{}.Normalised()
		m.panel.acct.pool.present = false
		return
	}
	m.panel.acct.pool.policy = policy.Normalised()
	m.panel.acct.pool.present = true
}

// ---- rendering ----------------------------------------------------------

// panelPoolLines renders the Pool tab.
func (m *runMode) panelPoolLines() []string {
	lines := []string{panelTitleOpen + "Account pool" + panelTitleClose}
	lines = append(lines, m.poolTabBar())
	lines = append(lines, "")

	if m.panel.acct.pool.allowOpen {
		lines = append(lines, m.poolAllowListLines()...)
	} else {
		for i := 0; i < poolRowCount; i++ {
			lines = append(lines, m.poolRowLine(i, i == m.panel.cursor))
		}
	}

	if notice := m.panel.acct.pool.notice; notice != "" {
		lines = append(lines, "", "  "+panelGray238+notice+panelGray238C)
	}
	lines = append(lines, "", "  "+panelGray238+endpoints.Path()+panelGray238C)
	if m.panel.acct.pool.allowOpen {
		lines = append(lines, panelKeyboardHint("↑/↓", "Navigate", "space", "Include", "esc", "Back"))
		return lines
	}
	lines = append(lines, panelKeyboardHint("↑/↓", "Navigate", "←/→", "Change", "enter", "Edit",
		"tab", "Accounts", "esc", "Exit"))
	return lines
}

// poolTabBar renders the tab bar: the /resume picker's shape, with the
// Accounts tab and the Pool tab.
func (m *runMode) poolTabBar() string {
	var b strings.Builder
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + cBlueOpen + name + pickerKeyClose)
			return
		}
		b.WriteString(cDimGrayOpen + name + pickerKeyClose)
	}
	b.WriteString("   ")
	writeTab("Accounts", m.panel.acct.tab != accountsTabPool)
	b.WriteString("    ")
	writeTab("Pool", m.panel.acct.tab == accountsTabPool)
	b.WriteString(" " + cDimGrayOpen + "  (tab to cycle)" + pickerKeyClose)
	return b.String()
}

// poolRowLine renders one row of the Pool tab.
//
// The value is rendered next to the name, and the row under the cursor
// is the one whose value the arrow keys change. A row being typed into
// shows what has been typed — the panel must not swallow the input.
func (m *runMode) poolRowLine(row int, cursor bool) string {
	name := poolRowLabels[row]
	value := m.poolRowValue(row)
	if m.panel.acct.pool.editing && m.panel.acct.pool.editRow == row {
		value = m.panel.textInput.Value() + searchCursorBlock
	}
	if cursor {
		return cBlueOpen + "> " + name + pickerKeyClose + "  " + cBlueOpen + value + pickerKeyClose
	}
	return "  " + name + "  " + cDimGrayOpen + value + pickerKeyClose
}

// poolRowValue renders the value of one row.
func (m *runMode) poolRowValue(row int) string {
	p := m.panel.acct.pool.policy
	switch row {
	case poolRowEnabled:
		return poolOnOff(p.Enabled)
	case poolRowStrategy:
		return string(p.Strategy)
	case poolRowThreshold:
		return fmt.Sprintf("%.0f%%", p.Threshold*100)
	case poolRowRefresh:
		return poolDurationText(p.RefreshInterval, accountpool.DefaultRefreshInterval)
	case poolRowCooldown:
		return poolDurationText(p.Cooldown, accountpool.DefaultCooldown)
	case poolRowAttempts:
		return strconv.Itoa(p.MaxAttempts)
	case poolRowSwitchOnQuota:
		return poolOnOff(p.SwitchOnQuotaErrorEnabled())
	case poolRowAccounts:
		if len(p.Accounts) == 0 {
			return fmt.Sprintf("all (%d)", m.poolAccountCount())
		}
		return fmt.Sprintf("%d of %d", len(p.Accounts), m.poolAccountCount())
	}
	return ""
}

// poolOnOff renders a boolean the way the panel's other toggles do.
func poolOnOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// poolDurationText renders a duration, falling back to the default when
// the file carries none.
func poolDurationText(raw string, fallback time.Duration) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback.String()
	}
	return raw
}

// poolAllowListLines renders the allow-list submenu.
func (m *runMode) poolAllowListLines() []string {
	lines := []string{"  " + panelGray238 + "Accounts in the pool" + panelGray238C, ""}
	allow := m.panel.acct.pool.policy.Accounts
	all := m.poolAllowed(nil, allow)
	lines = append(lines, m.poolAllowRow("(all accounts)", all, m.panel.acct.pool.allowCursor == 0))
	cursor := 1
	for _, acc := range m.poolAccountEmails() {
		lines = append(lines, m.poolAllowRow(acc, m.poolAllowed(&acc, allow), m.panel.acct.pool.allowCursor == cursor))
		cursor++
	}
	return lines
}

// poolAllowRow renders one row of the allow-list submenu.
func (m *runMode) poolAllowRow(name string, allowed, cursor bool) string {
	mark := "[ ]"
	if allowed {
		mark = "[x]"
	}
	if cursor {
		return cBlueOpen + "> " + mark + " " + name + pickerKeyClose
	}
	return "  " + mark + " " + name
}

// poolAccountCount is how many accounts the registry knows about.
func (m *runMode) poolAccountCount() int {
	return len(m.poolAccountEmails())
}

// poolAccountEmails returns the registry's account addresses, sorted.
func (m *runMode) poolAccountEmails() []string {
	rows := m.panel.acct.rows
	emails := make([]string, 0, len(rows))
	for _, acc := range rows {
		email := strings.TrimSpace(acc.Email)
		if email == "" {
			continue
		}
		emails = append(emails, email)
	}
	accountpool.SortEmails(emails)
	return emails
}

// poolAllowed reports whether the allow-list puts the named account in
// the pool. A nil name means the "(all accounts)" row.
func (m *runMode) poolAllowed(email *string, allow []string) bool {
	if email == nil {
		return len(allow) == 0
	}
	for _, a := range allow {
		if strings.EqualFold(a, *email) {
			return true
		}
	}
	return false
}

// ---- keys ---------------------------------------------------------------

// handlePoolPanelKey owns every key the Pool tab receives.
//
// The tab has two modes: the field list and the allow-list submenu.
// Both are handled here so the keys never fall through to the generic
// navigation handler — the arrow keys change a value, they do not move
// a cursor.
func (m *runMode) handlePoolPanelKey(key tea.Key) (bool, tea.Cmd) {
	pool := &m.panel.acct.pool
	if pool.editing {
		return m.handlePoolEditKey(key)
	}
	if pool.allowOpen {
		return m.handlePoolAllowKey(key)
	}

	switch key.String() {
	case "esc":
		return true, m.closePanel()
	case "tab":
		m.switchAccountsTab(accountsTabAccounts)
		return true, nil
	case "shift+tab":
		m.switchAccountsTab(accountsTabAccounts)
		return true, nil
	case "up":
		m.poolMoveCursor(-1)
		return true, nil
	case "down":
		m.poolMoveCursor(+1)
		return true, nil
	case "left":
		m.poolChange(-1)
		return true, nil
	case "right":
		m.poolChange(+1)
		return true, nil
	case "enter":
		return true, m.poolActivateRow()
	case "space":
		m.poolChange(+1)
		return true, nil
	}
	return true, nil
}

// handlePoolEditKey owns every key the row being typed into receives.
func (m *runMode) handlePoolEditKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		m.panel.acct.pool.editing = false
		return true, nil
	case tea.KeyEnter:
		return true, m.poolCommitEdit()
	}
	updated, cmd := m.panel.textInput.Update(keyMsgFromKey(key))
	m.panel.textInput = updated
	return true, cmd
}

// handlePoolAllowKey owns every key the allow-list submenu receives.
func (m *runMode) handlePoolAllowKey(key tea.Key) (bool, tea.Cmd) {
	emails := m.poolAccountEmails()
	switch key.String() {
	case "esc":
		m.panel.acct.pool.allowOpen = false
		m.panel.acct.pool.allowCursor = 0
		return true, nil
	case "up":
		if m.panel.acct.pool.allowCursor > 0 {
			m.panel.acct.pool.allowCursor--
		}
		return true, nil
	case "down":
		if m.panel.acct.pool.allowCursor < len(emails) {
			m.panel.acct.pool.allowCursor++
		}
		return true, nil
	case "space", "enter":
		m.poolToggleAllow()
		return true, nil
	}
	return true, nil
}

// poolMoveCursor moves the field cursor by delta rows.
func (m *runMode) poolMoveCursor(delta int) {
	c := m.panel.cursor + delta
	if c < 0 {
		c = 0
	}
	if c >= poolRowCount {
		c = poolRowCount - 1
	}
	m.panel.cursor = c
}

// poolActivateRow acts on the row under the cursor.
//
// A free-text row opens the input; a toggle row flips; the allow-list
// row opens the submenu.
func (m *runMode) poolActivateRow() tea.Cmd {
	switch m.panel.cursor {
	case poolRowThreshold, poolRowRefresh, poolRowCooldown, poolRowAttempts:
		m.openPoolEdit()
		return nil
	case poolRowAccounts:
		m.panel.acct.pool.allowOpen = true
		m.panel.acct.pool.allowCursor = 0
		return nil
	}
	m.poolChange(+1)
	return nil
}

// openPoolEdit opens the input on the row under the cursor.
func (m *runMode) openPoolEdit() {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = poolInputLimit
	in.SetWidth(poolInputWidth)
	in.Focus()
	m.panel.textInput = in
	m.panel.acct.pool.editing = true
	m.panel.acct.pool.editRow = m.panel.cursor
}

// poolCommitEdit writes the typed value back into the policy.
//
// A value that does not parse is refused with a notice and the row
// stays open: the panel must not write a policy it could not read back.
func (m *runMode) poolCommitEdit() tea.Cmd {
	raw := strings.TrimSpace(m.panel.textInput.Value())
	switch m.panel.acct.pool.editRow {
	case poolRowThreshold:
		value, err := parsePoolThreshold(raw)
		if err != nil {
			m.panel.acct.pool.notice = err.Error()
			return nil
		}
		m.panel.acct.pool.policy.Threshold = value
	case poolRowRefresh:
		d, err := parsePoolDuration(raw)
		if err != nil {
			m.panel.acct.pool.notice = err.Error()
			return nil
		}
		m.panel.acct.pool.policy.RefreshInterval = d
	case poolRowCooldown:
		d, err := parsePoolDuration(raw)
		if err != nil {
			m.panel.acct.pool.notice = err.Error()
			return nil
		}
		m.panel.acct.pool.policy.Cooldown = d
	case poolRowAttempts:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			m.panel.acct.pool.notice = "the attempt count must be a number of at least 1"
			return nil
		}
		m.panel.acct.pool.policy.MaxAttempts = n
	}
	m.panel.acct.pool.editing = false
	return m.savePoolPolicy()
}

// poolChange changes the value of the row under the cursor by one step.
//
// The direction is a step, not a choice: a toggle flips, a strategy
// walks its list, a number moves by one step.
func (m *runMode) poolChange(delta int) {
	pool := &m.panel.acct.pool
	switch m.panel.cursor {
	case poolRowEnabled:
		pool.policy.Enabled = !pool.policy.Enabled
	case poolRowStrategy:
		pool.policy.Strategy = poolNextStrategy(pool.policy.Strategy, delta)
	case poolRowThreshold:
		pool.policy.Threshold = clampPoolThreshold(pool.policy.Threshold + float64(delta)*0.05)
	case poolRowRefresh:
		pool.policy.RefreshInterval = poolNextDuration(pool.policy.RefreshInterval, accountpool.DefaultRefreshInterval, delta)
	case poolRowCooldown:
		pool.policy.Cooldown = poolNextDuration(pool.policy.Cooldown, accountpool.DefaultCooldown, delta)
	case poolRowAttempts:
		n := pool.policy.MaxAttempts + delta
		if n < 1 {
			n = 1
		}
		pool.policy.MaxAttempts = n
	case poolRowSwitchOnQuota:
		on := !pool.policy.SwitchOnQuotaErrorEnabled()
		pool.policy.SwitchOnQuotaError = &on
	default:
		return
	}
	m.savePoolPolicy()
}

// poolToggleAllow turns the account under the submenu's cursor in or
// out of the pool.
//
// The "(all accounts)" row clears the allow-list: an empty list is what
// the policy reads as "every account".
func (m *runMode) poolToggleAllow() {
	pool := &m.panel.acct.pool
	emails := m.poolAccountEmails()
	cursor := pool.allowCursor
	if cursor == 0 {
		pool.policy.Accounts = nil
		m.savePoolPolicy()
		return
	}
	if cursor > len(emails) {
		return
	}
	email := emails[cursor-1]
	allow := pool.policy.Accounts
	next := make([]string, 0, len(allow)+1)
	found := false
	for _, a := range allow {
		if strings.EqualFold(a, email) {
			found = true
			continue
		}
		next = append(next, a)
	}
	if !found {
		next = append(next, email)
	}
	accountpool.SortEmails(next)
	pool.policy.Accounts = next
	m.savePoolPolicy()
}

// savePoolPolicy writes the policy out.
//
// The write goes through the file's own writer, so the section is the
// one the routing reads — and the routing reads it per turn, so the
// next turn runs on the policy that was just written. There is nothing
// to rebuild and nothing to confirm.
func (m *runMode) savePoolPolicy() tea.Cmd {
	policy := m.panel.acct.pool.policy
	if err := endpoints.SaveAccountPool(&policy); err != nil {
		m.panel.acct.pool.notice = "error: " + err.Error()
		return nil
	}
	m.panel.acct.pool.present = true
	m.panel.acct.pool.notice = "saved — the next turn runs on it"
	return nil
}

// ---- helpers ------------------------------------------------------------

// The input's own limits.
const (
	poolInputLimit = 32
	poolInputWidth = 24
)

// poolNextStrategy walks the strategy list.
func poolNextStrategy(current accountpool.Strategy, delta int) accountpool.Strategy {
	list := accountpool.Strategies()
	idx := 0
	for i, s := range list {
		if s == current {
			idx = i
			break
		}
	}
	idx = (idx + delta + len(list)) % len(list)
	return list[idx]
}

// poolNextDuration walks the duration ladder.
func poolNextDuration(current string, fallback time.Duration, delta int) string {
	value := strings.TrimSpace(current)
	if value == "" {
		value = fallback.String()
	}
	idx := -1
	for i, d := range poolDurationLadder {
		if d == value {
			idx = i
			break
		}
	}
	if idx < 0 {
		// A duration the user typed is not on the ladder: start the
		// walk at the ladder entry that is nearest to it.
		parsed, err := time.ParseDuration(value)
		if err != nil {
			parsed = fallback
		}
		idx = poolNearestLadder(parsed)
	}
	idx = (idx + delta + len(poolDurationLadder)) % len(poolDurationLadder)
	return poolDurationLadder[idx]
}

// poolNearestLadder is the index of the ladder entry nearest to d.
func poolNearestLadder(d time.Duration) int {
	best := 0
	bestDelta := time.Duration(1<<63 - 1)
	for i, entry := range poolDurationLadder {
		parsed, err := time.ParseDuration(entry)
		if err != nil {
			continue
		}
		delta := parsed - d
		if delta < 0 {
			delta = -delta
		}
		if delta < bestDelta {
			best = i
			bestDelta = delta
		}
	}
	return best
}

// parsePoolThreshold reads a threshold.
//
// The policy stores a fraction, but a user thinks in percent, so both
// "15" and "15%" and "0.15" are accepted.
func parsePoolThreshold(raw string) (float64, error) {
	raw = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(raw), "%"))
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, fmt.Errorf("the threshold must be a number")
	}
	if value > 1 {
		// A number above one is a percentage.
		value /= 100
	}
	return clampPoolThreshold(value), nil
}

// clampPoolThreshold keeps the threshold inside 0..1.
func clampPoolThreshold(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

// parsePoolDuration reads a duration and renders it the way the policy
// stores it.
func parsePoolDuration(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("the duration must not be empty")
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		return "", fmt.Errorf("the duration must be a positive time (30s, 2m, 1h)")
	}
	return d.String(), nil
}
