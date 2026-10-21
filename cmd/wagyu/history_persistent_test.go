package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/convstore"
)

// TestLoadHistory_PopulatesHistoryFromDisk pins the cycle-286 parity fix:
// agy loads previous prompt history from history.jsonl on startup so the
// user can immediately use the Up arrow to cycle through prompts across sessions.
func TestLoadHistory_PopulatesHistoryFromDisk(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	// Pre-seed history.jsonl with 3 entries.
	entries := []convstore.HistoryEntry{
		{Display: "first command", Workspace: "/workspace"},
		{Display: "second command", Workspace: "/workspace"},
		{Display: "/help", Workspace: "/workspace", Type: "slash_command"},
	}
	for _, e := range entries {
		if err := convstore.AppendHistoryEntry(e); err != nil {
			t.Fatalf("AppendHistoryEntry failed: %v", err)
		}
	}

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	if len(m.history) != 0 {
		t.Fatalf("expected empty m.history before loadHistory(), got %d", len(m.history))
	}

	m.loadHistory()
	if len(m.history) != 3 {
		t.Fatalf("expected 3 entries in m.history after loadHistory(), got %d: %v", len(m.history), m.history)
	}

	// Navigate backward with Up arrow.
	m.historyUp()
	if got := m.input.Value(); got != "/help" {
		t.Errorf("first Up = %q, want %q", got, "/help")
	}

	m.historyUp()
	if got := m.input.Value(); got != "second command" {
		t.Errorf("second Up = %q, want %q", got, "second command")
	}

	m.historyUp()
	if got := m.input.Value(); got != "first command" {
		t.Errorf("third Up = %q, want %q", got, "first command")
	}

	// Navigate forward with Down arrow.
	m.historyDown()
	if got := m.input.Value(); got != "second command" {
		t.Errorf("first Down = %q, want %q", got, "second command")
	}

	m.historyDown()
	if got := m.input.Value(); got != "/help" {
		t.Errorf("second Down = %q, want %q", got, "/help")
	}

	m.historyDown()
	if got := m.input.Value(); got != "" {
		t.Errorf("third Down (back to edit) = %q, want empty", got)
	}
}

// TestSubmitPrompt_CapturesSlashCommandInHistory pins the requirement from
// the reference changelog: "Added support for capturing slash command history,
// allowing users to use the up arrow to replay previously entered slash commands."
func TestSubmitPrompt_CapturesSlashCommandInHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.input.SetValue("/clear")
	_ = m.submitPrompt()

	if len(m.history) != 1 || m.history[0] != "/clear" {
		t.Fatalf("m.history after /clear submit = %v, want [/clear]", m.history)
	}

	// Verify Up arrow recalls the slash command.
	m.historyUp()
	if got := m.input.Value(); got != "/clear" {
		t.Fatalf("historyUp recalled %q, want %q", got, "/clear")
	}

	// Verify history.jsonl recorded Type="slash_command".
	data, err := os.ReadFile(filepath.Join(home, "history.jsonl"))
	if err != nil {
		t.Fatalf("failed to read history.jsonl: %v", err)
	}
	var entry convstore.HistoryEntry
	if err := json.Unmarshal(data[:len(data)-1], &entry); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if entry.Display != "/clear" || entry.Type != "slash_command" {
		t.Errorf("persisted entry = %+v, want Display=/clear, Type=slash_command", entry)
	}
}

// TestSubmitPrompt_CapturesStreamingQueueInHistory verifies that prompts
// submitted with Enter while a turn is generating are added to history immediately.
func TestSubmitPrompt_CapturesStreamingQueueInHistory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.streaming = true
	m.input.SetValue("queued while streaming")
	_ = m.submitPrompt()

	if len(m.streamQueue) != 1 || m.streamQueue[0] != "queued while streaming" {
		t.Fatalf("streamQueue = %v, want [queued while streaming]", m.streamQueue)
	}

	if len(m.history) != 1 || m.history[0] != "queued while streaming" {
		t.Fatalf("m.history = %v, want [queued while streaming]", m.history)
	}
}

// TestSubmitOrQueuePrompt_CapturesQueueModePrompt verifies that prompts
// queued in queue mode (Alt+M / Cmd+Enter) are captured into history.
func TestSubmitOrQueuePrompt_CapturesQueueModePrompt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/workspace", true)
	m.queueMode = true
	m.input.SetValue("queue mode prompt")
	_ = m.submitOrQueuePrompt()

	if m.queuedPrompt != "queue mode prompt" {
		t.Fatalf("queuedPrompt = %q, want %q", m.queuedPrompt, "queue mode prompt")
	}
	if len(m.history) != 1 || m.history[0] != "queue mode prompt" {
		t.Fatalf("m.history = %v, want [queue mode prompt]", m.history)
	}
}
