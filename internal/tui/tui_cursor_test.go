package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
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

func TestCursor_VisibleDuringStreamingAndThinking(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	// Idle state: cursor visible
	vIdle := m.View()
	if vIdle.Cursor == nil {
		t.Fatal("cursor should be visible in idle prompt")
	}

	// Streaming: cursor must remain visible in input box
	m.conversation.streaming = true
	vStream := m.View()
	if vStream.Cursor == nil {
		t.Fatal("cursor must be visible in composer during streaming")
	}
	lines := strings.Split(vStream.Content, "\n")
	if vStream.Cursor.Y >= len(lines) {
		t.Fatalf("cursor Y=%d outside view rows %d", vStream.Cursor.Y, len(lines))
	}
	promptLine := ansi.Strip(lines[vStream.Cursor.Y])
	if !strings.HasPrefix(promptLine, ">") {
		t.Errorf("cursor row %d is not the prompt line: %q", vStream.Cursor.Y, promptLine)
	}

	// Thinking: cursor must remain visible in input box
	m.conversation.streaming = false
	m.conversation.thinking = true
	vThink := m.View()
	if vThink.Cursor == nil {
		t.Fatal("cursor must be visible in composer during thinking")
	}
	lines = strings.Split(vThink.Content, "\n")
	if vThink.Cursor.Y >= len(lines) {
		t.Fatalf("cursor Y=%d outside view rows %d", vThink.Cursor.Y, len(lines))
	}
	promptLine = ansi.Strip(lines[vThink.Cursor.Y])
	if !strings.HasPrefix(promptLine, ">") {
		t.Errorf("cursor row %d is not the prompt line: %q", vThink.Cursor.Y, promptLine)
	}
}

func TestCursor_VisibleWhenBashRunning(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	m.session.store.SetBashCancel(func() {})
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("cursor must remain visible in composer when bash command is running")
	}
	lines := strings.Split(v.Content, "\n")
	if v.Cursor.Y >= len(lines) {
		t.Fatalf("cursor Y=%d outside view rows %d", v.Cursor.Y, len(lines))
	}
	promptLine := ansi.Strip(lines[v.Cursor.Y])
	if !strings.HasPrefix(promptLine, ">") {
		t.Errorf("cursor row %d is not the prompt line: %q", v.Cursor.Y, promptLine)
	}

	m.session.store.CancelBash()
	v = m.View()
	if v.Cursor == nil {
		t.Errorf("cursor should remain visible when bash command finishes")
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

func TestCursor_VisibleWhenBtwActive(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 24

	c := &sidequestion.Card{
		ID:       "btw-test",
		Question: "What is caching?",
		Answer:   "Fast storage.",
		State:    sidequestion.StateDone,
	}
	m.overlays.btwCards = []*sidequestion.Card{c}
	m.overlays.activeBtwIndex = 0
	m.overlays.activeBtwID = c.ID

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("cursor must remain visible in composer when btw card is active")
	}

	lines := strings.Split(v.Content, "\n")
	if v.Cursor.Y >= len(lines) {
		t.Fatalf("cursor Y=%d outside view rows %d", v.Cursor.Y, len(lines))
	}
	promptLine := ansi.Strip(lines[v.Cursor.Y])
	if !strings.HasPrefix(promptLine, ">") {
		t.Errorf("cursor Y=%d is not prompt line: %q", v.Cursor.Y, promptLine)
	}

	// Verify layout ordering:
	// btw card is rendered ABOVE the top prompt rule line, NOT between the two prompt rules!
	cardRow := -1
	topRuleRow := -1
	promptRow := -1
	bottomRuleRow := -1

	rulePattern := ansi.Strip(m.promptRule(true))
	for i, l := range lines {
		stripped := ansi.Strip(l)
		if strings.Contains(stripped, "What is caching?") && cardRow == -1 {
			cardRow = i
		}
		if stripped == rulePattern {
			if topRuleRow == -1 {
				topRuleRow = i
			} else if bottomRuleRow == -1 {
				bottomRuleRow = i
			}
		}
		if strings.HasPrefix(stripped, ">") && promptRow == -1 {
			promptRow = i
		}
	}

	if cardRow == -1 {
		t.Fatalf("btw card question not found in view:\n%s", v.Content)
	}
	if topRuleRow == -1 {
		t.Fatalf("top prompt rule not found in view:\n%s", v.Content)
	}
	if promptRow == -1 {
		t.Fatalf("prompt row not found in view:\n%s", v.Content)
	}

	if cardRow >= topRuleRow {
		t.Errorf("layout bug: btw card row %d must be ABOVE top prompt rule row %d", cardRow, topRuleRow)
	}
	if topRuleRow >= promptRow {
		t.Errorf("top prompt rule row %d must be ABOVE prompt row %d", topRuleRow, promptRow)
	}
	if bottomRuleRow != -1 && promptRow >= bottomRuleRow {
		t.Errorf("prompt row %d must be ABOVE bottom prompt rule row %d", promptRow, bottomRuleRow)
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

func TestCursor_ChineseInputAndStreamingStability(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 100
	m.viewport.height = 30

	// 1. Typing Chinese into the composer.
	chineseText := "你好，世界！"
	m.prompt.input.SetValue(chineseText)
	m.prompt.input.CursorEnd()

	v := m.View()
	if v.Cursor == nil {
		t.Fatal("cursor must be visible in composer after typing Chinese")
	}

	expectedX := 2 + ansi.StringWidth(chineseText) // "> " has width 2, "你好，世界！" has width 12
	if v.Cursor.X != expectedX {
		t.Errorf("cursor X = %d, want %d for Chinese text %q", v.Cursor.X, expectedX, chineseText)
	}

	// 2. Start streaming / thinking with live spinner.
	m.conversation.streaming = true
	m.conversation.thinking = true
	m.conversation.liveThinkingSnippet = "Searching the codebase..."

	vStream := m.View()
	if vStream.Cursor == nil {
		t.Fatal("cursor must remain visible in composer during streaming with Chinese input")
	}

	// Cursor X must still be parked at the end of the Chinese input.
	if vStream.Cursor.X != expectedX {
		t.Errorf("during streaming, cursor X = %d, want %d", vStream.Cursor.X, expectedX)
	}

	// Verify cursor Y points to the prompt row, not to the spinner row!
	lines := strings.Split(vStream.Content, "\n")
	if vStream.Cursor.Y >= len(lines) {
		t.Fatalf("cursor Y=%d outside view rows %d", vStream.Cursor.Y, len(lines))
	}
	promptLine := ansi.Strip(lines[vStream.Cursor.Y])
	if !strings.HasPrefix(promptLine, ">") {
		t.Errorf("cursor Y=%d is not prompt line: %q", vStream.Cursor.Y, promptLine)
	}

	// Check that spinner exists on an earlier row and cursor is NOT on the spinner row.
	spinnerFound := false
	for i, l := range lines {
		stripped := ansi.Strip(l)
		if strings.Contains(stripped, "Searching the codebase...") {
			spinnerFound = true
			if i == vStream.Cursor.Y {
				t.Fatalf("cursor drifted to spinner row %d!", i)
			}
			if i >= vStream.Cursor.Y {
				t.Errorf("spinner row %d should be above cursor prompt row %d", i, vStream.Cursor.Y)
			}
			break
		}
	}
	if !spinnerFound {
		t.Error("expected live thinking snippet row to be present in view")
	}
}
