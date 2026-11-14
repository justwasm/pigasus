package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/views/swarmboard"
	"github.com/justwasm/pigasus/internal/ui/views/taskspanel"
)

func TestSwarmBoard_SlashAndToggle(t *testing.T) {
	m := diffTestModel()
	initRootModelInput(m)
	m.activeItems.Panel = m.newActiveItemsPanel()
	m.swarm.Board = m.newSwarmBoard()

	if m.swarm.Board.Active() {
		t.Fatal("swarm board should initially be closed")
	}

	// 1. /swarm board toggles the board open
	handled, _ := m.handleSlashCommand("/swarm board")
	if !handled {
		t.Fatal("expected /swarm board to be handled")
	}
	if !m.swarm.Board.Active() {
		t.Fatal("expected /swarm board to activate swarm board")
	}

	// 2. Esc closes the board
	_, _ = m.handleEscKeyMsg()
	if m.swarm.Board.Active() {
		t.Fatal("expected Esc to close swarm board")
	}

	// 3. alt+s toggles the board open
	_, _, handledAltS := m.handleGlobalOrModalKeys(tea.Key{Code: 's', Mod: tea.ModAlt})
	if !handledAltS || !m.swarm.Board.Active() {
		t.Fatal("expected alt+s to toggle board open")
	}

	// 4. While open, keyboard keys route to swarm board
	m.swarm.Board.ApplySnapshot(&swarmboard.SwarmSnapshot{
		Title: "Refactor",
		Members: []swarmboard.Member{
			{Name: "alice", Role: "worker", State: "working"},
			{Name: "bob", Role: "reviewer", State: "waiting"},
		},
		Posts: []swarmboard.Post{
			{Seq: 1, Author: "coordinator", Body: "@alice please inspect module"},
		},
	})
	// Tab moves focus between panes
	m.handleGlobalOrModalKeys(tea.Key{Code: tea.KeyTab})
	if m.swarm.Board.FocusPane() != swarmboard.PanePosts {
		t.Fatalf("expected focused pane = Posts, got %d", m.swarm.Board.FocusPane())
	}

	// Esc closes board
	m.handleGlobalOrModalKeys(tea.Key{Code: tea.KeyEsc})
	if m.swarm.Board.Active() {
		t.Fatal("expected Esc to close board")
	}
}

func TestSwarmTrayRow_Integration(t *testing.T) {
	m := diffTestModel()
	initRootModelInput(m)
	m.activeItems.Panel = m.newActiveItemsPanel()
	m.swarm.Board = m.newSwarmBoard()

	m.swarm.Board.ApplySnapshot(&swarmboard.SwarmSnapshot{
		Title: "Code Cleanup",
		Members: []swarmboard.Member{
			{Name: "worker-1", State: "working"},
		},
		Posts: []swarmboard.Post{
			{Seq: 1, Author: "coordinator", Body: "Start cleaning up"},
		},
		IsDone: false,
	})

	item := m.SwarmItem()
	if item == nil {
		t.Fatal("expected non-nil SwarmItem")
	}
	if item.Title != "Code Cleanup" || item.PostCount != 1 {
		t.Fatalf("unexpected SwarmItem: %+v", item)
	}

	// Render active items tray
	rows := m.activeItems.Panel.Rows()
	if !strings.Contains(rows, "Swarm(Code Cleanup)") {
		t.Fatalf("tray rows missing Swarm(Code Cleanup): %q", rows)
	}

	// Alt+x dismisses swarm
	_, _, handledAltX := m.handleGlobalOrModalKeys(tea.Key{Code: 'x', Mod: tea.ModAlt})
	if !handledAltX {
		t.Fatal("expected alt+x to be handled")
	}
	if m.SwarmItem() != nil {
		t.Fatal("expected SwarmItem to be nil after dismissal")
	}
	if strings.Contains(m.activeItems.Panel.Rows(), "Swarm") {
		t.Fatalf("tray rows should not contain dismissed swarm: %q", m.activeItems.Panel.Rows())
	}
}

// TestSwarmBoard_DetailEscReturnsToBoard pins the AGY 1.5.6 fix:
// "Fixed pressing esc in a swarm member's conversation opened from /swarm board
// with o dropping you back at the prompt; it now returns to the board with the
// same selection, filter, focused pane, and zoom."
func TestSwarmBoard_DetailEscReturnsToBoard(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.OpenSwarmBoard()
	if !m.swarm.Board.Active() {
		t.Fatal("expected swarm board to be open")
	}

	// Apply snapshot
	m.swarm.Board.ApplySnapshot(&swarmboard.SwarmSnapshot{
		Title: "Test Swarm",
		Members: []swarmboard.Member{
			{Name: "worker-1", Role: "Builder", State: "working", ConversationID: "conv-w1"},
		},
	})

	// Press 'o' to open member subagent detail
	m.handleGlobalOrModalKeys(tea.Key{Code: 'o', Text: "o"})
	if !m.swarm.fromBoard {
		t.Fatal("expected swarm.fromBoard to be set")
	}
	if !m.tasksPanel().Detail() {
		t.Fatal("expected tasks panel detail to be open")
	}

	// Press Esc in task detail view -> sends taskspanel.DismissedMsg
	m.Update(taskspanel.DismissedMsg{FromActiveItems: true})

	// Must return back to the Swarm Board
	if !m.swarm.Board.Active() {
		t.Fatal("expected swarm board to be reopened after Esc from member detail")
	}
	if m.swarm.fromBoard {
		t.Fatal("expected swarm.fromBoard to be reset")
	}
}
