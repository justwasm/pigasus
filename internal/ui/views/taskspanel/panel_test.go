// panel_test.go — the /tasks panel's own tests.
//
// The panel is the subject here: it is built directly, with the seams
// a test can answer, and the assertions read its own rows. The host's
// wiring (the registry subscription, the status row's "· N tasks"
// segment, the kill that needs the session's subagent manager) is
// tested in internal/cliapp.

package taskspanel

import (
	"fmt"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
)

// testPanel builds a panel over a fresh registry.
//
// The panel has no Size seam, so the terminal's size is unknown — the
// path the unit tests take and the one the detail view's fallback
// height belongs to.
//
// The killed slice records the tasks the panel asked the host to kill,
// so a test can assert the kill seam without a subagent manager.
func testPanel(t *testing.T, reg *taskregistry.Registry) (*Panel, *[]*taskregistry.Task) {
	t.Helper()
	killed := &[]*taskregistry.Task{}
	p := New(Seams{
		Registry: func() *taskregistry.Registry { return reg },
		Kill: func(task *taskregistry.Task) {
			*killed = append(*killed, task)
		},
		Close: func() tea.Cmd { return nil },
	})
	return p, killed
}

// testPanelSized builds a panel that knows the terminal's size.
func testPanelSized(t *testing.T, reg *taskregistry.Registry, width, height int) (*Panel, *[]*taskregistry.Task) {
	t.Helper()
	killed := &[]*taskregistry.Task{}
	p := New(Seams{
		Registry: func() *taskregistry.Registry { return reg },
		Kill: func(task *taskregistry.Task) {
			*killed = append(*killed, task)
		},
		Size:  func() (int, int) { return width, height },
		Close: func() tea.Cmd { return nil },
	})
	return p, killed
}

// seedTask inserts a single task into reg with the given state.
func seedTask(reg *taskregistry.Registry, id, name string, kind taskregistry.Kind, state taskregistry.State) *taskregistry.Task {
	task := &taskregistry.Task{
		ID:    id,
		Name:  name,
		Kind:  kind,
		State: state,
	}
	switch state {
	case taskregistry.StateRunning:
		task.StartedAt = time.Now().Add(-2 * time.Second)
	case taskregistry.StateDone:
		task.StartedAt = time.Now().Add(-12 * time.Second)
		task.EndedAt = time.Now()
		task.ExitCode = 0
	default:
		task.StartedAt = time.Now()
	}
	reg.Register(task)
	return task
}

// stripAll removes every ANSI escape sequence (SGR + cursor) so the
// assertions compare plain text. The /tasks panel emits 24-bit RGB
// sequences, so the stripper has to be broader than an SGR-only one.
func stripAll(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
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

// plainLines renders the panel with the ANSI escapes stripped.
func plainLines(p *Panel) []string {
	raw := p.Lines()
	out := make([]string, 0, len(raw))
	for _, l := range raw {
		out = append(out, stripAll(l))
	}
	return out
}

// TestListEmpty — the reference's empty form: the title, the dim-gray
// "No background tasks." placeholder, a blank row and the legend.
func TestListEmpty(t *testing.T) {
	reg := taskregistry.New()
	p, _ := testPanel(t, reg)

	lines := plainLines(p)
	if len(lines) != 4 {
		t.Fatalf("panel lines = %d, want 4:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	if !strings.HasPrefix(lines[0], "Tasks") {
		t.Errorf("line[0] = %q, want to start with %q", lines[0], "Tasks")
	}
	if !strings.Contains(lines[1], "No background tasks.") {
		t.Errorf("line[1] = %q, want substring %q", lines[1], "No background tasks.")
	}
	if !strings.HasPrefix(lines[3], "Keyboard:") {
		t.Errorf("footer = %q, want to start with %q", lines[3], "Keyboard:")
	}
}

// TestListRows — the row is the reference's own: the cursor's row
// carries the "> " prefix, the others two spaces, and the row carries
// the task's name and its status cell (never its id).
func TestListRows(t *testing.T) {
	reg := taskregistry.New()
	now := time.Now()
	reg.Register(&taskregistry.Task{
		ID: "t3", Name: "echo done", Kind: taskregistry.KindShell,
		State: taskregistry.StateDone, StartedAt: now.Add(-12 * time.Second), EndedAt: now,
		ExitCode: 0,
	})
	reg.Register(&taskregistry.Task{
		ID: "t1", Name: "bash -c sleep 60", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-2 * time.Second),
	})
	reg.Register(&taskregistry.Task{
		ID: "t2", Name: "ddgr --num=5 cats", Kind: taskregistry.KindShell,
		State: taskregistry.StateRunning, StartedAt: now.Add(-1 * time.Second),
	})
	p, _ := testPanel(t, reg)

	lines := plainLines(p)
	// 6 rows: 1 header + 3 task rows + 1 trailing blank + 1 footer.
	if len(lines) != 6 {
		t.Fatalf("panel lines = %d, want 6:\n%s", len(lines), strings.Join(lines, "\n"))
	}
	wantRows := []struct {
		name   string
		status string
	}{
		{"echo done", "completed"},
		{"bash -c sleep 60", "running"},
		{"ddgr --num=5 cats", "running"},
	}
	for i, want := range wantRows {
		line := lines[1+i]
		if !strings.Contains(line, want.status) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.status)
		}
		if !strings.Contains(line, want.name) {
			t.Errorf("line[%d] = %q, want to contain %q", 1+i, line, want.name)
		}
		if strings.Contains(line, want.name+want.name) {
			t.Errorf("line[%d] = %q, want the name once", 1+i, line)
		}
	}
	// The cursor's row is the first one and it carries the "> " prefix.
	if !strings.HasPrefix(lines[1], "> ") {
		t.Errorf("line[1] = %q, want the '> ' prefix", lines[1])
	}
	for _, i := range []int{2, 3} {
		if !strings.HasPrefix(lines[i], "  ") {
			t.Errorf("line[%d] = %q, want the idle prefix", i, lines[i])
		}
	}
	if !strings.HasPrefix(lines[5], "Keyboard:") {
		t.Errorf("footer = %q, want Keyboard:", lines[5])
	}
}

