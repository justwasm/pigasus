// tui_active_items_test.go — the host's side of the active-items
// block.
//
// The block itself is a panel now
// (internal/ui/views/activeitemspanel): its own tests live there.
// This file pins the host's wiring — the alt+j that toggles the
// focus, the prompt's ↓ that hands the focus over, the key routing
// while the block owns the keyboard, the /tasks detail view and the
// block's place in the prompt's layout.

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// activeItemsHost builds a host with a task registry and the
// active-items block wired to it.
func activeItemsHost(t *testing.T, height int) *RootModel {
	t.Helper()
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)
	_ = os.WriteFile(filepath.Join(tempHome, "settings.json"), []byte("{}"), 0644)

	m := &RootModel{
		tasks: TasksModel{
			reg: taskregistry.New(),
		},
		prompt: PromptModel{
			input: tas.New(),
		},
		viewport: uiState{
			width:  80,
			height: height,
		},
	}
	m.activeItems.Panel = m.newActiveItemsPanel()
	m.prompt.input.Focus()
	return m
}

// TestActiveItems_HostFocusAndRows — alt+j takes the focus, the rows
// carry the reference's own cells, and Esc gives the focus back.
func TestActiveItems_HostFocusAndRows(t *testing.T) {
	m := activeItemsHost(t, 30)

	// 1. Nothing running -> no rows.
	if got := m.activeItems.Panel.Rows(); got != "" {
		t.Fatalf("expected no rows when no tasks running, got %q", got)
	}

	// 2. Register a running task and a subagent.
	t1 := &taskregistry.Task{
		ID:        "subagent-1001",
		Name:      "Search Shanghai Weather",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-15 * time.Second),
	}
	t2 := &taskregistry.Task{
		ID:        "task-1002",
		Name:      "Running tests in background",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	m.tasks.reg.Register(t1)
	m.tasks.reg.Register(t2)

	rows := m.activeItems.Panel.Rows()
	if !strings.Contains(rows, "Search Shanghai Weather") {
		t.Errorf("expected rows to contain 'Search Shanghai Weather', got:\n%s", rows)
	}
	if !strings.Contains(rows, "Running tests in background") {
		t.Errorf("expected rows to contain 'Running tests in background', got:\n%s", rows)
	}
	if !strings.Contains(rows, "●") {
		t.Errorf("expected bullet ● in rows, got:\n%s", rows)
	}
	// The reference's task row carries neither the task's id nor an
	// elapsed time — the id only lives in the /tasks panel.
	//
	// 【反编译】 render.RenderTaskItem (0x1028c8f30) renders the
	// marker, the time cell, the name and the status cell — the
	// record has no other string field.
	//
	// The subagent row is the reference's own and it does carry the
	// elapsed time: render.RenderSubagentItem (0x1028c8c30) ends it
	// with " · 15s" while the subagent is still running — the bare
	// duration; a finished one shows " · worked 15s" instead.
	if strings.Contains(rows, "subagent-1001") {
		t.Errorf("expected no task id in the row, got:\n%s", rows)
	}
	if strings.Contains(rows, "task-1002") {
		t.Errorf("expected no task id in the row, got:\n%s", rows)
	}
	if !strings.Contains(rows, "Agent(Search Shanghai Weather)") {
		t.Errorf("expected the subagent row's tool info, got:\n%s", rows)
	}
	if !strings.Contains(rows, "· 15s") {
		t.Errorf("expected the subagent row's elapsed cell, got:\n%s", rows)
	}
	if strings.Contains(rows, "worked") {
		t.Errorf("expected the running form of the elapsed cell, got:\n%s", rows)
	}
	if !strings.Contains(rows, "Processing...") {
		t.Errorf("expected the subagent row's status word, got:\n%s", rows)
	}

	// 3. alt+j takes the focus.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModAlt, Text: "j"}))
	if !m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to be focused after alt+j")
	}

	// The focused view carries the selection marker "> " on the first
	// item.
	focusedRows := m.activeItems.Panel.Rows()
	if !strings.Contains(focusedRows, "> ") {
		t.Errorf("expected cursor marker '> ' in focused rows, got:\n%s", focusedRows)
	}

	// Down arrow moves the cursor to the second item.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.activeItems.Panel.Cursor() != 1 {
		t.Errorf("expected cursor at 1 after KeyDown, got %d", m.activeItems.Panel.Cursor())
	}

	// Esc un-focuses activeItems.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to lose focus after Esc")
	}

	// 4. Enter opens the task detail view in the /tasks panel.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModAlt, Text: "j"}))
	m.activeItems.Panel.SelectFirst()
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.overlays.panel.kind != panelregistry.Tasks {
		t.Errorf("expected panelregistry.Tasks to be open, got %v", m.overlays.panel.kind)
	}
	if m.overlays.panel.tp == nil || !m.overlays.panel.tp.Detail() {
		t.Errorf("expected /tasks panel to be in detail view")
	}
	if m.overlays.panel.tp.DetailID() != "subagent-1001" {
		t.Errorf("expected detail ID 'subagent-1001', got %q", m.overlays.panel.tp.DetailID())
	}
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to be unfocused after opening detail view")
	}

	// Close the panel.
	m.overlays.panel = slashPanel{}
}

