package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/vim"
)

// The reference's NORMAL-mode "!" inserts the character and switches to
// INSERT mode (official-docs/cli/vim-editor-mode.md), so the shell
// command is typed into the composer and the submit path is what runs
// it. The "!" never arms the bash mode in vim mode.

// TestVimBangReachesTheComposer: the "!" is the engine's, not the host's
// bash-mode marker's — arming the mode would swallow the character
// before the engine ever saw it.
func TestVimBangReachesTheComposer(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimPress(t, m, runeKey('!'))
	if m.session.store.BashMode {
		t.Fatal("! armed bash mode in vim mode")
	}
	if got := m.prompt.input.Value(); got != "!" {
		t.Fatalf("composer = %q, want %q", got, "!")
	}
	if m.prompt.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want %v", m.prompt.vimEng.Mode, vim.Insert)
	}
}

// TestVimSlashReachesTheComposer: the "/" is inserted the same way, so a
// slash command is typed straight into the composer.
func TestVimSlashReachesTheComposer(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimPress(t, m, runeKey('/'))
	if got := m.prompt.input.Value(); got != "/" {
		t.Fatalf("composer = %q, want %q", got, "/")
	}
	if m.prompt.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want %v", m.prompt.vimEng.Mode, vim.Insert)
	}
}

// TestVimBangSubmitRunsTheShellCommand: a vim-mode prompt that begins
// with "!" runs as a shell command, with the "!" stripped — and never
// reaches the model.
func TestVimBangSubmitRunsTheShellCommand(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "!echo hello")
	if got := m.prompt.input.Value(); got != "!echo hello" {
		t.Fatalf("composer = %q, want %q", got, "!echo hello")
	}
	// ctrl+s submits from Insert mode (vim.insert.submit).
	vimPress(t, m, ctrlKey('s'))
	if !m.conversation.streaming {
		t.Fatal("streaming not set while the command runs")
	}
	if len(m.SessionMessages()) != 0 {
		t.Fatalf("shell command reached the model session: %#v", m.SessionMessages())
	}
	if got := m.prompt.promptHistory.Entries()[m.prompt.promptHistory.Len()-1]; got != "!echo hello" {
		t.Fatalf("history entry = %q, want %q", got, "!echo hello")
	}
}

// TestVimBangIsNotShellOutsideVimMode: outside vim mode nothing changes —
// the "!" arms the bash mode and the buffer stays empty.
func TestVimBangIsNotShellOutsideVimMode(t *testing.T) {
	m := bashModeRootModel(t)
	m.Update(runeKey('!'))
	if !m.session.store.BashMode {
		t.Fatal("! on the empty composer did not arm bash mode")
	}
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("composer = %q, want empty (the ! is the mode marker)", got)
	}
}

// TestVimSlashModelOpensPanel verifies that typing "/model" and pressing Enter in
// Vim mode (both directly and in Insert mode) opens the Switch Model panel,
// rather than inserting a literal newline into the composer.
func TestVimSlashModelOpensPanel(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "/model")
	if got := m.prompt.input.Value(); got != "/model" {
		t.Fatalf("composer = %q, want %q", got, "/model")
	}
	if !m.prompt.picker.Visible() {
		t.Fatal("slash picker popup should be visible while typing /model in vim mode")
	}

	vimPress(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.overlays.panel.kind != panelregistry.Model {
		t.Fatalf("panel kind = %v, want panelregistry.Model", m.overlays.panel.kind)
	}
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("composer after opening panel = %q, want empty", got)
	}

	vimPress(t, m, vimEscKey())
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("panel kind after esc = %v, want None", m.overlays.panel.kind)
	}
	if m.prompt.vimEng.Mode != vim.Normal {
		t.Fatalf("vim mode after exiting panel = %v, want Normal", m.prompt.vimEng.Mode)
	}
}

func TestVimSlashModelInInsertModeOpensPanel(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	// Press 'i' to explicitly enter Insert mode
	vimPress(t, m, runeKey('i'))
	if m.prompt.vimEng.Mode != vim.Insert {
		t.Fatalf("vim mode after 'i' = %v, want Insert", m.prompt.vimEng.Mode)
	}

	vimType(t, m, "/model")
	if got := m.prompt.input.Value(); got != "/model" {
		t.Fatalf("composer = %q, want %q", got, "/model")
	}
	if !m.prompt.picker.Visible() {
		t.Fatal("slash picker popup should be visible while typing /model in Insert mode")
	}

	vimPress(t, m, tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter}))
	if m.overlays.panel.kind != panelregistry.Model {
		t.Fatalf("panel kind = %v, want panelregistry.Model", m.overlays.panel.kind)
	}
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("composer after opening panel = %q, want empty", got)
	}

	vimPress(t, m, vimEscKey())
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("panel kind after esc = %v, want None", m.overlays.panel.kind)
	}
	if m.prompt.vimEng.Mode != vim.Normal {
		t.Fatalf("vim mode after exiting panel = %v, want Normal", m.prompt.vimEng.Mode)
	}
}
