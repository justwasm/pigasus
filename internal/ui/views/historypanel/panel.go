// Package historypanel is the /history interactive prompt history picker:
// an overlay that lists past prompt inputs with live search filtering,
// scope toggling (current workspace vs all), multi-line preview, and
// selection into the composer.
package historypanel

import (
	"strings"

	tea "charm.land/bubbletea/v2"
)

// Scope controls whether the panel displays history for the current workspace or all workspaces.
type Scope int

const (
	ScopeWorkspace Scope = iota // Current workspace only
	ScopeAll                    // All workspaces
)

// Seams are what the history panel needs from the host.
type Seams struct {
	// Entries returns the raw history entries in chronological order (oldest first).
	Entries func() []Entry

	// CurrentWorkspace returns the working directory of the active session.
	CurrentWorkspace func() string

	// Height returns the terminal height for layout budget sizing.
	Height func() int

	// Pick sends the chosen prompt text back to the composer.
	Pick func(text string) tea.Cmd

	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the prompt history viewer and selector.
type Panel struct {
	seams  Seams
	scope  Scope
	query  string
	cursor int
	window int
}

// New builds a new prompt history panel.
func New(seams Seams) *Panel {
	return &Panel{
		seams: seams,
		scope: ScopeWorkspace,
	}
}

// Scope returns the active scope filter.
func (p *Panel) Scope() Scope {
	if p == nil {
		return ScopeWorkspace
	}
	return p.scope
}

// SetScope sets the active scope filter.
func (p *Panel) SetScope(s Scope) {
	if p != nil {
		p.scope = s
		p.cursor = 0
		p.window = 0
	}
}

// Query returns the search filter query.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// SetQuery sets the search filter query.
func (p *Panel) SetQuery(q string) {
	if p != nil {
		p.query = q
		p.cursor = 0
		p.window = 0
	}
}

// Cursor returns the active selection index.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor sets the selection index and clamps.
func (p *Panel) SetCursor(c int) {
	if p == nil {
		return
	}
	p.cursor = c
	p.clamp()
}

// RowCount returns the number of filtered entries matching.
func (p *Panel) RowCount() int {
	return len(p.filteredEntries())
}

func (p *Panel) height() int {
	if p == nil || p.seams.Height == nil {
		return 0
	}
	return p.seams.Height()
}

// filteredEntries returns history entries newest first, filtered by scope and query.
func (p *Panel) filteredEntries() []Entry {
	if p == nil || p.seams.Entries == nil {
		return nil
	}
	raw := p.seams.Entries()
	if len(raw) == 0 {
		return nil
	}

	curWS := ""
	if p.seams.CurrentWorkspace != nil {
		curWS = p.seams.CurrentWorkspace()
	}

	lowerQuery := strings.ToLower(strings.TrimSpace(p.query))

	var res []Entry
	seen := make(map[string]bool)

	// Traverse backwards so newest is first.
	for i := len(raw) - 1; i >= 0; i-- {
		e := raw[i]
		t := strings.TrimSpace(e.Text)
		if t == "" {
			continue
		}
		// Scope check
		if p.scope == ScopeWorkspace && curWS != "" && e.Workspace != "" && e.Workspace != curWS {
			continue
		}
		// Deduplicate identical prompt text
		if seen[t] {
			continue
		}
		seen[t] = true

		// Query check
		if lowerQuery != "" && !strings.Contains(strings.ToLower(t), lowerQuery) {
			continue
		}

		res = append(res, e)
	}

	return res
}

func (p *Panel) currentEntry() (Entry, bool) {
	entries := p.filteredEntries()
	if len(entries) == 0 || p.cursor < 0 || p.cursor >= len(entries) {
		return Entry{}, false
	}
	return entries[p.cursor], true
}

func (p *Panel) close() tea.Cmd {
	if p != nil && p.seams.Close != nil {
		return p.seams.Close()
	}
	return nil
}

// visible is the number of rows visible in the budget.
func (p *Panel) visible(total int) int {
	v := historyListHeight(p.height())
	if v > total {
		v = total
	}
	if v < 0 {
		v = 0
	}
	return v
}

// clamp keeps cursor and window inside the filtered entries list.
func (p *Panel) clamp() {
	if p == nil {
		return
	}
	total := p.RowCount()
	if total == 0 {
		p.cursor = 0
		p.window = 0
		return
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.cursor > total-1 {
		p.cursor = total - 1
	}
	visible := p.visible(total)
	if p.window < 0 {
		p.window = 0
	}
	if p.cursor < p.window {
		p.window = p.cursor
	}
	if p.cursor >= p.window+visible {
		p.window = p.cursor - visible + 1
	}
	if p.window > total-visible {
		p.window = total - visible
	}
	if p.window < 0 {
		p.window = 0
	}
}
