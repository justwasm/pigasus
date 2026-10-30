package task

import (
	"errors"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestStateStyle(t *testing.T) {
	label, _ := StateStyle(StateRunning)
	if label != "running" {
		t.Errorf("expected running label, got %q", label)
	}

	// 【反编译】 model.taskStatus() (1.4.7) returns "completed" for a
	// finished task — not the clone's older "done".
	label, _ = StateStyle(StateDone)
	if label != "completed" {
		t.Errorf("expected completed label, got %q", label)
	}

	// 【反编译】 the reference's row does not pad the state word — the
	// gap in front of the status cell is the row's own padding cell.
	for _, s := range []State{StateRunning, StatePending, StateDone, StateFailed, StateKilled} {
		label, _ := StateStyle(s)
		if strings.Contains(label, " ") {
			t.Errorf("StateStyle(%q) label = %q, want no padding", s, label)
		}
	}
}

func TestStateWordAndTaskStatus(t *testing.T) {
	// 【反编译】 model.taskStatus() (0x102bebf70) has four leaves —
	// "running", "cancelled", "completed" and "failed" — so a task
	// that is registered but not started yet is a "running" one.
	if got := StateWord(StatePending); got != "running" {
		t.Errorf("StateWord(StatePending) = %q, want running", got)
	}
	if got := StateWord(StateKilled); got != "cancelled" {
		t.Errorf("StateWord(StateKilled) = %q, want cancelled", got)
	}

	// The reference appends the exit code to the completed / failed
	// forms only.
	done := &Task{State: StateDone, ExitCode: 0}
	if got := TaskStatus(done); got != "completed (exit 0)" {
		t.Errorf("TaskStatus(done) = %q, want %q", got, "completed (exit 0)")
	}
	failed := &Task{State: StateFailed, ExitCode: 1}
	if got := TaskStatus(failed); got != "failed (exit 1)" {
		t.Errorf("TaskStatus(failed) = %q, want %q", got, "failed (exit 1)")
	}
	running := &Task{State: StateRunning}
	if got := TaskStatus(running); got != "running" {
		t.Errorf("TaskStatus(running) = %q, want running", got)
	}
	cancelled := &Task{State: StateKilled, ExitCode: 137}
	if got := TaskStatus(cancelled); got != "cancelled" {
		t.Errorf("TaskStatus(cancelled) = %q, want cancelled", got)
	}
}

func TestElapsed(t *testing.T) {
	if got := Elapsed(nil); got != "—" {
		t.Errorf("nil task elapsed = %q, want '—'", got)
	}

	task := &Task{
		StartedAt: time.Now().Add(-15 * time.Second),
	}
	if got := Elapsed(task); !strings.HasSuffix(got, "s") {
		t.Errorf("elapsed = %q, want suffix 's'", got)
	}
}

// stripANSI removes the SGR escapes from a rendered row so the test can
// look at the cells themselves.
func stripANSI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// TestRowLine — the row is the reference's own: a two-cell prefix, the
// status marker, the bracketed time, the name, the padding and the
// status cell.
//
// 【反编译】 model.TasksModel.View (0x102bed580) formats the row with
// "%s%s %s %s%s%s" (0x10349ee8e) and the six arguments are the
// prefix, the marker, the time cell, the name, the padding and the
// status cell.
func TestRowLine(t *testing.T) {
	task := &Task{
		ID:        "sub-12345",
		Name:      "sleep 10",
		State:     StateRunning,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}

	plain := stripANSI(RowLine(task, false, 80))
	// The idle prefix is two spaces, the marker is the reference's
	// "●", the time cell is the bracketed timestamp and the name
	// follows it.
	if !strings.HasPrefix(plain, "  ● [15:04:05] sleep 10") {
		t.Errorf("row = %q, want the reference's cell order", plain)
	}
	// The status cell is the last one and the row is exactly as wide
	// as the width it was given.
	if !strings.HasSuffix(plain, "running") {
		t.Errorf("row = %q, want the status cell last", plain)
	}
	if got := lipgloss.Width(plain); got != 80 {
		t.Errorf("row width = %d, want 80", got)
	}
	// The reference's row carries neither the task's id nor an
	// elapsed time.
	if strings.Contains(plain, "sub-12345") {
		t.Errorf("row = %q, want no task id", plain)
	}

	// The cursor's row carries the "> " prefix.
	cursor := stripANSI(RowLine(task, true, 80))
	if !strings.HasPrefix(cursor, "> ● [15:04:05]") {
		t.Errorf("cursor row = %q, want the '> ' prefix", cursor)
	}

	// The status cell carries the exit code.
	// 【反编译】 model.TasksModel.View (0x102bed580).
	doneTask := &Task{
		ID:        "shell-1",
		Name:      "npm test",
		State:     StateDone,
		ExitCode:  0,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}
	done := stripANSI(RowLine(doneTask, false, 80))
	if !strings.HasSuffix(done, "completed (exit 0)") {
		t.Errorf("the row does not carry the exit code: %q", done)
	}
	if got := lipgloss.Width(done); got != 80 {
		t.Errorf("row width = %d, want 80", got)
	}

	// A task without a timestamp gets the reference's blank cell —
	// ten spaces, so the time column keeps its width.
	blank := stripANSI(RowLine(&Task{Name: "x", State: StateRunning}, false, 80))
	if !strings.HasPrefix(blank, "  ● [          ] ") {
		t.Errorf("row = %q, want the blank time cell", blank)
	}
}

// TestRowLineTruncation — a name that does not fit is cut with the
// reference's ellipsis and the row keeps its width.
//
// 【反编译】 the name is cut to max(10, width − len(status) − 17) and
// the padding is max(2, width − len(status) − width(name) − 15).
func TestRowLineTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	task := &Task{
		Name:      long,
		State:     StateRunning,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}

	plain := stripANSI(RowLine(task, false, 80))
	if got := lipgloss.Width(plain); got != 80 {
		t.Errorf("row width = %d, want 80", got)
	}
	// 80 − len("running") (7) − 17 = 56 cells of name.
	if !strings.Contains(plain, strings.Repeat("x", 53)+"...") {
		t.Errorf("row = %q, want the name cut at 53 cells with an ellipsis", plain)
	}
	// The smallest padding is two cells.
	if !strings.Contains(plain, "..."+"  "+"running") {
		t.Errorf("row = %q, want the two-cell padding", plain)
	}
}

