package cliapp

import (
	"testing"

	"github.com/justwasm/pigasus/vim"
)

// 【一手】 agy 1.4.9, the vendor's own release notes: "a half-typed
// operator such as `d` carrying over after you submit a prompt".
//
// The composer is cleared by the submit, and the editor is re-seeded
// from it — with the half-typed operator intact.

func TestVimPendingOperatorCarriesOverTheSubmit(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	// "d" is a half-typed operator: the engine waits for its motion.
	vimPress(t, m, runeKey('d'))
	if m.vimEng.Pending != "d" {
		t.Fatalf("pending = %q, want d", m.vimEng.Pending)
	}
	// ctrl+s submits and clears the composer.
	vimPress(t, m, ctrlKey('s'))
	if got := m.input.Value(); got != "" {
		t.Fatalf("composer after submit = %q, want empty", got)
	}
	// The operator is still waiting for its motion.
	if m.vimEng.Pending != "d" {
		t.Fatalf("pending after submit = %q, want d", m.vimEng.Pending)
	}
}

func TestVimCarriedOperatorDoesNotSwallowTheNextKey(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	vimPress(t, m, runeKey('d'))
	vimPress(t, m, ctrlKey('s'))
	// "i" is the way into Insert mode; the stale operator must not eat
	// it.
	vimPress(t, m, runeKey('i'))
	if m.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want Insert", m.vimEng.Mode)
	}
}
