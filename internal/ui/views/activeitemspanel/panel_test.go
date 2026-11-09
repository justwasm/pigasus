// panel_test.go — the active-items block's own tests.
//
// The panel is the subject here: it is built directly, with the seams
// a test can answer, and the assertions read its own rows. The host's
// wiring (the routing of the keys, the prompt's ↓ that hands the
// focus over, the /tasks detail view) is tested in internal/cliapp.

package activeitemspanel

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	settings "github.com/justwasm/pigasus/internal/core/settings"
)

// harness is a panel over a task registry.
//
// The registry is a field so the seam can read it, and the detail view
// the panel opens is recorded so a test can assert it.
type harness struct {
	reg    *taskregistry.Registry
	panel  *Panel
	width  int
	height int
	vim    bool

	opened    bool
	openedID  string
	openedSub bool
}

// newHarness builds a panel over an empty registry.
func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		reg:    taskregistry.New(),
		width:  80,
		height: 30,
	}
	h.panel = New(Seams{
		Registry:   func() *taskregistry.Registry { return h.reg },
		Width:      func() int { return h.width },
		Height:     func() int { return h.height },
		VimEnabled: func() bool { return h.vim },
		OpenDetail: func(id string, isSubagent bool) {
			h.opened = true
			h.openedID = id
			h.openedSub = isSubagent
		},
	})
	return h
}

// tempHome points the settings store at a scratch directory.
//
// The "Show Active Tasks" gate reads settings.json, so a test that
// does not own a home would read the user's own file.
func tempHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
}

// runningTask is a running shell task that started a fixed time ago.
func runningTask(id, name string, started time.Time) *taskregistry.Task {
	return &taskregistry.Task{
		ID:        id,
		Name:      name,
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: started,
	}
}

// TestPanel_RowsEmpty — nothing in flight, nothing drawn.
func TestPanel_RowsEmpty(t *testing.T) {
	tempHome(t)
	h := newHarness(t)

	if got := h.panel.Rows(); got != "" {
		t.Fatalf("expected no rows without a running task, got %q", got)
	}
	if got := h.panel.Count(); got != 0 {
		t.Fatalf("expected count 0, got %d", got)
	}
}

// TestPanel_RowsTask — the task row is the reference's own.
//
// 【反编译】 render.RenderTaskItem (0x1028c8f30) renders the marker,
// the time cell, the name and the status cell — the record has no
// other string field.
func TestPanel_RowsTask(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	started := time.Date(2026, 11, 2, 15, 4, 5, 0, time.UTC)
	h.reg.Register(runningTask("task-1", "Running tests in background", started))

	rows := h.panel.Rows()
	for _, want := range []string{"●", "[15:04:05]", "Running tests in background", "running"} {
		if !strings.Contains(rows, want) {
			t.Errorf("expected the row to contain %q, got:\n%s", want, rows)
		}
	}
	// The id lives in the /tasks panel, not in this row.
	if strings.Contains(rows, "task-1") {
		t.Errorf("expected no task id in the row, got:\n%s", rows)
	}
	// The unfocused row carries the idle prefix.
	if !strings.HasPrefix(rows, "  ") {
		t.Errorf("expected the idle prefix, got:\n%s", rows)
	}
}

// TestPanel_RowsSubagent — the subagent row carries the tool info and
// the elapsed cell.
//
// 【反编译】 render.RenderSubagentItem (0x1028c8c30) ends the row with
// the elapsed cell, and the tool info reads "Agent(<name>)".
func TestPanel_RowsSubagent(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.reg.Register(&taskregistry.Task{
		ID:        "subagent-1001",
		Name:      "Search Shanghai Weather",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-15 * time.Second),
	})

	rows := h.panel.Rows()
	for _, want := range []string{"●", "Agent(Search Shanghai Weather)", "Processing..."} {
		if !strings.Contains(rows, want) {
			t.Errorf("expected the row to contain %q, got:\n%s", want, rows)
		}
	}
	if !regexp.MustCompile(`· \d+s`).MatchString(rows) {
		t.Errorf("expected the elapsed cell, got:\n%s", rows)
	}
	if strings.Contains(rows, "worked") {
		t.Errorf("expected the running form of the elapsed cell, got:\n%s", rows)
	}
}

// TestPanel_RowsCursor — the focused cursor's row carries the "> "
// prefix.
//
// 【反编译】 model.ActiveItemsModel.View (0x102bb9710) gives every row
// a two-cell prefix — "> " on the focused cursor's row, "  " on the
// others.
func TestPanel_RowsCursor(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.reg.Register(runningTask("task-1", "First", time.Now()))
	h.reg.Register(runningTask("task-2", "Second", time.Now()))

	h.panel.SetFocus(true)
	rows := h.panel.Rows()
	if !strings.Contains(rows, "> ") {
		t.Errorf("expected the cursor marker in the focused rows, got:\n%s", rows)
	}

	// The cursor moves with ↓, and the marker follows it.
	h.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected the cursor at 1, got %d", h.panel.Cursor())
	}
}