// TestRowLineNameFlattened — a name that carries a newline is squeezed
// onto one line before it is measured.
//
// 【反编译】 render.TruncateAnsi (0x1028c8270) flattens the string
// with render.FlattenToLine (0x1028c8220) first.
func TestRowLineNameFlattened(t *testing.T) {
	task := &Task{
		Name:      "echo one\n  echo two",
		State:     StateRunning,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}
	plain := stripANSI(RowLine(task, false, 80))
	if !strings.Contains(plain, "echo one echo two") {
		t.Errorf("row = %q, want the name flattened onto one line", plain)
	}
}

// TestActiveRowLine — the active-items row is the reference's own: a
// two-cell prefix, the status marker, the bracketed time, the name and
// the status cell.
//
// 【反编译】 render.RenderTaskItem (0x1028c8f30) builds the row out of
// three parts — fmt.Sprintf("%s %s", marker, timeCell), the styled name
// and " " + the styled status word — and joins them with
// runtime.concatstring3.
func TestActiveRowLine(t *testing.T) {
	task := &Task{
		ID:        "sub-12345",
		Name:      "sleep 10",
		State:     StateRunning,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}

	plain := stripANSI(ActiveRowLine(task, false, 80))
	if plain != "  ● [15:04:05] sleep 10 running" {
		t.Errorf("row = %q, want the reference's cell order", plain)
	}
	// The reference's row carries neither the task's id nor an
	// elapsed time.
	if strings.Contains(plain, "sub-12345") {
		t.Errorf("row = %q, want no task id", plain)
	}
	// The cursor's row carries the "> " prefix.
	cursor := stripANSI(ActiveRowLine(task, true, 80))
	if !strings.HasPrefix(cursor, "> ● [15:04:05]") {
		t.Errorf("cursor row = %q, want the '> ' prefix", cursor)
	}
	// The status cell carries the exit code.
	//
	// 【反编译】 render.RenderTaskItem (0x1028c8f30) appends
	// " (exit %d)" (0x103439584) to the completed and failed forms.
	done := stripANSI(ActiveRowLine(&Task{
		Name:     "npm test",
		State:    StateDone,
		ExitCode: 0,
	}, false, 80))
	if !strings.HasSuffix(done, "completed (exit 0)") {
		t.Errorf("the row does not carry the exit code: %q", done)
	}
	// A task without a timestamp gets an empty cell — the
	// active-items row does not pad the column the way the /tasks row
	// does.
	blank := stripANSI(ActiveRowLine(&Task{Name: "x", State: StateRunning}, false, 80))
	if !strings.HasPrefix(blank, "  ● [] x") {
		t.Errorf("row = %q, want the empty time cell", blank)
	}
}

// TestActiveRowLineTruncation — a name that does not fit is cut with
// the reference's ellipsis.
//
// 【反编译】 the name is cut to max(0, width − width(cell) −
// width(statusCell)): the cell is the marker, a space and the time
// cell, the status cell is a space and the status word.
func TestActiveRowLineTruncation(t *testing.T) {
	long := strings.Repeat("x", 200)
	task := &Task{
		Name:      long,
		State:     StateRunning,
		StartedAt: time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC),
	}

	plain := stripANSI(ActiveRowLine(task, false, 80))
	// 80 − the prefix (2) − the cell (13) − the status cell (8) = 57
	// cells of name.
	if !strings.Contains(plain, strings.Repeat("x", 54)+"...") {
		t.Errorf("row = %q, want the name cut at 54 cells with an ellipsis", plain)
	}
	if !strings.HasSuffix(plain, " running") {
		t.Errorf("row = %q, want the status cell last", plain)
	}
}

