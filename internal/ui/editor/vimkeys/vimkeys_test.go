package vimkeys

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/vim"
)

// The engine's own names, in the order the switch spells them.
func TestNamed(t *testing.T) {
	cases := []struct {
		key  tea.Key
		want string
	}{
		{tea.Key{Code: tea.KeyEsc}, "esc"},
		{tea.Key{Code: tea.KeyEnter}, "enter"},
		{tea.Key{Code: tea.KeyBackspace}, "backspace"},
		{tea.Key{Code: tea.KeyDelete}, "delete"},
		{tea.Key{Code: tea.KeyLeft}, "left"},
		{tea.Key{Code: tea.KeyRight}, "right"},
		{tea.Key{Code: tea.KeyUp}, "up"},
		{tea.Key{Code: tea.KeyDown}, "down"},
		{tea.Key{Code: tea.KeyHome}, "home"},
		{tea.Key{Code: tea.KeyEnd}, "end"},
		{tea.Key{Code: tea.KeyTab}, "tab"},
	}
	for _, tc := range cases {
		got := Named(tc.key)
		if got.Name != tc.want {
			t.Errorf("Named(%+v) = %q, want %q", tc.key, got.Name, tc.want)
		}
		if got.Rune != 0 {
			t.Errorf("Named(%+v) carried a rune: %+v", tc.key, got)
		}
	}
}

// A code the engine does not name is not a named key: the caller tells
// the two apart with a plain comparison against the zero Key.
func TestNamed_UnknownCodeIsZeroKey(t *testing.T) {
	if k := Named(tea.Key{Code: 'a'}); k != (vim.Key{}) {
		t.Errorf("a should not be a named key, got %+v", k)
	}
	if k := Named(tea.Key{}); k != (vim.Key{}) {
		t.Errorf("an empty key should not be a named key, got %+v", k)
	}
}

// The modifiers travel with the named key.
func TestNamed_KeepsModifiers(t *testing.T) {
	k := Named(tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl})
	if k.Name != "enter" || k.Mod != vim.Ctrl {
		t.Errorf("ctrl+enter → %+v", k)
	}
}

// Enter arrives with the text populated in some drivers; the named
// mapping must still win so it cannot be typed as a literal newline.
func TestNamed_EnterWithText(t *testing.T) {
	if k := Named(tea.Key{Code: tea.KeyEnter, Text: "\n"}); k.Name != "enter" {
		t.Errorf("enter with text → %+v, want name enter", k)
	}
}

func TestRune(t *testing.T) {
	k, ok := Rune(tea.Key{Code: 'a'})
	if !ok || k.Rune != 'a' {
		t.Errorf("a → %+v/%v", k, ok)
	}
	// A bare modifier press carries no code at all.
	if _, ok := Rune(tea.Key{}); ok {
		t.Error("a bare modifier (no code) must not map to a key")
	}
	// The enter code has a rune mapping available as well.
	if _, ok := Rune(tea.Key{Code: tea.KeyEnter, Text: "\n"}); !ok {
		t.Error("enter should still have a rune mapping available")
	}
}

func TestModifiers(t *testing.T) {
	cases := []struct {
		mod  tea.KeyMod
		want vim.Mod
	}{
		{0, 0},
		{tea.ModCtrl, vim.Ctrl},
		{tea.ModAlt, vim.Alt},
		{tea.ModShift, vim.Shift},
		{tea.ModCtrl | tea.ModAlt, vim.Ctrl | vim.Alt},
		{tea.ModCtrl | tea.ModAlt | tea.ModShift, vim.Ctrl | vim.Alt | vim.Shift},
	}
	for _, tc := range cases {
		if got := Modifiers(tc.mod); got != tc.want {
			t.Errorf("Modifiers(%v) = %v, want %v", tc.mod, got, tc.want)
		}
	}
}

func TestBadge(t *testing.T) {
	cases := []struct {
		enabled bool
		mode    vim.Mode
		want    string
	}{
		{false, vim.Normal, ""},
		{false, vim.Insert, ""},
		{true, vim.Normal, ""},
		{true, vim.Insert, "-- INSERT --"},
		{true, vim.Visual, "-- VISUAL --"},
		{true, vim.VisualLine, "-- V-LINE --"},
	}
	for _, tc := range cases {
		if got := Badge(tc.enabled, tc.mode); got != tc.want {
			t.Errorf("Badge(%v, %v) = %q, want %q", tc.enabled, tc.mode, got, tc.want)
		}
	}
}
