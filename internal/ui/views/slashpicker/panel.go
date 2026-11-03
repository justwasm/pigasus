// Package slashpicker is the slash-command completion popup: the
// overlay the composer raises above the prompt block when the buffer
// starts with "/".
//
// The panel owns the overlay's state — the filtered rows, the
// highlight, the scroll window, the on-screen rectangle — and the rows
// it writes. The host that owns the composer keeps the facts the
// panel cannot know (the terminal's width, whether the alternate
// screen is up, which screen row is the composer, what the composer
// holds) and hands them to the panel as seams.
//
// 【一手】 agy 1.4.6, live tmux capture: the popup renders five
// visible rows, the "↑ N more" / "↓ N more" tails, a blank separator
// and the "↑/↓ Navigate · enter Select · tab Complete" hint row.
//
// Lifecycle:
//
//	idle         the buffer does not start with "/"
//	picker-empty the buffer starts with "/" but the filter returns no
//	             row (the overlay stays visible and renders the
//	             "   No matches" row — cycle 93)
//	picker-shown the buffer starts with "/" and at least one row
//	             matches
//
// The behaviour matches the v1.3.5 binary's captures:
//
//	"/a"       -> popup open, scroll offset 0, /add-dir highlighted
//	"/add"     -> popup open, /add-dir highlighted (filtered subset)
//	"/xyz"     -> popup open but empty; renders "   No matches"
//	Up arrow   -> the previous row of the filtered subset
//	Down arrow -> the next row of the filtered subset
//	Tab        -> replaces the buffer with the selected command's name
//	Enter      -> submits the buffer (as text; the "/" is preserved)
//	Esc        -> closes the popup
//
// The popup state is held outside the buffer (no "/command \x00"
// sentinel in the composer): it is a derived view of the buffer and
// the highlight.
package slashpicker

