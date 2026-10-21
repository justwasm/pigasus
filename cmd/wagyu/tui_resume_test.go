package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// TestResume_PopulatesFromStoreDir verifies cycle 167: when /resume
// runs the picker populates from convstore.ListConversations and the
// rendered rows include the real conversations under StoreDir().
func TestResume_PopulatesFromStoreDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)

	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	steps1 := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "hello world"},
		{Idx: 1, Type: convstore.StepModelResponse, Status: 3, Text: "hi there"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "aaaa1111-0000-4000-8000-000000000001.db"),
		"traj-1", steps1); err != nil {
		t.Fatal(err)
	}
	steps2 := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "fix the bug please"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "bbbb2222-0000-4000-8000-000000000002.db"),
		"traj-2", steps2); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/resume")
	if !handled {
		t.Fatal("/resume should be handled")
	}
	if !m.resumeMode {
		t.Fatal("/resume should open the picker")
	}
	if len(m.resumeList) != 2 {
		t.Fatalf("expected 2 conversations, got %d", len(m.resumeList))
	}
	body := m.resumeView()
	// Cycle 184: the picker shows the conversation's title (the
	// first user-input step's text, trimmed to one line), not its
	// file name / trajectory ID. The two test conversations have
	// distinct titles, so check for those.
	if !strings.Contains(body, "fix the bug please") {
		t.Errorf("picker missing first conversation title: %q", body)
	}
	if !strings.Contains(body, "hello world") {
		t.Errorf("picker missing second conversation title: %q", body)
	}
}

// TestResume_UpDownMovesCursor verifies cycle 167: keyboard arrows
// in the resume picker move the cursor; Esc cancels.
func TestResume_UpDownMovesCursor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}
	for i, label := range []string{"first", "second", "third"} {
		id := filepath.Join(dir, "conversations",
			"00000000-0000-4000-8000-00000000000"+string(rune('1'+i))+".db")
		_ = convstore.WriteConversation(id, "traj-"+label,
			[]convstore.Step{
				{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: label},
			})
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	_, _ = m.handleSlashCommand("/resume")
	if !m.resumeMode {
		t.Fatal("/resume should open")
	}
	if m.resumeCursor != 0 {
		t.Fatalf("initial cursor = %d, want 0", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 1 {
		t.Fatalf("after down, cursor = %d, want 1", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 2 {
		t.Fatalf("after 2nd down, cursor = %d, want 2", m.resumeCursor)
	}
	m.Update(key(tea.KeyDown, 0))
	if m.resumeCursor != 2 {
		t.Fatalf("down should clamp at last (2), got %d", m.resumeCursor)
	}
	m.Update(key(tea.KeyUp, 0))
	if m.resumeCursor != 1 {
		t.Fatalf("after up, cursor = %d, want 1", m.resumeCursor)
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.resumeMode {
		t.Fatal("Esc should close the picker")
	}
}

// TestFork_WritesConvStoreAndAppearsInPicker verifies cycle 167 +
// cycle 184: /fork writes a new SQLite db and switches the current
// session to it, which (per REF v1.4.0) hides it from the next
// /resume — the picker only surfaces past conversations. To check
// that a past conversation still appears, this test pre-writes a
// second .db that the fork does *not* touch.
func TestFork_WritesConvStoreAndAppearsInPicker(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "conversations"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Past conversation: not touched by /fork, so it survives in
	// the picker. Written with a known title so the assertion can
	// match by content rather than by file name.
	pastSteps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "an older prompt"},
	}
	if err := convstore.WriteConversation(
		filepath.Join(dir, "conversations", "bbbb2222-0000-4000-8000-000000000002.db"),
		"traj-past", pastSteps); err != nil {
		t.Fatal(err)
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session = []backend.Message{
		{Role: "user", Content: "do thing 1"},
		{Role: "assistant", Content: "ok"},
		{Role: "user", Content: "do thing 2"},
		{Role: "assistant", Content: "ok done"},
	}

	_, _ = m.handleSlashCommand("/fork")
	if len(m.session) == 0 {
		t.Fatal("fork should not wipe the live session")
	}
	entries, err := os.ReadDir(filepath.Join(dir, "conversations"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 dbs (past + fork), got %d", len(entries))
	}

	m.resumeMode = false
	m.resumeList = nil
	_, _ = m.handleSlashCommand("/resume")
	if !m.resumeMode {
		t.Fatalf("/resume should open with the past conversation present (resp=%v)", m.response)
	}
	// The forked conversation is now the current session, so the
	// picker should show only the past one.
	if len(m.resumeList) != 1 {
		t.Fatalf("expected 1 conversation (the fork is current and hidden), got %d", len(m.resumeList))
	}
	if m.resumeList[0].Title == "" || !strings.Contains(m.resumeList[0].Title, "older prompt") {
		t.Errorf("picker should show the past conversation, got %q", m.resumeList[0].Title)
	}
}
