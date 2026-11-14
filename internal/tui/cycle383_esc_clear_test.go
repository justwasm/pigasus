// cycle383_esc_clear_test.go — the host's Esc and `?` keys, as cycle 383
// aligned them with the reference's own documentation.
//
//   - B-21: the composer is cleared on `esc esc`, not on one Esc
//     ("Type `esc esc` to clear your prompt box (when no streaming is
//     active)", official-docs/cli/using.md:22).
//   - B-22: `?` opens the shortcuts overlay when the composer is empty
//     ("Type `?` to get help and list all slash commands",
//     official-docs/cli/using.md:24; "Press `?` in NORMAL mode to open
//     the shortcuts overlay", official-docs/cli/vim-editor-mode.md:73).
//     When text is present, `?` is typed as a character (or used for
//     motions in Vim Normal mode).
//
// The overlay itself is a panel (cycle 381, invariant 103) and it is
// modal: the host only routes the key and asks it to open.

package tui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

// escPress sends one Esc through the run loop.
func escPress(t *testing.T, m *RootModel) {
	t.Helper()
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
}

// TestEscEscClearsThePrompt pins the reference's two-press clear: the
// first Esc arms it, the second one empties the composer.
//
// Cycle 383 (B-21): the clone cleared the prompt on the first Esc, so
// a single Esc could throw away a draft the user had not finished.
func TestEscEscClearsThePrompt(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("precondition: the composer holds %q, want %q", got, "hello")
	}

	// First Esc: arms the clear, the draft survives.
	escPress(t, m)
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("one Esc must not clear the composer, got %q", got)
	}

	// Second Esc: the pair is complete, the composer is empty.
	escPress(t, m)
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("the second Esc must clear the composer, got %q", got)
	}
}

// TestEscPairIsBrokenByAnotherKey pins that the pair is two Escs in a
// row: any other key drops the arming, so the next Esc arms again
// instead of clearing.
func TestEscPairIsBrokenByAnotherKey(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}

	// The intervening key must not be an alt binding: Terminal.app sends
	// Option+<letter> as Esc then the letter, so a *bound* letter folds
	// into that binding (TestAlt_VimMode_NotDoubleHandled pins that side).
	// The pair-breaking key is therefore the first letter the keymap does
	// not claim.
	breaking := byte(0)
	if kb := m.prompt.keybindings; kb != nil {
		for r := 'a'; r <= 'z'; r++ {
			if !kb.Has(r) {
				breaking = byte(r)
				break
			}
		}
	}
	if breaking == 0 {
		t.Skip("every letter is an alt binding; no letter can break the pair")
	}

	escPress(t, m)
	// The intervening key breaks the pair and is inserted as text.
	m.Update(runeKey(rune(breaking)))
	want := "hi" + string(breaking)
	if got := m.prompt.input.Value(); got != want {
		t.Fatalf("the composer holds %q, want %q", got, want)
	}

	// The next Esc arms again — it does not clear.
	escPress(t, m)
	if got := m.prompt.input.Value(); got != want {
		t.Fatalf("one Esc after a broken pair must not clear the composer, got %q", got)
	}

	// And the Esc that follows it clears.
	escPress(t, m)
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("the second Esc must clear the composer, got %q", got)
	}
}

// TestEscThatClosesAnOverlayIsNotPartOfThePair pins that an Esc which
// ends up somewhere else keeps its old meaning: it closes the overlay,
// and it neither clears the composer nor arms the pair.
func TestEscThatClosesAnOverlayIsNotPartOfThePair(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}

	// The overlay is a modal panel: open it over the draft.
	m.help.Panel.Open(0)
	if !m.help.Panel.Active() {
		t.Fatal("the /help overlay should be open")
	}

	// The Esc closes the overlay — the composer keeps its text.
	escPress(t, m)
	if m.help.Panel.Active() {
		t.Fatal("the Esc should have closed the /help overlay")
	}
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("the Esc that closed the overlay must not touch the composer, got %q", got)
	}

	// And the pair was not armed by it: the next Esc only arms.
	escPress(t, m)
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("one Esc must not clear the composer, got %q", got)
	}
}

