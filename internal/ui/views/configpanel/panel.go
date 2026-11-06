// panel.go — the /config settings picker's interactive half.
//
// The package's other file holds the *decisions* (the catalogue, the
// display rules, the wrap); this file and its companions hold the
// interaction: the panel's state, its rendering, its keys and its
// writes.
//
// The host keeps only the seams: what the panel cannot answer itself —
// the live theme, the alternate screen, the seasonal theme, the way out
// of the panel, and the transcript block the locked telemetry row
// answers with.

package configpanel

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// ApplyTheme repaints the whole interface in the named scheme.
	//
	// The panel previews a scheme while the user walks the Color Scheme
	// row's values, so the theme is not the panel's to apply: the host
	// owns the prompt's styles, the markdown renderer and the transcript.
	ApplyTheme func(scheme string) tea.Cmd
	// ApplyAltScreen switches the session between the terminal's
	// alternate screen buffer and the inline view.
	ApplyAltScreen func(stored any)
	// ApplyVerbosity notifies the host when the verbosity setting changes.
	ApplyVerbosity func(verbosity string)
	// RefreshSeasonal re-reads the seasonal theme after its row changed.
	RefreshSeasonal func()
	// Close leaves the panel.
	Close func() tea.Cmd
	// Respond writes a transcript block. The locked telemetry row
	// answers with one.
	Respond func(cmd, msg string)
	// SeasonalEvent is the seasonal event in season right now, or nil
	// when no event is running.
	//
	// The clock is the host's: the seasonal row is drawn only while the
	// theme is in season, and the host is what pins the calendar in
	// its tests.
	SeasonalEvent func() *seasonal.Event
	// OverrideFor reports whether the given row is overridden by a launch
	// flag or session setting, and returns the override description suffix
	// (e.g. "(overridden by --sandbox)").
	OverrideFor func(row Row) (overrideDisplay string, isOverridden bool)
}

// Panel is the /config settings picker.
//
// The panel owns the whole surface: the search box, the row list, the
// inline option submenu and the value the reference never persists.
// Nothing of it is left in the host but the seams.
type Panel struct {
	seams Seams

	// query is the search box's text.
	query string
	// cursor is the selected row.
	cursor int
	// window is the index of the first visible row.
	window int
	// submenu reports whether the inline option picker is open.
	submenu bool
	// submenuCursor is the selected value inside the submenu.
	submenuCursor int
	// submenuOrigVal is the value the submenu opened on, so Esc can put
	// the theme back.
	submenuOrigVal string
	// memory holds the values of the rows the reference never persists
	// (Enable Telemetry toggles without touching settings.json), keyed
	// by settings key.
	memory map[string]string
}

// New builds the /config panel.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the index of the selected row in Rows().
//
// The cursor is the panel's own: the host's generic list navigation does
// not touch it, because the panel owns every key it receives.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index of Rows().
//
// The index is clamped into the list, so a stale index cannot strand the
// cursor on a row that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	rows := p.Rows()
	if idx < 0 || (len(rows) > 0 && idx >= len(rows)) {
		idx = 0
	}
	p.cursor = idx
}

// Query is the search box's text.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// SubmenuOpen reports whether the inline option submenu is open.
func (p *Panel) SubmenuOpen() bool {
	if p == nil {
		return false
	}
	return p.submenu
}

// ---- the seams ---------------------------------------------------------

func (p *Panel) applyTheme(scheme string) tea.Cmd {
	if p == nil || p.seams.ApplyTheme == nil {
		return nil
	}
	return p.seams.ApplyTheme(scheme)
}

func (p *Panel) applyAltScreen(stored any) {
	if p == nil || p.seams.ApplyAltScreen == nil {
		return
	}
	p.seams.ApplyAltScreen(stored)
}

func (p *Panel) applyVerbosity(v string) {
	if p == nil || p.seams.ApplyVerbosity == nil {
		return
	}
	p.seams.ApplyVerbosity(v)
}

func (p *Panel) refreshSeasonal() {
	if p == nil || p.seams.RefreshSeasonal == nil {
		return
	}
	p.seams.RefreshSeasonal()
}

func (p *Panel) close() tea.Cmd {
	if p == nil {
		return nil
	}
	if p.seams.Close != nil {
		return p.seams.Close()
	}
	return DismissCmd()
}

func (p *Panel) respond(cmd, msg string) {
	if p == nil || p.seams.Respond == nil {
		return
	}
	p.seams.Respond(cmd, msg)
}

func (p *Panel) seasonalEvent() *seasonal.Event {
	if p == nil || p.seams.SeasonalEvent == nil {
		return nil
	}
	return p.seams.SeasonalEvent()
}