func TestStatusSuffix(t *testing.T) {
	if got := StatusSuffix(0); got != "" {
		t.Errorf("StatusSuffix(0) = %q, want empty", got)
	}
	if got := StatusSuffix(1); got != " · 1 task" {
		t.Errorf("StatusSuffix(1) = %q, want ' · 1 task'", got)
	}
	if got := StatusSuffix(3); got != " · 3 tasks" {
		t.Errorf("StatusSuffix(3) = %q, want ' · 3 tasks'", got)
	}
}

// TestActiveSubagentRowLine — the subagent row is the reference's own.
//
// 【反编译】 render.RenderSubagentItem (0x1028c8c30) renders the
// marker, the tool info, the status word and the worked cell:
//
//	● Agent(<name>)  <status> · worked 1m30s
//
// Unlike the task row it carries the elapsed time.
func TestActiveSubagentRowLine(t *testing.T) {
	start := time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC)
	sub := &Task{
		ID:        "subagent-1001",
		Name:      "sleep 10",
		Kind:      KindSubagent,
		State:     StateRunning,
		StartedAt: time.Now().Add(-90 * time.Second),
	}

	plain := stripANSI(ActiveSubagentRowLine(sub, false, 80))
	if plain != "  ● Agent(sleep 10)  Processing... · 1m30s" {
		t.Errorf("row = %q, want the reference's cell order", plain)
	}
	// The cursor's row carries the "> " prefix.
	cursor := stripANSI(ActiveSubagentRowLine(sub, true, 80))
	if !strings.HasPrefix(cursor, "> ● Agent(sleep 10)") {
		t.Errorf("cursor row = %q, want the '> ' prefix", cursor)
	}

	// 【反编译】 render.GetSubagentRenderInfo (0x1028c83f0) picks the
	// status word from the subagent's own state: "Done", "Error: " plus
	// the message, "Killed" and "Processing...".
	done := stripANSI(ActiveSubagentRowLine(&Task{
		Name:      "sleep 10",
		Kind:      KindSubagent,
		State:     StateDone,
		StartedAt: start,
		EndedAt:   start.Add(90 * time.Second),
	}, false, 80))
	if !strings.Contains(done, "Agent(sleep 10)  Done · worked 1m30s") {
		t.Errorf("the row does not carry the 'Done' word: %q", done)
	}
	failed := stripANSI(ActiveSubagentRowLine(&Task{
		Name:      "sleep 10",
		Kind:      KindSubagent,
		State:     StateFailed,
		Err:       errors.New("boom"),
		StartedAt: start,
		EndedAt:   start.Add(time.Second),
	}, false, 80))
	if !strings.Contains(failed, "Error: boom") {
		t.Errorf("the row does not carry the error message: %q", failed)
	}
	killed := stripANSI(ActiveSubagentRowLine(&Task{
		Name:      "sleep 10",
		Kind:      KindSubagent,
		State:     StateKilled,
		StartedAt: start,
		EndedAt:   start.Add(time.Second),
	}, false, 80))
	if !strings.Contains(killed, "Killed") {
		t.Errorf("the row does not carry the 'Killed' word: %q", killed)
	}

	// 【反编译】 a subagent without a name shows its id, cut to eight
	// bytes — render.GetSubagentRenderInfo (0x1028c83f0).
	nameless := stripANSI(ActiveSubagentRowLine(&Task{
		ID:        "subagent-1001",
		Kind:      KindSubagent,
		State:     StateRunning,
		StartedAt: time.Now().Add(-time.Second),
	}, false, 80))
	if !strings.Contains(nameless, "Agent(subagent)") {
		t.Errorf("row = %q, want the id cut to eight bytes", nameless)
	}
}

// TestActiveSubagentRowLineTruncation — the status word is the row's
// flexible part, not the name.
//
// 【反编译】 render.RenderSubagentItem (0x1028c8c30) cuts the status
// word to max(0, width − width(cell) − width(elapsedCell)).
func TestActiveSubagentRowLineTruncation(t *testing.T) {
	start := time.Date(2026, 10, 26, 15, 4, 5, 0, time.UTC)
	sub := &Task{
		Name:      strings.Repeat("x", 200),
		Kind:      KindSubagent,
		State:     StateDone,
		StartedAt: start,
		EndedAt:   start.Add(90 * time.Second),
	}

	plain := stripANSI(ActiveSubagentRowLine(sub, false, 40))
	// 40 − the prefix (2) − the cell (207) − the elapsed cell (15) is
	// negative, so the status word is cut away entirely.
	if !strings.Contains(plain, "Agent("+strings.Repeat("x", 200)+")  ") {
		t.Errorf("row = %q, want the name left alone", plain)
	}
	if !strings.HasSuffix(plain, " · worked 1m30s") {
		t.Errorf("row = %q, want the elapsed cell last", plain)
	}
	if strings.Contains(plain, "Done") {
		t.Errorf("row = %q, want the status word cut away", plain)
	}
}