// TestCursorMoves — the reference's "tasks" scope binds (KeyUp,
// KeyDown), so the arrows move the cursor over the rows.
func TestCursorMoves(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "t1", "first", taskregistry.KindShell, taskregistry.StateDone)
	seedTask(reg, "t2", "second", taskregistry.KindShell, taskregistry.StateDone)
	seedTask(reg, "t3", "third", taskregistry.KindShell, taskregistry.StateDone)
	p, _ := testPanel(t, reg)

	if got := p.Cursor(); got != 0 {
		t.Fatalf("cursor = %d, want 0", got)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("Down reported unhandled")
	}
	if got := p.Cursor(); got != 1 {
		t.Errorf("cursor after Down = %d, want 1", got)
	}
	// The cursor's row carries the marker.
	lines := plainLines(p)
	if !strings.HasPrefix(lines[2], "> ") {
		t.Errorf("line[2] = %q, want the '> ' prefix", lines[2])
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up reported unhandled")
	}
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor after Up = %d, want 0", got)
	}
	// The cursor never leaves the rows.
	for i := 0; i < 5; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyUp})
	}
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor after five Ups = %d, want 0", got)
	}
	for i := 0; i < 9; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if got := p.Cursor(); got != 2 {
		t.Errorf("cursor after nine Downs = %d, want 2", got)
	}
}

func TestOpenTaskDetailDirect(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "sub-direct", "inspect directly", taskregistry.KindSubagent, taskregistry.StateRunning)
	p, _ := testPanel(t, reg)

	p.OpenTaskDetail("sub-direct", true)
	if !p.Detail() {
		t.Fatalf("expected Detail() == true")
	}
	if p.DetailID() != "sub-direct" {
		t.Fatalf("expected DetailID() == %q, got %q", "sub-direct", p.DetailID())
	}
}

// TestPageKeysMoveTheCursor — ←/→ page the cursor, as the reference's
// (KeyLeft, KeyRight) group does.
func TestPageKeysMoveTheCursor(t *testing.T) {
	reg := taskregistry.New()
	for i := 0; i < 25; i++ {
		seedTask(reg, fmt.Sprintf("t%02d", i), "task", taskregistry.KindShell, taskregistry.StateDone)
	}
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if got := p.Cursor(); got != 10 {
		t.Errorf("cursor after Right = %d, want 10", got)
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor after Left = %d, want 0", got)
	}
}

