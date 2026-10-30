// Package taskregistry — detail.go.
//
// The reference's /tasks panel opens a detail view when the user
// presses Enter on a row. There are two of them, and they are two
// different models in the reference's TUI layer
// (google3/third_party/jetski/cli/model/model):
//
//   - model.TaskDetailModel     (cli/model/task_detail.go)     — a
//     background task (a shell the model ran in the background).
//   - model.SubagentDetailModel (cli/model/subagent_detail.go) — an
//     agent (subagent) task.
//
// 【反编译】 1.4.7 binary — see
// docs/cycles/cycle-315-tui-subagent-task-panels/deliverable.md and
// docs/cycles/cycle-316-tui-detail-headers/deliverable.md.
//
// Every label rendered below is the reference's own string, recovered
// from the binary:
//
//	Task Details / Command:  / Status:   / Type:     / Started:
//	Output / (no output)
//
// and for the subagent view:
//
//	main ▸  / worked %s / ID:  / Prompt / (no prompt available)
//	Tools / No tools used yet / Answer
//
// The two views do not share a layout. The task detail's body is
// flush left; the subagent detail indents its body by two cells and
// its tool list by four, and its header is five lines deep.
package task

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/palette"
)

// The reference's task-detail labels. Every one of them is a literal
// in the 1.4.7 binary.
const (
	// TaskDetailTitle is the reference's task-detail heading.
	TaskDetailTitle = "Task Details"

	detailLabelCommand = "Command: "
	detailLabelStatus  = "Status:  "
	detailLabelType    = "Type:    "
	detailLabelStarted = "Started: "

	// detailSectionOutput heads the captured-output block.
	detailSectionOutput = "Output"

	// detailNoOutput is the reference's empty-output form.
	detailNoOutput = "(no output)"
)

// The reference's rules and indents.
//
// 【反编译】 the task detail draws a fixed forty-cell rule under its
// metadata block; the subagent detail draws one that follows the
// panel width.
const (
	// detailRuleCell is the reference's rule character — "─" (U+2500).
	detailRuleCell = "─"

	// detailRuleCells is the task detail's rule length.
	//
	// 【反编译】 model.TaskDetailModel.buildTaskDetailLines calls
	// strings.Repeat("─", 0x28) — forty cells, a constant, not the
	// panel width.
	detailRuleCells = 40

	// detailRuleMaxCells and detailRuleMargin give the subagent
	// detail's rule length: min(60, width-4).
	//
	// 【反编译】 model.SubagentDetailModel.buildHeaderLines computes
	// 0x3c and clamps it to the model's width minus four.
	detailRuleMaxCells = 60
	detailRuleMargin   = 4

	// detailIndent is the reference's two-cell body indent and
	// detailItemIndent its four-cell item indent.
	//
	// 【反编译】 the subagent detail's section headings and their
	// content are prefixed with the two-byte string at 0x1035128da
	// ("  "); the tool list's items with the four-byte string at
	// 0x1033c6525 ("    ").
	detailIndent     = "  "
	detailItemIndent = "    "
)

// The reference's subagent-detail header literals.
//
// 【反编译】 model.SubagentDetailModel.buildHeaderLines.
const (
	// detailMainPrefix is the reference's "main ▸ " prefix — the path
	// from the main agent down to this one.
	detailMainPrefix = "main ▸ "

	// detailArrow is the reference's " ▸ " separator.
	detailArrow = " ▸ "

	// detailWorkedFormat is the reference's "worked %s" line.
	detailWorkedFormat = "worked %s"

	// detailIDPrefix is the reference's id line. The binary's
	// "ID: [%s]" literal is only used here through its first four
	// bytes — the reference's header shows "ID: <id>", without the
	// brackets.
	detailIDPrefix = "ID: "

	// detailStepsFormat is the reference's "<a> <b> · <c> · <N> steps"
	// line.
	detailStepsFormat = "%s %s · %s · %d steps"
)

