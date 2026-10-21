// keytext.go — cycle 278: one definition of "this key inserts text".
package main

import tea "charm.land/bubbletea/v2"

// isTypedText reports whether key carries printable text the user meant to
// insert into a search/filter field.
//
// Shift must be allowed. Terminals report an uppercase letter (or a shifted
// symbol such as "_") with ModShift set, so the old `key.Mod == 0` guards
// silently dropped them: typing "Fixing" into the /resume search produced
// "ixing", and "A_B" produced "_". Ctrl and Alt are the modifiers that mean
// "this is a command, not text".
func isTypedText(key tea.Key) bool {
	return key.Text != "" &&
		!key.Mod.Contains(tea.ModCtrl) &&
		!key.Mod.Contains(tea.ModAlt)
}