import (
	"strings"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// ModelRow is one row of the "/model <query>" completion popup: the
// display label the reference shows ("Gemini 3.8 Flash (High)") and
// the catalogue id ("gemini-3.8-flash-high").
type ModelRow struct {
	Label string
	ID    string
}

// Seams are what the panel needs from the host that owns it.
//
// A nil seam reads as its zero value: the panel falls back to a
// hidden popup, a zero width, a composer that holds nothing. The
// panel never reaches into the host.
type Seams struct {
	// AltScreen reports whether the session runs in the alternate
	// screen. The mouse handlers answer false outside it.
	AltScreen func() bool
	// Width returns the terminal's width in columns.
	Width func() int
	// IsInputY reports whether a screen row belongs to the composer.
	IsInputY func(y int) bool
	// Buffer returns the composer's current text.
	Buffer func() string
	// SetBuffer replaces the composer's text.
	SetBuffer func(s string)
	// CustomCommands returns additional dynamic slash commands (e.g. from the active Agent backend).
	CustomCommands func() []slashcommands.Command
	// ScopeContext returns the active engine scope and capabilities for filtering commands.
	ScopeContext func() slashcommands.ScopeContext
}

// Panel is the slash-command completion popup.
//
// Every method is safe on a nil panel: a host that never built the
// popup answers "not visible" and writes nothing.
type Panel struct {
	seams Seams

	// visible is true while the overlay should be rendered. True when
	// the buffer starts with "/", regardless of whether the filter
	// returned 0 or more rows (cycle 93).
	visible bool
	// empty is true when visible but the row set is empty.
	empty bool
	// all is the filtered row set (recomputed when the buffer changes).
	all []slashcommands.Command
	// idx is the highlighted row. -1 when the row set is empty.
	idx int
	// hidden is a session-level set of slash command names to exclude
	// from the popup whatever the buffer holds. Cycle 264: agy 1.4.1
	// hides "/remote-control" when AGY_ADC_AUTH is in effect.
	hidden map[string]bool
	// modelMode says the popup holds the model list rather than the
	// command list — the buffer is "/model <query>".
	modelMode bool
	// modelSource returns the catalogue the model mode filters. It is
	// a function so the catalogue can change under the popup (the
	// user edited the provider configuration) without the caller
	// re-arming the popup on every keystroke.
	modelSource func() []ModelRow
	// query is the text after "/model " in the model mode.
	query string

	// customCommandsSource returns the dynamic slash commands (e.g. from an active agent backend).
	customCommandsSource func() []slashcommands.Command

	// offset is the index of the first row drawn — the scroll window.
	offset int
	// layout is the popup's internal structure, filled in by Rows().
	layout layout
	// geom is the popup's on-screen rectangle.
	geom geom
	// bodyStart is the screen row the overlay starts on.
	bodyStart int
}

// New builds the popup with the host's seams.
func New(seams Seams) *Panel {
	return &Panel{seams: seams}
}

// Reset clears the popup's state. The host calls it whenever the
// buffer's slash prefix is removed or replaced with a non-slash
// character.
func (p *Panel) Reset() {
	if p == nil {
		return
	}
	p.visible = false
	p.empty = false
	p.all = nil
	p.idx = -1
	p.modelMode = false
	p.query = ""
}

// SetModelCatalogueSource installs the function that returns the rows
// the "/model <query>" popup offers. It is called on every
// re-derivation, so a catalogue that changes (the user edited the
// provider configuration) shows up on the next keystroke. A nil source
// leaves the model mode empty.
func (p *Panel) SetModelCatalogueSource(fn func() []ModelRow) {
	if p == nil {
		return
	}
	p.modelSource = fn
}

// SetCustomCommandsSource installs the function that returns dynamic slash commands
// offered by the active agent backend.
func (p *Panel) SetCustomCommandsSource(fn func() []slashcommands.Command) {
	if p == nil {
		return
	}
	p.customCommandsSource = fn
}

// IsModelMode reports whether the popup holds the model list rather
// than the command list.
func (p *Panel) IsModelMode() bool {
	return p != nil && p.modelMode
}

// Query returns the text after "/model " in the model mode. Empty in
// the command mode.
func (p *Panel) Query() string {
	if p == nil {
		return ""
	}
	return p.query
}

// SetHiddenCommands replaces the popup's hidden-name set. Empty or nil
// argument list clears the set. Names are normalised to start with
// "/" so callers can pass either "/remote-control" or
// "remote-control" interchangeably.
//
// Cycle 264: agy 1.4.1 hides the /remote-control command when ADC
// is in effect. The host calls this once at start-up; the popup
// consults the set during Update, so callers do not have to call it
// again on every keystroke.
func (p *Panel) SetHiddenCommands(names ...string) {
	if p == nil {
		return
	}
	if len(names) == 0 {
		p.hidden = nil
		return
	}
	set := make(map[string]bool, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		if !strings.HasPrefix(n, "/") {
			n = "/" + n
		}
		set[n] = true
	}
	p.hidden = set
}

// Update re-derives the popup's state from the current buffer. It
// reports whether the state changed (the caller should redraw).
//
// The rules, matching the v1.3.5 binary:
//
//   - the buffer does not start with "/" -> hidden
//   - the buffer starts with "/" but matches no row -> visible and
//     empty (cycle 93: the reference shows the "   No matches"
//     overlay for /xyz and friends)
//   - the buffer starts with "/" and matches -> visible and populated
//   - when the filtered count drops below idx+1, the highlight clamps
//     to the last valid row
//
// The "/model <query>" line is the one exception: it is not a command
// prefix but an *argument* being typed, and the reference swaps the
// command rows for the model rows (【一手】 agy 1.4.6, live).
func (p *Panel) Update(buffer []rune) bool {
	if p == nil {
		return false
	}
	prevVisible := p.visible
	prevLen := len(p.all)
	if query, ok := modelQuery(buffer); ok {
		p.modelMode = true
		p.query = query
		p.all = p.filterModels(query)
		p.visible = true
		p.clampSelection()
		return true
	}
	p.modelMode = false
	p.query = ""
	if len(buffer) == 0 || buffer[0] != '/' {
		p.Reset()
		return p.visible != prevVisible || len(p.all) != prevLen
	}
	tok := readToken(buffer)
	if len([]rune(tok)) < len(buffer) {
		p.Reset()
		return p.visible != prevVisible || len(p.all) != prevLen
	}
	p.all = p.filterVisibleCommands(tok)
	p.visible = true
	p.clampSelection()
	return true
}

// clampSelection keeps the highlight inside the filtered row set.
func (p *Panel) clampSelection() {
	if len(p.all) == 0 {
		p.empty = true
		p.idx = -1
		return
	}
	p.empty = false
	if p.idx < 0 {
		p.idx = 0
	} else if p.idx >= len(p.all) {
		p.idx = len(p.all) - 1
	}
}

// filterVisibleCommands returns the rows the buffer's token matches,
// minus the hidden names, merging in dynamic agent commands when available.
func (p *Panel) filterVisibleCommands(prefix string) []slashcommands.Command {
	var sctx slashcommands.ScopeContext
	if p.seams.ScopeContext != nil {
		sctx = p.seams.ScopeContext()
	}
	if p.customCommandsSource != nil {
		sctx.Dynamic = p.customCommandsSource()
	} else if p.seams.CustomCommands != nil {
		sctx.Dynamic = p.seams.CustomCommands()
	}
	cmds := slashcommands.FilterScoped(prefix, sctx)
	if len(p.hidden) == 0 || len(cmds) == 0 {
		return cmds
	}
	filtered := cmds[:0:0]
	for _, c := range cmds {
		if !p.hidden[c.Name] {
			filtered = append(filtered, c)
		}
	}
	return filtered
}

// filterModels returns the catalogue rows whose label or id contains
// the query, case-insensitively.
//
// The reference matches a *substring* of the display label or of the
// catalogue id — not a subsequence. 【一手】 agy 1.4.6, live: "onnet"
// matches "Claude Sonnet 4.6 (Thinking)" (both the label and the id
// contain it), "4.6" matches both Claude rows (the label), and "son"
// matches only the Sonnet row: "Claude Opus 4.6 (Thinking)" has no
// "son" substring, even though a subsequence matcher would accept it.
func (p *Panel) filterModels(query string) []slashcommands.Command {
	if p.modelSource == nil {
		return nil
	}
	rows := p.modelSource()
	q := strings.ToLower(query)
	out := make([]slashcommands.Command, 0, len(rows))
	for _, r := range rows {
		if q != "" &&
			!strings.Contains(strings.ToLower(r.Label), q) &&
			!strings.Contains(strings.ToLower(r.ID), q) {
			continue
		}
		out = append(out, slashcommands.Command{Name: r.Label, Description: r.ID})
	}
	return out
}

// Selected returns the highlighted row. nil when the popup is not
// visible.
func (p *Panel) Selected() *slashcommands.Command {
	if p == nil || !p.visible || p.idx < 0 || p.idx >= len(p.all) {
		return nil
	}
	return &p.all[p.idx]
}

// Visible reports whether the popup should be rendered.
func (p *Panel) Visible() bool {
	return p != nil && p.visible
}

// Empty reports whether the popup is visible but holds no filtered
// row (cycle 93). The renderer emits the "   No matches" row and the
// hint row instead of the popup's rows and the "↓ N more" tail.
// Always false when the popup is not visible.
func (p *Panel) Empty() bool {
	return p != nil && p.visible && len(p.all) == 0
}

// Len returns the number of filtered rows.
func (p *Panel) Len() int {
	if p == nil {
		return 0
	}
	return len(p.all)
}

// SelectedAt returns a pointer to the i-th row of the filtered list,
// or nil when i is out of range. Cycle 60 added this so the renderer
// can enumerate the visible rows without copying the slice.
func (p *Panel) SelectedAt(i int) *slashcommands.Command {
	if p == nil || i < 0 || i >= len(p.all) {
		return nil
	}
	return &p.all[i]
}

// Idx returns the highlighted row's index. -1 when the popup is
// hidden.
func (p *Panel) Idx() int {
	if p == nil {
		return -1
	}
	return p.idx
}

// MoveUp moves the highlight up by 1, clamping at 0. It reports
// whether the highlight actually changed.
func (p *Panel) MoveUp() bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx <= 0 {
		return false
	}
	p.idx--
	return true
}

