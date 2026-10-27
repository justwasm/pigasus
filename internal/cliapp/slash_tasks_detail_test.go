package cliapp

import (
	"fmt"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/taskregistry"
)

// detailText renders the open panel's rows with the ANSI escapes
// stripped, so the assertions can match plain text.
func detailText(m *runMode) string {
	var b strings.Builder
	for _, l := range m.panelLines() {
		b.WriteString(stripAll(l))
		b.WriteString("\n")
	}
	return b.String()
}

// openTaskDetailForTest seeds one task and presses Enter on it, the way
// the user does in the /tasks panel.
func openTaskDetailForTest(t *testing.T, m *runMode, task *taskregistry.Task) {
	t.Helper()
	m.panel = slashPanel{kind: panelTasks, cmd: "/tasks", cursor: 0}
	handled, _ := m.tasksPanelKey(tea.Key{Code: tea.KeyEnter})
	if !handled {
		t.Fatal("tasksPanelKey(Enter) reported unhandled")
	}
	if m.panel.taskDetailID != task.ID {
		t.Fatalf("detail id = %q, want %q", m.panel.taskDetailID, task.ID)
	}
}

// TestSlashTasksEnterOpensTaskDetail — the cycle-315 bug: Enter used to
// kill the task and show nothing. It now opens the task detail.
func TestSlashTasksEnterOpensTaskDetail(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateDone)
	task.Command = "bash -c sleep 60"
	task.Output = "line one\nline two"

	openTaskDetailForTest(t, m, task)

	if m.panel.kind != panelTaskDetail {
		t.Fatalf("panel kind = %v, want panelTaskDetail", m.panel.kind)
	}
	got := detailText(m)
	for _, want := range []string{
		"Task Details",
		"Command: bash -c sleep 60",
		"Status:  completed",
		"Type:    shell",
		"Output",
		"line one",
		"line two",
		"Started: ",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("task detail missing %q:\n%s", want, got)
		}
	}
	// The legend names the keys the reference's "task_detail" scope
	// binds.
	if !strings.Contains(got, "Keyboard:") && !strings.Contains(got, "↑/↓") {
		t.Errorf("task detail has no legend row:\n%s", got)
	}
}

// TestSlashTasksEnterOpensSubagentDetail — an agent (subagent) row opens
// the reference's other detail model, the subagent detail.
func TestSlashTasksEnterOpensSubagentDetail(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-2", "review the diff", taskregistry.KindSubagent, taskregistry.StateDone)
	task.Prompt = "review the diff please"
	task.Output = "the answer"
	task.Tools = []string{"read_file", "run_command"}
	task.Steps = []string{"read_file", "run_command", "the answer"}

	openTaskDetailForTest(t, m, task)

	if m.panel.kind != panelSubagentDetail {
		t.Fatalf("panel kind = %v, want panelSubagentDetail", m.panel.kind)
	}
	got := detailText(m)
	for _, want := range []string{
		// The reference's subagent-detail header is five lines deep
		// and has no "Agent(...)" title.
		// 【反编译】 model.SubagentDetailModel.buildHeaderLines.
		"main ▸ review the diff",
		"worked 12s",
		"done review the diff · subagent · 3 steps",
		"ID: sub-2",
		strings.Repeat("─", 60),
		// The body is indented by two cells, the tool list by four.
		// 【反编译】 model.SubagentDetailModel.buildContentLines.
		"  Prompt",
		"  review the diff please",
		"  Tools",
		"    read_file",
		"    run_command",
		"  Answer",
		"  the answer",
		// The trajectory section comes after the answer and numbers
		// its steps.
		// 【反编译】 model.SubagentDetailModel.buildContentLines.
		"  Trajectory — review the diff (3 steps)",
		"    1. read_file",
		"    2. run_command",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("subagent detail missing %q:\n%s", want, got)
		}
	}
	// The last step sits below the fold — the body scrolls.
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if !handled {
		t.Fatal("Down in the detail view reported unhandled")
	}
	got = detailText(m)
	if !strings.Contains(got, "    3. the answer") {
		t.Errorf("subagent detail missing %q:\n%s", "    3. the answer", got)
	}
}

