// mention_host_test.go — the "@" file-mention dropdown's wiring in the
// host.
//
// The dropdown is a panel (internal/ui/views/mentionpicker); the
// panel's own tests drive the panel directly. This test drives the
// *host*: it types into the composer, asks the panel for its rows and
// routes the keys the panel owns.
//
// The end-to-end wiring is the point: the host re-derives the
// dropdown's state from the composer after every keystroke, the rows
// reach the frame, and Tab and Enter reach the panel.

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mentionWorkspace builds a workspace with one file to mention.
func mentionWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(""), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return dir
}

// mentionMode builds a host whose workspace holds one file.
func mentionMode(t *testing.T) *RootModel {
	t.Helper()
	m := sizedMode(80, 24, 0)
	m.session.cwd = mentionWorkspace(t)
	return m
}

// TestMentionPicker_RowsReachTheFrame covers the dropdown's rows
// reaching the painted frame.
func TestMentionPicker_RowsReachTheFrame(t *testing.T) {
	m := mentionMode(t)
	m.prompt.input.SetValue("look at @ma")
	m.prompt.atFilePicker.Update()
	if !m.prompt.atFilePicker.Visible() {
		t.Fatal("setup: the dropdown should be visible for a mention")
	}

	frame := m.View().Content
	if !strings.Contains(stripAnsi(frame), "main.go") {
		t.Errorf("the painted frame should carry the dropdown's rows, got:\n%s", frame)
	}
}

// TestMentionPicker_TabCompletesTheMention covers the Tab path: the
// mention is completed into the composer and the dropdown stays open
// for the next directory or file.
func TestMentionPicker_TabCompletesTheMention(t *testing.T) {
	m := mentionMode(t)
	m.prompt.input.SetValue("look at @ma")
	m.prompt.atFilePicker.Update()
	if !m.prompt.atFilePicker.Visible() {
		t.Fatal("setup: the dropdown should be visible for a mention")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})

	if got := m.prompt.input.Value(); got != "look at @main.go" {
		t.Errorf("the composer after Tab = %q, want %q", got, "look at @main.go")
	}
	if !m.prompt.atFilePicker.Visible() {
		t.Error("Tab must keep the dropdown open for directory and file browsing")
	}
}

// TestMentionPicker_EnterCompletesAndCloses covers the Enter path: the
// mention is completed into the composer and the dropdown closes.
func TestMentionPicker_EnterCompletesAndCloses(t *testing.T) {
	m := mentionMode(t)
	m.prompt.input.SetValue("look at @ma")
	m.prompt.atFilePicker.Update()
	if !m.prompt.atFilePicker.Visible() {
		t.Fatal("setup: the dropdown should be visible for a mention")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if got := m.prompt.input.Value(); got != "look at @main.go" {
		t.Errorf("the composer after Enter = %q, want %q", got, "look at @main.go")
	}
	if m.prompt.atFilePicker.Visible() {
		t.Error("Enter must close the dropdown")
	}
}

// TestMentionPicker_EscClosesTheDropdown covers the Esc path.
func TestMentionPicker_EscClosesTheDropdown(t *testing.T) {
	m := mentionMode(t)
	m.prompt.input.SetValue("look at @ma")
	m.prompt.atFilePicker.Update()
	if !m.prompt.atFilePicker.Visible() {
		t.Fatal("setup: the dropdown should be visible for a mention")
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})

	if m.prompt.atFilePicker.Visible() {
		t.Error("Esc must close the dropdown")
	}
}
