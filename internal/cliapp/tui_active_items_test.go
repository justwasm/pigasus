package cliapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/taskregistry"
)

func TestActiveItemsModel_RenderAndKeyNav(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)
	_ = os.WriteFile(filepath.Join(tempHome, "settings.json"), []byte("{}"), 0644)

	m := &runMode{
		taskReg:     taskregistry.New(),
		activeItems: newActiveItemsModel(),
		width:       80,
		height:      30,
	}

	// 1. Initially no active items -> activeItemsView is empty
	if got := m.activeItemsView(); got != "" {
		t.Fatalf("expected empty view when no tasks running, got %q", got)
	}

	// 2. Register a running task and a subagent
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
	m.taskReg.Register(t1)
	m.taskReg.Register(t2)

	view := m.activeItemsView()
	if !strings.Contains(view, "Search Shanghai Weather") {
		t.Errorf("expected view to contain 'Search Shanghai Weather', got:\n%s", view)
	}
	if !strings.Contains(view, "Running tests in background") {
		t.Errorf("expected view to contain 'Running tests in background', got:\n%s", view)
	}
	if !strings.Contains(view, "●") {
		t.Errorf("expected bullet ● in view, got:\n%s", view)
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
	if strings.Contains(view, "subagent-1001") {
		t.Errorf("expected no task id in the row, got:\n%s", view)
	}
	if strings.Contains(view, "task-1002") {
		t.Errorf("expected no task id in the row, got:\n%s", view)
	}
	if !strings.Contains(view, "Agent(Search Shanghai Weather)") {
		t.Errorf("expected the subagent row's tool info, got:\n%s", view)
	}
	if !strings.Contains(view, "· 15s") {
		t.Errorf("expected the subagent row's elapsed cell, got:\n%s", view)
	}
	if strings.Contains(view, "worked") {
		t.Errorf("expected the running form of the elapsed cell, got:\n%s", view)
	}
	if !strings.Contains(view, "Processing...") {
		t.Errorf("expected the subagent row's status word, got:\n%s", view)
	}

	// 3. Test focus with alt+j
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModAlt, Text: "j"}))
	if !m.activeItems.Focused() {
		t.Errorf("expected activeItems to be focused after alt+j")
	}

	// View when focused should contain selection marker "> " on first item
	focusedView := m.activeItemsView()
	if !strings.Contains(focusedView, "> ") {
		t.Errorf("expected cursor marker '> ' in focused view, got:\n%s", focusedView)
	}

	// Down arrow moves cursor to second item
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))
	if m.activeItems.cursor != 1 {
		t.Errorf("expected cursor at 1 after KeyDown, got %d", m.activeItems.cursor)
	}

	// Esc un-focuses activeItems
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.activeItems.Focused() {
		t.Errorf("expected activeItems to lose focus after Esc")
	}

	// 4. Test hideActiveItems configuration gate
	if err := auth.SetSettingsField("hideActiveItems", true); err != nil {
		t.Fatalf("SetSettingsField failed: %v", err)
	}

	if got := m.activeItemsView(); got != "" {
		t.Errorf("expected activeItemsView to be suppressed when hideActiveItems=true, got %q", got)
	}
}