// TestActiveItems_HostDownFromPrompt — the prompt's ↓ hands the focus
// to the block, and ↑ at the top gives it back.
func TestActiveItems_HostDownFromPrompt(t *testing.T) {
	m := activeItemsHost(t, 30)
	m.tasks.reg.Register(&taskregistry.Task{
		ID:        "task-1",
		Name:      "Job",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	})

	// 5a. Down arrow on empty input focuses ActiveItems
	m.prompt.input.SetValue("")
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems unfocused before Down arrow")
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if !m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to gain focus on Down arrow from empty prompt")
	}
	if m.activeItems.Panel.Cursor() != 0 {
		t.Errorf("expected cursor at 0 after Down arrow focus switch, got %d", m.activeItems.Panel.Cursor())
	}

	// 5b. Up arrow at cursor 0 defocuses ActiveItems back to prompt
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to lose focus on Up arrow at cursor 0")
	}

	// 5c. Multi-line input: Down arrow only switches focus when on last line
	m.prompt.input.SetValue("line 1\nline 2")
	m.prompt.SetInputCursorOffset(0) // line 1
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected Down arrow on line 1 of multi-line prompt NOT to focus active items")
	}
	// Cursor is now on line 2 (the last line)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if !m.activeItems.Panel.Focused() {
		t.Errorf("expected Down arrow on last line of multi-line prompt to focus active items")
	}
	m.activeItems.Panel.SetFocus(false)
	m.prompt.input.SetValue("")
}

// TestActiveItems_DefocusOnTypingAndNoKill — the block owns no key of
// its own: a letter reaches the composer and nothing is killed.
func TestActiveItems_DefocusOnTypingAndNoKill(t *testing.T) {
	m := activeItemsHost(t, 30)

	task := &taskregistry.Task{
		ID:        "task-2001",
		Name:      "Compilation job",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	}
	m.tasks.reg.Register(task)

	// Focus active items
	m.activeItems.Panel.SetFocus(true)
	m.activeItems.Panel.SelectFirst()

	// 1. Pressing 'x' or 'k' must NOT kill the task
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	gotTask := m.tasks.reg.Get("task-2001")
	if gotTask == nil || gotTask.State != taskregistry.StateRunning {
		t.Fatalf("task was unexpectedly killed on 'x' key press!")
	}
	// 'x' was an unhandled key, so it must have defocused active items
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to defocus on arbitrary letter 'x'")
	}

	// 2. Typing characters when focused defocusses active items and types into input
	m.activeItems.Panel.SetFocus(true)
	m.prompt.input.SetValue("")
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to defocus when typing 'a'")
	}
	if m.prompt.input.Value() != "a" {
		t.Errorf("expected input to receive 'a', got %q", m.prompt.input.Value())
	}
}

// TestActiveItems_VimModeNavigation — in vim mode 'j' focuses the
// block and moves its cursor.
func TestActiveItems_VimModeNavigation(t *testing.T) {
	m := activeItemsHost(t, 30)
	m.prompt.vimOn = true // Enable vim mode

	t1 := &taskregistry.Task{
		ID:        "task-1",
		Name:      "Job 1",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-10 * time.Second),
	}
	t2 := &taskregistry.Task{
		ID:        "task-2",
		Name:      "Job 2",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-5 * time.Second),
	}
	m.tasks.reg.Register(t1)
	m.tasks.reg.Register(t2)

	// In vim Normal mode, pressing 'j' on empty prompt focuses active items
	m.prompt.input.SetValue("")
	m.prompt.vimEng = nil // reset vim engine
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if !m.activeItems.Panel.Focused() {
		t.Fatalf("expected 'j' in vim normal mode on empty prompt to focus active items")
	}
	if m.activeItems.Panel.Cursor() != 0 {
		t.Fatalf("expected cursor at 0, got %d", m.activeItems.Panel.Cursor())
	}

	// In active items, pressing 'j' moves cursor down to item 1
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if m.activeItems.Panel.Cursor() != 1 {
		t.Errorf("expected cursor at 1 after 'j', got %d", m.activeItems.Panel.Cursor())
	}

	// In active items, pressing 'k' moves cursor up to item 0
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'k', Text: "k"}))
	if m.activeItems.Panel.Cursor() != 0 {
		t.Errorf("expected cursor at 0 after 'k', got %d", m.activeItems.Panel.Cursor())
	}

	// In active items, pressing 'k' at cursor 0 defocuses back to prompt
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'k', Text: "k"}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected activeItems to lose focus after 'k' at cursor 0")
	}

	// Test multi-line buffer in vim mode: 'j' moves line down first before focusing active items
	m.prompt.input.SetValue("first\nsecond")
	m.prompt.vimEng = nil
	eng := m.vimEngine()
	eng.Row = 0 // first line
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if m.activeItems.Panel.Focused() {
		t.Errorf("expected 'j' on line 0 to move to line 1, not focus active items")
	}
	if m.vimEngine().Row != 1 {
		t.Errorf("expected vim row to be 1, got %d", m.vimEngine().Row)
	}

	// Now on line 1 (the last line), 'j' should focus active items
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Text: "j"}))
	if !m.activeItems.Panel.Focused() {
		t.Errorf("expected 'j' on last line of vim buffer to focus active items")
	}
}

