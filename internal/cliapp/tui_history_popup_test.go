// tui_history_popup_test.go — the composer overlays are armed by
// *typing*, not by a history recall.
//
// Cycle 295. The reference (agy 1.4.7) stores slash commands in its
// prompt history — history.jsonl entries carry "type":"slash_command"
// — so paging up the history lands on a buffer that starts with "/".
// The reference leaves its command popup closed for such a recall and
// keeps paging; only the next typed keystroke re-derives the popup.
//
// 【一手】 agy 1.4.7, live (tmux 220×40, 2026-10-25):
//
//	Up            → composer "/agents" (recalled slash_command),
//	               command popup CLOSED
//	Up            → composer holds the next-older entry — the
//	               history pages across slash commands
//	Down          → back to "/agents", popup still CLOSED
//	Up, then "x"  → composer "/agentsx", popup OPEN ("No matches")
//
// The @ file-mention dropdown follows the same rule: recalling an
// entry that contains "@zzzprobe" leaves the dropdown closed.
//
// Before cycle 295 the recall re-derived both overlays, so the popup
// swallowed the next Up/Down and the history stopped paging at the
// first slash command in the list.
package cliapp

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// historyPopupMode returns a runMode whose ANTIGRAVITY_HOME and cwd
// are both inside a temp dir: the recall must not touch the user's
// real agy home, and the @ dropdown must not walk a real tree.
func historyPopupMode(t *testing.T) *runMode {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", dir, true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	return m
}

// A recalled slash command must not arm the command popup, and the
// history must keep paging past it.
func TestHistoryUp_SlashCommandEntryKeepsTheCommandPopupClosed(t *testing.T) {
	m := historyPopupMode(t)
	m.pushHistory("older prompt")
	m.pushHistory("/accounts")

	m.Update(upKey())
	if got := m.input.Value(); got != "/accounts" {
		t.Fatalf("Up #1: input = %q, want %q", got, "/accounts")
	}
	if m.picker.Visible() {
		t.Errorf("the recalled slash command armed the command popup; it must stay closed")
	}

	// The next Up has to page the history instead of moving the
	// popup's highlight.
	m.Update(upKey())
	if got := m.input.Value(); got != "older prompt" {
		t.Errorf("Up #2: input = %q, want %q — the history must page past a slash command",
			got, "older prompt")
	}
	if m.picker.Visible() {
		t.Errorf("the command popup is visible after paging past the slash command")
	}
}

// The same for the way back down.
func TestHistoryDown_SlashCommandEntryKeepsTheCommandPopupClosed(t *testing.T) {
	m := historyPopupMode(t)
	m.pushHistory("older prompt")
	m.pushHistory("/accounts")

	m.Update(upKey())
	m.Update(upKey())
	if got := m.input.Value(); got != "older prompt" {
		t.Fatalf("pre-condition: input = %q, want %q", got, "older prompt")
	}

	m.Update(downKey())
	if got := m.input.Value(); got != "/accounts" {
		t.Errorf("Down: input = %q, want %q — the history must page back to the slash command",
			got, "/accounts")
	}
	if m.picker.Visible() {
		t.Errorf("the recalled slash command armed the command popup; it must stay closed")
	}
}

// A recalled @ mention must not arm the file-mention dropdown.
func TestHistoryUp_AtMentionEntryKeepsTheFileDropdownClosed(t *testing.T) {
	m := historyPopupMode(t)
	m.pushHistory("older prompt")
	m.pushHistory("look at @notes.md please")

	m.Update(upKey())
	if got := m.input.Value(); got != "look at @notes.md please" {
		t.Fatalf("Up #1: input = %q, want %q", got, "look at @notes.md please")
	}
	if m.atFileDropdown.Visible() {
		t.Errorf("the recalled @ mention armed the file dropdown; it must stay closed")
	}

	m.Update(upKey())
	if got := m.input.Value(); got != "older prompt" {
		t.Errorf("Up #2: input = %q, want %q — the history must page past the @ mention",
			got, "older prompt")
	}
	if m.atFileDropdown.Visible() {
		t.Errorf("the file dropdown is visible after paging past the @ mention")
	}
}

// The next *typed* keystroke re-derives the popup — the reference
// shows the "No matches" row once the user types one character into
// the recalled "/agents".
func TestHistoryUp_TypedKeyAfterRecallArmsTheCommandPopup(t *testing.T) {
	m := historyPopupMode(t)
	m.pushHistory("/accounts")

	m.Update(upKey())
	if got := m.input.Value(); got != "/accounts" {
		t.Fatalf("pre-condition: input = %q, want %q", got, "/accounts")
	}
	if m.picker.Visible() {
		t.Fatalf("pre-condition: the popup must be closed right after the recall")
	}

	m.Update(runeKey('x'))
	if got := m.input.Value(); got != "/accountsx" {
		t.Errorf("after typing 'x': input = %q, want %q", got, "/accountsx")
	}
	if !m.picker.Visible() {
		t.Errorf("the typed keystroke did not re-derive the command popup")
	}
}
