package main

// vim_mode_test.go — cycle 216: the TUI half of the Vim editing mode.
//
// These tests drive the run loop the way a terminal does (synthetic
// tea.KeyPressMsg events) with ANTIGRAVITY_HOME pointed at a temp dir, so
// the "Editor Mode" gate is deterministic and the user's real
// settings.json is never touched.

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/vim"
)

// vimTestMode builds a runMode in an isolated config home seeded with
// the given settings. NewRunMode itself is inert (vim off); the settings
// are applied the same way the production entry point does it.
func vimTestMode(t *testing.T, settings map[string]any) *runMode {
	t.Helper()
	cycle196TestHome(t, settings)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.reloadVimSettings()
	return m
}

// vimPress sends one key event through the run loop.
func vimPress(t *testing.T, m *runMode, msg tea.Msg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(msg)
	return cmd
}

// vimType feeds a string of printable runes.
func vimType(t *testing.T, m *runMode, s string) {
	t.Helper()
	for _, r := range s {
		vimPress(t, m, runeKey(r))
	}
}

func vimEscKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
}

// --- key mapping -------------------------------------------------------

func TestVimKeyMapping(t *testing.T) {
	if k := vimNamedKey(tea.Key{Code: tea.KeyEsc}); k.Name != "esc" {
		t.Errorf("esc → %+v", k)
	}
	if k := vimNamedKey(tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl}); k.Name != "enter" || k.Mod != vim.Ctrl {
		t.Errorf("ctrl+enter → %+v", k)
	}
	if k := vimNamedKey(tea.Key{Code: 'a'}); k != (vim.Key{}) {
		t.Errorf("a should not be a named key, got %+v", k)
	}
	if k, ok := vimRuneKey(tea.Key{Code: 'a'}); !ok || k.Rune != 'a' {
		t.Errorf("a → %+v/%v", k, ok)
	}
	if _, ok := vimRuneKey(tea.Key{}); ok {
		t.Error("a bare modifier (no code) must not map to a key")
	}
	// Enter arrives with Text populated in some drivers; the named mapping
	// must still win so it cannot be typed as a literal newline.
	if _, ok := vimRuneKey(tea.Key{Code: tea.KeyEnter, Text: "\n"}); !ok {
		t.Error("enter should still have a rune mapping available")
	}
	if k := vimNamedKey(tea.Key{Code: tea.KeyEnter, Text: "\n"}); k.Name != "enter" {
		t.Errorf("enter with text → %+v, want name enter", k)
	}
}

// --- mode gating -------------------------------------------------------

func TestVimDisabledLeavesInputAlone(t *testing.T) {
	m := vimTestMode(t, map[string]any{})
	if m.vimEnabled() {
		t.Fatal("vim should be off with empty settings")
	}
	vimType(t, m, "abc")
	if got := m.input.Value(); got != "abc" {
		t.Fatalf("non-vim textarea = %q, want abc", got)
	}
	if m.vimEng != nil {
		t.Fatal("the engine must not be built when vim is off")
	}
	if got := m.vimBadge(); got != "" {
		t.Fatalf("badge = %q, want empty", got)
	}
}

func TestVimNormalModeConsumesRunes(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	if !m.vimEnabled() {
		t.Fatal("vim should be on")
	}
	// In Normal mode "a" opens Insert; the following runes are text.
	vimType(t, m, "abc")
	if got := m.input.Value(); got != "bc" {
		t.Fatalf("textarea = %q, want bc", got)
	}
	if m.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want Insert", m.vimEng.Mode)
	}
	// Esc returns to Normal without touching the buffer.
	vimPress(t, m, vimEscKey())
	if m.vimEng.Mode != vim.Normal {
		t.Fatalf("mode = %v, want Normal", m.vimEng.Mode)
	}
	if got := m.input.Value(); got != "bc" {
		t.Fatalf("textarea after Esc = %q, want bc", got)
	}
	// Esc never clears the prompt in vim mode.
	if got := m.input.Value(); got == "" {
		t.Fatal("Esc must not clear the prompt in vim mode")
	}
}

// --- submit ------------------------------------------------------------

func TestVimInsertCtrlSSubmits(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello")
	if got := m.input.Value(); got != "hello" {
		t.Fatalf("textarea = %q, want hello", got)
	}
	vimPress(t, m, ctrlKey('s'))
	if !m.streaming {
		t.Fatal("ctrl+s should submit from Insert mode")
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("textarea after submit = %q, want empty", got)
	}
}

func TestVimNormalEnterSubmits(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	// A bare Enter in Normal mode submits: the vim-normal map binds only
	// ctrl+s / ctrl+enter, so Enter falls through to the global binding.
	vimPress(t, m, enterKey())
	if !m.streaming {
		t.Fatal("plain Enter in Normal mode should submit")
	}

	// ctrl+s is the other Normal-mode submit binding.
	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m2, "iabc")
	vimPress(t, m2, vimEscKey())
	vimPress(t, m2, ctrlKey('s'))
	if !m2.streaming {
		t.Fatal("ctrl+s should submit from Normal mode")
	}
}

func TestVimOpenLineEntersInsert(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	vimPress(t, m, runeKey('o'))
	if got := m.vimBadge(); got != "-- INSERT --" {
		t.Fatalf("badge after o = %q, want -- INSERT --", got)
	}
	if got := m.input.Value(); got != "abc\n" {
		t.Fatalf("textarea after o = %q, want %q", got, "abc\n")
	}

	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m2, "iab")
	vimPress(t, m2, vimEscKey())
	vimPress(t, m2, runeKey('O'))
	if got := m2.input.Value(); got != "\nab" {
		t.Fatalf("textarea after O = %q, want %q", got, "\nab")
	}
}

