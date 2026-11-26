package tui

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/app/tools/tasks"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

const maxVisibleStepTrayRows = 7

type stepCounts struct {
	done       int
	inProgress int
	pending    int
	skipped    int
	failed     int
	total      int
}

func (c stepCounts) skippedNote() string {
	var parts []string
	if c.total > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d steps", c.done, c.total))
	}
	if c.failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", c.failed))
	}
	if c.skipped > 0 {
		parts = append(parts, fmt.Sprintf("%d skipped", c.skipped))
	}
	return strings.Join(parts, " · ")
}

// stepTray tracks the live checklist produced by update_steps during a turn.
type stepTray struct {
	items []tasks.StepItem
}

func (t *stepTray) reset() {
	t.items = nil
}

func (t *stepTray) sync(items []tasks.StepItem) {
	if len(items) == 0 {
		t.items = nil
		return
	}
	cp := make([]tasks.StepItem, len(items))
	copy(cp, items)
	t.items = cp
}

func (t *stepTray) syncFromToolStep(argsJSON string, result string, isErr bool) bool {
	if isErr {
		return false
	}
	items := tasks.ParseChecklistMarkdown(result)
	if len(items) == 0 && strings.TrimSpace(argsJSON) != "" {
		var p struct {
			Steps []tasks.StepItem `json:"steps"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &p); err == nil && tasks.ValidateUpdateSteps(p.Steps) == nil {
			items = p.Steps
		}
	}
	if len(items) == 0 {
		return false
	}
	t.sync(items)
	return true
}

func (t *stepTray) counts() stepCounts {
	var c stepCounts
	for _, item := range t.items {
		switch item.Status {
		case tasks.StepStatusDone:
			c.done++
		case tasks.StepStatusInProgress:
			c.inProgress++
		case tasks.StepStatusSkipped:
			c.skipped++
		case tasks.StepStatusFailed:
			c.failed++
		default:
			c.pending++
		}
	}
	c.total = len(t.items) - c.skipped
	return c
}

// visible reports whether the checklist has unfinished work to display.
// Once every step is done, skipped, or failed, the tray hides automatically.
func (t *stepTray) visible() bool {
	if len(t.items) == 0 {
		return false
	}
	c := t.counts()
	return c.pending > 0 || c.inProgress > 0
}

func (t *stepTray) firstFailure() int {
	for i, item := range t.items {
		if item.Status == tasks.StepStatusFailed {
			return i
		}
	}
	return -1
}

func (t *stepTray) windowRange(maxVisible int) (start, end int) {
	n := len(t.items)
	if n == 0 {
		return 0, 0
	}
	if maxVisible <= 0 {
		maxVisible = maxVisibleStepTrayRows
	}
	if n <= maxVisible {
		return 0, n
	}
	anchor := -1
	for i, item := range t.items {
		if item.Status == tasks.StepStatusInProgress {
			anchor = i
			break
		}
	}
	if anchor < 0 {
		for i, item := range t.items {
			if item.Status == tasks.StepStatusPending {
				anchor = i
				break
			}
		}
	}
	if anchor < 0 {
		anchor = t.firstFailure()
	}
	if anchor < 0 {
		anchor = n - 1
	}
	start = anchor - maxVisible/2
	if start < 0 {
		start = 0
	}
	if start+maxVisible > n {
		start = n - maxVisible
	}
	return start, start + maxVisible
}

func (t *stepTray) treeRowCount() int {
	if !t.visible() {
		return 0
	}
	start, end := t.windowRange(maxVisibleStepTrayRows)
	count := end - start
	if start > 0 {
		count++
	}
	if end < len(t.items) {
		count++
	}
	return count
}

func (t *stepTray) statusSummary() string {
	if !t.visible() {
		return ""
	}
	return t.counts().skippedNote()
}

func (t *stepTray) renderTreeRows(width int) []string {
	if !t.visible() {
		return nil
	}
	start, end := t.windowRange(maxVisibleStepTrayRows)
	hasEarlier := start > 0
	hasMore := end < len(t.items)
	rows := make([]string, 0, t.treeRowCount())

	if hasEarlier {
		line := "  " + palette.DimGrayOpen + fmt.Sprintf("├─ ⋮ %d earlier", start) + palette.DimGrayClose
		if width > 0 {
			line = tuilib.Truncate(line, width, "...")
		}
		rows = append(rows, line)
	}

	for i := start; i < end; i++ {
		isLast := i == end-1 && !hasMore
		line := renderStepRow(t.items[i], isLast, width)
		rows = append(rows, line)
	}

	if hasMore {
		line := "  " + palette.DimGrayOpen + fmt.Sprintf("└─ ⋮ %d more", len(t.items)-end) + palette.DimGrayClose
		if width > 0 {
			line = tuilib.Truncate(line, width, "...")
		}
		rows = append(rows, line)
	}
	return rows
}

func renderStepRow(item tasks.StepItem, isLast bool, width int) string {
	branch := "├─ "
	if isLast {
		branch = "└─ "
	}
	prefix := "  " + palette.DimGrayOpen + branch + palette.DimGrayClose

	var glyph, title string
	switch item.Status {
	case tasks.StepStatusDone:
		glyph = palette.ToolBulletOpen + "✓" + palette.ToolBulletClose
		title = palette.DimGrayOpen + item.Title + palette.DimGrayClose
	case tasks.StepStatusInProgress:
		glyph = palette.ToolBulletRunningOpen + "●" + palette.ToolBulletClose
		title = item.Title
	case tasks.StepStatusSkipped:
		glyph = palette.DimGrayOpen + "–" + palette.DimGrayClose
		title = palette.DimGrayOpen + item.Title + palette.DimGrayClose
	case tasks.StepStatusFailed:
		glyph = palette.ToolBulletErrOpen + "✗" + palette.ToolBulletClose
		title = palette.ToolBulletErrOpen + item.Title + palette.ToolBulletClose
	default:
		glyph = palette.DimGrayOpen + "○" + palette.DimGrayClose
		title = item.Title
	}

	line := prefix + glyph + " " + title
	if width > 0 {
		line = tuilib.Truncate(line, width, "...")
	}
	return line
}
