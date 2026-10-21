package convstore

// find_helpers_test.go — small SQL helpers used only by find_test.go.
//
// Keeping the helper out of find.go itself means production builds don't
// carry a database/sql write path: production code is read-only and
// routes through ListConversations + HasConversation + ConversationPath.
// The tests, however, need to seed the conversation_summaries.db index
// because FindMostRecent reads workspace_uris from it (and the
// `Project` cell is left empty when only WriteConversation was called,
// which makes the basename fallback in rowMatchesWorkspace a no-op).

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// seedIndex inserts a single conversation_summaries row whose
// workspace_uris column is the absolute file:// URI of `ws`. The test
// also writes the conversation db through WriteConversation so the
// listFromDir fallback path produces a row; with both writes in place
// the cycle-resume FindMostRecent scan resolves the seed workspace
// correctly regardless of which code path returns the row.
func seedIndex(t *testing.T, id, ws string) {
	t.Helper()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	uri := "file://" + filepath.Clean(ws)
	if err := os.MkdirAll(StoreDir(), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeConversationSummaryRow(filepath.Join(StoreDir(), "conversation_summaries.db"), id, uri, now); err != nil {
		t.Fatal(err)
	}
}

// writeConversationSummaryRow emits one row into conversation_summaries
// using the same schema as write.go's updateConversationSummariesDB.
func writeConversationSummaryRow(dbPath, id, uri, now string) error {
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		return err
	}
	defer sdb.Close()

	if _, err := sdb.Exec(`CREATE TABLE IF NOT EXISTS conversation_summaries (
		conversation_id text PRIMARY KEY,
		title text,
		preview text,
		step_count integer,
		workspace_uris text,
		last_modified_time text)`); err != nil {
		return err
	}
	_, err = sdb.Exec(`INSERT INTO conversation_summaries (conversation_id, title, preview, step_count, workspace_uris, last_modified_time)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(conversation_id) DO UPDATE SET
			title=excluded.title,
			preview=excluded.preview,
			step_count=excluded.step_count,
			workspace_uris=excluded.workspace_uris,
			last_modified_time=excluded.last_modified_time`,
		id, "synthetic", "synthetic", 1, uri, now)
	return err
}
