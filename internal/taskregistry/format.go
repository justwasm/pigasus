package taskregistry

import (
	"fmt"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/palette"
)

// The reference's own state labels.
//
// 【反编译】 model.taskStatus() (0x102bebf70) in the 1.4.7 binary
// returns exactly one of "running", "completed", "failed" or
// "cancelled" — four leaves and no fifth; the /tasks row and the
// detail view both render that word. See
// docs/cycles/cycle-315-tui-subagent-task-panels/deliverable.md and
// docs/cycles/cycle-318-tui-tasks-row-status/deliverable.md.
const (
	stateWordRunning   = "running"
	stateWordCompleted = "completed"
	stateWordFailed    = "failed"
	stateWordCancelled = "cancelled"
)

// StateWord returns the reference's label for a task state, unpadded.
//
// 【反编译】 model.taskStatus() has no fifth leaf, so a task that is
// registered but not started yet is a "running" one — the reference
// never shows a separate word for it.
func StateWord(s State) string {
	switch s {
	case StateRunning, StatePending:
		return stateWordRunning
	case StateDone:
		return stateWordCompleted
	case StateFailed:
		return stateWordFailed
	case StateKilled:
		return stateWordCancelled
	default:
		return string(s)
	}
}

// TaskStatus renders a task's status the way the reference does: the
// state word, plus " (exit N)" when the task carries an exit code.
//
// 【反编译】 model.taskStatus() appends the exit code to the
// "completed" and "failed" forms only — a cancelled or running task
// never shows one.
//
// The /tasks row renders this very string as its status cell; the
// reference does *not* pad the word — the gap in front of it is the
// row's own padding cell (see RowLine).
func TaskStatus(t *Task) string {
	word, suffix := statusParts(t)
	return word + suffix
}

// statusParts splits a task's status into its word and its exit
// suffix. The suffix is empty unless the task is completed or failed
// and carries an exit code.
//
// 【反编译】 model.taskStatus() (0x102bebf70) builds the cell in two
// steps: it formats the exit code with " (exit %d)" (0x103439584) and
// concatenates the result onto the word — "completed" (0x10341f664)
// or "failed" (0x1033de9b3). The two other leaves are the bare words
// "running" (0x1033f0519) and "cancelled" (0x10341efda).
//
// 【反编译】 the exit code is the record's `ExitCode` (+0x38) and the
// reference drops it when the record's `NoExitCode` bool (+0x40) is
// set — the clone's ExitCodeUnset sentinel is the same thing.
func statusParts(t *Task) (word, suffix string) {
	if t == nil {
		return stateWordRunning, ""
	}
	word = StateWord(t.State)
	if t.State != StateDone && t.State != StateFailed {
		return word, ""
	}
	if t.ExitCode == ExitCodeUnset {
		return word, ""
	}
	return word, fmt.Sprintf(" (exit %d)", t.ExitCode)
}

