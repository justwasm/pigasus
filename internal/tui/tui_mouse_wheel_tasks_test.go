package tui

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestTasksPanel_MouseWheelNavigationAndDetailScroll(t *testing.T) {
	m := seedRootModel()
	m.viewport.altScreen = true
	m.viewport.width = 80
	m.viewport.height = 24
	m.session.cwd = "/tmp/test"
	m.activeItems.Panel = m.newActiveItemsPanel()

	m.tasks.reg.Register(&taskregistry.Task{
		ID:        "t-1",
		Name:      "task 1",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-10 * time.Second),
	})
	m.tasks.reg.Register(&taskregistry.Task{
		ID:        "t-2",
		Name:      "task 2",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-5 * time.Second),
	})

	// Open /tasks panel
	if !m.openSlashPanel("tasks") {
		t.Fatal("openSlashPanel(tasks) = false, want true")
	}

	tp := m.tasksPanel()
	if tp == nil {
		t.Fatal("tasksPanel is nil")
	}
	if tp.Cursor() != 0 {
		t.Fatalf("initial cursor = %d, want 0", tp.Cursor())
	}

	// 1. Wheel down moves cursor down in list view
	wheelDown := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})
	if !m.handleMouseWheel(wheelDown) {
		t.Fatal("handleMouseWheel(down) returned false")
	}
	if tp.Cursor() != 1 {
		t.Errorf("cursor after wheel down = %d, want 1", tp.Cursor())
	}

	// 2. Wheel up moves cursor up in list view
	wheelUp := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp})
	if !m.handleMouseWheel(wheelUp) {
		t.Fatal("handleMouseWheel(up) returned false")
	}
	if tp.Cursor() != 0 {
		t.Errorf("cursor after wheel up = %d, want 0", tp.Cursor())
	}

	// 3. Open detail view
	tp.OpenTaskDetail("t-1", false)
	if !tp.Detail() {
		t.Fatal("expected detail view to be open")
	}

	// 4. Wheel down in detail view scrolls body
	if !m.handleMouseWheel(wheelDown) {
		t.Fatal("handleMouseWheel(down) in detail returned false")
	}

	// 5. Wheel up in detail view scrolls body back
	if !m.handleMouseWheel(wheelUp) {
		t.Fatal("handleMouseWheel(up) in detail returned false")
	}
}

func TestActiveItems_DetailEscReturnsDirectlyToActiveItems(t *testing.T) {
	m := seedRootModel()
	m.viewport.altScreen = true
	m.viewport.width = 80
	m.viewport.height = 24
	m.session.cwd = "/tmp/test"
	m.activeItems.Panel = m.newActiveItemsPanel()

	m.tasks.reg.Register(&taskregistry.Task{
		ID:        "t-1",
		Name:      "sleep 100",
		Kind:      taskregistry.KindShell,
		State:     taskregistry.StateRunning,
		StartedAt: time.Now().Add(-10 * time.Second),
	})

	// Focus active items
	m.activeItems.Panel.SetFocus(true)
	if !m.activeItems.Panel.Focused() {
		t.Fatal("expected active items focused")
	}

	// 1. Enter on active item opens detail directly
	m.openTasksDetail("t-1", false)
	if m.overlays.panel.kind != panelregistry.Tasks {
		t.Fatalf("panel kind = %v, want Tasks", m.overlays.panel.kind)
	}
	if !m.tasksPanel().Detail() {
		t.Fatal("expected detail view open")
	}
	if !m.tasksPanel().FromActiveItems() {
		t.Fatal("expected fromActiveItems = true")
	}

	// 2. Esc from direct detail view closes panel and refocusses active items
	initialRespLen := len(m.conversation.response)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd != nil {
		m.Update(cmd())
	}

	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("expected panel closed after Esc, got %v", m.overlays.panel.kind)
	}
	if !m.activeItems.Panel.Focused() {
		t.Fatal("expected active items to be refocused after Esc from direct detail")
	}
	if len(m.conversation.response) != initialRespLen {
		t.Fatalf("expected no exit notice in conversation, got %d responses (was %d)", len(m.conversation.response), initialRespLen)
	}

	// 3. Open via /tasks command
	m.activeItems.Panel.SetFocus(false)
	m.openSlashPanel("tasks")
	if m.overlays.panel.kind != panelregistry.Tasks {
		t.Fatalf("panel kind = %v, want Tasks", m.overlays.panel.kind)
	}

	// Enter detail from within /tasks
	m.tasksPanel().SetCursor(0)
	handled, _ := m.tasksPanel().HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || !m.tasksPanel().Detail() {
		t.Fatal("expected detail view open from /tasks list")
	}
	if m.tasksPanel().FromActiveItems() {
		t.Fatal("expected fromActiveItems = false when opened from /tasks list")
	}

	// Esc in detail view returns to /tasks list (panel remains open)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.overlays.panel.kind != panelregistry.Tasks {
		t.Fatalf("expected panel still open after first Esc from /tasks detail, got %v", m.overlays.panel.kind)
	}
	if m.tasksPanel().Detail() {
		t.Fatal("expected detail view closed after first Esc")
	}

	// Esc in /tasks list closes panel and adds exit notice
	_, cmd = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd != nil {
		m.Update(cmd())
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("expected panel closed after second Esc, got %v", m.overlays.panel.kind)
	}
	if len(m.conversation.response) <= initialRespLen {
		t.Fatal("expected exit notice in conversation after exiting /tasks command")
	}
	if m.activeItems.Panel.Focused() {
		t.Fatal("expected active items NOT focused after exiting /tasks command")
	}
}

