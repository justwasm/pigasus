package main

// continue_test_helpers_test.go — exported-into-test-only helpers used
// by continue_test.go. Writes a single conversation_summaries.db row
// for the cycle-resume tests so we don't have to round-trip through
// SaveConversation (which derives a title from the first user message).

import (
	"database/sql"
	"path/filepath"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// convstoreStoreDir delegates to the package's StoreDir. Keeping a
// named wrapper means the helper doesn't import the convstore symbol
// directly into its parameter set.
func convstoreStoreDir() string { return convstore.StoreDir() }

// writeConversationSummaryRow is a small wrapper around the same
// INSERT statement internal/convstore/write.go issues. Wrapping at the
// test layer keeps the production code (sqlite.go, write.go) unchanged
// and lets the test seed the workspace_uri column on demand.
func writeConversationSummaryRow(dbPath, id, uri, now string) error {
	if dbPath == "" {
		dbPath = filepath.Join(convstoreStoreDir(), "conversation_summaries.db")
	}
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

// nowUTC returns a stable RFC3339Nano timestamp for tests that don't
// care about the exact value.
func nowUTC() string { return time.Now().UTC().Format(time.RFC3339Nano) }