// TestPanel_RowsOverflow — the window's tails name the rows that do
// not fit.
func TestPanel_RowsOverflow(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.height = 30 // three rows fit
	for i := 0; i < 8; i++ {
		h.reg.Register(runningTask("task-"+string(rune('a'+i)), "Job", time.Now()))
	}

	rows := h.panel.Rows()
	if !strings.Contains(rows, "↓ 5 more") {
		t.Errorf("expected the tail to name the five hidden rows, got:\n%s", rows)
	}
	if strings.Contains(rows, "↑") {
		t.Errorf("expected no head tail at the top of the list, got:\n%s", rows)
	}

	// Moving the cursor down scrolls the window, and the head tail
	// appears.
	h.panel.SetFocus(true)
	for i := 0; i < 5; i++ {
		h.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	rows = h.panel.Rows()
	if !strings.Contains(rows, "↑ 3 more") {
		t.Errorf("expected the head tail to name the three rows above, got:\n%s", rows)
	}
	if !strings.Contains(rows, "↓ 2 more") {
		t.Errorf("expected the tail to name the two rows below, got:\n%s", rows)
	}
}

// TestPanel_KeyNav — the block's keys.
func TestPanel_KeyNav(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.reg.Register(runningTask("task-1", "First", time.Now()))
	h.reg.Register(runningTask("task-2", "Second", time.Now()))

	// A key the block does not own is not handled.
	if handled, _ := h.panel.HandleKey(tea.Key{Code: tea.KeyDown}); handled {
		t.Fatalf("expected an unfocused block to answer false")
	}

	h.panel.SetFocus(true)
	h.panel.SelectFirst()

	// ↓ moves the cursor.
	if handled, _ := h.panel.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatalf("expected ↓ to be handled")
	}
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected the cursor at 1, got %d", h.panel.Cursor())
	}
	// ↓ at the last row keeps the cursor.
	h.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected the cursor to stay at 1, got %d", h.panel.Cursor())
	}
	// ↑ moves it back.
	h.panel.HandleKey(tea.Key{Code: tea.KeyUp})
	if h.panel.Cursor() != 0 {
		t.Fatalf("expected the cursor at 0, got %d", h.panel.Cursor())
	}
	// ↑ at the first row gives the focus back to the prompt.
	if handled, _ := h.panel.HandleKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatalf("expected ↑ to be handled")
	}
	if h.panel.Focused() {
		t.Fatalf("expected the block to lose the focus at the top of the list")
	}

	// Esc gives the focus back to the prompt.
	h.panel.SetFocus(true)
	if handled, _ := h.panel.HandleKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatalf("expected Esc to be handled")
	}
	if h.panel.Focused() {
		t.Fatalf("expected the block to lose the focus on Esc")
	}

	// Enter opens the row's detail view.
	h.panel.SetFocus(true)
	if handled, _ := h.panel.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatalf("expected Enter to be handled")
	}
	if !h.opened {
		t.Fatalf("expected the detail view to open")
	}
	if h.openedID != "task-1" {
		t.Fatalf("expected the detail view of task-1, got %q", h.openedID)
	}
	if h.openedSub {
		t.Fatalf("expected a task, not a subagent")
	}

	// Any other key gives the focus back to the prompt and is not
	// handled — the letter reaches the composer.
	h.panel.SetFocus(true)
	if handled, _ := h.panel.HandleKey(tea.Key{Code: 'x', Text: "x"}); handled {
		t.Fatalf("expected an unowned key not to be handled")
	}
	if h.panel.Focused() {
		t.Fatalf("expected the block to lose the focus on an unowned key")
	}
}

// TestPanel_KeyVim — in vim mode "j" and "k" move the cursor.
func TestPanel_KeyVim(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.vim = true
	h.reg.Register(runningTask("task-1", "First", time.Now()))
	h.reg.Register(runningTask("task-2", "Second", time.Now()))

	h.panel.SetFocus(true)
	h.panel.SelectFirst()

	h.panel.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected the cursor at 1, got %d", h.panel.Cursor())
	}
	h.panel.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if h.panel.Cursor() != 0 {
		t.Fatalf("expected the cursor at 0, got %d", h.panel.Cursor())
	}
	// "k" at the top gives the focus back to the prompt.
	h.panel.HandleKey(tea.Key{Code: 'k', Text: "k"})
	if h.panel.Focused() {
		t.Fatalf("expected the block to lose the focus at the top of the list")
	}
}

// TestPanel_Window — the window is one row per ten cells of height.
func TestPanel_Window(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	for _, tc := range []struct{ height, want int }{
		{50, 5},
		{20, 2},
		{8, 1},
		{100, 5},
	} {
		h.height = tc.height
		h.panel.clamp(10, tc.height)
		if h.panel.maxVisible != tc.want {
			t.Errorf("height %d: expected %d visible rows, got %d", tc.height, tc.want, h.panel.maxVisible)
		}
	}
}

