package convstore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestAppendConversationStepsNamesNewConversation covers the remote
// path's title: a conversation the clone creates gets the generated
// title, because the Web UI shows the summaries row's title and its
// own fallback is the string "Untitled Conversation".
func TestAppendConversationStepsNamesNewConversation(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "c1c1c1c1-d2d2-4000-8000-000000000701"
	steps := []Step{
		{Type: StepUserInput, Status: 3, Text: "reply with exactly one word: ok"},
		{Type: StepModelResponse, Status: 3, Text: "ok"},
	}
	if err := AppendConversationSteps(id, ws, steps); err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}
	want := GenerateConversationTitle("reply with exactly one word: ok")
	if want == "" {
		t.Fatal("the test prompt must produce a title")
	}
	if got := readTitleFromIndex(t, id); got != want {
		t.Errorf("summaries title = %q, want %q", got, want)
	}
	if got := readCacheTitle(t, id); got != want {
		t.Errorf("cache title = %q, want %q", got, want)
	}
}

// TestAppendConversationStepsKeepsReferenceTitle covers the other
// half: a conversation the clone did not create keeps the title the
// reference gave it. The remote path appends to those, and the title
// is the reference's own.
func TestAppendConversationStepsKeepsReferenceTitle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "c2c2c2c2-d3d3-4000-8000-000000000702"
	if err := SaveConversation(id, ws, []Step{
		{Type: StepUserInput, Status: 3, Text: "first message"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	if err := WriteSummariesTitle(id, "reference-title"); err != nil {
		t.Fatalf("WriteSummariesTitle: %v", err)
	}
	if err := AppendConversationSteps(id, ws, []Step{
		{Type: StepModelResponse, Status: 3, Text: "answer"},
	}); err != nil {
		t.Fatalf("AppendConversationSteps: %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "reference-title" {
		t.Errorf("title = %q, want %q", got, "reference-title")
	}
}

// TestWriteSummaryRowKeepsStoredTitle covers the upsert rule: a write
// with no title of its own must not erase the stored title. The Web
// UI's JetboxWriteSummary request carries the summary the instance
// gave it, and a summary with no title must not blank the name the
// Web UI shows.
func TestWriteSummaryRowKeepsStoredTitle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "c3c3c3c3-d4d4-4000-8000-000000000703"
	if err := SaveConversation(id, ws, []Step{
		{Type: StepUserInput, Status: 3, Text: "first message"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	if err := WriteSummaryRow(SummaryRow{
		ConversationID: id,
		Title:          "named-conversation",
		Preview:        "named-conversation",
		StepCount:      1,
	}); err != nil {
		t.Fatalf("WriteSummaryRow: %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "named-conversation" {
		t.Fatalf("title = %q, want %q", got, "named-conversation")
	}
	if err := WriteSummaryRow(SummaryRow{ConversationID: id, StepCount: 2}); err != nil {
		t.Fatalf("WriteSummaryRow (empty title): %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "named-conversation" {
		t.Errorf("empty title must not erase the stored one, got %q", got)
	}
}

// TestWriteSummaryRowOnCloneOnlyStore covers the write path on a
// store the clone made on its own: the table then has the reference's
// columns, so the write names columns that exist.
func TestWriteSummaryRowOnCloneOnlyStore(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	id := "c4c4c4c4-d5d5-4000-8000-000000000704"
	if err := SaveConversation(id, filepath.Join(root, "ws"), []Step{
		{Type: StepUserInput, Status: 3, Text: "first message"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	if err := WriteSummaryRow(SummaryRow{
		ConversationID: id,
		Title:          "named-conversation",
		Preview:        "named-conversation",
		StepCount:      1,
		Status:         "CASCADE_RUN_STATUS_IDLE",
	}); err != nil {
		t.Fatalf("WriteSummaryRow: %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "named-conversation" {
		t.Errorf("title = %q, want %q", got, "named-conversation")
	}
}

// readCacheTitle reads the Title field of a conversation's entry in
// the metadata cache.
func readCacheTitle(t *testing.T, id string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(StoreDir(), "cache", "conversation_metadata.json"))
	if err != nil {
		t.Fatal(err)
	}
	var meta metadataFile
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatal(err)
	}
	entry, ok := meta.Conversations[id]
	if !ok {
		t.Fatalf("no cache entry for %s", id)
	}
	return entry.Summary.Title
}
