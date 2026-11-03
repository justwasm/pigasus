// panel_render.go — the /tasks panel's rows.
//
// The frame is the reference's own: the title, one row per task, the
// "older hidden" note, a blank row and the keyboard legend.
//
//	Tasks
//	> ● [15:04:05] bash -c sleep 60                running
//	  ● [15:04:05] bash -c ls -la /tmp | head -50    running
//	  ● [15:04:05] bash -c echo done               completed
//
// 【反编译】 the row is "<prefix><marker> [<time>] <name><padding><status>":
// the prefix is "> " on the cursor's row and "  " on the others, the
// marker is the reference's "●" in the state's colour, the time cell is
// the task's start time in brackets, and the padding pushes the status
// cell to the right edge of the panel.
//
// The detail view is the reference's own too: a fixed header, a
// scrollable body window, the line counter and the legend.
//
// 【反编译】 model.TaskDetailModel.KeyboardHints binds KeyUp / KeyDown,
// KeyLeft / KeyRight and KeyEscape in the "task_detail" scope.

package taskspanel

import (
	"fmt"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	tuiviewer "github.com/justwasm/pigasus/internal/ui/foundations/viewer"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// The panel's own SGR spans.
//
// The palette's colours follow the selected colour scheme, so the
// scheme-dependent ones are read from the palette at draw time.
const (
	// panelTitleOpen is the reference's own title span: bold on the
	// default foreground.
	panelTitleOpen = "\x1b[1m\x1b[39m"
	// panelTitleClose closes the title's bold span.
	panelTitleClose = "\x1b[0m"
)

// taskDetailFallbackRows is how many body rows the detail view shows
// when the terminal height is unknown (the unit-test path). It is the
// same fallback the /usage and /changelog viewers use.
const taskDetailFallbackRows = 17

// Lines renders the panel: the task list, or the detail view when one is
// open.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}
	if p.detail {
		return p.detailLines()
	}
	return p.listLines()
}

// listLines renders the task list.
func (p *Panel) listLines() []string {
	lines := []string{
		panelTitleOpen + "Tasks" + panelTitleClose,
	}
	tasks := p.snapshot()
	if len(tasks) == 0 {
		lines = append(lines,
			"  "+palette.DimGrayOpen+"No background tasks."+palette.DimGrayClose)
	} else {
		width, _ := p.size()
		for i, t := range tasks {
			lines = append(lines, taskregistry.RowLine(t, i == p.cursor, width))
		}
		if filtered := p.filtered(); filtered > 0 {
			lines = append(lines,
				"  "+palette.DimGrayOpen+fmt.Sprintf("(%d older hidden — ref to see all)", filtered)+palette.DimGrayClose)
		}
	}
	lines = append(lines,
		"",
		tuilib.PanelKeyboardHint("↑/↓", "Navigate", "←/→", "Page", "enter", "View output",
			"k", "Kill Task", "x", "Remove Task From List"))
	return lines
}

// detailLines renders the open detail view.
//
// 【反编译】 the reference has one detail model per row kind: a
// background task opens the task detail, an agent (subagent) task opens
// the subagent detail.
//
// 【反编译】 the subagent detail has no "Task Details" title — its
// header is the five lines model.SubagentDetailModel.buildHeaderLines
// renders, and the first of them ("main ▸ <name>") is the one the clone
// paints as the panel's title.
func (p *Panel) detailLines() []string {
	t := p.detailTask()
	var header, body []string
	width, _ := p.size()
	if p.detailSubagent {
		header = taskregistry.SubagentDetailHeader(t, taskregistry.SubagentHeaderContext{
			Width: width,
		})
		if len(header) > 0 {
			header[0] = panelTitleOpen + header[0] + panelTitleClose
		}
		body = taskregistry.SubagentDetailBody(t, width)
	} else {
		header = []string{panelTitleOpen + taskregistry.TaskDetailTitle + panelTitleClose}
		body = taskregistry.TaskDetailBody(t, width)
	}
	return p.detailView(header, body)
}

// detailView renders a detail view: a fixed header, a scrollable body
// window, the line counter (when the body does not fit) and the legend
// row.
func (p *Panel) detailView(header, body []string) []string {
	lines := append([]string{}, header...)
	visible := p.detailVisibleRows(len(header))
	total := len(body)
	win := tuiviewport.ScrollWindow(total, p.window, visible)
	p.window = win.Start()
	lines = append(lines, win.SliceRows(body)...)
	if visible < total {
		lines = append(lines, tuiviewer.Counter(win.Start()+1, win.End(), total))
	}
	lines = append(lines, "",
		tuilib.PanelDotHint("  ", "↑/↓", "Scroll", "←/→", "Page", "esc", "Close"))
	return lines
}

// detailVisibleRows is how many body rows the detail view shows.
//
// The header is part of the panel's chrome, so a deeper header (the
// subagent detail's is five lines, the task detail's one) leaves fewer
// rows for the body.
func (p *Panel) detailVisibleRows(headerRows int) int {
	if headerRows < 1 {
		headerRows = 1
	}
	_, height := p.size()
	if height <= 0 {
		n := taskDetailFallbackRows - (headerRows - 1)
		if n < 1 {
			n = 1
		}
		return n
	}
	// The chrome is the header, the line counter, a blank row and the
	// legend.
	n := height - 6 - (headerRows - 1)
	if n < 1 {
		n = 1
	}
	return n
}

// detailHeaderRows is how many rows the open detail view's header
// takes. The task detail's header is its "Task Details" title; the
// subagent detail's is the five lines the reference's buildHeaderLines
// renders.
func (p *Panel) detailHeaderRows() int {
	if !p.detailSubagent {
		return 1
	}
	width, _ := p.size()
	return len(taskregistry.SubagentDetailHeader(p.detailTask(),
		taskregistry.SubagentHeaderContext{Width: width}))
}
