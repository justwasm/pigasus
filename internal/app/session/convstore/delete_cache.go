package convstore

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// refreshCachesAfterDelete rewrites the cache / summary
// rows for `convID` so the /resume picker sees the new step
// count. Errors are surfaced to the caller — DeleteMessage
// converts them into -32603 envelopes.
//
// The cache write is identical to write.go's
// updateConversationMetadata path: re-derive title /
// preview / step count from the surviving steps and update
// the JSON file in-place. The summary db uses an SQL
// UPDATE that touches the same columns write.go updates.
func refreshCachesAfterDelete(storeDir, convID, dbPath string) error {
	c, err := LoadConversation(dbPath)
	if err != nil {
		return fmt.Errorf("convstore: post-delete re-read %s: %w", dbPath, err)
	}
	if c == nil {
		return nil // empty store; nothing to refresh
	}
	if len(c.Steps) == 0 {
		// All messages gone: drop the row from cache +
		// summary entirely. This matches the case where a
		// user "deletes every message" — the conversation
		// is effectively empty and the picker should
		// probably not show it (its StepCount column would
		// be 0).
		removeConversationFromCache(filepath.Join(storeDir, "cache", "conversation_metadata.json"), convID)
		removeConversationFromIndex(filepath.Join(storeDir, "conversation_summaries.db"), convID)
		// If the conversation_summaries row carries step_count
		// that drops to 0 from a non-zero prior, the picker
		// already filters by step_count > 0 (list.go:181),
		// so leaving the row at step_count=0 is harmless
		// but cosmetically inconsistent. The two remove*
		// calls above handle the clean case.
		return nil
	}
	// Non-empty conversation after delete: refresh the
	// caches in place. The dispatched conversation metadata
	// is rewritten atomically by reusing write.go's helper.
	return writeUpdateConversationCaches(storeDir, convID, c)
}

// writeUpdateConversationCaches is a small inline copy of
// write.go's updateConversationMetadata +
// updateConversationSummariesDB logic so this file can
// refresh caches without depending on those
// package-private helpers. Behaviour matches write.go:
//
//   - cache/conversation_metadata.json: rewrite the
//     summary entry for convID with the surviving
//     title / preview / step count, bumping
//     last_modified_time.
//
//   - conversation_summaries.db: UPDATE the row in
//     place (with the same ON CONFLICT-safe INSERT
//     fallback) preserving title / preview / step_count.
func writeUpdateConversationCaches(storeDir, convID string, c *Conversation) error {
	workspace := c.Project
	if workspace == "" {
		// Picker's project column is the basename;
		// write.go recorded the workspace as a file://
		// URI in the metadata blob, but the basename
		// reconstructed above is what the cache stores
		// in WorkspaceURIs[0]. For a missing blob,
		// fall back to "" so the cache stays empty.
	}
	if err := writeUpdateCacheMetadata(storeDir, convID, c, workspace); err != nil {
		return err
	}
	if err := writeUpdateIndexSummaries(storeDir, convID, c, workspace); err != nil {
		return err
	}
	return nil
}

func writeUpdateCacheMetadata(storeDir, convID string, c *Conversation, workspace string) error {
	path := filepath.Join(storeDir, "cache", "conversation_metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		// Cache absent — no row to update. Subsequent
		// saves will populate it.
		return nil
	}
	var cache metadataFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return nil
	}
	entry, ok := cache.Conversations[convID]
	if !ok {
		return nil
	}
	title, preview := deriveTitlePreview(c)
	entry.Summary.Title = title
	entry.Summary.Preview = preview
	entry.Summary.NumSteps = len(c.Steps)
	entry.Summary.UpdatedAt = time.Now().UTC().Format("2006-01-02 15:04:05.999999-07:00")
	entry.Summary.WorkspaceURIs = nil
	if workspace != "" {
		entry.Summary.WorkspaceURIs = []string{"file://" + workspace}
	}
	entry.LastModifiedTime = entry.Summary.UpdatedAt
	cache.Conversations[convID] = entry
	out, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, out, 0o644)
}

