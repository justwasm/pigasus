package tui

import (
	"testing"

	"github.com/justwasm/pigasus/vim"
)

// The vim engine's NORMAL-mode "u" and "U" are the undo and the redo
// (official-docs/cli/vim-editor-mode.md:192 — "In NORMAL mode, `u` and
// `U` are undo and redo").
//
// The composer is the engine's buffer in vim mode, so the two keys move
// the *composer*, not a stack of the host's own. The host must not
// steal either key on the way to the engine.

// TestVimEngineStartsInNormalMode: the precondition of the "/" and "!"
// pins in vim_bash_test.go — with vim on and no "Insert First", the
// engine opens in NORMAL mode, so the keys that only NORMAL mode binds
// are the keys the user reaches first.
func TestVimEngineStartsInNormalMode(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	if !m.vimEnabled() {
		t.Fatal("vim should be on")
	}
	if got := m.vimMode(); got != vim.Normal {
		t.Fatalf("mode = %v, want %v", got, vim.Normal)
	}
}

// TestVimUndoRedoRoundTrip: the whole path, from the key to the
// engine's stack to the textarea.
//
// "a" opens INSERT, so "abc" leaves "bc" in the composer; "u" empties
// it and "U" brings the text back.
func TestVimUndoRedoRoundTrip(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "abc")
	if got := m.prompt.input.Value(); got != "bc" {
		t.Fatalf("composer = %q, want %q", got, "bc")
	}
	vimPress(t, m, vimEscKey())
	if got := m.vimMode(); got != vim.Normal {
		t.Fatalf("mode = %v, want %v", got, vim.Normal)
	}

	vimPress(t, m, runeKey('u'))
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("composer after u = %q, want empty", got)
	}
	if got := m.vimMode(); got != vim.Normal {
		t.Fatalf("mode after u = %v, want %v", got, vim.Normal)
	}

	vimPress(t, m, runeKey('U'))
	if got := m.prompt.input.Value(); got != "bc" {
		t.Fatalf("composer after U = %q, want %q", got, "bc")
	}
}