// The reference's subagent-detail state vocabulary.
//
// 【反编译】 model.SubagentDetailModel.buildHeaderLines compares the
// agent's status against "error", "killed", "done" and "running" —
// its own four words, not the ones model.taskStatus() returns
// (running / waiting / completed / failed / cancelled).
const (
	SubagentStateRunning = "running"
	SubagentStateDone    = "done"
	SubagentStateError   = "error"
	SubagentStateKilled  = "killed"
)

// The reference's subagent-detail labels.
const (
	detailSectionPrompt = "Prompt"
	detailNoPrompt      = "(no prompt available)"

	detailSectionTools = "Tools"
	detailNoTools      = "No tools used yet"

	detailSectionAnswer = "Answer"

	// detailSectionTrajectoryFormat is the reference's trajectory
	// heading.
	//
	// 【反编译】 model.SubagentDetailModel.buildContentLines formats
	// it at +0x1ac0 — the literal "Trajectory — %s (%d steps)" is at
	// 0x10358a89d and is referenced exactly once in the binary.
	detailSectionTrajectoryFormat = "Trajectory — %s (%d steps)"

	// detailNoSteps is the reference's empty-trajectory form.
	//
	// 【反编译】 the same function references "No steps yet"
	// (0x10346f0a3) at +0x1cd8.
	detailNoSteps = "No steps yet"

	// detailStepFormat is the reference's numbered step form.
	//
	// 【反编译】 "%d. %s" (0x1033df139) is referenced at +0x293c of
	// the same function — after the trajectory heading — and is the
	// binary's only numbered-list format. See the cycle report's 存疑
	// section: the reference may number the trajectory's steps with a
	// different form.
	detailStepFormat = "%d. %s"

	// detailNoSubagentData is the reference's empty-subagent form.
	detailNoSubagentData = "  (no subagent data)"

	// detailErrorFormat is the reference's error line. The reference
	// formats it as "⚠ Error%s — %q" — the first verb is a suffix
	// (an exit code, say) the clone does not have, so it renders the
	// bare "⚠ Error — " form.
	detailErrorFormat = "⚠ Error — %q"
)

// detailTimeLayout is the reference's timestamp layout — the literal
// "2006-01-02 15:04:05" in the 1.4.7 binary.
const detailTimeLayout = "2006-01-02 15:04:05"

// detailMissing is the clone's marker for a field the registry does
// not know. It matches the "—" the panel's elapsed column already
// uses for an unknown value.
const detailMissing = "—"

// TaskDetailBody renders the body of the reference's task-detail view
// for t.
//
// 【反编译】 model.TaskDetailModel.buildTaskDetailLines emits, in
// order:
//
//	Command: <command>
//	Status:  <status>
//	Type:    <type>
//	────────────────────────────────────────  (forty cells)
//	Output
//	<the captured output, one line per row>   (or "(no output)")
//	Started: <2006-01-02 15:04:05>
//
// The reference's "Type:" value is "Agent" or "User" — whether the
// task was started by the model or by the user. Every task the clone
// registers comes from a tool call, so the origin would be constant;
// the clone renders the task's *kind* there instead, which is the
// axis its rows actually distinguish.
func TaskDetailBody(t *Task) []string {
	if t == nil {
		return []string{detailNoOutput}
	}
	lines := []string{
		detailLabelCommand + detailValue(t.Command),
		detailLabelStatus + TaskStatus(t),
		detailLabelType + detailValue(string(t.Kind)),
		strings.Repeat(detailRuleCell, detailRuleCells),
		detailSectionOutput,
	}
	lines = append(lines, detailOutputLines(t.Output)...)
	if !t.StartedAt.IsZero() {
		lines = append(lines, detailLabelStarted+t.StartedAt.Format(detailTimeLayout))
	}
	if t.Err != nil {
		lines = append(lines, fmt.Sprintf(detailErrorFormat, t.Err.Error()))
	}
	return lines
}

