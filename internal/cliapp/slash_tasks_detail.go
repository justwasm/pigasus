// slash_tasks_detail.go — cycle 315: the /tasks detail views.
//
// The reference opens a detail view when the user presses Enter on a
// row of the /tasks panel:
//
//   - model.TaskDetailModel     (cli/model/task_detail.go)     — a
//     background task (a shell the model ran in the background).
//   - model.SubagentDetailModel (cli/model/subagent_detail.go) — an
//     agent (subagent) task.
//
// 【反编译】 1.4.7 binary — see
// docs/cycles/cycle-315-tui-subagent-task-panels/deliverable.md.
//
// The two views are modal panels in the clone. The reference's models
// are full-screen bubbletea models, but every reference panel the
// clone implements is a modal overlay above the conversation, and a
// detail view is a panel like any other.
//
// The views share the clone's scroll machinery: the body's first
// visible row lives in slashPanel.window, the visible height comes
// from the terminal height, and the legend row names the keys the
// reference's keybinding scope binds:
//
//	【反编译】 model.TaskDetailModel.KeyboardHints binds KeyUp /
//	KeyDown, KeyLeft / KeyRight and KeyEscape in the "task_detail"
//	scope.
//
// Leaving the view with Esc goes back to the /tasks list — the
// reference's detail view is a sub-view of the panel, so it does not
// print the "Exited /tasks command" line. A second Esc leaves the
// /tasks panel itself, which does print it.
package cliapp

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/taskregistry"
)

// taskDetailFallbackRows is how many body rows a detail view shows
// when the terminal height is unknown (the unit-test path). It is the
// same fallback the /usage and /changelog viewers use.
const taskDetailFallbackRows = 17

// openTaskDetail opens the detail view for t.
//
// The reference has one detail model per row kind: a background task
// opens the task detail, an agent (subagent) task opens the subagent
// detail. The clone keeps that split — the two views render different
// fields.
//
// 【反编译】 model.TasksModel.updateList copies the selected row into
// the model's detail fields and returns a command that opens the
// detail view.
func (m *runMode) openTaskDetail(t *taskregistry.Task) {
	if t == nil {
		return
	}
	m.panel.taskDetailID = t.ID
	m.panel.window = 0
	if t.Kind == taskregistry.KindSubagent {
		m.panel.kind = panelSubagentDetail
		return
	}
	m.panel.kind = panelTaskDetail
}

// closeTaskDetail leaves the detail view and goes back to the /tasks
// list. The list keeps its cursor, so Esc lands the user back on the
// row they opened.
func (m *runMode) closeTaskDetail() tea.Cmd {
	m.panel.kind = panelTasks
	m.panel.window = 0
	return nil
}

// selectedTask returns the task the detail view is showing, or nil
// when the registry no longer knows it (the user removed the row with
// "x" while the detail view was open).
func (m *runMode) selectedTask() *taskregistry.Task {
	if m.taskReg == nil || m.panel.taskDetailID == "" {
		return nil
	}
	return m.taskReg.Get(m.panel.taskDetailID)
}

// panelTaskDetailLines renders the task-detail view.
func (m *runMode) panelTaskDetailLines() []string {
	t := m.selectedTask()
	header := []string{panelTitleOpen + taskregistry.TaskDetailTitle + panelTitleClose}
	return m.taskDetailView(header, taskregistry.TaskDetailBody(t))
}

// panelSubagentDetailLines renders the subagent-detail view.
//
// 【反编译】 the reference's subagent detail has no "Task Details"
// title — its header is the five lines
// model.SubagentDetailModel.buildHeaderLines renders, and the first
// of them ("main ▸ <name>") is the one the clone paints as the
// panel's title.
func (m *runMode) panelSubagentDetailLines() []string {
	t := m.selectedTask()
	header := taskregistry.SubagentDetailHeader(t, taskregistry.SubagentHeaderContext{
		Width: m.width,
	})
	if len(header) > 0 {
		header[0] = panelTitleOpen + header[0] + panelTitleClose
	}
	return m.taskDetailView(header, taskregistry.SubagentDetailBody(t))
}

// taskDetailView renders a detail view: a fixed header, a scrollable
// body window, the line counter (when the body does not fit) and the
// legend row.
func (m *runMode) taskDetailView(header, body []string) []string {
	lines := append([]string{}, header...)
	visible := m.taskDetailVisibleRows(len(header))
	total := len(body)
	maxOff := total - visible
	if maxOff < 0 {
		maxOff = 0
	}
	if m.panel.window < 0 {
		m.panel.window = 0
	}
	if m.panel.window > maxOff {
		m.panel.window = maxOff
	}
	off := m.panel.window
	end := off + visible
	if end > total {
		end = total
	}
	lines = append(lines, body[off:end]...)
	if visible < total {
		lines = append(lines, viewerCounter(off+1, off+visible, total))
	}
	lines = append(lines, "",
		panelDotHint("  ", "↑/↓", "Scroll", "←/→", "Page", "esc", "Close"))
	return lines
}

// taskDetailVisibleRows is how many body rows a detail view shows.
//
// The header is part of the panel's chrome, so a deeper header (the
// subagent detail's is five lines, the task detail's one) leaves
// fewer rows for the body.
func (m *runMode) taskDetailVisibleRows(headerRows int) int {
	if headerRows < 1 {
		headerRows = 1
	}
	if m.height <= 0 {
		n := taskDetailFallbackRows - (headerRows - 1)
		if n < 1 {
			n = 1
		}
		return n
	}
	// The chrome is the header, the line counter, a blank row and the
	// legend.
	n := m.height - 6 - (headerRows - 1)
	if n < 1 {
		n = 1
	}
	return n
}

// handleTaskDetailKey owns every key while a detail view is open.
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
func (m *runMode) handleTaskDetailKey(key tea.Key) (bool, tea.Cmd) {
	switch key.Code {
	case tea.KeyEsc:
		return true, m.closeTaskDetail()
	case tea.KeyUp:
		m.taskDetailScroll(-1)
		return true, nil
	case tea.KeyDown:
		m.taskDetailScroll(1)
		return true, nil
	case tea.KeyLeft, tea.KeyPgUp:
		m.taskDetailScroll(-m.taskDetailVisibleRows(m.taskDetailHeaderRows()))
		return true, nil
	case tea.KeyRight, tea.KeyPgDown:
		m.taskDetailScroll(m.taskDetailVisibleRows(m.taskDetailHeaderRows()))
		return true, nil
	}
	return true, nil
}

// taskDetailScroll moves the body window by delta rows. The render
// clamps the window to the body's length.
func (m *runMode) taskDetailScroll(delta int) {
	m.panel.window += delta
	if m.panel.window < 0 {
		m.panel.window = 0
	}
}

// taskDetailHeaderRows is how many rows the open detail view's header
// takes. The task detail's header is its "Task Details" title; the
// subagent detail's is the five lines the reference's
// buildHeaderLines renders.
func (m *runMode) taskDetailHeaderRows() int {
	if m.panel.kind == panelSubagentDetail {
		return len(taskregistry.SubagentDetailHeader(m.selectedTask(),
			taskregistry.SubagentHeaderContext{Width: m.width}))
	}
	return 1
}
