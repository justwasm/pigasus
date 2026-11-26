package tui

import (
	"fmt"
	"strings"
	"testing"

	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// TestSelectionHostComposition exercises the real textarea adapter and host
// routing. Clipboard I/O is replaced locally; no program, shell or RootModel
// constructor is needed to prove module/host composition.
func TestSelectionHostComposition(t *testing.T) {
	var writes []string
	previous := clipboard.Writer
	clipboard.Writer = func(text string) error {
		writes = append(writes, text)
		return nil
	}
	t.Cleanup(func() { clipboard.Writer = previous })

	m := &RootModel{}
	m.prompt.input = tas.New()
	m.prompt.input.SetWidth(40)
	m.prompt.input.SetValue("hello world")
	m.prompt.promptScreenY = 4
	m.viewport.altScreen = true
	m.viewport.width = 40
	m.viewport.height = 20

	// The existing composer key dispatcher delegates Cmd+A to selection.Model.
	_, cmd := m.updateEditorKey(tea.KeyPressMsg{Code: 'a', Mod: tea.ModSuper})
	assertClipboardCommand(t, cmd, "hello world")
	if len(writes) != 1 || writes[0] != "hello world" {
		t.Fatalf("clipboard writes = %q, want [hello world]", writes)
	}
	if m.prompt.input.Column() != 11 {
		t.Fatalf("textarea cursor = %d, want 11", m.prompt.input.Column())
	}
	if got := m.prompt.ApplyInputSelectionHighlight("> hello world"); !strings.Contains(got, "\x1b[0m\x1b[7mhello world\x1b[0m") {
		t.Fatalf("select-all render missing reverse video: %q", got)
	}

	// Mouse routing starts and extends the module's drag. The release
	// writes nothing to the clipboard (agy 1.5.17: a release must not
	// overwrite it), so the copy is Ctrl+C's.
	m.updateMouseClick(tea.Mouse{X: 2, Y: 4, Button: tea.MouseLeft})
	m.updateMouseMotion(tea.Mouse{X: 7, Y: 4})
	if _, cmd := m.updateMouseReleaseInputSel(tea.Mouse{X: 7, Y: 4, Button: tea.MouseLeft}); cmd != nil {
		t.Fatalf("release produced a cmd; a release must not copy")
	}
	if len(writes) != 1 {
		t.Fatalf("clipboard writes after the release = %q, want only the select-all", writes)
	}
	if m.prompt.InputSelectionDragging() {
		t.Fatal("mouse release left drag active")
	}
	// Ctrl+C over the surviving highlight is the copy.
	_, cmd = m.updateEditorKey(tea.KeyPressMsg(tea.Key{Code: 'c', Mod: tea.ModCtrl}))
	assertClipboardCommand(t, cmd, "hello")
	if len(writes) != 2 || writes[1] != "hello" {
		t.Fatalf("clipboard writes = %q, want [hello world hello]", writes)
	}

	// The shared editing dispatcher still consumes the module's selection.
	m.updateEditorKey(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.prompt.input.Value(); got != " world" {
		t.Fatalf("textarea after selection delete = %q, want space+world", got)
	}
	if m.prompt.HasInputSelection() || m.prompt.input.Column() != 0 {
		t.Fatalf("delete left active=%v cursor=%d", m.prompt.HasInputSelection(), m.prompt.input.Column())
	}
}

func assertClipboardCommand(t *testing.T, cmd tea.Cmd, text string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected terminal clipboard command")
	}
	got := fmt.Sprint(cmd())
	want := fmt.Sprint(tea.SetClipboard(text)())
	if got != want {
		t.Fatalf("terminal command = %q, want %q", got, want)
	}
}
