// pool.go — the /accounts panel's second tab: the account-routing policy.
//
// # What it is
//
// The account-routing policy (the "account_pool" section of
// ~/.openagy/config.json) had two homes before this file: the
// configuration file, and the doc. The file is the policy's home, but a
// file the user has to edit by hand is a policy they forget about. This
// tab gives the policy a front end: the second tab of /accounts, next to
// the account list.
//
// The rows are the policy's own fields:
//
//	Enabled            on / off
//	Strategy           sticky · round_robin · quota_aware · failover · aggregate
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
// the file's reader is in. The tab holds no configuration logic of its
// own: it builds an accountpool.Config and hands it over, and the
// writer rewrites the one section that changed and leaves the rest of
// the document alone.

package accountspanel

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/accountpool"
	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// poolState is the Pool tab's state.
//
// The policy is re-read from the configuration file every time the tab
// is shown, so the panel shows what is on disk and not what was on disk
// when the panel was last used — the file is shared with the CLI and
// the wrapper.
type poolState struct {
	// policy is the section the file carries, with its defaults filled
	// in.
	policy accountpool.Config

	// present reports whether the file carries the section at all. A
	// file without one is "no routing", which is the same behaviour as a
	// policy that is switched off — but the panel says which one it is.
	present bool

	// notice is the tab's one-line status: the last write's answer.
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

// ReloadPolicy re-reads the policy from the configuration file.
//
// A file without the section is not an error: it is a pool that is
// switched off, and the panel shows the defaults with "off".
func (p *Panel) ReloadPolicy() {
	if p == nil {
		return
	}
	policy := endpoints.CurrentAccountPool()
	if policy == nil {
		p.pool.policy = accountpool.Config{}.Normalised()
		p.pool.present = false
		return
	}
	p.pool.policy = policy.Normalised()
	p.pool.present = true
}

// ---- rendering ----------------------------------------------------------

// poolLines renders the Pool tab.
func (p *Panel) poolLines() []string {
	lines := []string{titleOpen + "Account pool" + titleClose}
	lines = append(lines, p.tabBar())
	lines = append(lines, "")

	if p.pool.allowOpen {
		lines = append(lines, p.poolAllowListLines()...)
	} else {
		for i := 0; i < poolRowCount; i++ {
			lines = append(lines, p.poolRowLine(i, i == p.cursor))
		}
	}

	if notice := p.pool.notice; notice != "" {
		styled := "  " + palette.SubtleOpen + notice + "\x1b[39m"
		if strings.HasPrefix(strings.ToLower(notice), "error") || strings.Contains(strings.ToLower(notice), "must be") {
			styled = "  " + palette.WarningOpen + "⚠ " + palette.WarningClose +
				palette.SubtleOpen + notice + "\x1b[39m"
		} else if strings.HasPrefix(notice, "saved ") || strings.HasPrefix(notice, "Switched ") {
			styled = "  " + palette.AgentFgOpen + "✓ " + palette.ToolBulletClose +
				palette.SubtleOpen + notice + "\x1b[39m"
		}
		lines = append(lines, "", styled)
	}
	lines = append(lines, "", "  "+palette.SubtleOpen+endpoints.Path()+"\x1b[39m")
	if p.pool.allowOpen {
		lines = append(lines, tuilib.PanelKeyboardHint("↑/↓", "Navigate", "space", "Include", "esc", "Back"))
		return lines
	}
	lines = append(lines, tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Change", "enter", "Edit",
		"tab", "Accounts", "esc", "Exit"))
	return lines
}

// tabBar renders the tab bar: the /resume picker's shape, with the
// Accounts tab and the Pool tab.
//
// The active tab is bold, underlined and blue; the inactive one is the
// separator grey; the row ends with "(tab to cycle)", because that is
// the reference's own way of showing that a surface has more than one
// view.
func (p *Panel) tabBar() string {
	var b strings.Builder
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + palette.BlueOpen + name + keyClose)
			return
		}
		b.WriteString(palette.DimGrayOpen + name + keyClose)
	}
	b.WriteString("   ")
	writeTab("Accounts", p.tab != TabPool)
	b.WriteString("    ")
	writeTab("Pool", p.tab == TabPool)
	b.WriteString(" " + palette.DimGrayOpen + "  (tab to cycle)" + keyClose)
	return b.String()
}

