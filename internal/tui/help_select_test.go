package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// Test_HelpSelectCommandViaEnter pins the Enter key behavior on /help commands tab:
// pressing Enter on a highlighted command completes its canonical name into the
// composer input, closes the overlay, and does not echo "Exited /help command".
func Test_HelpSelectCommandViaEnter(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/help")
	if !handled || !m.viewport.helpPanel.Active() {
		t.Fatal("/help should open overlay")
	}
	// Switch to tab 1 (COMMANDS)
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyTab}))
	if m.viewport.helpPanel.Tab() != 1 {
		t.Fatalf("tab = %d, want 1", m.viewport.helpPanel.Tab())
	}

	cmds := slashcommands.Sorted()
	if len(cmds) < 2 {
		t.Fatalf("expected at least 2 slash commands, got %d", len(cmds))
	}

	// Move cursor down 1 row to select the second command
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}))

	// Press Enter
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))

	// Overlay must be closed
	if m.viewport.helpPanel.Active() {
		t.Error("help overlay should be closed after Enter")
	}

	// Input buffer must have the selected command
	wantCmd := cmds[1].Name
	if m.composer.input.Value() != wantCmd {
		t.Errorf("m.composer.input.Value() = %q, want %q", m.composer.input.Value(), wantCmd)
	}

	// Must not echo "Exited /help command" into the response
	for _, line := range m.engine.response {
		if strings.Contains(line, "Exited /help command") {
			t.Errorf("unexpected exit echo in response: %q", line)
		}
	}
}