// StateStyle returns the state label and ANSI color escape for a Task
// State.
//
// 【反编译】 the reference's model.taskStatus() has no separate word
// for a registered-but-not-started task, so a pending one is a
// "running" one.
//
// 【反编译】 the reference paints the row's marker and its status word
// with one of four styles out of `render.Styles` — ToolDone,
// ToolRunning, ToolCanceled and ToolError — picked in this order:
//
//	running   — the record's `Running` bool
//	cancelled — the record's `IsAgentTask` bool and its `Status`
//	            (CORTEX_STEP_STATUS_CANCELED, 6)
//	failed    — the record's `IsError` bool
//	done      — everything else
//
// The record is `types.BackgroundTaskInfo`: `Running` sits at +0x18,
// `IsError` at +0x30, `IsAgentTask` at +0x41 and `Status` at +0x44.
// (`render.StatusMarker`, 0x1028c8040, and `render.RenderTaskItem`,
// 0x1028c8f30, read the four styles at the same offsets inside
// `render.Styles`, so the marker and the status word always carry the
// same colour.) The colours themselves come from the colour scheme the
// reference ran under; `palette` carries the recovered
// scheme table.
//
// 【反编译】 the record has three producers, and only one of them can
// reach the cancelled word. `store.(*TaskManager).GetBackgroundTasks`
// (0x1029ce670) and `model.activeTasks` (0x102bef370) project the
// store's `types.CommandOutput` into it field by field — they leave
// `Status` and `IsAgentTask` at zero, so a shell command is never
// "cancelled". `model.agentTaskInfo` (0x102bef940) builds the record
// for an *agent* step instead: it sets `Status` to the step's own
// status, `IsError` to (status == CORTEX_STEP_STATUS_ERROR, 7),
// `NoExitCode` to the same boolean as `Running`, and `IsAgentTask` to
// true — the last one out of the static composite literal the
// compiler copies into the record, not out of a store. The clone's
// `StateKilled` is that agent step. See
// docs/cycles/cycle-325-tui-task-record-producer/deliverable.md.
//
// 【存疑】 the reference's two branches disagree about the failed
// task: the scheme branch paints it with the scheme's agent colour,
// the fallback branch paints it red. The clone keeps the colours its
// own capture produced rather than either branch.
//
// The label is the bare word — the reference's row does not pad it;
// the padding in front of the status cell is the row's own.
func StateStyle(s State) (label, color string) {
	switch s {
	case StateRunning, StatePending:
		return stateWordRunning, palette.BlueOpen
	case StateDone:
		return stateWordCompleted, palette.ToolAddOpen
	case StateFailed:
		return stateWordFailed, palette.ToolRemoveOpen
	case StateKilled:
		return stateWordCancelled, palette.WarningOpen
	default:
		return string(s), palette.DimGrayOpen
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

// The /tasks row's cells.
//
// 【反编译】 model.TasksModel.View (0x102bed580) builds one row with a
// single fmt.Sprintf("%s%s %s %s%s%s", …) — the format literal is
// fourteen bytes long and sits at 0x10349ee8e. Its six arguments are,
// in order:
//
//	0  the prefix       "> " on the selected row, "  " on every other
//	1  the marker       render.StatusMarker — "●" in the state's colour
//	2  the time cell    "[15:04:05]" / "[n/a]" / "[          ]"
//	3  the name         render.TruncateAnsi(name, …)
//	4  the padding      strings.Repeat(" ", …)
//	5  the status cell  "running" / "cancelled" / "completed" / "failed"
//
// The two spaces the format carries sit between the marker and the
// time cell, and between the time cell and the name.
const (
	// rowPrefixSelected is the selected row's two-cell prefix.
	rowPrefixSelected = "> "
	// rowPrefixIdle is the prefix of every other row.
	rowPrefixIdle = "  "
	// rowMarker is the reference's status marker: render.StatusMarker
	// (0x1028c8040) returns three bytes at 0x1033bf73c — the filled
	// circle "●" — in the state's own colour.
	rowMarker = "●"
	// rowTimeLayout is the reference's timestamp layout.
	rowTimeLayout = "15:04:05"
	// rowTimeBlank is the reference's cell content for a task without
	// a timestamp — ten spaces, so the time column keeps its width.
	rowTimeBlank = "          "
	// rowFixedCells is the width of the cells the row always carries:
	// the prefix (2) + the marker (1) + the two spaces + the time
	// cell ("[15:04:05]", ten cells) = 15.
	rowFixedCells = 15
	// rowTruncationReserve is the room the name's truncation leaves:
	// the fixed cells (15) plus the smallest padding (2).
	rowTruncationReserve = 17
	// rowMinNameWidth is the floor of the name's width.
	rowMinNameWidth = 10
	// rowMinPadding is the floor of the padding.
	rowMinPadding = 2
	// rowDefaultWidth is the width the row is laid out in when the
	// caller has none to give. The reference always has one — its
	// model carries the panel's width.
	rowDefaultWidth = 80
)

// RowLine renders a single task row for the /tasks panel.
//
// width is the width the row is laid out in: the reference's row pads
// the name so that the status cell lands on the right edge of it, so
// the row comes out exactly that wide.
//
// 【反编译】 model.TasksModel.View (0x102bed580) — the row's format is
// "%s%s %s %s%s%s" (0x10349ee8e) and its six arguments are the prefix,
// the marker, the time cell, the name, the padding and the status
// cell.
func RowLine(t *Task, cursor bool, width int) string {
	if t == nil {
		return ""
	}
	if width <= 0 {
		width = rowDefaultWidth
	}
	status := TaskStatus(t)
	_, color := StateStyle(t.State)

	// 【反编译】 the name is cut so that the row's fixed cells and the
	// status cell still fit: max(10, width − len(status) − 17).
	nameWidth := width - len(status) - rowTruncationReserve
	if nameWidth < rowMinNameWidth {
		nameWidth = rowMinNameWidth
	}
	name := truncateAnsi(t.Name, nameWidth)

	// 【反编译】 the padding fills the gap between the name and the
	// status cell: max(2, width − len(status) − width(name) − 15).
	pad := width - len(status) - lipgloss.Width(name) - rowFixedCells
	if pad < rowMinPadding {
		pad = rowMinPadding
	}

	prefix := rowPrefixIdle
	if cursor {
		prefix = palette.BlueOpen + rowPrefixSelected + palette.BlueClose
	}

	return prefix +
		color + rowMarker + StateClose() + " " +
		palette.DimGrayOpen + rowTimeCell(t) + palette.DimGrayClose + " " +
		name +
		strings.Repeat(" ", pad) +
		color + status + StateClose()
}

// rowTimeCell renders the row's time cell.
//
// 【反编译】 model.TasksModel.View (0x102bed580) formats the record's
// time with "15:04:05" (0x103408697) and wraps it in brackets
// ("[%s]", 0x1033c69c9). A zero time becomes ten spaces
// (0x10343958e) — the cell keeps the column's width — and a time that
// is only half set becomes "n/a" (0x1033bf78a).
//
// The reference's record carries one time per row and the clone shows
// the task's start time; which of the two the reference's field holds
// is still 存疑 — see
// docs/cycles/cycle-319-tui-tasks-row-layout/deliverable.md.
func rowTimeCell(t *Task) string {
	if t == nil || t.StartedAt.IsZero() {
		return "[" + rowTimeBlank + "]"
	}
	return "[" + t.StartedAt.Format(rowTimeLayout) + "]"
}

// The ActiveItems row's cells.
//
// 【反编译】 render.RenderTaskItem (0x1028c8f30) builds one row out of
// three parts:
//
//	cell        fmt.Sprintf("%s %s", marker, timeCell) — 0x1033de947
//	statusCell  " " + the styled status word — runtime.concatstring2
//	result      cell + the styled name + statusCell — runtime.concatstring3
//
// The time cell is fmt.Sprintf("[%s] ", t) — "[15:04:05] ", with the
// space the format carries — and the name is cut to
// max(0, width − width(cell) − width(statusCell)).
//
// Unlike the /tasks row this one has no padding cell: the status word
// follows the name directly, separated by a single space.
const (
	// activeTimeLayout is the reference's timestamp layout.
	activeTimeLayout = "15:04:05"
	// activePrefixWidth is the width of the row's prefix — "> " on the
	// focused cursor's row, "  " on every other one.
	activePrefixWidth = 2
	// activeMinBodyWidth is the floor of the width the row's body is
	// laid out in.
	activeMinBodyWidth = 1
)

// ActiveRowLine renders a single row of the active-items block.
//
// width is the width of the whole row, prefix included: the reference
// gives the row's body the width minus the prefix's two cells.
//
// 【反编译】 model.ActiveItemsModel.View (0x102bb9710) gives every row
// a two-cell prefix — "> " on the focused cursor's row, "  " on the
// others — and hands the rest of the row to render.RenderTaskItem
// (0x1028c8f30).
func ActiveRowLine(t *Task, selected bool, width int) string {
	if t == nil {
		return ""
	}
	if width <= 0 {
		width = rowDefaultWidth
	}
	status := TaskStatus(t)
	_, color := StateStyle(t.State)

	// 【反编译】 the marker is render.StatusMarker (0x1028c8040) — the
	// "●" in the state's own colour — and the time cell keeps the
	// space its format carries.
	cell := color + rowMarker + StateClose() + " " + activeTimeCell(t)
	statusCell := " " + color + status + StateClose()

	// 【反编译】 the name is cut so that the marker, the time cell and
	// the status cell still fit: max(0, width − width(cell) −
	// width(statusCell)).
	body := width - activePrefixWidth
	if body < activeMinBodyWidth {
		body = activeMinBodyWidth
	}
	nameWidth := body - lipgloss.Width(cell) - lipgloss.Width(statusCell)
	if nameWidth < 0 {
		nameWidth = 0
	}
	name := truncateAnsi(t.Name, nameWidth)

	prefix := rowPrefixIdle
	if selected {
		prefix = palette.BlueOpen + rowPrefixSelected + palette.BlueClose
	}

	return prefix + cell + name + statusCell
}

// activeTimeCell renders the active-items row's time cell.
//
// 【反编译】 render.RenderTaskItem (0x1028c8f30) formats the record's
// time with "[%s] " (0x1033d1a88). A zero time becomes an empty cell
// ("[] ") and a time that is only half set becomes "[n/a] " — the
// ActiveItems row does not pad the column the way the /tasks row does.
//
// The reference's record carries one time per row and the clone shows
// the task's start time; which of the two the reference's field holds
// is still 存疑 — see
// docs/cycles/cycle-319-tui-tasks-row-layout/deliverable.md.
func activeTimeCell(t *Task) string {
	if t == nil || t.StartedAt.IsZero() {
		return "[] "
	}
	return "[" + t.StartedAt.Format(activeTimeLayout) + "] "
}

// The ActiveItems subagent row.
//
// 【反编译】 render.RenderSubagentItem (0x1028c8c30) builds one row
// out of three parts:
//
//	cell        fmt.Sprintf("%s %s  ", marker, toolInfo) — 0x1033f186e
//	statusCell  the status word, cut to what is left of the row
//	elapsedCell " · 1m30s" / " · worked 1m30s"
//	result      cell + statusCell + elapsedCell — runtime.concatstring3
//
// The marker and the tool info come out of
// render.GetSubagentRenderInfo (0x1028c83f0), which paints the marker
// with the subagent's own colour, builds the tool info as
// "Agent(<name>)" (0x103420393) and returns the status word and the
// elapsed cell beside them.
//
// The status word — not the name — is the row's flexible part: it is
// cut to max(0, width − width(cell) − width(elapsedCell)).
//
// The elapsed cell has two forms, and the reference's own subagent
// detail header draws the same line: a subagent that is still working
// shows the bare duration, one that is done shows the duration behind
// a "worked".
const (
	// activeSubagentLabel is the reference's label for a subagent that
	// calls no tool: "Agent(%s)" (0x103420393).
	activeSubagentLabel = "Agent(%s)"
	// activeElapsedFormat is the reference's elapsed cell for a
	// subagent that is still working: " · %s" (0x1033df883).
	activeElapsedFormat = " · %s"
	// activeWorkedFormat is the reference's elapsed cell for a subagent
	// that is done: " · worked %s" (0x103487a62).
	activeWorkedFormat = " · worked %s"
	// activeSubagentNameMax is how many bytes of the subagent's id the
	// reference keeps when the name comes out as the generic
	// "Subagent".
	activeSubagentNameMax = 8
)

// The reference's own status words for a subagent.
//
// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) reads the
// subagent's own status string and picks one of four words: "done" and
// "idle" become "Done" (0x1033c6a11), "error" becomes "Error: " plus
// the message (0x1033f1247), "killed" becomes "Killed" (0x1033df87d)
// and a subagent that is still working shows "Processing..."
// (0x103487a55).
const (
	activeSubagentDone       = "Done"
	activeSubagentError      = "Error: "
	activeSubagentKilled     = "Killed"
	activeSubagentProcessing = "Processing..."
)

// ActiveSubagentRowLine renders a single row of the active-items block
// for a subagent.
//
// width is the width of the whole row, prefix included: the reference
// gives the row's body the width minus the prefix's two cells.
//
// 【反编译】 model.ActiveItemsModel.renderItem (0x102bb9f50) picks the
// row's renderer by the record's kind — a task goes to
// render.RenderTaskItem (0x1028c8f30) and a subagent to
// render.RenderSubagentItem (0x1028c8c30), which renders
//
//	● Agent(<name>)  <status><elapsed>
func ActiveSubagentRowLine(t *Task, selected bool, width int) string {
	if t == nil {
		return ""
	}
	if width <= 0 {
		width = rowDefaultWidth
	}

	// 【反编译】 the cell is fmt.Sprintf("%s %s  ", marker, toolInfo) —
	// the styled marker, the tool info and the two spaces the format
	// carries.
	cell := activeSubagentMarker(t) + " " + activeSubagentToolInfo(t) + "  "

	// 【反编译】 the elapsed cell carries the time the subagent has been
	// running, rounded to the second.
	elapsed := ""
	if e := activeSubagentElapsedCell(t); e != "" {
		elapsed = palette.DimGrayOpen + e + palette.DimGrayClose
	}

	// 【反编译】 the status word is cut so that the marker, the tool
	// info and the elapsed cell still fit: max(0, width − width(cell) −
	// width(elapsedCell)).
	body := width - activePrefixWidth
	if body < activeMinBodyWidth {
		body = activeMinBodyWidth
	}
	statusWidth := body - lipgloss.Width(cell) - lipgloss.Width(elapsed)
	if statusWidth < 0 {
		statusWidth = 0
	}
	status := truncateAnsi(activeSubagentStatus(t), statusWidth)

	prefix := rowPrefixIdle
	if selected {
		prefix = palette.BlueOpen + rowPrefixSelected + palette.BlueClose
	}

	// 【反编译】 the reference paints the status word with the theme's
	// own style (sp+0x17000+0x320), which was not decoded; the clone
	// uses the subagent's state colour — the colour its marker carries
	// — as the task row does.
	return prefix + cell +
		SubagentStateColor(t.State) + status + StateClose() +
		elapsed
}

// activeSubagentMarker returns the row's marker.
//
// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) paints the
// "●" with lipgloss.Color("220") while the subagent works, "34" once
// it is done and "196" when it failed or was killed — the same three
// colours the subagent detail's state word carries.
func activeSubagentMarker(t *Task) string {
	return SubagentStateColor(t.State) + rowMarker + StateClose()
}

// activeSubagentToolInfo returns the row's tool-info cell.
//
// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) builds it as
// "Agent(<name>)" while the subagent calls no tool, and as
// "<tool> › <name>" — the " › " separator (5 B, vm 0x1033d1150) — while
// it is inside one. The clone
// keeps no "current tool step" — the registry only remembers the tools
// a subagent called — so it always takes the first form.
func activeSubagentToolInfo(t *Task) string {
	return fmt.Sprintf(activeSubagentLabel, activeSubagentName(t))
}

// activeSubagentName returns the name the row carries.
//
// 【反编译】 render.formatSubagentName (0x1028f39f0) joins the
// subagent's name and its label with ": " and falls back to the
// generic "Subagent" (0x103407b67) when it has neither. When the name
// comes out as that generic word, render.GetSubagentRenderInfo
// (0x1028c83f0) uses the subagent's id instead, cut to eight bytes.
//
// The clone's registry has no label of its own, so the name stands
// alone — and an empty name takes the id's place.
func activeSubagentName(t *Task) string {
	name := flattenToLine(t.Name)
	if name != "" {
		return name
	}
	id := flattenToLine(t.ID)
	if len(id) > activeSubagentNameMax {
		id = id[:activeSubagentNameMax]
	}
	return id
}

// activeSubagentStatus returns the row's status word.
//
// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) reads the
// subagent's own status string: "Done" for a finished one, "Error: "
// plus the message for a failed one, "Killed" for a killed one and
// "Processing..." while it is still working.
func activeSubagentStatus(t *Task) string {
	switch t.State {
	case StateDone:
		return activeSubagentDone
	case StateFailed:
		if t.Err != nil {
			return activeSubagentError + t.Err.Error()
		}
		return activeSubagentError
	case StateKilled, StateRemoved:
		return activeSubagentKilled
	default:
		return activeSubagentProcessing
	}
}

// activeSubagentElapsedCell returns the row's elapsed cell.
//
// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) formats it
// " · %s" (0x1033df883) while the subagent is still running — the time
// since it started, rounded to the second (time.Duration.Round) — and
// " · worked %s" (0x103487a62) once it is done, with the difference of
// the two times its record carries. A subagent that is done but has no
// end time, or whose duration is not positive, has no cell at all.
//
// The same rule draws the second line of the reference's subagent
// detail header.
func activeSubagentElapsedCell(t *Task) string {
	if t == nil || t.StartedAt.IsZero() {
		return ""
	}
	if t.State == StateRunning || t.State == StatePending {
		return fmt.Sprintf(activeElapsedFormat, time.Since(t.StartedAt).Round(time.Second))
	}
	d := t.EndedAt.Sub(t.StartedAt)
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf(activeWorkedFormat, d.Round(time.Second))
}

// truncateAnsi cuts a string to a display width and marks the cut the
// way the reference does.
//
// 【反编译】 render.TruncateAnsi (0x1028c8270) flattens the string to
// a single line and hands it to render.TruncateAnsiLine
// (0x1028c82e0): a width under one gives "", a string that already
// fits comes back unchanged, a width under four gives one dot, and
// anything else is cut to width−3 cells and given "...".
func truncateAnsi(s string, width int) string {
	if width < 1 {
		return ""
	}
	s = flattenToLine(s)
	if lipgloss.Width(s) <= width {
		return s
	}
	if width < 4 {
		return "."
	}
	return ansi.Truncate(s, width-3, "") + "..."
}

// flattenToLine squeezes a string onto one line: every run of
// whitespace becomes a single space.
//
// 【反编译】 render.FlattenToLine (0x1028c8220) is
// strings.Join(strings.Fields(s), " ").
func flattenToLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
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
