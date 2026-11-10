package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestCursor_HiddenWhenHelpActive(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Idle state: cursor should be visible in composer.
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be visible in idle prompt")
	}

	// Open /help on tab 0 (guides/about).
	m.help.Panel.Open(0)
	if !m.help.Panel.Active() {
		t.Fatal("help panel should be active")
	}
	v = m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in /help mode (tab 0), got: %+v", v.Cursor)
	}

	// Open on tab 2 (shortcuts).
	m.help.Panel.Open(2)
	v = m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in /help mode (tab 2 shortcuts), got: %+v", v.Cursor)
	}

	// Close help panel: cursor should be visible again.
	m.help.Panel.Cancel()
	v = m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be restored after closing /help")
	}
}

func TestCursor_HiddenWhenSlashPanelsActive(t *testing.T) {
	panelsToTest := []struct {
		kind panelregistry.Kind
		cmd  string
	}{
		{panelregistry.Model, "/model"},
		{panelregistry.Skills, "/skills"},
		{panelregistry.Config, "/config"},
		{panelregistry.Diff, "/diff"},
		{panelregistry.Changelog, "/changelog"},
		{panelregistry.Tasks, "/tasks"},
		{panelregistry.Rewind, "/rewind"},
		{panelregistry.Accounts, "/accounts"},
		{panelregistry.Feedback, "/feedback"},
	}

	for _, pt := range panelsToTest {
		t.Run(pt.cmd, func(t *testing.T) {
			m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
			m.viewport.width = 80
			m.viewport.height = 24

			m.overlays.panel = slashPanel{kind: pt.kind, cmd: pt.cmd}
			if m.overlays.panel.kind != pt.kind {
				t.Fatalf("expected panel %v to be open", pt.kind)
			}

			v := m.View()
			if v.Cursor != nil {
				t.Errorf("cursor must be hidden when %s panel is open, got: %+v", pt.cmd, v.Cursor)
			}

			m.closePanelSilent()
			v = m.View()
			if v.Cursor == nil {
				t.Errorf("cursor should be restored after closing %s panel", pt.cmd)
			}
		})
	}
}

func TestCursor_HiddenWhenResumePickerActive(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	if err := m.overlays.resumePicker.Open(); err != nil {
		t.Fatalf("failed to open resume picker: %v", err)
	}

	v := m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden when resume picker is active, got: %+v", v.Cursor)
	}

	m.overlays.resumePicker.Close()
	v = m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be restored after closing resume picker")
	}
}

func TestCursor_HiddenWhenActiveItemsFocused(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	if m.activeItems.Panel == nil {
		t.Skip("activeItems is nil")
	}

	m.activeItems.Panel.SetFocus(true)
	v := m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden when activeItems is focused, got: %+v", v.Cursor)
	}

	m.activeItems.Panel.SetFocus(false)
	v = m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be restored when activeItems is unfocused")
	}
}

func TestCursor_HiddenDuringStreamingAndThinking(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Streaming: cursor hidden
	m.conversation.streaming = true
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden during streaming, got: %+v", v.Cursor)
	}

	m.conversation.streaming = false
	if v := m.View(); v.Cursor == nil {
		t.Errorf("cursor should be restored when streaming finishes")
	}

	// Thinking: cursor hidden
	m.conversation.thinking = true
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden during thinking, got: %+v", v.Cursor)
	}

	m.conversation.thinking = false
	if v := m.View(); v.Cursor == nil {
		t.Errorf("cursor should be restored when thinking finishes")
	}
}

func TestCursor_HiddenWhenBashRunning(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	m.session.store.SetBashCancel(func() {})
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden when bash command is running, got: %+v", v.Cursor)
	}

	m.session.store.CancelBash()
	if v := m.View(); v.Cursor == nil {
		t.Errorf("cursor should be restored when bash command finishes")
	}
}

func TestCursor_HiddenWhenInputBlurred(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	m.prompt.input.Blur()
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden when input is blurred, got: %+v", v.Cursor)
	}

	m.prompt.input.Focus()
	if v := m.View(); v.Cursor == nil {
		t.Errorf("cursor should be restored when input is focused")
	}
}

func TestCursor_HiddenWhenTextSelected(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Input selection
	m.prompt.input.SetValue("hello")
	m.prompt.InputSelStart = 0
	m.prompt.InputSelEnd = 3
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden when input text is selected, got: %+v", v.Cursor)
	}
	m.prompt.InputSelStart = 0
	m.prompt.InputSelEnd = 0

	// Viewport selection dragging
	m.viewport.SelDragging = true
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden when viewport text is selected, got: %+v", v.Cursor)
	}
}

func TestCursor_HiddenWhenBtwActive(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	m.overlays.activeBtwID = "btw-test"
	if v := m.View(); v.Cursor != nil {
		t.Errorf("cursor must be hidden when btw card is active, got: %+v", v.Cursor)
	}

	m.overlays.activeBtwID = ""
	if v := m.View(); v.Cursor == nil {
		t.Errorf("cursor should be restored when btw card closes")
	}
}

func TestFullViewportPanel_TakesEntireScreen_AndEnablesAltScreen(t *testing.T) {
	// Start with inline mode (altScreen = false)
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", false)
	m.viewport.width = 80
	m.viewport.height = 24

	// Verify starting in inline mode
	v := m.View()
	if v.AltScreen {
		t.Fatalf("expected initial AltScreen=false in inline mode")
	}
	if v.Cursor == nil {
		t.Fatalf("expected cursor in idle inline prompt")
	}

	// 1. Open /diff: full-viewport panel should switch AltScreen=true and hide cursor
	m.openSlashPanel("diff")
	if !m.isFullViewportPanel() {
		t.Fatalf("expected diff to be recognized as full viewport panel")
	}
	v = m.View()
	if !v.AltScreen {
		t.Errorf("expected AltScreen=true for /diff")
	}
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in full-viewport /diff view, got: %+v", v.Cursor)
	}

	// Exit /diff
	m.closePanelSilent()
	v = m.View()
	if v.AltScreen {
		t.Errorf("expected AltScreen=false after exiting /diff")
	}
	if v.Cursor == nil {
		t.Errorf("cursor should be restored after exiting /diff")
	}

	// 2. Open /tasks detail: should also be full-viewport and switch AltScreen=true
	m.openSlashPanel("tasks")
	// In tasks list mode: it is an inline drawer under composer
	if m.isFullViewportPanel() {
		t.Fatalf("tasks list should not be a full viewport panel")
	}
	v = m.View()
	if v.AltScreen {
		t.Errorf("expected AltScreen=false for /tasks list in inline mode")
	}

	// Open task detail
	m.tasksPanel().OpenTaskDetail("task-123", false)
	if !m.isFullViewportPanel() {
		t.Fatalf("task detail must be recognized as full viewport panel")
	}
	v = m.View()
	if !v.AltScreen {
		t.Errorf("expected AltScreen=true for task detail")
	}
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in task detail, got: %+v", v.Cursor)
	}

	// Close detail via Esc key
	m.tasksPanel().HandleKey(tea.Key{Code: tea.KeyEsc})
	if m.isFullViewportPanel() {
		t.Fatalf("task detail should be closed")
	}
	m.closePanelSilent()
}
