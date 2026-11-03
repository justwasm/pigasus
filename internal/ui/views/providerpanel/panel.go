// panel.go — the /provider panel's interactive half.
//
// The package's other files hold the *decisions* (which rows match, what a
// form's values mean); this file and its companions hold the interaction:
// the panel's state, its rendering and its keys.
//
// The host keeps only the seams: what the panel cannot answer itself —
// the browser, the clipboard, the session's current model, and the way
// out of the panel.

package providerpanel

import (
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/models"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/palette"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// OpenBrowser opens a URL in the user's browser.
	OpenBrowser func(url string) error
	// ClipboardRead reads the clipboard's text.
	ClipboardRead func() string
	// ClipboardWrite writes text to the clipboard.
	ClipboardWrite func(text string) error
	// CurrentModel is the label of the model the session is running.
	CurrentModel func() string
	// Close closes the panel.
	Close func() tea.Cmd
}

// Panel is the /provider panel: its state, its rendering and its keys.
//
// The panel owns every key it receives — the list mode and the form mode
// alike — so the host never has to route a key into it and never has to
// know which mode it is in.
type Panel struct {
	seams Seams

	// tab is the active view tab: Configured, Presets, or All.
	tab int
	// query is the active search filter string.
	query string
	// searchActive is true when the search input has focus.
	searchActive bool

	// providers is the provider section as it was last read.
	providers map[string]models.ProviderConfig
	// rows is the rendered list: one row per provider, one per model it
	// serves, and the built-in group at the end.
	rows []Row
	// notice is the last write's result, or the reason it failed.
	notice string
	// confirm is the index of the row a pending delete targets. `d` twice
	// on the same row carries the delete out; any other key clears it.
	confirm int
	// form is the edit form. nil means the list is showing.
	form *Form
	// verifying is true while a verification probe is in flight.
	verifying bool
	// verify is the outcome of the last probe.
	verify *models.VerifyResult
	// verifyLabel names what was probed ("provider gateway").
	verifyLabel string

	// collapsed tracks which provider IDs or group headers are collapsed.
	// Defaults to expanded for all configured providers and groups.
	collapsed map[string]bool

	// oauth holds the active browser OAuth flow if running.
	oauth *OAuthState

	// cachedTab, cachedQuery, cachedRows cache FilterRows results across frames.
	cachedTab   int
	cachedQuery string
	cachedRows  []Row
	cacheValid  bool

	// cursor is the selected row of the list.
	cursor int
	// window is the index of the first visible row.
	window int

	// input is the form's text field.
	input textinput.Model
}

// New builds the panel.
//
// The panel is empty until Reload reads the configuration file: the rows
// are re-read on every open and after every write, so the panel shows
// what is on disk and not what was on disk when it was last used.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Reload re-reads the configuration and rebuilds the rows.
//
// The panel reads the file on every open and after every write: the file
// is shared with the CLI and the wrapper, so the panel must not hold a
// stale copy of it.
func (p *Panel) Reload() {
	if p == nil {
		return
	}
	providers, err := models.ProviderEditor().List()
	if err != nil {
		p.providers = nil
		p.rows = nil
		p.notice = err.Error()
		return
	}
	p.providers = providers
	p.rows = BuildRows(providers)
	if p.cursor >= len(p.rows) {
		p.cursor = 0
	}
	p.confirm = -1
	p.cacheValid = false
}

// Rows returns the filtered rows currently on screen, based on the active
// tab and the search query.
func (p *Panel) Rows() []Row {
	if p == nil {
		return nil
	}
	if p.collapsed == nil {
		p.collapsed = map[string]bool{}
	}
	if p.cacheValid && p.cachedTab == p.tab && p.cachedQuery == p.query {
		return p.cachedRows
	}
	p.cachedRows = FilterRowsWithCollapse(p.rows, p.providers, TabKind(p.tab), p.query, p.collapsed)
	p.cachedTab = p.tab
	p.cachedQuery = p.query
	p.cacheValid = true
	return p.cachedRows
}

// RowCount is the number of rows the list holds.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	return len(p.rows)
}

// CurrentRow returns the row the cursor points at in Rows().
//
// If the cursor is within the unfiltered rows (e.g. in test setup), it
// falls back to them.
func (p *Panel) CurrentRow() (Row, bool) {
	if p == nil {
		return Row{}, false
	}
	rows := p.Rows()
	if p.cursor >= 0 && p.cursor < len(rows) {
		return rows[p.cursor], true
	}
	if p.cursor >= 0 && p.cursor < len(p.rows) {
		return p.rows[p.cursor], true
	}
	return Row{}, false
}

