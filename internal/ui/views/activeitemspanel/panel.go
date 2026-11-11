// Package activeitemspanel is the active-items block: the inline list
// of the work a session has in flight, drawn under the prompt box.
//
// The panel owns the block: the cursor, the scroll window, the rows it
// writes and the keys it handles. The host that owns the prompt keeps
// the facts the panel cannot know — the task registry the rows come
// from, the terminal's size, whether vim mode is on and the way into
// the /tasks detail view — and hands them to the panel as seams.
//
// # What it is
//
// 【反编译】 the reference's model.ActiveItemsModel
// (google3/third_party/jetski/cli/model/model.ActiveItemsModel) draws
// the running and pending tasks and subagents under the input box:
//
//  1. Gated by the hideActiveItems setting: when "Show Active Tasks" is
//     switched off the block is not drawn at all.
//
//  2. The rows come from the task registry — the running and pending
//     records, in the order they started.
//
//  3. The cursor moves with ↑/↓, Esc gives the focus back to the
//     prompt, Enter opens the row's detail view in /tasks, and any
//     other key gives the focus back to the prompt as well.
//
//  4. Every row is a single line:
//
//     > ● [15:04:05] <name> <status>
//     ● Agent(<name>)  <status> · 15s
//
// The "> " prefix marks the cursor's row, the "●" marker carries the
// state's colour, and the block shows at most five rows — fewer when
// the terminal is short — with a "↑ N more" / "↓ N more" tail for the
// rows that do not fit.
//
// # Where it lives
//
// The panel is here: the cursor, the scroll window, the rows and the
// keys. The row format is the app tier's — the registry's
// ActiveRowLine and ActiveSubagentRowLine draw a single row, and the
// registry's Active() answers which records are in flight.
//
// The host keeps the seams and the routing: it asks the panel for the
// rows when it paints a frame, it asks the panel before it routes a
// key the panel owns, and it asks the panel whether the block is worth
// showing when the prompt's ↓ hands the focus over.
package activeitemspanel

import (
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	settings "github.com/justwasm/pigasus/internal/core/settings"
)

// Seams are what the panel needs from the host that owns it.
//
// A nil seam reads as its zero value: the panel sees an empty
// registry, a zero width and no vim mode. The panel never reaches
// into the host.
type Seams struct {
	// Registry returns the live task registry.
	//
	// The rows are read fresh on every draw, so a task that started
	// or finished since the last frame shows up without the panel
	// being rebuilt.
	Registry func() *taskregistry.Registry
	// Width returns the terminal's width in cells.
	//
	// The width lays a row out: the name is cut so that the marker,
	// the time cell and the status cell still fit.
	Width func() int
	// Height returns the terminal's height in cells.
	//
	// The height sizes the window: the block shows one row per ten
	// cells of height, at most five.
	Height func() int
	// VimEnabled reports whether vim mode is on.
	//
	// In vim mode "j" and "k" move the cursor, exactly as the
	// reference's own ActiveItemsModel does.
	VimEnabled func() bool
	// OpenDetail opens a task's detail view.
	//
	// The detail view belongs to the /tasks panel, so the host owns
	// the way into it.
	OpenDetail func(id string, isSubagent bool)

	// Swarm returns the active swarm item, if any.
	Swarm func() *SwarmItem
	// OpenSwarmBoard opens the swarm board view overlay.
	OpenSwarmBoard func()
	// DismissSwarm dismisses the active swarm item from the tray.
	DismissSwarm func()
}

// SwarmItem describes the swarm row displayed in the active-items tray.
type SwarmItem struct {
	Title     string
	Latest    string
	PostCount int
	IsDone    bool
	Dismissed bool
}

// TrayItem is an entry shown in the active-items tray: either a Task or a Swarm.
type TrayItem struct {
	Task  *taskregistry.Task
	Swarm *SwarmItem
}

// Panel is the active-items block.
//
// Every method is safe on a nil panel: a host that never built the
// block answers "nothing is running" and writes nothing.
type Panel struct {
	seams Seams

	// cursor is the selected row's index in the item list.
	cursor int
	// scroll is the index of the first row the block shows.
	scroll int
	// focused is true while the block owns the keyboard.
	focused bool
	// maxVisible is how many rows the block shows at once.
	maxVisible int
}

// defaultMaxVisible is how many rows the block shows when the
// terminal's height is unknown.
//
// 【反编译】 the reference's window is five rows tall.
const defaultMaxVisible = 5

// maxVisibleCeiling and maxVisibleFloor bound the window the
// terminal's height may ask for.
const (
	maxVisibleCeiling = 5
	maxVisibleFloor   = 1
)

// New builds the active-items block with the host's seams.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, maxVisible: defaultMaxVisible}
}

// ---- the panel's state, read from outside ------------------------------

// Focused reports whether the block owns the keyboard.
func (p *Panel) Focused() bool {
	return p != nil && p.focused
}

// HandleWheel scrolls the active items block when mouse wheel is used while focused.
func (p *Panel) HandleWheel(up bool) bool {
	if p == nil || !p.focused {
		return false
	}
	items := p.entries()
	total := len(items)
	if total == 0 {
		return false
	}
	if up {
		if p.cursor > 0 {
			p.cursor--
			p.adjustScroll(total, p.height())
		}
	} else {
		if p.cursor < total-1 {
			p.cursor++
			p.adjustScroll(total, p.height())
		}
	}
	return true
}

