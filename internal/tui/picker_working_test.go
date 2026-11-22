package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// TestPickerWorking_SelectContextOpensPanelAndDoesNotQueue verifies the AGY 1.5.14 parity:
// When an agent is working (m.conversation.streaming = true), typing /con and selecting
// /context (via Enter) sets the composer to "/context " and immediately displays the /context
// panel, rather than queueing "/con" into StreamQueue.
func TestPickerWorking_SelectContextOpensPanelAndDoesNotQueue(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.streaming = true

	// Type /con
	m.Update(runeKey('/'))
	m.Update(runeKey('c'))
	m.Update(runeKey('o'))
	m.Update(runeKey('n'))

	if !m.prompt.picker.Visible() {
		t.Fatal("picker should be visible after typing /con")
	}

	// Select /context in the picker
	found := false
	for i := 0; i < m.prompt.picker.Len(); i++ {
		m.prompt.picker.SetIdx(i)
		if sel := m.prompt.picker.Selected(); sel != nil && sel.Name == "/context" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected /context to be present in picker suggestions for /con")
	}

	// Press Enter
	m.Update(enterKey())

	// Parity assertions:
	// 1. Picker should be closed
	if m.prompt.picker.Visible() {
		t.Error("picker should not be visible after Enter")
	}

	// 2. Composer must be "/context "
	if got := m.prompt.input.Value(); got != "/context " {
		t.Errorf("composer input = %q, want %q", got, "/context ")
	}

	// 3. /context panel must be opened immediately
	if m.overlays.panel.kind != panelregistry.Context {
		t.Errorf("panel kind = %v, want panelregistry.Context", m.overlays.panel.kind)
	}

	// 4. Nothing should be queued into StreamQueue
	if len(m.session.store.StreamQueue) != 0 {
		t.Errorf("StreamQueue = %v, want empty (nothing queued)", m.session.store.StreamQueue)
	}
	if m.session.store.QueuedPrompt != "" {
		t.Errorf("QueuedPrompt = %q, want empty", m.session.store.QueuedPrompt)
	}
}

// TestPickerIdle_SelectContextOpensPanel verifies the same completion behavior when idle.
func TestPickerIdle_SelectContextOpensPanel(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.streaming = false

	// Type /con
	m.Update(runeKey('/'))
	m.Update(runeKey('c'))
	m.Update(runeKey('o'))
	m.Update(runeKey('n'))

	if !m.prompt.picker.Visible() {
		t.Fatal("picker should be visible after typing /con")
	}

	// Select /context in the picker
	found := false
	for i := 0; i < m.prompt.picker.Len(); i++ {
		m.prompt.picker.SetIdx(i)
		if sel := m.prompt.picker.Selected(); sel != nil && sel.Name == "/context" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("expected /context to be present in picker suggestions for /con")
	}

	// Press Enter
	m.Update(enterKey())

	if m.prompt.picker.Visible() {
		t.Error("picker should not be visible after Enter")
	}
	if got := m.prompt.input.Value(); got != "/context " {
		t.Errorf("composer input = %q, want %q", got, "/context ")
	}
	if m.overlays.panel.kind != panelregistry.Context {
		t.Errorf("panel kind = %v, want panelregistry.Context", m.overlays.panel.kind)
	}
}

// TestPickerWorking_TabThenEnterContextOpensPanel verifies that Tab completion followed
// by Enter opens the context panel without queueing while streaming.
func TestPickerWorking_TabThenEnterContextOpensPanel(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.streaming = true

	// Type /con
	m.Update(runeKey('/'))
	m.Update(runeKey('c'))
	m.Update(runeKey('o'))
	m.Update(runeKey('n'))

	// Select /context
	for i := 0; i < m.prompt.picker.Len(); i++ {
		m.prompt.picker.SetIdx(i)
		if sel := m.prompt.picker.Selected(); sel != nil && sel.Name == "/context" {
			break
		}
	}

	// Press Tab to complete
	m.Update(key(tea.KeyTab, 0))

	if got := m.prompt.input.Value(); got != "/context " {
		t.Fatalf("composer after Tab = %q, want %q", got, "/context ")
	}

	// Press Enter on the completed "/context "
	m.Update(enterKey())

	if m.overlays.panel.kind != panelregistry.Context {
		t.Errorf("panel kind = %v, want panelregistry.Context", m.overlays.panel.kind)
	}
	if len(m.session.store.StreamQueue) != 0 {
		t.Errorf("StreamQueue = %v, want empty", m.session.store.StreamQueue)
	}
}
