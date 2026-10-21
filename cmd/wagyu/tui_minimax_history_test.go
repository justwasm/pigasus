package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/convstore"
)

// mockAgwBackend implements both backend.Client and agwBackend.
type mockAgwBackend struct {
	cfg backend.AgwK0sConfig
}

func (m *mockAgwBackend) Stream(_ context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 1)
	ch <- backend.StreamChunk{Text: "ok", Done: true}
	close(ch)
	return ch, nil
}

func (m *mockAgwBackend) AgwConfig() backend.AgwK0sConfig {
	return m.cfg
}

// TestMiniMaxHeaderAndFooter verifies that when MiniMax M3 is configured,
// the TUI Header banner (row 3) and Footer status row display "MiniMax M3"
// instead of hardcoded "Gemini 3.8 Flash".
func TestMiniMaxHeaderAndFooter(t *testing.T) {
	// Case 1: Backend is agw with default ExternalModel "MiniMax-M3"
	be := &mockAgwBackend{
		cfg: backend.AgwK0sConfig{
			ExternalModel: "MiniMax-M3",
		},
	}
	m := NewRunMode(be, "user@test.com", "", "", "/test/workspace", false)
	if m.model != "MiniMax M3" {
		t.Fatalf("m.model = %q, want 'MiniMax M3'", m.model)
	}
	if m.statusModel != "MiniMax M3" {
		t.Fatalf("m.statusModel = %q, want 'MiniMax M3'", m.statusModel)
	}

	view := m.View().Content
	if !strings.Contains(view, "MiniMax M3") {
		t.Errorf("View() should contain 'MiniMax M3'")
	}
	if strings.Contains(view, "Gemini 3.8 Flash") {
		t.Errorf("View() should NOT contain hardcoded 'Gemini 3.8 Flash', got:\n%s", view)
	}
	// MiniMax M3 has no effort tiers - status row must not have "· high"
	if strings.Contains(view, "· high") || strings.Contains(view, "· medium") || strings.Contains(view, "· low") {
		t.Errorf("View() should NOT have effort suffix for MiniMax M3, got:\n%s", view)
	}

	// Case 2: Explicit model flag "MiniMax M3"
	m2 := NewRunMode(nil, "user@test.com", "MiniMax M3", "", "/test/workspace", false)
	if m2.model != "MiniMax M3" || m2.statusModel != "MiniMax M3" {
		t.Fatalf("m2.model = %q, m2.statusModel = %q", m2.model, m2.statusModel)
	}
	view2 := m2.View().Content
	if !strings.Contains(view2, "MiniMax M3") {
		t.Errorf("View() should contain 'MiniMax M3'")
	}
	if strings.Contains(view2, "Gemini 3.8 Flash") {
		t.Errorf("View() should NOT contain hardcoded 'Gemini 3.8 Flash'")
	}

	// Case 3: Slug "minimax-m3" normalises to "MiniMax M3"
	m3 := NewRunMode(nil, "user@test.com", "minimax-m3", "", "/test/workspace", false)
	if m3.model != "MiniMax M3" || m3.statusModel != "MiniMax M3" {
		t.Fatalf("m3.model = %q, m3.statusModel = %q", m3.model, m3.statusModel)
	}

	// Case 4: statusEffortSuffix must be empty for MiniMax M3
	suffix := m.statusEffortSuffix()
	if suffix != "" {
		t.Errorf("statusEffortSuffix() = %q, want empty for MiniMax M3", suffix)
	}
}