// TestQuestionMarkTypesWithTextInTheComposer verifies that `?` only opens
// the shortcuts overlay on an empty prompt. When text is present in the composer,
// typing `?` inserts the character rather than opening /help.
func TestQuestionMarkTypesWithTextInTheComposer(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}

	m.Update(runeKey('?'))
	if m.help.Panel.Active() {
		t.Fatal("`?` must NOT open the /help overlay when text is in the composer")
	}
	if got := m.prompt.input.Value(); got != "hello?" {
		t.Errorf("the composer holds %q, want %q", got, "hello?")
	}

	// Clearing the composer restores `?` as the shortcuts overlay shortcut.
	m.prompt.input.SetValue("")
	m.Update(runeKey('?'))
	if !m.help.Panel.Active() {
		t.Fatal("`?` on empty composer must open the /help overlay")
	}
	if m.help.Panel.Tab() != 2 {
		t.Errorf("the overlay's tab = %d, want 2 (shortcuts)", m.help.Panel.Tab())
	}
}

// TestQuestionMarkTypesInVimInsertMode pins the one exception: in the
// vim engine's INSERT mode `?` is a character of the buffer, not the
// shortcuts-overlay key. The reference's own guide scopes the key to
// NORMAL mode (official-docs/cli/vim-editor-mode.md:73).
func TestQuestionMarkTypesInVimInsertMode(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim", "vimInsertFirst": true})
	if !m.vimEnabled() {
		t.Fatal("vim should be on")
	}

	m.Update(runeKey('?'))
	if m.help.Panel.Active() {
		t.Fatal("`?` in the vim engine's INSERT mode must not open the overlay")
	}
	if got := m.prompt.input.Value(); got != "?" {
		t.Errorf("the composer holds %q, want %q", got, "?")
	}
}

// TestQuestionMarkOpensHelpInVimNormalMode pins the other half of the
// vim rule: in NORMAL mode `?` is the shortcuts-overlay key on an empty prompt.
func TestQuestionMarkOpensHelpInVimNormalMode(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})

	m.Update(runeKey('?'))
	if !m.help.Panel.Active() {
		t.Fatal("`?` in the vim engine's NORMAL mode must open the overlay on empty prompt")
	}
	if m.help.Panel.Tab() != 2 {
		t.Errorf("the overlay's tab = %d, want 2 (shortcuts)", m.help.Panel.Tab())
	}
}

// TestQuestionMarkInVimNormalModeWithText verifies that when the prompt contains text,
// '?' does not open the shortcuts panel in Vim Normal mode.
func TestQuestionMarkInVimNormalModeWithText(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	m.prompt.input.SetValue("hello")

	m.Update(runeKey('?'))
	if m.help.Panel.Active() {
		t.Fatal("`?` in Vim NORMAL mode must not open shortcuts when prompt has text")
	}
}

func TestSlashPanels_EscClosesPanel(t *testing.T) {
	cmds := []string{
		"provider", "model", "agents", "config", "skills",
		"hooks", "permissions", "accounts", "tasks",
		"feedback", "credits", "diff", "add-dir", "rename",
		"usage", "changelog", "codesearch", "mcp", "context", "artifact",
	}
	for _, cmd := range cmds {
		m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
		if !m.openSlashPanel(cmd) {
			t.Fatalf("failed to open %s", cmd)
		}
		if m.overlays.panel.kind == panelregistry.None {
			t.Fatalf("panel %s not open", cmd)
		}
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
		if m.overlays.panel.kind != panelregistry.None {
			t.Errorf("panel %s did not close on Esc: kind=%v", cmd, m.overlays.panel.kind)
		}
	}
}

func TestSlashPanels_EscTimeoutMsgClosesPanel(t *testing.T) {
	cmds := []string{"provider", "model", "agents", "config", "skills"}
	for _, cmd := range cmds {
		m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
		if !m.openSlashPanel(cmd) {
			t.Fatalf("failed to open %s", cmd)
		}
		m.prompt.altEscBuf.Pending = true
		m.Update(keybind.EscTimeoutMsg{})
		if m.overlays.panel.kind != panelregistry.None {
			t.Errorf("panel %s did not close on EscTimeoutMsg: kind=%v", cmd, m.overlays.panel.kind)
		}
	}
}
