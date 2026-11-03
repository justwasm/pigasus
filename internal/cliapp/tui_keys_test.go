// tui_keys_test.go — the key helpers the TUI tests share.
//
// The helpers build the key messages the session's Update path
// receives, and strip the ANSI escapes off a rendered row.
package cliapp

import (
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// key builds a key press message.
func key(code rune, mod tea.KeyMod) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
}

// stripAnsi removes the ANSI escapes from a rendered row.
func stripAnsi(s string) string { return ansi.Strip(s) }