// TestKillAsksTheHost — the kill belongs to the host; the panel only
// asks for it, and only for a running row.
func TestKillAsksTheHost(t *testing.T) {
	reg := taskregistry.New()
	// The registry sorts by the start time, so the running task is the
	// older one and lands on the cursor's row.
	running := seedTask(reg, "t1", "running", taskregistry.KindShell, taskregistry.StateRunning)
	running.StartedAt = time.Now().Add(-30 * time.Second)
	done := seedTask(reg, "t2", "done", taskregistry.KindShell, taskregistry.StateDone)
	done.StartedAt = time.Now().Add(-2 * time.Second)
	p, killed := testPanel(t, reg)

	// The cursor is on the running row.
	p.HandleKey(tea.Key{Code: 'k'})
	if len(*killed) != 1 {
		t.Fatalf("kill seam called %d times, want 1", len(*killed))
	}
	if (*killed)[0].ID != "t1" {
		t.Errorf("killed task = %q, want %q", (*killed)[0].ID, "t1")
	}

	// A finished row is a no-op — no made-up exit code.
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: 'k'})
	if len(*killed) != 1 {
		t.Errorf("kill seam called %d times, want 1 (a finished row is a no-op)", len(*killed))
	}
}

// TestRemoveHidesTheRow — the reference's KeyTaskRemove binding hides
// the row: it stays in the registry, the panel stops showing it.
func TestRemoveHidesTheRow(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "t1", "first", taskregistry.KindShell, taskregistry.StateDone)
	seedTask(reg, "t2", "second", taskregistry.KindShell, taskregistry.StateDone)
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: 'x'})
	if got := p.RowCount(); got != 1 {
		t.Fatalf("row count after the remove = %d, want 1", got)
	}
	if reg.Get("t1") == nil {
		t.Error("the removed task left the registry")
	}
	// The cursor stays inside the rows.
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor after the remove = %d, want 0", got)
	}
}

// TestEnterOpensTheTaskDetail — the cycle-315 bug: Enter used to kill
// the task and show nothing. It now opens the row's detail view.
func TestEnterOpensTheTaskDetail(t *testing.T) {
	reg := taskregistry.New()
	task := seedTask(reg, "sub-1", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateDone)
	task.Command = "bash -c sleep 60"
	task.Output = "line one\nline two"
	p, killed := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Detail() {
		t.Fatal("Enter did not open the detail view")
	}
	if len(*killed) != 0 {
		t.Errorf("Enter killed %d tasks, want 0", len(*killed))
	}
	if got := p.DetailID(); got != "sub-1" {
		t.Errorf("detail id = %q, want %q", got, "sub-1")
	}
	got := strings.Join(plainLines(p), "\n")
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
	if !strings.Contains(got, "↑/↓") {
		t.Errorf("task detail has no legend row:\n%s", got)
	}
}

// TestEnterOpensTheSubagentDetail — an agent (subagent) row opens the
// reference's other detail model, the subagent detail.
func TestEnterOpensTheSubagentDetail(t *testing.T) {
	reg := taskregistry.New()
	task := seedTask(reg, "sub-2", "review the diff", taskregistry.KindSubagent, taskregistry.StateDone)
	task.Prompt = "review the diff please"
	task.Output = "the answer"
	task.Tools = []string{"read_file", "run_command"}
	task.Steps = []string{"read_file", "run_command", "the answer"}
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Detail() {
		t.Fatal("Enter did not open the detail view")
	}
	got := strings.Join(plainLines(p), "\n")
	for _, want := range []string{
		// The reference's subagent-detail header is five lines deep and
		// has no "Agent(...)" title.
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
		// The trajectory section comes after the answer and numbers its
		// steps.
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
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	got = strings.Join(plainLines(p), "\n")
	if !strings.Contains(got, "    3. the answer") {
		t.Errorf("subagent detail missing %q:\n%s", "    3. the answer", got)
	}
}

// TestDetailEmptyForms — the reference's empty forms.
func TestDetailEmptyForms(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "sub-3", "nothing yet", taskregistry.KindSubagent, taskregistry.StateRunning)
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	got := strings.Join(plainLines(p), "\n")
	for _, want := range []string{"(no prompt available)", "No tools used yet", "(no output)", "No steps yet"} {
		if !strings.Contains(got, want) {
			t.Errorf("subagent detail missing %q:\n%s", want, got)
		}
	}

	// A background task with no captured output renders the reference's
	// "(no output)" form.
	reg2 := taskregistry.New()
	seedTask(reg2, "sub-4", "bash -c true", taskregistry.KindShell, taskregistry.StateRunning)
	p2, _ := testPanel(t, reg2)
	p2.HandleKey(tea.Key{Code: tea.KeyEnter})
	if got := strings.Join(plainLines(p2), "\n"); !strings.Contains(got, "(no output)") {
		t.Errorf("task detail missing %q:\n%s", "(no output)", got)
	}
}

// TestDetailEscReturnsToList — Esc leaves the detail view but keeps
// /tasks open; the list keeps its cursor.
func TestDetailEscReturnsToList(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "sub-5", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Detail() {
		t.Fatal("Enter did not open the detail view")
	}
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.Detail() {
		t.Error("Esc did not leave the detail view")
	}
	if got := p.Cursor(); got != 0 {
		t.Errorf("cursor after Esc = %d, want 0", got)
	}
}

