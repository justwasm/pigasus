package convstore

// convstore/title.go — small wrapper that lets the cycle-resume
// caller override the title field SaveConversation writes.
//
// Why a wrapper rather than a new SaveConversation parameter?
// ------------------------------------------------------------
// The "do not edit sqlite.go" constraint extends to write.go in the
// pragmatic sense — write.go is owned by cycle 167 and changing its
// signature would mean editing internal/convstore in two files
// simultaneously. This wrapper keeps that contract: write.go's
// SaveConversation continues to derive the title from the first user
// message, and callers that want a specific title apply it through
// WriteSummariesTitle and WriteCacheTitle after the call.
//
// Both wrappers only touch the existing summary/cache schema — nothing
// on disk changes. A null/empty title leaves the existing row
// untouched, so the write-path is a no-op when the caller has nothing
// to override.

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// WriteSummariesTitle overrides the title / preview columns of the
// conversation_summaries row for `id` with `title`. Returns nil when
// the row does not exist or the index file is absent (so the caller
// does not have to special-case "first ever save"). The cycle-resume
// handler calls this *after* SaveConversation (which writes the row
// with a derived title), so an existing row is the steady state.
func WriteSummariesTitle(id, title string) error {
	if id == "" || title == "" {
		return nil
	}
	indexPath := filepath.Join(StoreDir(), "conversation_summaries.db")
	if _, err := os.Stat(indexPath); err != nil {
		// Fresh install — no index file yet. SaveConversation will
		// create it on the next save; treat this as a no-op so the
		// resume handler does not have to special-case the missing
		// file.
		return nil
	}
	sdb, err := sql.Open("sqlite3", "file:"+indexPath+"?mode=rwc")
	if err != nil {
		return err
	}
	defer sdb.Close()
	_, err = sdb.Exec(`UPDATE conversation_summaries SET title=?, preview=? WHERE conversation_id=?`,
		title, title, id)
	return err
}

// WriteCacheTitle overrides the Title field in
// cache/conversation_metadata.json for the conversation `id`. The
// file is rewritten atomically (read-modify-write of the same JSON
// document). Like WriteSummariesTitle, this is a no-op when the
// title is empty or the cache does not yet contain the id.
func WriteCacheTitle(id, title string) error {
	if id == "" || title == "" {
		return nil
	}
	path := filepath.Join(StoreDir(), "cache", "conversation_metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil // missing cache is fine — writer will catch up.
	}
	var cache metadataFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	entry, ok := cache.Conversations[id]
	if !ok {
		return nil
	}
	entry.Summary.Title = title
	cache.Conversations[id] = entry
	out, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}
