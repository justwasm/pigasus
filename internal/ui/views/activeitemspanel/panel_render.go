// panel_render.go — the active-items block's rows.
//
// The block is the reference's own: one line per running or pending
// record, the "> " prefix on the cursor's row, and the "↑ N more" /
// "↓ N more" tails for the rows that do not fit the window.
//
//	  ↑ 2 more
//	> ● [15:04:05] bash -c sleep 60 running
//	  ● Agent(Explore)  Processing... · 15s
//	  ↓ 3 more
//
// 【反编译】 model.ActiveItemsModel.View (0x102bb9710) gives every row
// a two-cell prefix — "> " on the focused cursor's row, "  " on the
// others — and hands the rest of the row to the renderer the record's
// kind picks in model.ActiveItemsModel.renderItem (0x102bb9f50).
//
// The row itself is drawn by the app tier: ActiveRowLine renders a
// task and ActiveSubagentRowLine renders a subagent.

package activeitemspanel

import (
	"fmt"
	"strings"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/palette"
)

// defaultWidth is the width a row is laid out in when the terminal's
// width is unknown.
const defaultWidth = 80

// Rows renders the block: the rows, the window's tails, or nothing at
// all.
//
// The block is empty when the setting switched it off, when nothing is
// in flight, or when the panel was never built.
func (p *Panel) Rows() string {
	if p == nil {
		return ""
	}
	if p.Hidden() {
		return ""
	}
	items := p.entries()
	if len(items) == 0 {
		return ""
	}

	total := len(items)
	p.clamp(total, p.height())

	maxVis := p.maxVisible
	start := p.scroll
	if start < 0 {
		start = 0
	}
	end := start + maxVis
	if end > total {
		end = total
	}

	width := p.width()
	if width <= 0 {
		width = defaultWidth
	}

	var lines []string

	// Header overflow: "  ↑ %d more"
	if start > 0 {
		lines = append(lines, fmt.Sprintf("  %s↑ %d more%s", palette.DimGrayOpen, start, palette.DimGrayClose))
	}

	for i := start; i < end; i++ {
		lines = append(lines, p.renderTrayItem(items[i], i == p.cursor, width))
	}

	// Footer overflow: "  ↓ %d more"
	if end < total {
		lines = append(lines, fmt.Sprintf("  %s↓ %d more%s", palette.DimGrayOpen, total-end, palette.DimGrayClose))
	}

	return strings.Join(lines, "\n")
}

func (p *Panel) renderTrayItem(item TrayItem, isCursor bool, width int) string {
	if item.Swarm != nil {
		return p.renderSwarmItem(item.Swarm, isCursor, width)
	}
	return p.renderItem(item.Task, isCursor, width)
}

// renderItem renders a single row of the block.
//
// 【反编译】 model.ActiveItemsModel.renderItem (0x102bb9f50) picks the
// row's renderer by the record's kind — a task goes to
// render.RenderTaskItem (0x1028c8f30) and a subagent to
// render.RenderSubagentItem (0x1028c8c30).
func (p *Panel) renderItem(t *taskregistry.Task, isCursor bool, width int) string {
	selected := p.focused && isCursor
	if t != nil && t.Kind == taskregistry.KindSubagent {
		return taskregistry.ActiveSubagentRowLine(t, selected, width)
	}
	return taskregistry.ActiveRowLine(t, selected, width)
}

func (p *Panel) renderSwarmItem(s *SwarmItem, isCursor bool, width int) string {
	selected := p.focused && isCursor
	prefix := "  "
	if selected {
		prefix = "> "
	}

	bullet := palette.AgentDoneOpen + "●" + palette.BlueClose
	label := "Swarm"
	if s.Title != "" {
		label = fmt.Sprintf("Swarm(%s)", s.Title)
	}

	var status string
	if s.IsDone {
		status = "Done"
	} else if s.PostCount == 0 {
		status = "Waiting for posts..."
	} else if s.Latest != "" {
		status = s.Latest
	} else {
		status = "Waiting for posts..."
	}

	plural := "posts"
	if s.PostCount == 1 {
		plural = "post"
	}
	countStr := fmt.Sprintf(" · %d %s", s.PostCount, plural)

	row := fmt.Sprintf("%s%s %s  %s%s%s%s", prefix, bullet, label, status, palette.DimGrayOpen, countStr, palette.DimGrayClose)
	if width > 0 {
		plain := stripANSI(row)
		if len(plain) > width {
			fixedLen := len(stripANSI(fmt.Sprintf("%s%s %s  %s", prefix, bullet, label, countStr))) + 4
			avail := width - fixedLen
			if avail > 4 && len(status) > avail {
				status = status[:avail-1] + "…"
			}
			row = fmt.Sprintf("%s%s %s  %s%s%s%s", prefix, bullet, label, status, palette.DimGrayOpen, countStr, palette.DimGrayClose)
		}
	}
	return row
}

func stripANSI(s string) string {
	var sb strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