// SubagentHeaderContext carries what the subagent-detail header needs
// and the registry row does not know: the parent agent's name (the
// reference's "▸ <parent>" tail) and the panel width (the rule's
// length).
type SubagentHeaderContext struct {
	// Parent is the parent agent's name. Every subagent the clone
	// starts is a child of the main agent, so it is empty and the
	// header reads "main ▸ <name>".
	Parent string
	// Width is the panel's width in cells.
	Width int
}

// SubagentDetailHeader renders the reference's subagent-detail header
// for t.
//
// 【反编译】 model.SubagentDetailModel.buildHeaderLines renders five
// lines:
//
//	main ▸ <title> [▸ <parent>]
//	<elapsed>                        (the bare duration while the agent runs)
//	  <state> <name> · <kind> · <N> steps
//	ID: <id>
//	  ───────────────                (min(60, width-4) cells)
//
// The reference's own third line is "%s %s · %s · %d steps"; which of
// the agent's fields fills each of its three words was not recovered
// from the binary (see the cycle report's 存疑 section), so the clone
// puts the state word, the name, the kind and the number of trajectory
// steps there.
//
// 【反编译】 the count is the same number the trajectory section shows
// — buildHeaderLines and buildContentLines both compute it as
// max(the model's step count, the counter they keep while they walk
// the steps) — so the clone reads it from the same place.
func SubagentDetailHeader(t *Task, ctx SubagentHeaderContext) []string {
	if t == nil {
		return []string{detailNoSubagentData}
	}
	title := t.Name
	if title == "" {
		title = t.ID
	}
	path := detailMainPrefix + title
	if ctx.Parent != "" {
		path += detailArrow + ctx.Parent
	}
	steps := fmt.Sprintf(detailStepsFormat,
		SubagentStateColor(t.State)+SubagentStateWord(t.State)+StateClose(),
		title, string(t.Kind), len(t.Steps))
	return []string{
		path,
		detailElapsedLine(t),
		detailIndent + steps,
		detailIDPrefix + t.ID,
		detailIndent + strings.Repeat(detailRuleCell, detailRuleCellsFor(ctx.Width)),
	}
}

// SubagentStateWord maps a registry state onto the reference's
// subagent-detail state word.
//
// 【反编译】 buildHeaderLines knows four of them: running, done,
// error and killed.
func SubagentStateWord(s State) string {
	switch s {
	case StateRunning, StatePending:
		return SubagentStateRunning
	case StateDone:
		return SubagentStateDone
	case StateFailed:
		return SubagentStateError
	case StateKilled, StateRemoved:
		return SubagentStateKilled
	case StateStopped:
		return "stopped"
	default:
		return string(s)
	}
}

// SubagentStateColor returns the escape the reference paints the
// state word with.
//
// 【反编译】 buildHeaderLines picks the colour from the state word:
// "error" and "killed" get 196, "done" gets 34 and everything else
// gets 220 — the ANSI-256 palette, as lipgloss renders those names.
func SubagentStateColor(s State) string {
	switch SubagentStateWord(s) {
	case SubagentStateError, SubagentStateKilled:
		return palette.AgentErrorOpen
	case SubagentStateDone:
		return palette.AgentDoneOpen
	default:
		return palette.AgentActiveOpen
	}
}