// poolRowLine renders one row of the Pool tab.
//
// The value is rendered next to the name, and the row under the cursor
// is the one whose value the arrow keys change. A row being typed into
// shows what has been typed — the panel must not swallow the input.
func (p *Panel) poolRowLine(row int, cursor bool) string {
	name := poolRowLabels[row]
	value := p.poolRowValue(row)
	if p.pool.editing && p.pool.editRow == row {
		value = p.textInput.Value() + searchCursorBlock
	} else if cursor {
		switch row {
		case poolRowEnabled, poolRowSwitchOnQuota:
			value = "‹ " + value + " ›  " + palette.DimGrayOpen + "(space to toggle)" + palette.DimGrayClose
		case poolRowStrategy, poolRowRefresh, poolRowCooldown, poolRowAttempts:
			value = "‹ " + value + " ›  " + palette.DimGrayOpen + "(←/→ to cycle)" + palette.DimGrayClose
		case poolRowThreshold:
			value = value + "  " + palette.DimGrayOpen + "(enter to edit)" + palette.DimGrayClose
		case poolRowAccounts:
			value = value + "  " + palette.DimGrayOpen + "(enter to manage)" + palette.DimGrayClose
		}
	}
	pad := 24 - len(name)
	if pad < 1 {
		pad = 1
	}
	if cursor {
		return palette.BlueOpen + "> " + name + keyClose + strings.Repeat(" ", pad) + value
	}
	return "  " + name + strings.Repeat(" ", pad) + value
}

// poolRowValue renders the value of one row.
func (p *Panel) poolRowValue(row int) string {
	pol := p.pool.policy
	switch row {
	case poolRowEnabled:
		return poolOnOff(pol.Enabled)
	case poolRowStrategy:
		return string(pol.Strategy)
	case poolRowThreshold:
		return fmt.Sprintf("%.0f%%", pol.Threshold*100)
	case poolRowRefresh:
		return poolDurationText(pol.RefreshInterval, accountpool.DefaultRefreshInterval)
	case poolRowCooldown:
		return poolDurationText(pol.Cooldown, accountpool.DefaultCooldown)
	case poolRowAttempts:
		return strconv.Itoa(pol.MaxAttempts)
	case poolRowSwitchOnQuota:
		return poolOnOff(pol.SwitchOnQuotaErrorEnabled())
	case poolRowAccounts:
		if len(pol.Accounts) == 0 {
			return fmt.Sprintf("all (%d)", p.poolAccountCount())
		}
		return fmt.Sprintf("%d of %d", len(pol.Accounts), p.poolAccountCount())
	}
	return ""
}