// TestDetailScrolls — ↑/↓ move the body's window, pgup/pgdn move it a
// page. The reference binds exactly those keys in its "task_detail"
// scope.
func TestDetailScrolls(t *testing.T) {
	reg := taskregistry.New()
	task := seedTask(reg, "sub-6", "long output", taskregistry.KindShell, taskregistry.StateDone)
	var b strings.Builder
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&b, "row %d\n", i)
	}
	task.Output = b.String()
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	got := strings.Join(plainLines(p), "\n")
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

	p.HandleKey(tea.Key{Code: tea.KeyDown})
	got = strings.Join(plainLines(p), "\n")
	if strings.Contains(got, "Command: ") {
		t.Errorf("the body did not scroll down:\n%s", got)
	}
	if !strings.Contains(got, "row 12") {
		t.Errorf("the body did not scroll down:\n%s", got)
	}

	// A page up scrolls back.
	p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	if !strings.Contains(strings.Join(plainLines(p), "\n"), "Command: ") {
		t.Error("a page up did not scroll the body back")
	}
}

// TestDetailOwnsEveryKey — the detail view is modal: the global keys
// must not reach the editor while it is open.
func TestDetailOwnsEveryKey(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "sub-7", "bash -c sleep 60", taskregistry.KindShell, taskregistry.StateRunning)
	p, _ := testPanel(t, reg)

	p.HandleKey(tea.Key{Code: tea.KeyEnter})
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
		handled, _ := p.HandleKey(key)
		if !handled {
			t.Errorf("key %v in the detail view reported unhandled", key)
		}
		if !p.Detail() {
			t.Fatalf("key %v left the detail view", key)
		}
	}
}

// TestListSwallowsPrintables — the panel is modal: a stray letter never
// reaches the prompt. Ctrl+C is the one key the panel does not own.
func TestListSwallowsPrintables(t *testing.T) {
	reg := taskregistry.New()
	seedTask(reg, "t1", "first", taskregistry.KindShell, taskregistry.StateDone)
	p, _ := testPanel(t, reg)

	for _, key := range []tea.Key{
		{Code: 'z', Text: "z"},
		{Code: tea.KeySpace, Text: " "},
		{Code: tea.KeyTab},
	} {
		handled, _ := p.HandleKey(key)
		if !handled {
			t.Errorf("key %v reported unhandled", key)
		}
	}
	handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	if handled {
		t.Error("Ctrl+C reported handled — the global handler must see it")
	}
}

// TestDetailViewLineWrapAndReflow tests that when terminal width changes,
// the detail view automatically wraps and reflows paragraphs.
func TestDetailViewLineWrapAndReflow(t *testing.T) {
	reg := taskregistry.New()
	task := seedTask(reg, "sub-wrap-panel", "long task", taskregistry.KindSubagent, taskregistry.StateDone)
	task.Prompt = "This is a very long prompt paragraph that exceeds forty columns easily and should be wrapped."
	task.Output = "This is an equally long answer paragraph that must line wrap and reflow on terminal window resize."

	currentWidth := 45
	currentHeight := 20
	p := New(Seams{
		Registry: func() *taskregistry.Registry { return reg },
		Size:     func() (int, int) { return currentWidth, currentHeight },
		Close:    func() tea.Cmd { return nil },
	})

	// Enter detail view
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.Detail() {
		t.Fatal("expected detail view to open")
	}

	lines45 := p.Lines()
	for i, l := range lines45 {
		plain := stripAll(l)
		if len([]rune(plain)) > 45 {
			t.Errorf("line %d at width 45 exceeds 45 runes (%d): %q", i, len([]rune(plain)), plain)
		}
	}

	// Resize terminal to 80 cols
	currentWidth = 80
	lines80 := p.Lines()
	for i, l := range lines80 {
		plain := stripAll(l)
		if len([]rune(plain)) > 80 {
			t.Errorf("line %d at width 80 exceeds 80 runes (%d): %q", i, len([]rune(plain)), plain)
		}
	}

	// Total body lines at 45 cols must be greater than at 80 cols due to reflow
	body45 := len(taskregistry.SubagentDetailBody(task, 45))
	body80 := len(taskregistry.SubagentDetailBody(task, 80))
	if body45 <= body80 {
		t.Errorf("expected body45 (%d) > body80 (%d) due to wrapping reflow", body45, body80)
	}
}
