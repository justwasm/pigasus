package swarmboard

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func sampleSwarmSnapshot() *SwarmSnapshot {
	return &SwarmSnapshot{
		Title: "The Parser Rewrite Swarm",
		Members: []Member{
			{Name: "coordinator", State: "working", Model: "gemini-2.5-pro", IsCoordinator: true, ConversationID: "conv-coord"},
			{Name: "parser", State: "working", Model: "gemini-2.5-flash", Progress: 60, ActiveTurns: 5, ConversationID: "conv-parser"},
			{Name: "verifier", State: "waiting", Model: "gemini-2.5-flash", Progress: 100, ActiveTurns: 3, ConversationID: "conv-verifier"},
			{Name: "tester", State: "done", Model: "gemini-2.5-flash", Progress: 100, ActiveTurns: 2, ConversationID: "conv-tester"},
		},
		Posts: []Post{
			{Seq: 1, Author: "coordinator", Body: "@parser Rewrite AST parser logic.", CreatedAt: time.Now()},
			{Seq: 2, Author: "parser", Body: "@verifier AST parser complete. Please verify.", CreatedAt: time.Now()},
		},
		Monitor: &MonitorStatus{
			Enabled:        true,
			State:          "healthy",
			LastHeartbeat:  time.Now(),
			StalledWorkers: nil,
			AlertCount:     0,
			Message:        "Swarm loop healthy; throughput 12 turns/min",
		},
	}
}

func TestBoard_PartitionedLayoutToggle(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()

	if b.Partitioned() {
		t.Fatal("expected default board layout to not be partitioned")
	}

	// 1. Toggle via SetPartitioned
	b.SetPartitioned(true)
	if !b.Partitioned() {
		t.Fatal("expected Partitioned() == true after SetPartitioned(true)")
	}

	// 2. Toggle via key 'm'
	b.HandleKey(tea.Key{Code: 'm', Text: "m"})
	if b.Partitioned() {
		t.Fatal("expected Partitioned() == false after pressing 'm'")
	}
	b.HandleKey(tea.Key{Code: 'm', Text: "m"})
	if !b.Partitioned() {
		t.Fatal("expected Partitioned() == true after pressing 'm' again")
	}
}

func TestBoard_DirectNumberKeyNavigation(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()
	b.ApplySnapshot(sampleSwarmSnapshot())
	b.SetPartitioned(true)

	// '1' -> Coordinator
	b.HandleKey(tea.Key{Code: '1', Text: "1"})
	if b.FocusPane() != PaneCoordinator {
		t.Errorf("FocusPane after '1' = %d, want %d (PaneCoordinator)", b.FocusPane(), PaneCoordinator)
	}

	// '2' -> Workers
	b.HandleKey(tea.Key{Code: '2', Text: "2"})
	if b.FocusPane() != PaneWorkers {
		t.Errorf("FocusPane after '2' = %d, want %d (PaneWorkers)", b.FocusPane(), PaneWorkers)
	}

	// '3' -> Monitor
	b.HandleKey(tea.Key{Code: '3', Text: "3"})
	if b.FocusPane() != PaneMonitor {
		t.Errorf("FocusPane after '3' = %d, want %d (PaneMonitor)", b.FocusPane(), PaneMonitor)
	}

	// '4' -> Posts
	b.HandleKey(tea.Key{Code: '4', Text: "4"})
	if b.FocusPane() != PanePosts {
		t.Errorf("FocusPane after '4' = %d, want %d (PanePosts)", b.FocusPane(), PanePosts)
	}

	// '5' -> Message
	b.HandleKey(tea.Key{Code: '5', Text: "5"})
	if b.FocusPane() != PaneMessage {
		t.Errorf("FocusPane after '5' = %d, want %d (PaneMessage)", b.FocusPane(), PaneMessage)
	}
}

func TestBoard_PartitionedTabCycling(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()
	b.ApplySnapshot(sampleSwarmSnapshot())
	b.SetPartitioned(true)

	b.focusPane = PaneCoordinator

	// Forward cycle: Coordinator (0) -> Workers (3) -> Monitor (4) -> Posts (1) -> Message (2) -> Coordinator (0)
	expectedSequence := []int{PaneWorkers, PaneMonitor, PanePosts, PaneMessage, PaneCoordinator}
	for i, want := range expectedSequence {
		b.HandleKey(tea.Key{Code: tea.KeyTab})
		if b.FocusPane() != want {
			t.Fatalf("step %d: FocusPane = %d, want %d", i, b.FocusPane(), want)
		}
	}

	// Backward cycle with Shift+Tab: Coordinator (0) -> Message (2)
	b.HandleKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if b.FocusPane() != PaneMessage {
		t.Fatalf("Shift+Tab from Coordinator: FocusPane = %d, want %d", b.FocusPane(), PaneMessage)
	}
}

