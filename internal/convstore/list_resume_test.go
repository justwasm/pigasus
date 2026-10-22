// list_cycle278_test.go — cycle 278: the /resume list's label fallback and
// source precedence, both pinned against agy 1.4.2 behaviour.
package convstore

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestConversationLabelFallback pins the three-step label chain the picker
// renders: AI summary → raw first prompt → conversation id.
//
// The last step is new in cycle 278. REF prints the bare id when both the
// title and the preview are empty (observed on a store of sessions the agent
// never titled); the clone printed "(no title)".
func TestConversationLabelFallback(t *testing.T) {
	const id = "0495d3b3-fb08-b3c0-9290-41ec9c5f0251"
	cases := []struct {
		name           string
		title, preview string
		want           string
	}{
		{"title wins", "Mouse Power Troubleshooting", "raw prompt", "Mouse Power Troubleshooting"},
		{"preview when untitled", "", "Use view_file with AbsolutePath=/etc/hosts", "Use view_file with AbsolutePath=/etc/hosts"},
		{"id when both empty", "", "", id},
		{"whitespace-only title falls through", "   ", "preview text", "preview text"},
		{"collapses newlines", "", "first line\nsecond line", "first line second line"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := conversationLabel(tc.title, tc.preview, id); got != tc.want {
				t.Errorf("conversationLabel(%q, %q) = %q, want %q", tc.title, tc.preview, got, tc.want)
			}
		})
	}
}

// TestListConversationsMergesIndexRowsOnDisk pins the cycle-282 correction
// of the cycle-278 "cache only" ruling: the picker is disk-driven, so an
// on-disk conversation the cache has never seen is still listed (the
// reference reaches it through scanConversations → listConversationsFromDir).
//
// Cycle 278 read its 38-entry-cache / 168-row-index store as "REF lists the
// cache's rows"; every index-only row there had no .db, so the same rows are
// excluded by the file check that this test keeps.
func TestListConversationsMergesIndexRowsOnDisk(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	convDir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Both conversations have a real .db, so the only difference between
	// them is which index knows about them.
	for _, id := range []string{"cache-only", "index-only"} {
		if err := WriteConversation(filepath.Join(convDir, id+".db"), "traj-"+id,
			[]Step{{Idx: 0, Type: StepUserInput, Status: 3, Text: "hello from " + id}}); err != nil {
			t.Fatal(err)
		}
	}

	cache := map[string]any{
		"conversations": map[string]any{
			"cache-only": map[string]any{
				"summary": map[string]any{
					"Title":     "From the cache",
					"NumSteps":  2,
					"UpdatedAt": "2026-10-19T18:00:00Z",
				},
				"last_modified_time": "2026-10-19T18:00:00Z",
			},
		},
	}
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "cache"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "cache", "conversation_metadata.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite3", "file:"+filepath.Join(root, "conversation_summaries.db")+"?mode=rwc")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`CREATE TABLE conversation_summaries (
		conversation_id TEXT PRIMARY KEY,
		title TEXT, preview TEXT, step_count INTEGER,
		last_modified_time TEXT, workspace_uris TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_summaries
		(conversation_id, title, preview, step_count, last_modified_time, workspace_uris)
		VALUES ('index-only', 'From the index', '', 4, '2026-10-19T19:00:00Z', '')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	got, err := ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]ConversationMeta{}
	for _, c := range got {
		byID[c.ID] = c
	}
	// The cached row keeps the cache's label (and its NumSteps, not the
	// file's) — the cache is still the primary source for rows it knows.
	if c, ok := byID["cache-only"]; !ok || c.Title != "From the cache" {
		t.Errorf("cache-only = %+v (present=%v), want the cached title", c, ok)
	}
	// The index-only row is a real file with steps, so the disk scan lists it.
	if c, ok := byID["index-only"]; !ok || c.Title != "From the index" {
		t.Errorf("index-only = %+v (present=%v), want the index row listed", c, ok)
	}
	if len(got) != 2 {
		t.Fatalf("ListConversations = %d rows, want 2", len(got))
	}
}

// TestListConversationsIndexFallbackWhenNoCache keeps the other half of the
// rule honest: with no cache file the index is still used.
func TestListConversationsIndexFallbackWhenNoCache(t *testing.T) {
	root := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", root)

	convDir := filepath.Join(root, "conversations")
	if err := os.MkdirAll(convDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := WriteConversation(filepath.Join(convDir, "only-index.db"), "traj", []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "hi"},
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
		last_modified_time TEXT, workspace_uris TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO conversation_summaries
		(conversation_id, title, preview, step_count, last_modified_time, workspace_uris)
		VALUES ('only-index', 'Indexed', '', 3, '2026-10-19T19:00:00Z', '')`); err != nil {
		t.Fatal(err)
	}
	db.Close()

	got, err := ListConversations()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "only-index" {
		t.Fatalf("ListConversations = %+v, want the single index row", got)
	}
}