// TestSlashTasksDetailEmptyForms — the reference's empty forms.
func TestSlashTasksDetailEmptyForms(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-3", "nothing yet", taskregistry.KindSubagent, taskregistry.StateRunning)

	openTaskDetailForTest(t, m, task)

	got := detailText(m)
	for _, want := range []string{"(no prompt available)", "No tools used yet", "(no output)", "No steps yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("subagent detail missing %q:\n%s", want, got)
		}
	}

	// A background task with no captured output renders the reference's
	// "(no output)" form.
	m2 := seedRunMode()
	shell := seedTask(m2.taskReg, "sub-4", "bash -c true", taskregistry.KindShell, taskregistry.StateRunning)
	openTaskDetailForTest(t, m2, shell)
	if !strings.Contains(detailText(m2), "(no output)") {
		t.Errorf("task detail missing %q:\n%s", "(no output)", detailText(m2))
	}
}

// TestSlashTasksDetailEscReturnsToList — Esc leaves the detail view but
// keeps /tasks open; a second Esc leaves /tasks and prints the notice.
func TestSlashTasksDetailEscReturnsToList(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-5", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)

	openTaskDetailForTest(t, m, task)
	m.panel.cursor = 0

	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("Esc in the detail view reported unhandled")
	}
	if m.panel.kind != panelTasks {
		t.Fatalf("panel kind after Esc = %v, want panelTasks", m.panel.kind)
	}
	if len(m.response) != 0 {
		t.Errorf("the detail view must not print the exit notice, got %q", m.response)
	}

	// The second Esc leaves /tasks.
	handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("Esc in /tasks reported unhandled")
	}
	if m.panel.kind != panelNone {
		t.Fatalf("panel kind after the second Esc = %v, want panelNone", m.panel.kind)
	}
	if !strings.Contains(strings.Join(m.response, "\n"), "Exited /tasks command") {
		t.Errorf("closing /tasks must print the exit notice, got %q", m.response)
	}
}

// TestSlashTasksDetailScrolls — ↑/↓ move the body's window, pgup/pgdn
// move it a page. The reference binds exactly those keys in its
// "task_detail" scope.
func TestSlashTasksDetailScrolls(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-6", "long output", taskregistry.KindShell, taskregistry.StateDone)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "row %d\n", i)
	}
	task.Output = b.String()

	openTaskDetailForTest(t, m, task)

	got := detailText(m)
	if !strings.Contains(got, "Command: ") {
		t.Fatalf("the body does not start at the metadata block:\n%s", got)
	}
	if !strings.Contains(got, "row 0") {
		t.Fatalf("the body does not show the first output row:\n%s", got)
	}
	if !strings.Contains(got, "row 11") {
		t.Errorf("the body window is shorter than the fallback height:\n%s", got)
	}
	if strings.Contains(got, "row 12") {
		t.Errorf("the body window shows more rows than the fallback height:\n%s", got)
	}

	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if !handled {
		t.Fatal("Down in the detail view reported unhandled")
	}
	got = detailText(m)
	if strings.Contains(got, "Command: ") {
		t.Errorf("the body did not scroll down:\n%s", got)
	}
	if !strings.Contains(got, "row 12") {
		t.Errorf("the body did not scroll down:\n%s", got)
	}

	// A page up scrolls back.
	handled, _ = m.handlePanelKey(tea.Key{Code: tea.KeyPgUp})
	if !handled {
		t.Fatal("PgUp in the detail view reported unhandled")
	}
	if m.panel.window != 0 {
		t.Errorf("window after PgUp = %d, want 0", m.panel.window)
	}
}

// TestSlashTasksDetailOwnsEveryKey — the detail view is modal: the
// global keys must not reach the editor while it is open.
func TestSlashTasksDetailOwnsEveryKey(t *testing.T) {
	m := seedRunMode()
	task := seedTask(m.taskReg, "sub-7", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)

	openTaskDetailForTest(t, m, task)

	for _, key := range []tea.Key{
		{Code: tea.KeyEnter},
		{Code: 'k'},
		{Code: 'x'},
		{Code: tea.KeyUp},
		{Code: tea.KeyDown},
		{Code: tea.KeyPgUp},
		{Code: tea.KeyPgDown},
		{Code: 'g', Mod: tea.ModCtrl},
	} {
		handled, _ := m.handlePanelKey(key)
		if !handled {
			t.Errorf("key %v in the detail view reported unhandled", key)
		}
		if m.panel.kind != panelTaskDetail {
			t.Fatalf("key %v left the detail view (kind = %v)", key, m.panel.kind)
		}
	}
}