func TestAgentsPanel_MouseWheelNavigationAndDetailScroll(t *testing.T) {
	m := seedRootModel()
	m.viewport.altScreen = true
	m.viewport.width = 80
	m.viewport.height = 24
	m.session.cwd = "/tmp/test"

	if !m.openSlashPanel("agents") {
		t.Fatal("openSlashPanel(agents) = false, want true")
	}

	ag := m.agentsPanel()
	if ag == nil {
		t.Fatal("agentsPanel is nil")
	}
	ag.SetCursor(0)

	wheelDown := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})
	wheelUp := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelUp})

	// 1. Wheel down moves cursor down in /agents
	if !m.handleMouseWheel(wheelDown) {
		t.Fatal("handleMouseWheel(down) returned false")
	}
	if ag.Cursor() <= 0 && ag.RowCount() > 1 {
		t.Errorf("cursor after wheel down = %d, want > 0", ag.Cursor())
	}

	// 2. Wheel up moves cursor back up
	if !m.handleMouseWheel(wheelUp) {
		t.Fatal("handleMouseWheel(up) returned false")
	}
	if ag.Cursor() != 0 {
		t.Errorf("cursor after wheel up = %d, want 0", ag.Cursor())
	}
}

func TestModalOverlays_AbsorbMouseWheelAndNeverLeak(t *testing.T) {
	m := seedRootModel()
	m.viewport.altScreen = true
	m.viewport.width = 80
	m.viewport.height = 24
	m.session.cwd = "/tmp/test"
	initialScroll := m.viewport.scroll

	wheelDown := tea.MouseWheelMsg(tea.Mouse{Button: tea.MouseWheelDown})

	for _, panelCmd := range []string{"skills", "model", "effort", "permissions", "hooks"} {
		if !m.openSlashPanel(panelCmd) {
			continue
		}
		if m.overlays.panel.kind == panelregistry.None {
			t.Fatalf("expected panel %s open", panelCmd)
		}

		// Mouse wheel must be absorbed by the modal and never change m.viewport.scroll
		handled := m.handleMouseWheel(wheelDown)
		if !handled {
			t.Fatalf("handleMouseWheel returned false for modal panel %s", panelCmd)
		}
		if m.viewport.scroll != initialScroll {
			t.Fatalf("mouse wheel leaked to transcript scroll in panel %s: got %d, want %d", panelCmd, m.viewport.scroll, initialScroll)
		}

		m.closePanel()
	}
}