// TestActiveItems_HostHiddenGate — the "Show Active Tasks" setting
// suppresses the block.
func TestActiveItems_HostHiddenGate(t *testing.T) {
	m := activeItemsHost(t, 30)
	m.tasks.reg.Register(&taskregistry.Task{
		ID:        "task-1",
		Name:      "Job",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	})

	if got := m.activeItems.Panel.Rows(); got == "" {
		t.Fatalf("expected the block to be drawn")
	}
	if err := settings.SetField("hideActiveItems", true); err != nil {
		t.Fatalf("SetField failed: %v", err)
	}
	if got := m.activeItems.Panel.Rows(); got != "" {
		t.Errorf("expected the block to be suppressed when hideActiveItems=true, got %q", got)
	}
}

// TestActiveItems_PromptLayoutThreeLines — the block sits under the
// input box, between two rules.
func TestActiveItems_PromptLayoutThreeLines(t *testing.T) {
	m := activeItemsHost(t, 24)

	// 1. Without active tasks: exactly 2 horizontal lines enclosing the input box.
	var b1 strings.Builder
	m.renderBodyPrompt(&b1)
	rendered1 := b1.String()
	sepCount1 := strings.Count(rendered1, m.separator())
	if sepCount1 != 2 {
		t.Errorf("expected 2 separator lines without active tasks, got %d in:\n%s", sepCount1, rendered1)
	}

	// 2. With active tasks: 3 horizontal lines separating input field and active items list.
	task := &taskregistry.Task{
		ID:        "subagent-1",
		Name:      "Alpha Demo Agent",
		Kind:      taskregistry.KindSubagent,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now(),
	}
	m.tasks.reg.Register(task)

	var b2 strings.Builder
	m.renderBodyPrompt(&b2)
	rendered2 := b2.String()
	sepCount2 := strings.Count(rendered2, m.separator())
	if sepCount2 != 3 {
		t.Errorf("expected 3 separator lines with active tasks, got %d in:\n%s", sepCount2, rendered2)
	}

	// Verify the relative ordering of the sections:
	// Line 1: top rule
	// Line 2: input field
	// Line 3: middle rule (closes input field)
	// Line 4: active items row
	// Line 5: bottom rule (closes active items)
	lines := strings.Split(strings.TrimSpace(rendered2), "\n")
	ruleIndices := []int{}
	taskRowIdx := -1
	for idx, line := range lines {
		if strings.Contains(line, strings.Repeat("─", 40)) {
			ruleIndices = append(ruleIndices, idx)
		}
		if strings.Contains(line, "Agent(Alpha Demo Agent)") {
			taskRowIdx = idx
		}
	}
	if len(ruleIndices) != 3 {
		t.Fatalf("expected 3 rule lines, got %d (indices %v)", len(ruleIndices), ruleIndices)
	}
	if taskRowIdx == -1 {
		t.Fatalf("active task row not found in rendered prompt:\n%s", rendered2)
	}
	// Task row must be AFTER rule 2 and BEFORE rule 3.
	if taskRowIdx <= ruleIndices[1] || taskRowIdx >= ruleIndices[2] {
		t.Errorf("active task row at line %d must be between rule 2 (line %d) and rule 3 (line %d)",
			taskRowIdx, ruleIndices[1], ruleIndices[2])
	}
}

// TestActiveItems_TimerTaskRow pins that timer tasks show the OpenCode-style
// auto-followup notice in the active items row.
func TestActiveItems_TimerTaskRow(t *testing.T) {
	m := activeItemsHost(t, 24)
	target := time.Date(2026, 11, 4, 18, 17, 0, 0, time.Local)
	task := &taskregistry.Task{
		ID:         "timer-1",
		Name:       "Health check probe",
		Kind:       taskregistry.KindTimer,
		State:      taskregistry.StateRunning,
		StartedAt:  time.Now(),
		TargetTime: target,
	}
	m.tasks.reg.Register(task)

	rows := m.activeItems.Panel.Rows()
	if !strings.Contains(rows, "⏳ 将在 18:17:00 自动探测并恢复跟进") {
		t.Errorf("expected timer notice in active items rows, got: %q", rows)
	}
}
