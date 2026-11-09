package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHistoryPersistence(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	// Append two entries
	e1 := HistoryEntry{
		Display:   "hello agy",
		Workspace: "/workspace/test",
	}
	if err := AppendHistoryEntry(e1); err != nil {
		t.Fatalf("AppendHistoryEntry: %v", err)
	}

	e2 := HistoryEntry{
		Display:        "/help",
		Workspace:      "/workspace/test",
		ConversationID: "conv-123",
	}
	if err := AppendHistoryEntry(e2); err != nil {
		t.Fatalf("AppendHistoryEntry: %v", err)
	}

	// Verify file content
	histPath := filepath.Join(tempHome, "history.jsonl")
	data, err := os.ReadFile(histPath)
	if err != nil {
		t.Fatalf("ReadFile history.jsonl: %v", err)
	}

	// Load history
	prompts := LoadHistory("/workspace/test", 10)
	if len(prompts) != 2 {
		t.Fatalf("got %d prompts, want 2", len(prompts))
	}
	if prompts[0] != "hello agy" || prompts[1] != "/help" {
		t.Fatalf("prompts = %v", prompts)
	}

	// Verify JSON structure of second line
	lines := splitLines(string(data))
	if len(lines) < 2 {
		t.Fatalf("lines = %v", lines)
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(lines[1]), &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if parsed["display"] != "/help" {
		t.Errorf("display = %v, want /help", parsed["display"])
	}
	if parsed["type"] != "slash_command" {
		t.Errorf("type = %v, want slash_command", parsed["type"])
	}
	if parsed["conversationId"] != "conv-123" {
		t.Errorf("conversationId = %v, want conv-123", parsed["conversationId"])
	}
}

func TestSaveConversation(t *testing.T) {
	tempHome := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tempHome)

	convID := "test-conv-uuid"
	ws := "/workspace/openagy"
	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "first prompt"},
		{Idx: 1, Type: StepModelResponse, Status: 3, Text: "first answer"},
	}

	if err := SaveConversation(convID, ws, steps); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}

	// 1. Verify db was created and can be loaded
	dbPath := filepath.Join(tempHome, "conversations", convID+".db")
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation: %v", err)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("loaded steps = %d, want 2", len(c.Steps))
	}
	if c.Steps[0].Text != "first prompt" || c.Steps[1].Text != "first answer" {
		t.Fatalf("unexpected step texts: %+v", c.Steps)
	}

	// 2. Verify last_conversations.json
	lastPath := filepath.Join(tempHome, "cache", "last_conversations.json")
	lastData, err := os.ReadFile(lastPath)
	if err != nil {
		t.Fatalf("ReadFile last_conversations.json: %v", err)
	}
	var lastMap map[string]string
	if err := json.Unmarshal(lastData, &lastMap); err != nil {
		t.Fatalf("Unmarshal last_conversations: %v", err)
	}
	if lastMap[ws] != convID {
		t.Errorf("lastMap[%q] = %q, want %q", ws, lastMap[ws], convID)
	}

	// 3. Verify conversation_metadata.json
	metaPath := filepath.Join(tempHome, "cache", "conversation_metadata.json")
	metaData, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("ReadFile conversation_metadata.json: %v", err)
	}
	var metaFile struct {
		Conversations map[string]struct {
			Summary struct {
				ID            string   `json:"ID"`
				Preview       string   `json:"Preview"`
				NumSteps      int      `json:"NumSteps"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(metaData, &metaFile); err != nil {
		t.Fatalf("Unmarshal metadata: %v", err)
	}
	entry, ok := metaFile.Conversations[convID]
	if !ok {
		t.Fatalf("entry not found in metadata")
	}
	if entry.Summary.Preview != "first prompt" {
		t.Errorf("preview = %q, want 'first prompt'", entry.Summary.Preview)
	}
	if entry.Summary.NumSteps != 2 {
		t.Errorf("numSteps = %d, want 2", entry.Summary.NumSteps)
	}

	// 4. Verify ListConversations discovers the saved conversation
	list, err := ListConversations()
	if err != nil {
		t.Fatalf("ListConversations: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == convID {
			found = true
			if item.Title != "first prompt" {
				t.Errorf("item.Title = %q, want 'first prompt'", item.Title)
			}
			if item.StepCount != 2 {
				t.Errorf("item.StepCount = %d, want 2", item.StepCount)
			}
			break
		}
	}
	if !found {
		t.Fatalf("conversation %q not found in ListConversations: %+v", convID, list)
	}

	// 5. Verify conversation_summaries.db contains the columns expected by agy /resume:
	// app_data_dir = 'antigravity-cli', project_id = 'default-cli-project',
	// status = 'CASCADE_RUN_STATUS_IDLE', and space-separated last_modified_time.
	sumDB, err := Open(filepath.Join(tempHome, "conversation_summaries.db"))
	if err != nil {
		t.Fatalf("Open conversation_summaries.db: %v", err)
	}
	defer sumDB.Close()
	var rowFound bool
	_ = sumDB.Scan("conversation_summaries", func(r *Row) error {
		if r.Text("conversation_id") == convID {
			rowFound = true
			if got := r.Text("app_data_dir"); got != "antigravity-cli" {
				t.Errorf("app_data_dir = %q, want 'antigravity-cli'", got)
			}
			if got := r.Text("project_id"); got != "default-cli-project" {
				t.Errorf("project_id = %q, want 'default-cli-project'", got)
			}
			if got := r.Text("status"); got != "CASCADE_RUN_STATUS_IDLE" {
				t.Errorf("status = %q, want 'CASCADE_RUN_STATUS_IDLE'", got)
			}
			modTime := r.Text("last_modified_time")
			if strings.Contains(modTime, "T") {
				t.Errorf("last_modified_time contains 'T': %q, want space-separated", modTime)
			}
		}
		return nil
	})
	if !rowFound {
		t.Fatalf("row for %q not found in conversation_summaries.db", convID)
	}

	// 6. Verify annotations/<id>.pbtxt exists and has expected format
	annPath := filepath.Join(tempHome, "annotations", convID+".pbtxt")
	annBytes, err := os.ReadFile(annPath)
	if err != nil {
		t.Fatalf("ReadFile annotation: %v", err)
	}
	if string(annBytes) != "title:\"first prompt\"\n" {
		t.Errorf("annotation = %q, want 'title:\"first prompt\"\\n'", string(annBytes))
	}

	// 7. Verify trajectory_metadata_blob exists in conversation db
	cDB, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open conversation db: %v", err)
	}
	defer cDB.Close()
	var metaFound bool
	_ = cDB.Scan("trajectory_metadata_blob", func(r *Row) error {
		if r.Text("id") == "main" {
			metaFound = true
			data := r.Bytes("data")
			if len(data) == 0 {
				t.Errorf("trajectory_metadata_blob data is empty")
			}
		}
		return nil
	})
	if !metaFound {
		t.Fatalf("trajectory_metadata_blob row 'main' not found in %s", dbPath)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
