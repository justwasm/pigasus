package main

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/convstore"
)

// TestCtrlD_ForwardDeleteWhenTextPresent verifies that when the prompt
// contains text, Ctrl+D deletes the character under the cursor forward
// and does not exit. When the prompt is empty, Ctrl+D triggers exit.
func TestCtrlD_ForwardDeleteWhenTextPresent(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.input.SetValue("hello")
	m.input.CursorStart()

	// Pressing Ctrl+D should delete 'h' (forward delete) -> "ello", not quit.
	m.Update(ctrlKey('d'))
	if m.quitting {
		t.Fatal("ctrl+d on non-empty prompt must not quit")
	}
	if got := m.input.Value(); got != "ello" {
		t.Fatalf("ctrl+d did not delete character forward: got %q, want 'ello'", got)
	}

	// With text selected, Ctrl+D deletes the selection.
	m.input.SetValue("abcdef")
	m.inputSelStart = 1
	m.inputSelEnd = 4 // "bcd"
	m.Update(ctrlKey('d'))
	if m.quitting {
		t.Fatal("ctrl+d on active selection must not quit")
	}
	if got := m.input.Value(); got != "aef" {
		t.Fatalf("ctrl+d did not delete selection: got %q, want 'aef'", got)
	}

	// When input is empty, Ctrl+D triggers exit.
	m.input.Reset()
	m.clearInputSelection()
	m.Update(ctrlKey('d'))
	if !m.quitting {
		t.Fatal("ctrl+d on empty prompt must trigger quit")
	}
}

// TestNewlineShortcuts verifies:
// 1. \ + Enter (Insert newline fallback): replaces trailing '\' before cursor with '\n'.
// 2. Alt+Enter and Shift+Enter: inserts newline '\n' at cursor.
func TestNewlineShortcuts(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)

	// 1. \ + Enter fallback
	m.input.SetValue("echo foo \\")
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.input.Value(); got != "echo foo \n" {
		t.Fatalf("\\ + Enter did not replace \\ with newline: got %q, want %q", got, "echo foo \n")
	}

	// 2. Alt+Enter
	m.input.SetValue("line 1")
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if got := m.input.Value(); got != "line 1\n" {
		t.Fatalf("Alt+Enter did not insert newline: got %q, want %q", got, "line 1\n")
	}

	// 3. Shift+Enter
	m.input.SetValue("line A")
	m.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if got := m.input.Value(); got != "line A\n" {
		t.Fatalf("Shift+Enter did not insert newline: got %q, want %q", got, "line A\n")
	}
}

// TestF2_GlobalOpensRenamePanel verifies that pressing F2 in the main
// prompt opens the rename conversation panel.
func TestF2_GlobalOpensRenamePanel(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.conversationName = "My Initial Name"

	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.panel.kind != panelRename {
		t.Fatalf("F2 did not open rename panel; panel.kind = %v", m.panel.kind)
	}
	if got := m.panel.textInput.Value(); got != "My Initial Name" {
		t.Errorf("rename textinput initial value = %q, want 'My Initial Name'", got)
	}
}

// TestResume_F4DeletesConversation verifies that pressing F4 in /resume
// mode deletes the selected conversation from disk and updates the list.
func TestResume_F4DeletesConversation(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("HOME", tmpDir)

	convID := "test-conv-f4"
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 1, Text: "hello"},
	}
	if err := convstore.SaveConversation(convID, "/workspace", steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.resumeMode = true
	m.reloadResumeList()
	if len(m.resumeList) == 0 {
		t.Fatalf("expected saved conversation in resumeList")
	}

	// Press F4 to delete
	m.Update(tea.KeyPressMsg{Code: tea.KeyF4})

	// Check on disk that conversation file was removed
	convPath := filepath.Join(tmpDir, ".gemini", "antigravity-cli", "conversations", convID+".db")
	if _, err := os.Stat(convPath); !os.IsNotExist(err) {
		t.Fatalf("conversation file still exists at %s after F4 delete", convPath)
	}
}

// TestResume_F2RenamesConversation verifies that pressing F2 in /resume
// mode closes resume mode and opens the rename panel for that conversation.
func TestResume_F2RenamesConversation(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.resumeMode = true
	m.resumeList = []convstore.ConversationMeta{
		{ID: "c1", Title: "Original Title", StepCount: 3},
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyF2})
	if m.resumeMode {
		t.Fatal("resumeMode should be closed after F2 rename")
	}
	if m.panel.kind != panelRename {
		t.Fatalf("F2 in resume should open panelRename; got %v", m.panel.kind)
	}
	if got := m.conversationName; got != "Original Title" {
		t.Errorf("conversationName = %q, want 'Original Title'", got)
	}
}
