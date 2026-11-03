// render.go — the panel's frame.
//
// The rows are drawn from the state the panel owns; the host has no say
// in what a row says.

package accountspanel

import (
	accounts "github.com/justwasm/pigasus/internal/features/accounts/core"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own glyphs.
//
// titleOpen / titleClose are the bold, default-fg title the panels use;
// keyClose resets the span a styled fragment opened; searchCursorBlock
// is the reverse-video cell the inline editor's cursor is drawn as.
const (
	titleOpen  = "\x1b[1m\x1b[39m"
	titleClose = "\x1b[0m"
	keyClose   = "\x1b[0m"

	searchCursorBlock = "\x1b[7m\x1b[37m \x1b[0m"
)

// Lines renders the panel.
//
// The two tabs share the title's shape and the tab bar, and nothing
// else: the account list is a list of accounts, and the Pool tab is the
// policy's own fields.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.tab == TabPool {
		return p.poolLines()
	}
	return p.accountsLines()
}

// accountsLines renders the account list.
func (p *Panel) accountsLines() []string {
	lines := []string{titleOpen + "Accounts" + titleClose}
	lines = append(lines, p.tabBar(), "")
	if len(p.rows) == 0 {
		lines = append(lines, "  "+palette.SubtleOpen+"No accounts. Press a to sign in."+"\x1b[39m")
	}
	for i, acc := range p.rows {
		lines = append(lines, p.accountRowLine(acc, i == p.cursor))
	}
	if p.notice != "" {
		lines = append(lines, "", noticeLine(p.notice))
	}
	status := p.status
	quotaErr := p.quotaErr
	if status != "" || quotaErr != "" {
		lines = append(lines, "")
		if status != "" {
			lines = append(lines, "  "+palette.SubtleOpen+status+"\x1b[39m")
		}
		if quotaErr != "" {
			lines = append(lines, "  "+palette.SubtleOpen+"quota: "+quotaErr+"\x1b[39m")
		}
	}
	lines = append(lines, "", "  "+palette.SubtleOpen+accounts.Path()+"\x1b[39m")
	lines = append(lines, accountsLegend())
	return lines
}

// RowCount is the cursor bound of the view that is showing.
//
// The Pool tab has a fixed number of fields; the account list has one
// row per account the registry knows about.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	if p.tab == TabPool {
		return poolRowCount
	}
	return len(p.rows)
}