// TestTUISessionPersistence verifies that running the TUI saves prompt history
// in history.jsonl and conversation database + cache in agy-compatible format.
func TestTUISessionPersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	ws := "/workspace/openagy-demo"
	m := NewRunMode(nil, "user@test.com", "MiniMax M3", "", ws, false)

	// 1. Submit prompt — the test environment's bubbletea KeyMsg path
	// doesn't update the textarea's internal buffer when there's no real
	// Program driving it. SetValue DOES update the buffer (it is the
	// bubbles API's setter), so the submit path sees the prompt. We use
	// it directly because the runeKey + Update dance is unreliable in
	// unit tests for this particular bubbles version.
	prompt := "hello from wagyu"
	m.input.SetValue(prompt)
	if got := m.input.Value(); got != prompt {
		t.Fatalf("input buffer = %q, want %q", got, prompt)
	}
	// Submit the prompt. The picker is not driven by SetValue alone (it
	// needs a KeyMsg-driven Update), so its Visible() flag stays false here
	// and submitPrompt falls through to the raw prompt path — which appends
	// the user message to m.session and writes the .db via saveSession.
	if cmd := m.submitPrompt(); cmd != nil {
		// Drain the returned stream command so we don't leak goroutines; the
		// streamPrompt path will append its own assistant message later via
		// backendChunkMsg, but in this test we drive that explicitly below.
		_ = cmd
	}
	dbPathCheck := filepath.Join(tempHome, "conversations", m.conversationID+".db")
	if _, err := os.Stat(dbPathCheck); err != nil {
		t.Fatalf("after submitPrompt: %s missing: %v\nm.session=%+v m.conversationID=%q", dbPathCheck, err, m.session, m.conversationID)
	}

	// 2. Simulate model reply via backendChunkMsg
	_, _ = m.Update(backendChunkMsg("I am MiniMax M3, ready to help."))

	// Verify conversation database was written and can be loaded
	dbPath := filepath.Join(tempHome, "conversations", m.conversationID+".db")
	conv, err := convstore.LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation(%s): %v", dbPath, err)
	}
	if len(conv.Steps) != 2 {
		t.Fatalf("loaded steps count = %d, want 2", len(conv.Steps))
	}
	if got := strings.TrimSpace(conv.Steps[0].Text); got != "hello from wagyu" {
		t.Errorf("step 0 text = %q, want 'hello from wagyu'", conv.Steps[0].Text)
	}
	if got := strings.TrimSpace(conv.Steps[1].Text); got != "I am MiniMax M3, ready to help." {
		t.Errorf("step 1 text = %q, want 'I am MiniMax M3, ready to help.'", conv.Steps[1].Text)
	}

	// Verify history.jsonl
	histPath := filepath.Join(tempHome, "history.jsonl")
	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("ReadFile history.jsonl: %v", err)
	}
	var histEntry convstore.HistoryEntry
	if err := json.Unmarshal([]byte(strings.TrimSpace(string(data))), &histEntry); err != nil {
		t.Fatalf("Unmarshal history entry: %v", err)
	}
	if histEntry.Display != "hello from wagyu" {
		t.Errorf("histEntry.Display = %q, want 'hello from wagyu'", histEntry.Display)
	}
	if histEntry.Workspace != ws {
		t.Errorf("histEntry.Workspace = %q, want %q", histEntry.Workspace, ws)
	}
	if histEntry.ConversationID != m.conversationID {
		t.Errorf("histEntry.ConversationID = %q, want %q", histEntry.ConversationID, m.conversationID)
	}

	// 3. Verify last_conversations.json maps workspace -> conversationID
	lastPath := filepath.Join(tempHome, "cache", "last_conversations.json")
	lastData, err := os.ReadFile(lastPath)
	if err != nil {
		t.Fatalf("ReadFile last_conversations.json: %v", err)
	}
	var lastMap map[string]string
	if err := json.Unmarshal(lastData, &lastMap); err != nil {
		t.Fatalf("Unmarshal lastMap: %v", err)
	}
	if lastMap[ws] != m.conversationID {
		t.Errorf("lastMap[%s] = %q, want %q", ws, lastMap[ws], m.conversationID)
	}

	// 4. Verify ListConversations discovers the saved conversation
	list, err := convstore.ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	found := false
	for _, c := range list {
		if c.ID == m.conversationID {
			found = true
			if c.Title != "hello from wagyu" {
				t.Errorf("c.Title = %q, want 'hello from wagyu'", c.Title)
			}
			if c.StepCount != 2 {
				t.Errorf("c.StepCount = %d, want 2", c.StepCount)
			}
			break
		}
	}
	if !found {
		t.Errorf("conversation %q not discovered in ListConversations: %+v", m.conversationID, list)
	}
}