// MoveDown moves the highlight down by 1, clamping at len(all)-1.
func (p *Panel) MoveDown() bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if p.idx >= len(p.all)-1 {
		return false
	}
	p.idx++
	return true
}

// SetIdx moves the highlight to the i-th row, clamping to the valid
// range. It reports whether the highlight actually changed. False when
// the popup is hidden or holds no row.
//
// Cycle 232 (the popup's mouse click and hover) drives SetIdx from
// the mouse handler: a click on row 3 or a hover over row 0 mirrors
// what an Up/Down arrow would do, but lands on a specific row
// instead of stepping.
func (p *Panel) SetIdx(i int) bool {
	if p == nil || !p.visible || len(p.all) == 0 {
		return false
	}
	if i < 0 {
		i = 0
	}
	if i >= len(p.all) {
		i = len(p.all) - 1
	}
	if p.idx == i {
		return false
	}
	p.idx = i
	return true
}

// readToken returns the leading slash-prefix token from buffer (up to
// the first whitespace, or the end of the buffer).
func readToken(buffer []rune) string {
	end := len(buffer)
	for i, r := range buffer {
		if r == ' ' || r == '\t' || r == '\n' {
			end = i
			break
		}
	}
	return string(buffer[:end])
}

// modelQuery reports whether the buffer is a "/model <query>" line and
// returns the query — the text after the command and its space.
//
// The reference enters the model mode as soon as the command is
// followed by a space, even with an empty query: "/model " lists every
// model (【一手】 agy 1.4.6, live). A bare "/model" is still a command
// prefix and shows the command rows.
func modelQuery(buffer []rune) (string, bool) {
	s := string(buffer)
	const prefix = "/model "
	if !strings.HasPrefix(s, prefix) {
		return "", false
	}
	return s[len(prefix):], true
}