// poolOnOff renders a boolean the way the panel's other toggles do.
func poolOnOff(on bool) string {
	if on {
		return palette.AgentFgOpen + "[ on ]" + palette.ToolBulletClose
	}
	return palette.DimGrayOpen + "[ off ]" + palette.DimGrayClose
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
func (p *Panel) poolAllowListLines() []string {
	lines := []string{"  " + palette.SubtleOpen + "Accounts in the pool" + "\x1b[39m", ""}
	allow := p.pool.policy.Accounts
	all := poolAllowed(nil, allow)
	lines = append(lines, p.poolAllowRow("(all accounts)", all, p.pool.allowCursor == 0))
	cursor := 1
	for _, acc := range p.poolAccountEmails() {
		lines = append(lines, p.poolAllowRow(acc, poolAllowed(&acc, allow), p.pool.allowCursor == cursor))
		cursor++
	}
	return lines
}

// poolAllowRow renders one row of the allow-list submenu.
func (p *Panel) poolAllowRow(name string, allowed, cursor bool) string {
	mark := "[ ]"
	if allowed {
		mark = palette.AgentFgOpen + "[x]" + palette.ToolBulletClose
	}
	if cursor {
		return palette.BlueOpen + "> " + keyClose + mark + " " + name
	}
	return "  " + mark + " " + name
}

// poolAccountCount is how many accounts the registry knows about.
func (p *Panel) poolAccountCount() int {
	return len(p.poolAccountEmails())
}

// poolAccountEmails returns the registry's account addresses, sorted.
func (p *Panel) poolAccountEmails() []string {
	rows := p.rows
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
func poolAllowed(email *string, allow []string) bool {
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

// poolMoveCursor moves the field cursor by delta rows.
func (p *Panel) poolMoveCursor(delta int) {
	c := p.cursor + delta
	if c < 0 {
		c = 0
	} else if c >= poolRowCount {
		c = poolRowCount - 1
	}
	p.cursor = c
}

// poolActivateRow acts on the row under the cursor.
//
// A free-text row opens the input; a toggle row flips; the allow-list
// row opens the submenu.
func (p *Panel) poolActivateRow() tea.Cmd {
	switch p.cursor {
	case poolRowThreshold, poolRowRefresh, poolRowCooldown, poolRowAttempts:
		p.openPoolEdit()
		return nil
	case poolRowAccounts:
		p.pool.allowOpen = true
		p.pool.allowCursor = 0
		return nil
	}
	p.poolChange(+1)
	return nil
}

// openPoolEdit opens the input on the row under the cursor.
func (p *Panel) openPoolEdit() {
	in := textinput.New()
	in.Prompt = ""
	in.CharLimit = poolInputLimit
	in.SetWidth(poolInputWidth)
	in.Focus()
	p.textInput = in
	p.pool.editing = true
	p.pool.editRow = p.cursor
}

// poolCommitEdit writes the typed value back into the policy.
//
// A value that does not parse is refused with a notice and the row
// stays open: the panel must not write a policy it could not read back.
func (p *Panel) poolCommitEdit() tea.Cmd {
	raw := strings.TrimSpace(p.textInput.Value())
	switch p.pool.editRow {
	case poolRowThreshold:
		value, err := parsePoolThreshold(raw)
		if err != nil {
			p.pool.notice = err.Error()
			return nil
		}
		p.pool.policy.Threshold = value
	case poolRowRefresh:
		d, err := parsePoolDuration(raw)
		if err != nil {
			p.pool.notice = err.Error()
			return nil
		}
		p.pool.policy.RefreshInterval = d
	case poolRowCooldown:
		d, err := parsePoolDuration(raw)
		if err != nil {
			p.pool.notice = err.Error()
			return nil
		}
		p.pool.policy.Cooldown = d
	case poolRowAttempts:
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 {
			p.pool.notice = "the attempt count must be a number of at least 1"
			return nil
		}
		p.pool.policy.MaxAttempts = n
	}
	p.pool.editing = false
	return p.savePoolPolicy()
}

// poolChange changes the value of the row under the cursor by one step.
//
// The direction is a step, not a choice: a toggle flips, a strategy
// walks its list, a number moves by one step.
func (p *Panel) poolChange(delta int) {
	switch p.cursor {
	case poolRowEnabled:
		p.pool.policy.Enabled = !p.pool.policy.Enabled
	case poolRowStrategy:
		p.pool.policy.Strategy = poolNextStrategy(p.pool.policy.Strategy, delta)
	case poolRowThreshold:
		p.pool.policy.Threshold = clampPoolThreshold(p.pool.policy.Threshold + float64(delta)*0.05)
	case poolRowRefresh:
		p.pool.policy.RefreshInterval = poolNextDuration(p.pool.policy.RefreshInterval, accountpool.DefaultRefreshInterval, delta)
	case poolRowCooldown:
		p.pool.policy.Cooldown = poolNextDuration(p.pool.policy.Cooldown, accountpool.DefaultCooldown, delta)
	case poolRowAttempts:
		n := p.pool.policy.MaxAttempts + delta
		if n < 1 {
			n = 1
		}
		p.pool.policy.MaxAttempts = n
	case poolRowSwitchOnQuota:
		on := !p.pool.policy.SwitchOnQuotaErrorEnabled()
		p.pool.policy.SwitchOnQuotaError = &on
	default:
		return
	}
	p.savePoolPolicy()
}

// poolToggleAllow turns the account under the submenu's cursor in or
// out of the pool.
//
// The "(all accounts)" row clears the allow-list: an empty list is what
// the policy reads as "every account".
func (p *Panel) poolToggleAllow() {
	emails := p.poolAccountEmails()
	cursor := p.pool.allowCursor
	if cursor <= 0 {
		p.pool.policy.Accounts = nil
		p.savePoolPolicy()
		return
	}
	if cursor > len(emails) {
		return
	}
	email := emails[cursor-1]
	allow := p.pool.policy.Accounts
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
	p.pool.policy.Accounts = next
	p.savePoolPolicy()
}

// savePoolPolicy writes the policy out.
//
// The write goes through the file's own writer, so the section is the
// one the routing reads — and the routing reads it per turn, so the
// next turn runs on the policy that was just written. There is nothing
// to rebuild and nothing to confirm.
func (p *Panel) savePoolPolicy() tea.Cmd {
	policy := p.pool.policy
	if err := endpoints.SaveAccountPool(&policy); err != nil {
		p.pool.notice = "error: " + err.Error()
		return nil
	}
	p.pool.present = true
	p.pool.notice = "saved — the next turn runs on it"
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