// SubagentDetailBody renders the reference's subagent-detail content
// for t.
//
// 【反编译】 model.SubagentDetailModel.buildContentLines renders the
// prompt, the tools the subagent called, its answer and its
// trajectory, with "(no prompt available)", "No tools used yet" and
// "No steps yet" as the empty forms. Every line is indented by two
// cells and the lists' items by four.
//
// The reference also renders a "User Backgrounded" badge; the clone
// has no such badge.
func SubagentDetailBody(t *Task) []string {
	if t == nil {
		return []string{detailNoSubagentData}
	}
	lines := []string{
		detailIndent + detailSectionPrompt,
	}
	if prompt := strings.TrimSpace(t.Prompt); prompt != "" {
		lines = append(lines, indentLines(splitDetailLines(prompt))...)
	} else {
		lines = append(lines, detailIndent+detailNoPrompt)
	}
	lines = append(lines, "", detailIndent+detailSectionTools)
	if len(t.Tools) == 0 {
		lines = append(lines, detailIndent+detailNoTools)
	} else {
		for _, name := range t.Tools {
			lines = append(lines, detailItemIndent+name)
		}
	}
	lines = append(lines, "", detailIndent+detailSectionAnswer)
	if out := strings.TrimSpace(t.Output); out != "" {
		lines = append(lines, indentLines(splitDetailLines(out))...)
	} else {
		lines = append(lines, detailIndent+detailNoOutput)
	}
	lines = append(lines, "", detailIndent+detailTrajectoryHeading(t))
	if len(t.Steps) == 0 {
		lines = append(lines, detailIndent+detailNoSteps)
	} else {
		for i, step := range t.Steps {
			lines = append(lines, detailItemIndent+fmt.Sprintf(detailStepFormat, i+1, step))
		}
	}
	if t.Err != nil {
		lines = append(lines, detailIndent+fmt.Sprintf(detailErrorFormat, t.Err.Error()))
	}
	return lines
}

// detailTrajectoryHeading renders the reference's trajectory heading —
// the agent's name and the number of steps it took.
//
// 【反编译】 buildContentLines formats "Trajectory — %s (%d steps)";
// the count is the larger of the model's step count and the counter
// it keeps while it walks the steps.
func detailTrajectoryHeading(t *Task) string {
	name := t.Name
	if name == "" {
		name = t.ID
	}
	return fmt.Sprintf(detailSectionTrajectoryFormat, name, len(t.Steps))
}

// detailElapsedLine renders the reference's second header line.
//
// 【反编译】 buildHeaderLines formats the elapsed time on its own
// while the agent is still running, and prefixes it with "worked "
// once the agent is done.
func detailElapsedLine(t *Task) string {
	if t.StartedAt.IsZero() {
		return detailMissing
	}
	end := t.EndedAt
	if end.IsZero() {
		end = time.Now()
	}
	d := end.Sub(t.StartedAt)
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	if SubagentStateWord(t.State) == SubagentStateRunning {
		return d.String()
	}
	return fmt.Sprintf(detailWorkedFormat, d)
}

// detailRuleCellsFor is how many cells the subagent detail's rule
// has.
//
// 【反编译】 buildHeaderLines repeats "─" min(0x3c, width-4) times.
func detailRuleCellsFor(width int) int {
	n := detailRuleMaxCells
	if width > 0 && width-detailRuleMargin < n {
		n = width - detailRuleMargin
	}
	if n < 1 {
		n = 1
	}
	return n
}

// detailOutputLines splits a captured output into the reference's
// body rows. The reference trims each row and drops the trailing
// newline; an empty output renders "(no output)".
func detailOutputLines(out string) []string {
	trimmed := strings.TrimRight(out, "\n")
	if strings.TrimSpace(trimmed) == "" {
		return []string{detailNoOutput}
	}
	rows := strings.Split(trimmed, "\n")
	for i, r := range rows {
		rows[i] = strings.TrimSpace(r)
	}
	return rows
}

// splitDetailLines splits a multi-line value into body rows, keeping
// the interior blank lines.
func splitDetailLines(s string) []string {
	return strings.Split(strings.TrimRight(s, "\n"), "\n")
}

// indentLines prefixes every row with the reference's body indent.
func indentLines(rows []string) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, detailIndent+r)
	}
	return out
}

// detailValue renders a field the registry may not know.
func detailValue(s string) string {
	if strings.TrimSpace(s) == "" {
		return detailMissing
	}
	return s
}
