// panel_keys.go — the /tasks panel's keys.
//
// The panel owns every key it receives while it is open: the list owns
// the cursor, the kill and the remove, and the detail view owns the
// scroll window. A key the panel does not own is not handled, so the
// host's global handlers (Ctrl+C) still see it.
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
// so ↑/↓ move the cursor, ←/→ page it, Enter opens the detail view,
// "k" kills the row and "x" removes it from the list.
//
// The detail view is modal: it takes the pager's keys and swallows every
// other key, so nothing reaches the prompt while it is open.

package taskspanel

import (
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
)

// pageRows is how many rows a page key moves the cursor.
//
// The panel lists every row it has (up to the registry's 20-row limit),
// so a "page" is a jump of ten rows — the clone's own page size.
const pageRows = 10

// Update owns every key the /tasks panel receives in Elm unidirectional style.
// It returns (handled, cmd).
func (p *Panel) Update(key tea.Key) (bool, tea.Cmd) {
	return p.HandleKey(key)
}

// HandleKey owns every key the /tasks panel receives.
//
// It answers (handled, cmd). A key the panel does not own is *not*
// handled, so the host's global handlers still see it — that is how
// Ctrl+C leaves the panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}
	if p.detail {
		return p.handleDetailKey(key)
	}
	return p.handleListKey(key)
}

// handleListKey handles a key while the task list is up.
func (p *Panel) handleListKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEnter:
		// 【反编译】 the reference's "tasks" scope binds KeyEnter: it
		// opens the row's detail view. It does not kill — the clone
		// used to kill on Enter, which was the cycle-315 bug.
		t := p.selected()
		p.openDetail()
		if t != nil {
			return true, OpenDetailCmd(t)
		}
		return true, nil
	case tea.KeySpace:
		// The reference binds only KeyEnter in the /tasks scope. A
		// space is swallowed here so it never reaches the prompt.
		return true, nil
	case tea.KeyUp:
		p.moveCursor(false)
		return true, nil
	case tea.KeyDown:
		p.moveCursor(true)
		return true, nil
	case tea.KeyLeft, tea.KeyPgUp:
		p.pageCursor(-pageRows)
		return true, nil
	case tea.KeyRight, tea.KeyPgDown:
		p.pageCursor(pageRows)
		return true, nil
	case tea.KeyEsc:
		cmd := p.close()
		if cmd == nil {
			cmd = DismissCmd()
		}
		return true, cmd
	}
	if key.Mod == 0 {
		switch key.Code {
		case 'k':
			t := p.selected()
			p.killSelected()
			if t != nil {
				return true, KillTaskCmd(t)
			}
			return true, nil
		case 'x':
			t := p.selected()
			p.removeSelected()
			if t != nil {
				return true, RemoveTaskCmd(t)
			}
			return true, nil
		}
	}
	// Ctrl+C is the one key the panel does not swallow: it is the
	// global "leave" key.
	if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
		return false, nil
	}
	// Every other key is swallowed: the panel is modal, so a stray
	// letter never reaches the prompt.
	return true, nil
}

// handleDetailKey handles a key while a detail view is up.
//
// 【反编译】 the reference's task-detail keybinding scope binds three
// groups (model.TaskDetailModel.KeyboardHints, 0x102beab20):
//
//	(Action "KeyUp", Action "KeyDown")
//	(Action "KeyLeft", Action "KeyRight")
//	(Scope "task_detail", Action "KeyEscape")
//
// so ↑/↓ move a line, ←/→ move a page and Esc leaves the view. The
// detail view is modal, so every other key is swallowed too.
func (p *Panel) handleDetailKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		p.closeDetail()
		return true, nil
	case tea.KeyUp:
		p.scroll(-1)
		return true, nil
	case tea.KeyDown:
		p.scroll(1)
		return true, nil
	case tea.KeyLeft, tea.KeyPgUp:
		p.scroll(-p.detailVisibleRows(p.detailHeaderRows()))
		return true, nil
	case tea.KeyRight, tea.KeyPgDown:
		p.scroll(p.detailVisibleRows(p.detailHeaderRows()))
		return true, nil
	}
	return true, nil
}

// moveCursor moves the cursor one row up or down.
//
// The cursor is clamped: it never leaves the rendered rows, so a task
// that finished and disappeared cannot strand it.
func (p *Panel) moveCursor(down bool) {
	nav := tuiviewport.ListNav{Cursor: p.cursor}
	if down {
		nav.MoveDown(1, p.RowCount(), 0)
	} else {
		nav.MoveUp(1, p.RowCount(), 0)
	}
	p.cursor = nav.Cursor
}

// pageCursor moves the cursor by a page, clamped into the rows.
func (p *Panel) pageCursor(delta int) {
	nav := tuiviewport.ListNav{Cursor: p.cursor}
	if delta < 0 {
		nav.MoveUp(-delta, p.RowCount(), 0)
	} else {
		nav.MoveDown(delta, p.RowCount(), 0)
	}
	p.cursor = nav.Cursor
}

// openDetail opens the detail view for the row under the cursor.
//
// The reference has one detail model per row kind: a background task
// opens the task detail, an agent (subagent) task opens the subagent
// detail. A cursor that names no row opens nothing.
func (p *Panel) openDetail() {
	t := p.selected()
	if t == nil {
		return
	}
	p.detail = true
	p.detailID = t.ID
	p.detailSubagent = t.Kind == taskregistry.KindSubagent
	p.window = 0
}

// closeDetail leaves the detail view and goes back to the list. The
// list keeps its cursor, so the user lands on the row they opened.
func (p *Panel) closeDetail() {
	p.detail = false
	p.detailID = ""
	p.detailSubagent = false
	p.window = 0
}

// killSelected kills the row under the cursor.
//
// 【反编译】 the reference's KeyTaskKill binding stops a running task.
// A row that is not running is a no-op — no made-up exit code, per the
// 1.4.0 changelog line ("shown as `completed` or `failed` without a
// made-up exit code").
func (p *Panel) killSelected() {
	t := p.selected()
	if t == nil {
		return
	}
	if t.State != taskregistry.StateRunning && t.State != taskregistry.StatePending {
		return
	}
	p.kill(t)
}

// removeSelected removes the row under the cursor from the list.
//
// The reference's KeyTaskRemove binding hides the row: it stays in the
// registry, the panel simply stops showing it.
func (p *Panel) removeSelected() {
	t := p.selected()
	if t == nil {
		return
	}
	reg := p.registry()
	if reg == nil {
		return
	}
	reg.Hide(t.ID)
	// Clamp the cursor: the row the user just removed may have been the
	// last one.
	n := p.RowCount()
	if n == 0 {
		p.cursor = 0
		return
	}
	if p.cursor >= n {
		p.cursor = n - 1
	}
}

// scroll moves the detail view's body window by delta rows.
//
// The window is clamped by the render, so a scroll past the end of the
// body lands on its last page.
func (p *Panel) scroll(delta int) {
	p.window += delta
	if p.window < 0 {
		p.window = 0
	}
}