// Notice sets the panel's notice line.
func (p *Panel) Notice(msg string) {
	if p == nil {
		return
	}
	p.notice = msg
}

// TabBar renders the tab bar modeled after /resume and /accounts:
// Configured, Presets, and All.
func (p *Panel) TabBar() string {
	if p == nil {
		return ""
	}
	var b strings.Builder
	writeTab := func(name string, active bool) {
		if active {
			b.WriteString("\x1b[1;4m" + palette.BlueOpen + name + keyClose)
			return
		}
		b.WriteString(palette.DimGrayOpen + name + keyClose)
	}
	b.WriteString("   ")
	writeTab("Configured", p.tab == int(TabConfigured))
	b.WriteString("    ")
	writeTab("Presets", p.tab == int(TabPresets))
	b.WriteString("    ")
	writeTab("All", p.tab == int(TabAll))
	b.WriteString(" " + palette.DimGrayOpen + "  (tab to cycle)" + keyClose)
	return b.String()
}

// VerifyLine renders the verification outcome: the probe that is in
// flight, or the last probe's answer.
//
// The answer is the panel's feedback on the credential — "the credential
// was rejected" and "the endpoint could not be reached" call for different
// fixes, so the probe's wording names the difference.
func (p *Panel) VerifyLine() string {
	if p == nil {
		return ""
	}
	if p.verifying {
		return palette.SubtleOpen + "verifying " + p.verifyLabel + "…" + panelGray238C
	}
	result := p.verify
	if result == nil {
		return ""
	}
	mark := verifyFailOpen + "✗" + verifyFailClose
	if result.OK {
		mark = verifyOKOpen + "✓" + verifyOKClose
	}
	detail := result.Detail
	if detail == "" {
		detail = "no answer"
	}
	label := p.verifyLabel
	if label == "" {
		return mark + " " + detail
	}
	return mark + " " + label + ": " + detail
}

// SetVerifyResult records the outcome of a verification probe.
func (p *Panel) SetVerifyResult(result models.VerifyResult) {
	if p == nil {
		return
	}
	p.verifying = false
	p.verify = &result
}

// IsCurrentModel reports whether modelID matches the session's active model.
func (p *Panel) IsCurrentModel(modelID string) bool {
	if p == nil || modelID == "" {
		return false
	}
	cur := ""
	if p.seams.CurrentModel != nil {
		cur = strings.TrimSpace(p.seams.CurrentModel())
	}
	if cur == "" {
		return false
	}
	if strings.EqualFold(modelID, cur) {
		return true
	}
	if slug := modelpicker.ResolveCatalogueID(cur); slug != "" && strings.EqualFold(modelID, slug) {
		return true
	}
	if base := modelpicker.BaseSlug(cur); base != "" && strings.EqualFold(modelID, base) {
		return true
	}
	return false
}

// visibleRowsFor returns how many /provider rows the panel draws at this
// terminal height, aligned with /config's golden proportions.
//
// Like /config (configpanel.VisibleRowsFor), this bounds the visible rows
// so that the list has generous room without ever blowing up to completely
// occupy the whole window on large item collections (presets/all), keeping
// the transcript visible and docking footer hints gracefully.
func visibleRowsFor(height, total int) int {
	if height <= 0 {
		return total
	}
	n := height*3/4 - 10
	if n > 10 {
		n = 10
	}
	if n < 4 {
		n = 4
	}
	if n > total {
		n = total
	}
	return n
}

// adjustWindow keeps the visible window around the cursor.
func (p *Panel) adjustWindow(cursor, visible, total int) {
	if p.window < 0 {
		p.window = 0
	}
	if cursor < p.window {
		p.window = cursor
	}
	if cursor >= p.window+visible {
		p.window = cursor - visible + 1
	}
	if p.window > total-visible {
		p.window = total - visible
	}
	if p.window < 0 {
		p.window = 0
	}
}

// moveCursor moves the list cursor by delta rows.
func (p *Panel) moveCursor(delta int) {
	if p == nil {
		return
	}
	rows := p.Rows()
	n := len(rows)
	if n == 0 {
		return
	}
	nav := tuiviewport.ListNav{Cursor: p.cursor}
	if delta < 0 {
		nav.MoveUp(-delta, n, 0)
	} else {
		nav.MoveDown(delta, n, 0)
	}
	if nav.Cursor != p.cursor {
		p.confirm = -1
	}
	p.cursor = nav.Cursor
}