// TestPanel_Clamp — the cursor stays inside the list, and an empty
// list gives the focus back to the prompt.
func TestPanel_Clamp(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.reg.Register(runningTask("task-1", "First", time.Now()))

	h.panel.SetFocus(true)
	h.panel.cursor = 7
	h.panel.Clamp()
	if h.panel.Cursor() != 0 {
		t.Fatalf("expected the cursor to be clamped to 0, got %d", h.panel.Cursor())
	}
	if !h.panel.Focused() {
		t.Fatalf("expected the block to keep the focus")
	}

	// An empty list unfocuses the block.
	h.reg.Hide("task-1")
	h.panel.Clamp()
	if h.panel.Focused() {
		t.Fatalf("expected the block to lose the focus with an empty list")
	}
}

// TestPanel_Hidden — the "Show Active Tasks" gate.
//
// The setting is the reference's own: the block is not drawn when
// "Show Active Tasks" is switched off.
func TestPanel_Hidden(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	h.reg.Register(runningTask("task-1", "First", time.Now()))

	if h.panel.Hidden() {
		t.Fatalf("expected the block to be visible by default")
	}
	if err := settings.SetField("hideActiveItems", true); err != nil {
		t.Fatalf("SetField failed: %v", err)
	}
	if !h.panel.Hidden() {
		t.Fatalf("expected the block to be hidden")
	}
	if got := h.panel.Rows(); got != "" {
		t.Fatalf("expected no rows while the block is hidden, got %q", got)
	}
	// The count still answers what is in flight.
	if got := h.panel.Count(); got != 1 {
		t.Fatalf("expected the count to stay 1, got %d", got)
	}
}

// TestPanel_Nil — a host that never built the block is safe.
func TestPanel_Nil(t *testing.T) {
	var p *Panel
	if p.Focused() {
		t.Fatalf("expected a nil panel not to be focused")
	}
	p.SetFocus(true)
	p.SelectFirst()
	p.Clamp()
	if got := p.Rows(); got != "" {
		t.Fatalf("expected a nil panel to write nothing, got %q", got)
	}
	if got := p.Count(); got != 0 {
		t.Fatalf("expected a nil panel to count nothing, got %d", got)
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); handled {
		t.Fatalf("expected a nil panel to handle nothing")
	}
	if p.Cursor() != 0 {
		t.Fatalf("expected a nil panel's cursor at 0")
	}
}

// TestPanel_NavigateMultipleSubagents verifies navigation and selection
// across running and historical (done, failed) subagents.
func TestPanel_NavigateMultipleSubagents(t *testing.T) {
	tempHome(t)
	h := newHarness(t)
	now := time.Now()

	h.reg.Register(&taskregistry.Task{
		ID:        "sub-1",
		Name:      "Subagent 1",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: now.Add(-30 * time.Second),
	})
	h.reg.Register(&taskregistry.Task{
		ID:        "sub-2",
		Name:      "Subagent 2",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateDone,
		StartedAt: now.Add(-20 * time.Second),
		EndedAt:   now.Add(-10 * time.Second),
	})
	h.reg.Register(&taskregistry.Task{
		ID:        "sub-3",
		Name:      "Subagent 3",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateFailed,
		StartedAt: now.Add(-10 * time.Second),
	})

	if count := h.panel.Count(); count != 3 {
		t.Fatalf("expected count 3, got %d", count)
	}

	h.panel.SetFocus(true)
	h.panel.SelectFirst()
	if h.panel.Cursor() != 0 {
		t.Fatalf("expected cursor at 0, got %d", h.panel.Cursor())
	}

	// Move to sub-2 (Done).
	h.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected cursor at 1, got %d", h.panel.Cursor())
	}

	// Move to sub-3 (Failed).
	h.panel.HandleKey(tea.Key{Code: tea.KeyDown})
	if h.panel.Cursor() != 2 {
		t.Fatalf("expected cursor at 2, got %d", h.panel.Cursor())
	}

	// Enter opens sub-3.
	h.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !h.opened || h.openedID != "sub-3" || !h.openedSub {
		t.Fatalf("expected sub-3 detail opened, got opened=%v id=%q isSub=%v", h.opened, h.openedID, h.openedSub)
	}

	// Move back up to sub-2 (Done).
	h.panel.HandleKey(tea.Key{Code: tea.KeyUp})
	if h.panel.Cursor() != 1 {
		t.Fatalf("expected cursor at 1 after KeyUp, got %d", h.panel.Cursor())
	}

	// Enter opens sub-2 (historical done subagent).
	h.opened = false
	h.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !h.opened || h.openedID != "sub-2" || !h.openedSub {
		t.Fatalf("expected sub-2 detail opened, got opened=%v id=%q isSub=%v", h.opened, h.openedID, h.openedSub)
	}
}