// --- Insert First ------------------------------------------------------

func TestVimInsertFirstOpensInInsert(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim", "vimInsertFirst": true})
	if got := m.vimBadge(); got != "-- INSERT --" {
		t.Fatalf("badge = %q, want -- INSERT --", got)
	}
	// The first rune is text, not a command.
	vimType(t, m, "hi")
	if got := m.input.Value(); got != "hi" {
		t.Fatalf("textarea = %q, want hi", got)
	}
	// A bare Enter submits...
	vimPress(t, m, enterKey())
	if !m.streaming {
		t.Fatal("bare Enter under Insert First should submit")
	}
}

func TestVimInsertFirstShiftEnterInsertsNewline(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim", "vimInsertFirst": true})
	vimType(t, m, "hi")
	vimPress(t, m, shiftEnterKey())
	if m.streaming {
		t.Fatal("shift+enter under Insert First must insert a newline, not submit")
	}
	if got := m.input.Value(); got != "hi\n" {
		t.Fatalf("textarea = %q, want %q", got, "hi\n")
	}
	// ctrl+j inserts a newline too.
	vimPress(t, m, ctrlKey('j'))
	if got := m.input.Value(); got != "hi\n\n" {
		t.Fatalf("textarea = %q, want %q", got, "hi\n\n")
	}
}

// --- textarea mirroring ------------------------------------------------

func TestVimCursorMirrorsEngine(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello world")
	vimPress(t, m, vimEscKey())
	// 0 then w: the engine lands on "world" (col 6).
	vimPress(t, m, runeKey('0'))
	vimPress(t, m, runeKey('w'))
	if m.vimEng.Col != 6 {
		t.Fatalf("engine col = %d, want 6", m.vimEng.Col)
	}
	// The textarea reports the same position (row 0, col 6).
	if row, col := m.input.Line(), m.input.Column(); row != 0 || col != 6 {
		t.Fatalf("textarea cursor = (row %d, col %d), want (0,6)", row, col)
	}
}

func TestVimEngineReseedsAfterHostRewrite(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello")
	vimPress(t, m, vimEscKey())
	// The host rewrites the buffer behind the engine's back (history
	// recall, picker substitution, slash rewrite).
	m.input.SetValue("world")
	vimType(t, m, "A!")
	if got := m.input.Value(); got != "world!" {
		t.Fatalf("textarea = %q, want world!", got)
	}
}

// --- badges and /help --------------------------------------------------

func TestVimStatusBadges(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	if got := m.vimBadge(); got != "" {
		t.Fatalf("Normal mode badge = %q, want none", got)
	}
	vimType(t, m, "i")
	if got := m.vimBadge(); got != "-- INSERT --" {
		t.Fatalf("Insert badge = %q", got)
	}
	// With an empty prompt the hint survives and the badge is prepended
	// to it (the reference's status line is "-- INSERT -- ? for …").
	if row := m.statusRow(); !strings.Contains(row, "-- INSERT --") || !strings.Contains(row, "? for shortcuts") {
		t.Fatalf("status row = %q, want badge before the hint", row)
	}
	vimPress(t, m, vimEscKey())
	vimType(t, m, "v")
	if got := m.vimBadge(); got != "-- VISUAL --" {
		t.Fatalf("Visual badge = %q", got)
	}
	vimType(t, m, "V")
	if got := m.vimBadge(); got != "-- V-LINE --" {
		t.Fatalf("Visual line badge = %q", got)
	}
	// The badge lands on the status row.
	if row := m.statusRow(); !strings.Contains(row, "-- V-LINE --") {
		t.Fatalf("status row missing badge: %q", row)
	}
}

func TestHelpTabsGainVimOnlyWhenEnabled(t *testing.T) {
	off := vimTestMode(t, map[string]any{})
	if got := helpTabs(off); len(got) != 3 {
		t.Fatalf("tabs with vim off = %v, want 3", got)
	}
	if got := renderTabBar(0); strings.Contains(got, helpVimTitle) {
		t.Fatal("the default tab bar must not mention vim")
	}

	on := vimTestMode(t, map[string]any{"editorMode": "vim"})
	tabs := helpTabs(on)
	if len(tabs) != 4 || tabs[3] != helpVimTitle {
		t.Fatalf("tabs with vim on = %v", tabs)
	}
	bar := renderTabBarTabs(3, tabs)
	if !strings.Contains(bar, helpVimTitle) {
		t.Fatalf("4-tab bar missing vim: %q", bar)
	}
	if got := helpListLen(3); got != len(helpVim) {
		t.Fatalf("helpListLen(3) = %d, want %d", got, len(helpVim))
	}
	out := renderHelp(3, "Gemini 3.8 Flash", "", "/tmp", "u@t", "id", on)
	if !strings.Contains(out, "Vim Modal Shortcuts") {
		t.Fatalf("vim help tab output = %q", out)
	}
	// helpListLen for the original tabs is unchanged.
	if got := helpListLen(2); got != len(helpShortcuts) {
		t.Fatalf("helpListLen(2) = %d, want %d", got, len(helpShortcuts))
	}
}

// --- global keys still reach the host ----------------------------------

func TestVimDeclinesGlobalKeys(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	// ctrl+c is not a vim binding: the engine declines it and the host's
	// two-step exit confirmation runs instead.
	vimPress(t, m, ctrlKey('c'))
	if !m.exitConfirm {
		t.Fatal("ctrl+c should arm the exit confirmation even in vim mode")
	}
	if m.quitting {
		t.Fatal("the first ctrl+c must not quit")
	}
	// Ctrl+D exits immediately.
	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimPress(t, m2, ctrlKey('d'))
	if !m2.quitting {
		t.Fatal("ctrl+d should quit even in vim mode")
	}
}
