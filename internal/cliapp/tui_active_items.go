package cliapp

import (
	"fmt"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/taskregistry"
	"github.com/justwasm/pigasus/palette"
)

// activeItemsModel implements the reference binary's ActiveItemsModel
// (google3/third_party/jetski/cli/model/model.ActiveItemsModel) in full parity:
//
//  1. Gated by hideActiveItems setting:
//     If hideActiveItems is true ("Show Active Tasks" is set to "off"),
//     ActiveItemsModel.View returns empty string.
//  2. Data source:
//     Reads running and pending tasks/subagents from taskregistry.Registry.
//  3. Selection & Scrolling:
//     Supports cursor selection, keyboard navigation (Up/Down/Esc), and
//     scrolling window (default 5 items max, clamps to terminal height).
//  4. Rendering format:
//     Renders each item on a single line:
//     - Prefix: "> " on the focused cursor's row, "  " on every other
//     - Marker: "●" in the state's colour
//     - Time cell: "[15:04:05] "
//     - Summary text (truncated to the available width)
//     - Status cell: the state word
//     - Overflow indicators ("  ↑ %d more", "  ↓ %d more") when total > maxVisible
type activeItemsModel struct {
	cursor     int
	scroll     int
	focused    bool
	maxVisible int
}

func newActiveItemsModel() activeItemsModel {
	return activeItemsModel{
		maxVisible: 5,
	}
}

func (m *activeItemsModel) Focused() bool {
	return m.focused
}

func (m *activeItemsModel) SetFocus(f bool) {
	m.focused = f
}

func (m *activeItemsModel) SelectFirst() {
	m.cursor = 0
	m.scroll = 0
}

func (m *activeItemsModel) SelectLast(items []*taskregistry.Task) {
	if len(items) > 0 {
		m.cursor = len(items) - 1
		m.adjustScroll(len(items))
	}
}

func (m *activeItemsModel) clamp(total int) {
	if total == 0 {
		m.cursor = 0
		m.scroll = 0
		m.focused = false
		return
	}
	if m.cursor >= total {
		m.cursor = total - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.scroll >= total {
		m.scroll = total - 1
	}
	if m.scroll < 0 {
		m.scroll = 0
	}
	m.adjustScroll(total)
}

func (m *activeItemsModel) adjustScroll(total int) {
	maxVis := m.maxVisible
	if maxVis < 1 {
		maxVis = 1
	}
	if maxVis > 5 {
		maxVis = 5
	}
	if m.cursor < m.scroll {
		m.scroll = m.cursor
	} else if m.scroll+maxVis <= m.cursor {
		m.scroll = (m.cursor - maxVis) + 1
	}
}

// getActiveItems returns tasks currently running or pending in the registry.
func (m *runMode) getActiveItems() []*taskregistry.Task {
	if m.taskReg == nil {
		return nil
	}
	all := m.taskReg.List()
	var active []*taskregistry.Task
	for _, t := range all {
		if t.State == taskregistry.StateRunning || t.State == taskregistry.StatePending {
			active = append(active, t)
		}
	}
	// Sort by StartedAt ascending
	sort.SliceStable(active, func(i, j int) bool {
		return active[i].StartedAt.Before(active[j].StartedAt)
	})
	return active
}

// isHideActiveItems returns true if the user configured "Show Active Tasks" to "off".
func isHideActiveItems() bool {
	raw, err := auth.SettingsField("hideActiveItems")
	if err != nil || raw == nil {
		return false
	}
	if b, ok := raw.(bool); ok {
		return b
	}
	return false
}

// renderActiveItem renders a single active item row.
//
// 【反编译】 model.ActiveItemsModel.View (0x102bb9710) gives every row
// a two-cell prefix — "> " on the focused cursor's row, "  " on the
// others — and hands the rest of the row to the renderer the record's
// kind picks in model.ActiveItemsModel.renderItem (0x102bb9f50).
//
// A task goes to render.RenderTaskItem (0x1028c8f30), which renders the
// marker, the time cell, the name and the status cell:
//
//	● [15:04:05] <name> <status>
//
// The reference's task row carries neither the task's id nor an elapsed
// time.
//
// A subagent goes to render.RenderSubagentItem (0x1028c8c30), which
// renders the marker, the tool info, the status word and the worked
// cell:
//
//	● Agent(<name>)  <status> · worked 1m30s
//
// 【反编译】 the reference's subagent row carries the elapsed time —
// the task row does not.
func renderActiveItem(t *taskregistry.Task, focused bool, isCursor bool, maxWidth int) string {
	selected := focused && isCursor
	if t != nil && t.Kind == taskregistry.KindSubagent {
		return taskregistry.ActiveSubagentRowLine(t, selected, maxWidth)
	}
	return taskregistry.ActiveRowLine(t, selected, maxWidth)
}

// activeItemsView renders the active items block under the input box.
func (m *runMode) activeItemsView() string {
	if isHideActiveItems() {
		return ""
	}
	items := m.getActiveItems()
	if len(items) == 0 {
		return ""
	}

	total := len(items)
	m.activeItems.clamp(total)

	maxVis := m.activeItems.maxVisible
	if m.height > 0 {
		calc := m.height / 10
		if calc < 1 {
			calc = 1
		}
		if calc > 5 {
			calc = 5
		}
		maxVis = calc
	}

	start := m.activeItems.scroll
	if start < 0 {
		start = 0
	}
	end := start + maxVis
	if end > total {
		end = total
	}

	width := m.width
	if width <= 0 {
		width = 80
	}

	var lines []string

	// Header overflow: "  ↑ %d more"
	if start > 0 {
		lines = append(lines, fmt.Sprintf("  %s↑ %d more%s", palette.DimGrayOpen, start, palette.DimGrayClose))
	}

	for i := start; i < end; i++ {
		isCursor := (i == m.activeItems.cursor)
		line := renderActiveItem(items[i], m.activeItems.focused, isCursor, width)
		lines = append(lines, line)
	}

	// Footer overflow: "  ↓ %d more"
	if end < total {
		lines = append(lines, fmt.Sprintf("  %s↓ %d more%s", palette.DimGrayOpen, total-end, palette.DimGrayClose))
	}

	return strings.Join(lines, "\n")
}

// handleActiveItemsKey processes navigation keys when ActiveItems has focus.
func (m *runMode) handleActiveItemsKey(key tea.Key) (bool, tea.Cmd) {
	if !m.activeItems.focused {
		return false, nil
	}
	items := m.getActiveItems()
	total := len(items)
	if total == 0 {
		m.activeItems.focused = false
		return false, nil
	}

	switch key.Code {
	case tea.KeyUp:
		if m.activeItems.cursor > 0 {
			m.activeItems.cursor--
			m.activeItems.adjustScroll(total)
		}
		return true, nil
	case tea.KeyDown:
		if m.activeItems.cursor < total-1 {
			m.activeItems.cursor++
			m.activeItems.adjustScroll(total)
		}
		return true, nil
	case tea.KeyEsc:
		m.activeItems.focused = false
		return true, nil
	case tea.KeyEnter:
		// Enter on selected item: kill or view details
		if m.activeItems.cursor >= 0 && m.activeItems.cursor < total {
			selected := items[m.activeItems.cursor]
			m.killSelectedTask(selected)
		}
		return true, nil
	case 'k':
		if m.activeItems.cursor >= 0 && m.activeItems.cursor < total {
			selected := items[m.activeItems.cursor]
			m.killSelectedTask(selected)
		}
		return true, nil
	}
	return false, nil
}
