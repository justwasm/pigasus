// Package vimkeys translates bubbletea key events into the vim
// engine's normalised keys, and renders the editor's mode badge.
//
// The engine itself — the public vim package — is a pure state machine
// that knows nothing about bubbletea, lipgloss or the run loop (see
// vim/vim.go). The mapping from a terminal key event to an engine key
// therefore had no home in the engine, and it sat in the TUI host
// instead. This package is that home: the host names the package, the
// package names the engine.
//
// Every function here is a pure translation, so the whole surface is
// unit-testable without a terminal, without a run loop and without a
// host.
//
// # The badge
//
// The badge is the status row's mode indicator — the engine's mode,
// spelled the way the reference spells it ("-- INSERT --", "-- VISUAL --",
// "-- V-LINE --"). It is empty when vim mode is off, and also in Normal
// mode: like Vim, Normal shows nothing.
//
// The three strings belong to the engine (vim.Mode.Badge); the "vim is
// off" rule belongs here, because whether vim mode is on is the host's
// fact and the rule is a rendering one.
package vimkeys

import (
	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/vim"
)

// Modifiers converts the bubbletea modifier set to the engine's.
//
// The engine knows three modifiers — Ctrl, Alt and Shift — and the
// terminal's set is a superset of them, so the extra bits are dropped
// rather than rejected: a key the engine does not know about is still
// a key the engine has to see.
func Modifiers(mod tea.KeyMod) vim.Mod {
	var out vim.Mod
	if mod.Contains(tea.ModCtrl) {
		out |= vim.Ctrl
	}
	if mod.Contains(tea.ModAlt) {
		out |= vim.Alt
	}
	if mod.Contains(tea.ModShift) {
		out |= vim.Shift
	}
	return out
}

// Named maps a special key code to the engine's named key.
//
// It reports the zero Key when the code is not one of the named keys,
// which lets the caller test it with a plain comparison. A named key
// wins over the key's text: Enter arrives with the text "\n" in some
// drivers, and it must never be typed as a literal newline.
func Named(key tea.Key) vim.Key {
	mod := Modifiers(key.Mod)
	switch key.Code {
	case tea.KeyEsc:
		return vim.Key{Name: "esc", Mod: mod}
	case tea.KeyEnter:
		return vim.Key{Name: "enter", Mod: mod}
	case tea.KeyBackspace:
		return vim.Key{Name: "backspace", Mod: mod}
	case tea.KeyDelete:
		return vim.Key{Name: "delete", Mod: mod}
	case tea.KeyLeft:
		return vim.Key{Name: "left", Mod: mod}
	case tea.KeyRight:
		return vim.Key{Name: "right", Mod: mod}
	case tea.KeyUp:
		return vim.Key{Name: "up", Mod: mod}
	case tea.KeyDown:
		return vim.Key{Name: "down", Mod: mod}
	case tea.KeyHome:
		return vim.Key{Name: "home", Mod: mod}
	case tea.KeyEnd:
		return vim.Key{Name: "end", Mod: mod}
	case tea.KeyTab:
		return vim.Key{Name: "tab", Mod: mod}
	}
	return vim.Key{}
}

// Rune maps anything with a key code to a rune key.
//
// It reports false for a bare modifier press (no code) or an unknown
// function key — the two shapes the engine has no meaning for.
func Rune(key tea.Key) (vim.Key, bool) {
	if key.Code == 0 {
		return vim.Key{}, false
	}
	return vim.Key{Rune: key.Code, Mod: Modifiers(key.Mod)}, true
}

// Badge returns the status row's mode badge.
//
// It is empty when vim mode is off, and also in Normal mode — like
// Vim, Normal shows nothing.
func Badge(enabled bool, mode vim.Mode) string {
	if !enabled {
		return ""
	}
	return mode.Badge()
}