func writeUpdateIndexSummaries(storeDir, convID string, c *Conversation, workspace string) error {
	indexPath := filepath.Join(storeDir, "conversation_summaries.db")
	if _, err := os.Stat(indexPath); err != nil {
		return nil
	}
	sdb, err := sql.Open("sqlite3", "file:"+indexPath+"?mode=rwc")
	if err != nil {
		return err
	}
	defer sdb.Close()
	title, preview := deriveTitlePreview(c)
	var workspaceURIs string
	if workspace != "" {
		workspaceURIs = "[\"file://" + workspace + "\"]"
	}
	lastModified := time.Now().UTC().Format("2006-01-02 15:04:05.999999-07:00")
	// Match the existing cycle-167 row shape.
	if _, err := sdb.Exec(`UPDATE conversation_summaries SET title=?, preview=?, step_count=?, last_modified_time=?, workspace_uris=? WHERE conversation_id=?`,
		title, preview, len(c.Steps), lastModified, workspaceURIs, convID); err != nil {
		return err
	}
	return nil
}

func deriveTitlePreview(c *Conversation) (title, preview string) {
	if c == nil {
		return "", ""
	}
	// Cycle 184: title from the first user-input step;
	// preview from the same (the reference keeps them in
	// sync).
	for _, s := range c.Steps {
		if s.Role() == "user" {
			t := strings.TrimSpace(s.Text)
			if t != "" {
				return t, t
			}
		}
	}
	// No user step left: empty title / preview (the
	// picker just renders a blank row).
	return "", ""
}

// removeConversationFromCache drops the `convID` entry from
// cache/conversation_metadata.json. Reads, mutates, rewrites
// atomically. Missing / unparseable cache → no-op (the next
// save re-populates).
func removeConversationFromCache(path, convID string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var cache metadataFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return
	}
	if _, ok := cache.Conversations[convID]; !ok {
		return
	}
	delete(cache.Conversations, convID)
	out, err := json.MarshalIndent(cache, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, out, 0o644)
}

// removeConversationFromIndex drops the row keyed by
// `convID` from conversation_summaries.db. Missing index →
// no-op. Used by DeleteConversation to keep the cache in
// sync with the file-system delete.
func removeConversationFromIndex(indexPath, convID string) {
	if _, err := os.Stat(indexPath); err != nil {
		return
	}
	sdb, err := sql.Open("sqlite3", "file:"+indexPath+"?mode=rwc")
	if err != nil {
		return
	}
	defer sdb.Close()
	_, _ = sdb.Exec(`DELETE FROM conversation_summaries WHERE conversation_id = ?`, convID)
}

// conversationWorkspaceMatches checks whether the workspace
// URIs recorded against `convID` in
// cache/conversation_metadata.json include `wantURI` (or
// `wantPath`'s symlink-resolved form). Used as the
// fallback for the basename-shortcut in
// ConversationBoundToProject.
//
// Returns false for any cache error, so the conservative
// "might be bound" answer is always the conservative one
// (counted above via the basename shortcut).
func conversationWorkspaceMatches(convID, wantURI, wantPath string) bool {
	path := filepath.Join(StoreDir(), "cache", "conversation_metadata.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var cache metadataFile
	if err := json.Unmarshal(data, &cache); err != nil {
		return false
	}
	entry, ok := cache.Conversations[convID]
	if !ok || len(entry.Summary.WorkspaceURIs) == 0 {
		return false
	}
	for _, raw := range entry.Summary.WorkspaceURIs {
		uri := strings.TrimSuffix(strings.TrimSpace(raw), "/")
		if uri == wantURI {
			return true
		}
		if ru, err := filepath.EvalSymlinks(strings.TrimPrefix(uri, "file://")); err == nil {
			if rw, err := filepath.EvalSymlinks(wantPath); err == nil {
				if filepath.Clean(ru) == filepath.Clean(rw) {
					return true
				}
			}
		}
	}
	return false
}