func TestBoard_PartitionedRendering_Coordinator(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()
	snap := sampleSwarmSnapshot()
	b.ApplySnapshot(snap)
	b.SetPartitioned(true)

	view := b.View()
	if !strings.Contains(view, "[1] COORDINATOR") {
		t.Errorf("View missing '[1] COORDINATOR':\n%s", view)
	}
	if !strings.Contains(view, "The Parser Rewrite Swarm") {
		t.Errorf("View missing swarm goal/title")
	}
	if !strings.Contains(view, "coordinator") {
		t.Errorf("View missing coordinator name")
	}
	if !strings.Contains(view, "gemini-2.5-pro") {
		t.Errorf("View missing coordinator model")
	}
	if !strings.Contains(view, "Tasks:") {
		t.Errorf("View missing htop-style tasks line")
	}
}

func TestBoard_PartitionedRendering_Workers_And_OpenSubagent(t *testing.T) {
	var openedConv string
	b := NewBoard(func(convID string) {
		openedConv = convID
	})
	b.SetSize(100, 30)
	b.Open()
	snap := sampleSwarmSnapshot()
	b.ApplySnapshot(snap)
	b.SetPartitioned(true)

	// Focus workers
	b.HandleKey(tea.Key{Code: '2', Text: "2"})
	view := b.View()

	if !strings.Contains(view, "[2] WORKERS (3)") {
		t.Errorf("View missing '[2] WORKERS (3)':\n%s", view)
	}
	if !strings.Contains(view, "parser") || !strings.Contains(view, "verifier") || !strings.Contains(view, "tester") {
		t.Errorf("View missing worker names")
	}

	// Navigate workers cursor
	if b.workersCursor != 0 {
		t.Fatalf("expected initial workersCursor=0, got %d", b.workersCursor)
	}
	b.HandleKey(tea.Key{Code: 'j', Text: "j"})
	if b.workersCursor != 1 {
		t.Fatalf("expected workersCursor=1 after 'j', got %d", b.workersCursor)
	}

	// Press 'o' to inspect subagent for worker #1 (verifier -> conv-verifier)
	b.HandleKey(tea.Key{Code: 'o', Text: "o"})
	if openedConv != "conv-verifier" {
		t.Fatalf("opened subagent convID = %q, want %q", openedConv, "conv-verifier")
	}
}

func TestBoard_PartitionedRendering_Monitor(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()
	snap := sampleSwarmSnapshot()
	b.ApplySnapshot(snap)
	b.SetPartitioned(true)

	view := b.View()
	if !strings.Contains(view, "[3] MONITOR") {
		t.Errorf("View missing '[3] MONITOR':\n%s", view)
	}
	if !strings.Contains(view, "HEALTHY") {
		t.Errorf("View missing 'HEALTHY' monitor status")
	}
	if !strings.Contains(view, "Pulse:") {
		t.Errorf("View missing Pulse indicator")
	}

	// Update monitor to stalled state
	b.SetMonitor(&MonitorStatus{
		Enabled:        true,
		State:          "stalled",
		LastHeartbeat:  time.Now(),
		StalledWorkers: []string{"parser"},
		AlertCount:     1,
		Message:        "Stall Alert: parser waiting for lock >45s",
	})

	stalledView := b.View()
	if !strings.Contains(stalledView, "STALLED") {
		t.Errorf("View missing 'STALLED' state after update")
	}
	if !strings.Contains(stalledView, "Stall Alert") {
		t.Errorf("View missing stall alert message")
	}
}

func TestBoard_ZoomPartitionedPane(t *testing.T) {
	b := NewBoard(nil)
	b.SetSize(100, 30)
	b.Open()
	b.ApplySnapshot(sampleSwarmSnapshot())
	b.SetPartitioned(true)

	// Focus Monitor (3) and zoom
	b.HandleKey(tea.Key{Code: '3', Text: "3"})
	b.HandleKey(tea.Key{Code: 'z', Text: "z"})

	v := b.View()
	if !strings.Contains(v, "[ZOOMED]") {
		t.Errorf("expected [ZOOMED] badge in header")
	}
	if !strings.Contains(v, "[3] MONITOR") {
		t.Errorf("zoomed view should contain [3] MONITOR")
	}
	// Unzoom
	b.HandleKey(tea.Key{Code: 'z', Text: "z"})
	if b.zoomed {
		t.Fatal("expected zoomed == false")
	}
}

func TestBoard_HtopProgressBar(t *testing.T) {
	tests := []struct {
		pct     int
		width   int
		wantSub string
	}{
		{0, 20, "[.............]   0%"},
		{50, 20, "[||||||.......]  50%"},
		{100, 20, "[|||||||||||||] 100%"},
	}

	for _, tt := range tests {
		got := renderProgressBar(tt.pct, tt.width)
		if !strings.Contains(got, "%") {
			t.Errorf("renderProgressBar(%d, %d) = %q, want percentage sign", tt.pct, tt.width, got)
		}
		if got != tt.wantSub {
			t.Errorf("renderProgressBar(%d, %d) = %q, want %q", tt.pct, tt.width, got, tt.wantSub)
		}
	}
}
