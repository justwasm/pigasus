package convstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestListSummariesFallsBackToTheCache covers the read rule: a row
// whose title is empty takes the name the metadata cache holds.
//
// The reference's own daemon reconciles the summaries table when it
// starts and rewrites the rows it can read with an empty title — the
// two binaries share one store, so a row the clone named comes back
// untitled. The reconcile pass leaves the metadata cache alone, and the
// row takes the name from there.
func TestListSummariesFallsBackToTheCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "e1e1e1e1-f2f2-4000-8000-000000000801"
	if err := SaveConversation(id, ws, []Step{
		{Type: StepUserInput, Status: 3, Text: "first message"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	// The clone names the conversation.
	if err := WriteSummariesTitle(id, "named-conversation"); err != nil {
		t.Fatalf("WriteSummariesTitle: %v", err)
	}
	if err := WriteCacheTitle(id, "named-conversation"); err != nil {
		t.Fatalf("WriteCacheTitle: %v", err)
	}
	// The reference's reconcile pass blanks the row's title and leaves
	// the cache alone.
	if err := WriteSummaryRow(SummaryRow{ConversationID: id, StepCount: 1}); err != nil {
		t.Fatalf("WriteSummaryRow: %v", err)
	}
	if got := readTitleFromIndex(t, id); got != "named-conversation" {
		t.Fatalf("the empty write must not erase the title, got %q", got)
	}
	// Force the row's title to be empty the way the reference's writer
	// leaves it, and check the list takes the name back from the cache.
	blankRowTitle(t, id)
	rows, err := ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	var found bool
	for _, row := range rows {
		if row.ConversationID != id {
			continue
		}
		found = true
		if row.Title != "named-conversation" {
			t.Errorf("title = %q, want %q", row.Title, "named-conversation")
		}
	}
	if !found {
		t.Fatalf("the conversation is missing from the summary list")
	}
}

// TestListSummariesPrefersTheRowTitle covers the other half: a row
// with a title of its own keeps it, whatever the cache says.
func TestListSummariesPrefersTheRowTitle(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	ws := filepath.Join(root, "ws")
	if err := os.MkdirAll(ws, 0o755); err != nil {
		t.Fatal(err)
	}
	id := "e2e2e2e2-f3f3-4000-8000-000000000802"
	if err := SaveConversation(id, ws, []Step{
		{Type: StepUserInput, Status: 3, Text: "first message"},
	}); err != nil {
		t.Fatalf("SaveConversation: %v", err)
	}
	if err := WriteCacheTitle(id, "cache-name"); err != nil {
		t.Fatalf("WriteCacheTitle: %v", err)
	}
	if err := WriteSummaryRow(SummaryRow{
		ConversationID: id,
		Title:          "row-name",
		Preview:        "row-name",
		StepCount:      1,
	}); err != nil {
		t.Fatalf("WriteSummaryRow: %v", err)
	}
	rows, err := ListSummaries()
	if err != nil {
		t.Fatalf("ListSummaries: %v", err)
	}
	for _, row := range rows {
		if row.ConversationID != id {
			continue
		}
		if row.Title != "row-name" {
			t.Errorf("title = %q, want %q", row.Title, "row-name")
		}
		return
	}
	t.Fatalf("the conversation is missing from the summary list")
}

// blankRowTitle empties a row's title the way the reference's writer
// leaves it: the column is written empty.
func blankRowTitle(t *testing.T, id string) {
	t.Helper()
	path := filepath.Join(StoreDir(), "conversation_summaries.db")
	sdb, err := sql.Open("sqlite3", "file:"+path+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	defer sdb.Close()
	if _, err := sdb.Exec(`UPDATE conversation_summaries SET title='' WHERE conversation_id=?`, id); err != nil {
		t.Fatal(err)
	}
}
