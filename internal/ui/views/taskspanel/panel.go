// Package taskspanel holds the /tasks panel: the list of background
// tasks and the two detail views it opens.
//
// # What it is
//
// The reference (`agy`) offers a panel of the background work a session
// has running: the shells the model started in the background, the
// subagents it declared. Every row is a task; the panel's keys kill a
// row ("k"), remove it from the list ("x") and open the row's detail
// view (Enter).
//
// 【反编译】 the reference's "tasks" scope binds three action groups
// (model.TasksModel.KeyboardHints, 0x102bea9a0):
//
//	(Action "KeyUp", Action "KeyDown")
//	(Action "KeyLeft", Action "KeyRight")
//	(Scope "tasks", Action "KeyEnter")
//	(Scope "tasks", Action "KeyTaskKill")
//	(Scope "tasks", Action "KeyTaskRemove")
//
// so ↑/↓ move the cursor, ←/→ page it, Enter opens the detail view, "k"
// kills and "x" removes.
//
// # Where it lives
//
// The panel itself is here: the rows, the cursor, the two detail views
// and their scroll window, and the keys. The host keeps only the seams
// — the registry the rows come from, the kill the host owns, the
// terminal's size and the way out of the panel.
//
// The panel is rebuilt every time it opens, so a stale copy of the
// cursor or of the detail view is never shown.
package taskspanel

import (
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// Seams are the host's answers the panel cannot answer itself.
//
// Every one of them is optional: a panel built without a seam behaves as
// if the seam were answered with "nothing happened", which keeps the
// package usable from a test without a running TUI.
type Seams struct {
	// Registry returns the live task registry.
	//
	// The rows are read fresh on every draw, so a task that finished
	// while the panel is open shows up without the panel being rebuilt.
	Registry func() *taskregistry.Registry
	// Kill stops a running task.
	//
	// The kill belongs to the host: it owns the subagent manager the
	// registry's tasks are attached to.
	Kill func(t *taskregistry.Task)
	// Size returns the terminal's width and height in cells.
	//
	// The width lays the rows out (the status cell lands on the right
	// edge of the panel) and the height sizes the detail view's window.
	Size func() (width, height int)
	// Close leaves the panel.
	Close func() tea.Cmd
}

// Panel is the /tasks panel.
//
// The panel owns the whole surface: the task list, the cursor, the two
// detail views and their scroll window. Nothing of it is left in the
// host but the seams.
type Panel struct {
	seams Seams

	// rows is the panel's cached row list.
	//
	// The cache carries the "older hidden" count the panel shows below
	// the list, so it is rebuilt from the registry before every read.
	rows *taskregistry.PanelRows

	// cursor is the selected row's index in the rendered rows.
	cursor int

	// detail says a detail view is open.
	detail bool

	// detailID is the registry id of the row the detail view shows.
	detailID string

	// detailSubagent says the open detail view is the subagent one.
	detailSubagent bool

	// window is the detail view's scroll offset, in body rows.
	window int
}

// New builds the /tasks panel.
//
// The panel opens with the first row under the cursor, so the cursor
// starts at 0.
func New(seams Seams) *Panel {
	return &Panel{seams: seams, rows: taskregistry.NewPanelRows()}
}

// ---- the panel's state, read from outside ------------------------------

// Cursor is the selected row's index in the rendered rows.
func (p *Panel) Cursor() int {
	if p == nil {
		return 0
	}
	return p.cursor
}

// SetCursor puts the cursor on the row at the given index.
//
// The index is clamped into the rendered rows, so a stale index cannot
// strand the cursor on a row that is not on screen.
func (p *Panel) SetCursor(idx int) {
	if p == nil {
		return
	}
	n := p.RowCount()
	if idx < 0 {
		idx = 0
	}
	if n > 0 && idx >= n {
		idx = n - 1
	}
	p.cursor = idx
}

// Detail reports whether a detail view is open.
func (p *Panel) Detail() bool {
	if p == nil {
		return false
	}
	return p.detail
}

// DetailID is the registry id of the row the detail view shows.
func (p *Panel) DetailID() string {
	if p == nil {
		return ""
	}
	return p.detailID
}

// OpenTaskDetail opens the detail view directly for a specific task ID.
func (p *Panel) OpenTaskDetail(id string, isSubagent bool) {
	if p == nil {
		return
	}
	p.detail = true
	p.detailID = id
	p.detailSubagent = isSubagent
	p.window = 0
}

// RowCount is the number of rows the list renders.
//
// The count is the *rendered* row count: the registry's most recent
// tasks, capped at the panel's limit. The cursor indexes these rows.
func (p *Panel) RowCount() int {
	if p == nil {
		return 0
	}
	return len(p.snapshot())
}

// ---- the seams ---------------------------------------------------------

// registry is the live task registry.
func (p *Panel) registry() *taskregistry.Registry {
	if p == nil || p.seams.Registry == nil {
		return nil
	}
	return p.seams.Registry()
}

// kill stops a running task.
func (p *Panel) kill(t *taskregistry.Task) {
	if p == nil || p.seams.Kill == nil || t == nil {
		return
	}
	p.seams.Kill(t)
}

// size is the terminal's width and height in cells.
func (p *Panel) size() (int, int) {
	if p == nil || p.seams.Size == nil {
		return 0, 0
	}
	return p.seams.Size()
}

// close leaves the panel.
func (p *Panel) close() tea.Cmd {
	if p == nil || p.seams.Close == nil {
		return nil
	}
	return p.seams.Close()
}

// ---- the registry, read through the panel's cache ----------------------

// snapshot is the rendered row list: the registry's most recent tasks.
//
// The cache is rebuilt first, so a task that started or finished since
// the last draw is on screen.
func (p *Panel) snapshot() []*taskregistry.Task {
	if p == nil {
		return nil
	}
	if p.rows == nil {
		p.rows = taskregistry.NewPanelRows()
	}
	if reg := p.registry(); reg != nil {
		p.rows.Rebuild(reg)
	}
	return p.rows.Snapshot()
}

// filtered is how many older tasks the panel's limit hid.
func (p *Panel) filtered() int {
	if p == nil || p.rows == nil {
		return 0
	}
	return p.rows.Filtered()
}

// selected is the task the cursor is on.
//
// It is nil when the registry is empty or the cursor names no row.
func (p *Panel) selected() *taskregistry.Task {
	if p == nil {
		return nil
	}
	tasks := p.snapshot()
	if p.cursor < 0 || p.cursor >= len(tasks) {
		return nil
	}
	return tasks[p.cursor]
}

// detailTask is the task the open detail view shows.
//
// It is nil when the registry no longer knows the row — the user removed
// it with "x" while the detail view was open.
func (p *Panel) detailTask() *taskregistry.Task {
	if p == nil || p.detailID == "" {
		return nil
	}
	reg := p.registry()
	if reg == nil {
		return nil
	}
	return reg.Get(p.detailID)
}
