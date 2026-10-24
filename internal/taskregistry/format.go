package taskregistry

import (
	"fmt"
	"time"

	"github.com/justwasm/pigasus/internal/palette"
)

// StateStyle returns the state label and ANSI color escape for a Task State.
func StateStyle(s State) (label, color string) {
	switch s {
	case StateRunning:
		return "running ", palette.BlueOpen
	case StatePending:
		return "pending ", palette.BlueOpen
	case StateDone:
		return "done    ", palette.ToolAddOpen
	case StateFailed:
		return "failed  ", palette.ToolRemoveOpen
	case StateKilled:
		return "killed  ", palette.WarningOpen
	default:
		return string(s) + " ", palette.DimGrayOpen
	}
}

// StateClose returns the ANSI closing sequence for state styling.
func StateClose() string {
	return "\033[39m"
}

// Elapsed formats the duration between StartedAt and EndedAt (or Now).
func Elapsed(t *Task) string {
	if t == nil || t.StartedAt.IsZero() {
		return "—"
	}
	end := t.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	d := end.Sub(t.StartedAt)
	if d < 0 {
		d = 0
	}
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
	}
	return fmt.Sprintf("%dh%02dm", int(d.Hours()), int(d.Minutes())%60)
}

func runeCount(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

// RowLine renders a single task row for the /tasks panel.
func RowLine(t *Task, cursor bool) string {
	stateStr, color := StateStyle(t.State)
	glyph := "⠿"
	elapsed := Elapsed(t)
	name := t.Name
	if runeCount(name) > 60 {
		name = truncateRunes(name, 57) + "..."
	}
	id := t.ID
	if runeCount(id) > 20 {
		id = truncateRunes(id, 17) + "..."
	}
	row := fmt.Sprintf("  ⎿  %s%s %s\033[39m  %s%s%s  %s  %s",
		color, glyph, stateStr,
		palette.DimGrayOpen, id, palette.DimGrayClose,
		name, elapsed)
	if t.State == StateFailed && t.Err != nil {
		reason := t.Err.Error()
		if runeCount(reason) > 40 {
			reason = truncateRunes(reason, 37) + "..."
		}
		row += "  " + color + "Error: " + reason + "\033[39m"
	}
	if cursor {
		row = "\033[1m" + row + "\033[22m"
	}
	return row
}

// StatusSuffix formats the right-side "· N tasks" segment.
func StatusSuffix(count int) string {
	if count <= 0 {
		return ""
	}
	if count == 1 {
		return " · 1 task"
	}
	return fmt.Sprintf(" · %d tasks", count)
}