// SetFocus hands the keyboard to the block, or takes it back.
func (p *Panel) SetFocus(f bool) {
	if p == nil {
		return
	}
	p.focused = f
}

// SelectFirst puts the cursor on the first row.
func (p *Panel) SelectFirst() {
	if p == nil {
		return
	}
	p.cursor = 0
	p.scroll = 0
}

// Cursor is the selected row's index in the item list.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// ---- the panel's facts -------------------------------------------------

// HasActiveSwarm reports whether an active, non-dismissed swarm is present.
func (p *Panel) HasActiveSwarm() bool {
	if p == nil || p.seams.Swarm == nil {
		return false
	}
	s := p.seams.Swarm()
	return s != nil && !s.Dismissed
}

// DismissSwarm dismisses the active swarm item from the tray.
func (p *Panel) DismissSwarm() {
	if p == nil || p.seams.DismissSwarm == nil {
		return
	}
	p.seams.DismissSwarm()
}

// OpenSwarmBoard opens the swarm board overlay.
func (p *Panel) OpenSwarmBoard() {
	if p == nil || p.seams.OpenSwarmBoard == nil {
		return
	}
	p.seams.OpenSwarmBoard()
}

// Count is how many items are in flight.
//
// The count ignores the "Show Active Tasks" setting: it answers what
// the session has running, not whether the block is drawn.
func (p *Panel) Count() int {
	if p == nil {
		return 0
	}
	return len(p.entries())
}

// Clamp keeps the cursor and the scroll window inside the item list.
//
// The host calls it when the terminal is resized: a list that shrank
// must not leave the cursor on a row that is gone.
func (p *Panel) Clamp() {
	if p == nil {
		return
	}
	p.clamp(len(p.entries()), p.height())
}

// Hidden reports whether the block is switched off.
//
// The setting is the reference's own: "Show Active Tasks" maps to the
// hideActiveItems key, and the block is not drawn when it is on.
func (p *Panel) Hidden() bool {
	if p == nil {
		return false
	}
	raw, err := settings.Field("hideActiveItems")
	if err != nil || raw == nil {
		return false
	}
	b, ok := raw.(bool)
	return ok && b
}

// ---- the panel's internals ---------------------------------------------

// registry is the live task registry.
func (p *Panel) registry() *taskregistry.Registry {
	if p == nil || p.seams.Registry == nil {
		return nil
	}
	return p.seams.Registry()
}

// items are the records the block shows: the running and pending ones,
// in the order they started.
func (p *Panel) items() []*taskregistry.Task {
	reg := p.registry()
	if reg == nil {
		return nil
	}
	return reg.Active()
}

// entries returns all active tray items: swarm row (if present) followed by tasks.
func (p *Panel) entries() []TrayItem {
	var out []TrayItem
	if p.HasActiveSwarm() {
		out = append(out, TrayItem{Swarm: p.seams.Swarm()})
	}
	tasks := p.items()
	for _, t := range tasks {
		out = append(out, TrayItem{Task: t})
	}
	return out
}

// width is the terminal's width.
func (p *Panel) width() int {
	if p == nil || p.seams.Width == nil {
		return 0
	}
	return p.seams.Width()
}

// height is the terminal's height.
func (p *Panel) height() int {
	if p == nil || p.seams.Height == nil {
		return 0
	}
	return p.seams.Height()
}

// vimEnabled reports whether vim mode is on.
func (p *Panel) vimEnabled() bool {
	if p == nil || p.seams.VimEnabled == nil {
		return false
	}
	return p.seams.VimEnabled()
}

// openDetail opens a task's detail view.
func (p *Panel) openDetail(id string, isSubagent bool) {
	if p == nil || p.seams.OpenDetail == nil {
		return
	}
	p.seams.OpenDetail(id, isSubagent)
}

// clamp keeps the cursor and the scroll window inside the item list.
//
// An empty list unfocuses the block: there is nothing to select.
func (p *Panel) clamp(total, height int) {
	if p == nil {
		return
	}
	if total == 0 {
		p.cursor = 0
		p.scroll = 0
		p.focused = false
		return
	}
	if p.cursor >= total {
		p.cursor = total - 1
	}
	if p.cursor < 0 {
		p.cursor = 0
	}
	if p.scroll >= total {
		p.scroll = total - 1
	}
	if p.scroll < 0 {
		p.scroll = 0
	}
	p.adjustScroll(total, height)
}

// adjustScroll moves the scroll window so that the cursor's row is
// inside it.
//
// The window is one row per ten cells of terminal height, at most five
// and at least one.
func (p *Panel) adjustScroll(total, height int) {
	if p == nil {
		return
	}
	maxVis := p.maxVisible
	if height > 0 {
		calc := height / 10
		if calc < maxVisibleFloor {
			calc = maxVisibleFloor
		}
		if calc > maxVisibleCeiling {
			calc = maxVisibleCeiling
		}
		maxVis = calc
		p.maxVisible = calc
	}
	if maxVis < maxVisibleFloor {
		maxVis = maxVisibleFloor
	}
	if maxVis > maxVisibleCeiling {
		maxVis = maxVisibleCeiling
	}
	if p.cursor < p.scroll {
		p.scroll = p.cursor
	} else if p.scroll+maxVis <= p.cursor {
		p.scroll = (p.cursor - maxVis) + 1
	}
}
