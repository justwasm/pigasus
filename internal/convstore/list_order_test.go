// list_cycle282_test.go — cycle 282: the /resume picker follows the
// reference's disk scan, not the JSON cache alone.
//
// ServerBackend.ListConversations (with the `summary-store-reads` feature
// flag off, as observed on agy 1.4.2) calls scanConversations, and its
// listConversationsFromDir walks conversations/*.db and merges every file
// the cache does not know. A conversation written by another client — the
// proxy's reference run, for example — is therefore listed by the
// reference even though nothing flushed a cache entry for it.
package convstore

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// TestListConversationsListsFileMissingFromBothIndexes covers the file the
// cache and the summaries table both miss: the scan must still list it.
func TestListConversationsListsFileMissingFromBothIndexes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	convDir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "11111111-2222-3333-4444-555555555555"
	if err := WriteConversation(filepath.Join(convDir, id+".db"), "traj-"+id, []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "HISTRENDER read internal/piagentbridge/bridge.go"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != id {
		t.Fatalf("ListConversations = %+v, want the uncached file listed", got)
	}
	if got[0].Title != "HISTRENDER read internal/piagentbridge/bridge.go" {
		t.Errorf("title = %q, want the first user input", got[0].Title)
	}
	if got[0].StepCount != 1 {
		t.Errorf("step count = %d, want 1", got[0].StepCount)
	}
}

// TestListConversationsUsesAnnotationTitleForUncachedFile pins the title
// source the reference's scan uses (annotations/<id>.pbtxt via
// cortex.AnnotationsManager): a file with an AI title shows that title, not
// the raw first prompt.
func TestListConversationsUsesAnnotationTitleForUncachedFile(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	convDir := filepath.Join(root, "conversations")
	annDir := filepath.Join(root, "annotations")
	for _, d := range []string{convDir, annDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	const id = "22222222-3333-4444-5555-666666666666"
	if err := WriteConversation(filepath.Join(convDir, id+".db"), "traj-"+id, []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "raw first prompt"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(annDir, id+".pbtxt"),
		[]byte("title:\"Mouse Power Troubleshooting\""), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Mouse Power Troubleshooting" {
		t.Fatalf("ListConversations = %+v, want the annotation title", got)
	}
}

// TestAnnotationTitleEscapes keeps the pbtxt reader honest about escaped
// quotes and newlines.
func TestAnnotationTitleEscapes(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)
	annDir := filepath.Join(root, "annotations")
	if err := os.MkdirAll(annDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const id = "33333333-4444-5555-6666-777777777777"
	body := "title:\"He said \\\"hi\\\"\\nthen left\""
	if err := os.WriteFile(filepath.Join(annDir, id+".pbtxt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, want := annotationTitle(id), `He said "hi" then left`; got != want {
		t.Errorf("annotationTitle = %q, want %q", got, want)
	}
}

// TestListConversationsSkipsSubagentIndexRows: a subagent conversation has a
// parent link in the summaries table. The reference hides those; the disk
// scan must not resurrect them via the file walk.
func TestListConversationsSkipsSubagentIndexRows(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	convDir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const child = "44444444-5555-6666-7777-888888888888"
	if err := WriteConversation(filepath.Join(convDir, child+".db"), "traj-"+child, []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "subagent work"},
	}); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite3", "file:"+filepath.Join(root, "conversation_summaries.db")+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE conversation_summaries (
		conversation_id TEXT PRIMARY KEY,
		title TEXT, preview TEXT, step_count INTEGER,
		last_modified_time TEXT, workspace_uris TEXT,
		parent_conversation_id TEXT, nesting_depth INTEGER)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_summaries
		(conversation_id, title, preview, step_count, last_modified_time, workspace_uris, parent_conversation_id, nesting_depth)
		VALUES (?, 'Subagent', '', 2, '2026-10-19T19:00:00Z', '', 'parent-id', 1)`, child); err != nil {
		t.Fatal(err)
	}
	db.Close()

	got, err := ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("ListConversations = %+v, want subagent rows hidden", got)
	}
}
