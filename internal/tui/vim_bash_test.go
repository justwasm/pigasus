package tui

import (
	"testing"

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
	if got := m.composer.input.Value(); got != "!" {
		t.Fatalf("composer = %q, want %q", got, "!")
	}
	if m.composer.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want %v", m.composer.vimEng.Mode, vim.Insert)
	}
}

// TestVimSlashReachesTheComposer: the "/" is inserted the same way, so a
// slash command is typed straight into the composer.
func TestVimSlashReachesTheComposer(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimPress(t, m, runeKey('/'))
	if got := m.composer.input.Value(); got != "/" {
		t.Fatalf("composer = %q, want %q", got, "/")
	}
	if m.composer.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want %v", m.composer.vimEng.Mode, vim.Insert)
	}
}

// TestVimBangSubmitRunsTheShellCommand: a vim-mode prompt that begins
// with "!" runs as a shell command, with the "!" stripped — and never
// reaches the model.
func TestVimBangSubmitRunsTheShellCommand(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "!echo hello")
	if got := m.composer.input.Value(); got != "!echo hello" {
		t.Fatalf("composer = %q, want %q", got, "!echo hello")
	}
	// ctrl+s submits from Insert mode (vim.insert.submit).
	vimPress(t, m, ctrlKey('s'))
	if !m.engine.streaming {
		t.Fatal("streaming not set while the command runs")
	}
	if len(m.engine.session) != 0 {
		t.Fatalf("shell command reached the model session: %#v", m.engine.session)
	}
	if got := m.composer.promptHistory.Entries()[m.composer.promptHistory.Len()-1]; got != "!echo hello" {
		t.Fatalf("history entry = %q, want %q", got, "!echo hello")
	}
}

// TestVimBangIsNotShellOutsideVimMode: outside vim mode nothing changes —
// the "!" arms the bash mode and the buffer stays empty.
func TestVimBangIsNotShellOutsideVimMode(t *testing.T) {
	m := bashModeRunMode(t)
	m.Update(runeKey('!'))
	if !m.session.store.BashMode {
		t.Fatal("! on the empty composer did not arm bash mode")
	}
	if got := m.composer.input.Value(); got != "" {
		t.Fatalf("composer = %q, want empty (the ! is the mode marker)", got)
	}
}
