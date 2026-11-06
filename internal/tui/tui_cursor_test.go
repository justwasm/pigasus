package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestCursor_HiddenWhenHelpActive(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Idle state: cursor should be visible in composer.
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be visible in idle prompt")
	}

	// Open /help on tab 0 (guides/about).
	m.viewport.helpPanel.Open(0)
	if !m.viewport.helpPanel.Active() {
		t.Fatal("help panel should be active")
	}
	v = m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in /help mode (tab 0), got: %+v", v.Cursor)
	}

	// Open on tab 2 (shortcuts).
	m.viewport.helpPanel.Open(2)
	v = m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden in /help mode (tab 2 shortcuts), got: %+v", v.Cursor)
	}

	// Close help panel: cursor should be visible again.
	m.viewport.helpPanel.Cancel()
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
			m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
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
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
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
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	if m.viewport.activeItems == nil {
		t.Skip("activeItems is nil")
	}

	m.viewport.activeItems.SetFocus(true)
	v := m.View()
	if v.Cursor != nil {
		t.Errorf("cursor must be hidden when activeItems is focused, got: %+v", v.Cursor)
	}

	m.viewport.activeItems.SetFocus(false)
	v = m.View()
	if v.Cursor == nil {
		t.Fatal("cursor should be restored when activeItems is unfocused")
	}
}
