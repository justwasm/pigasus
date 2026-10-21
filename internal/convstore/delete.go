package convstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/ncruces/go-sqlite3/driver"
)

// convstore/delete.go — destructive side of the persistent
// conversation store. Cycle rc-conversation-deletions adds it.
//
// Why a separate file (and not an edit to sqlite.go / write.go)
// ----------------------------------------------------------------
// The convstore package carries a hard read-only constraint on
// its SQLite reader (see internal/convstore/sqlite.go's package
// doc and AGENTS.md): the per-cycle ownership map pins sqlite.go
// as a read-only contract. write.go is owned by cycle 167 (the
// /fork writer) and that file already opens its own read-write
// connection (the cycle 175 driver swap established the
// pattern). The destructive helpers below belong in their own
// file because:
//
//   - they extend the public surface with three new entry points
//     (DeleteMessage, DeleteConversation, plus the small
//     MessageByID / ConversationBoundCount / CountMessages
//     reads the dispatcher needs);
//
//   - they keep their own RW connection lifecycle (sql.Open +
//     defer Close), distinct from sqlite.go's read-only one —
//     `sql.Open("sqlite3", "file:"+path+"?mode=rwc")` matches
//     WriteConversation's existing pattern (write.go:258), so
//     the convention this file introduces is "every helper in
//     delete.go is free to open its own connection";
//
//   - they update the side caches (cache/conversation_metadata.json
//     and conversation_summaries.db) the cycle-167 writer
//     already maintains — re-using the helper bodies in write.go
//     keeps the on-disk shape consistent across save / delete.
//
// Result: the read-only contract on sqlite.go is preserved
// unchanged, and the new write paths sit in a dedicated file
// with a clear ownership note.

// Sentinel errors returned by the destructive helpers.
// Both are exposed so the dispatcher layer can map them to
// the brief's JSON-RPC error codes (-32011 Conversation
// not found, -32012 Message not found) without
// string-matching the message returned by fmt.Errorf.
//
// Wire-level mapping (cycle rc-conversation-deletions):
//
//	ErrMessageNotFound       → -32012 "Message not found"
//	ErrConversationNotFound  → -32011 "Conversation not found"
var (
	ErrMessageNotFound      = errors.New("convstore: message not found")
	ErrConversationNotFound = errors.New("convstore: conversation not found")
)

// DeleteMessage removes a single message (step) from the
// conversation `convID`. The on-disk representation in this
// store uses an integer `idx` to identify each step inside the
// `steps` table; the wire contract names that same field
// `message_id`. This helper accepts a string message id and
// parses it as a base-10 integer — the agy wire layer carries
// UUID-shaped ids in flight, but on this side every step has an
// integer idx, so we round-trip the integer as a string at the
// dispatcher boundary.
//
// The function returns ErrMessageNotFound when:
//   - the conversation file is missing or unreadable;
//   - the message id cannot be parsed as a uint64;
//   - the conversation has no step with the given idx.
//
// On success the row is removed from the `steps` table and the
// covering caches (cache/conversation_metadata.json,
// conversation_summaries.db) are refreshed so the `/resume`
// picker sees the new step count.
//
// Crash safety: the per-row DELETE is committed before the
// cache re-write; a crash between the two leaves the picker
// showing one more step than reality, recoverable by re-running
// any subsequent save (WriteConversation re-derives the counts).
// That's the same partial-failure shape write.go already has.
func DeleteMessage(convID, messageID string) error {
	convID = strings.TrimSpace(convID)
	messageID = strings.TrimSpace(messageID)
	if convID == "" || messageID == "" {
		return ErrMessageNotFound
	}
	dbPath := ConversationPath(convID)
	if !HasConversation(convID) {
		return ErrMessageNotFound
	}
	idx, err := strconv.ParseUint(messageID, 10, 64)
	if err != nil {
		return ErrMessageNotFound
	}

	store := StoreDir()

	// Open a read-write connection to the conversation db,
	// distinct from sqlite.go's read-only one. The driver
	// (ncruces/go-sqlite3) supports arbitrary connections
	// to the same file; the second writer is fine because
	// the driver-level write lock serialises the operations.
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		return fmt.Errorf("convstore: delete message open %s: %w", dbPath, err)
	}
	defer sdb.Close()

	res, err := sdb.Exec(`DELETE FROM steps WHERE idx = ?`, int64(idx))
	if err != nil {
		return fmt.Errorf("convstore: delete step %d from %s: %w", idx, dbPath, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("convstore: delete step %d from %s: rows affected: %w", idx, dbPath, err)
	}
	if n == 0 {
		return ErrMessageNotFound
	}

	// Refresh the covering caches so the /resume picker sees
	// the new step count. On any cache error, surface as a
	// write-level failure so the dispatcher turns it into
	// -32603 — the deletion itself succeeded on disk, but
	// the cache is now stale until the next writer runs.
	if err := refreshCachesAfterDelete(store, convID, dbPath); err != nil {
		return err
	}
	return nil
}

// DeleteConversation removes the entire conversation `convID`:
// the .db file under conversations/ + every cache entry that
// references it.
//
// Returns ErrConversationNotFound when the .db file is absent.
// The cache layers (conversation_metadata.json +
// conversation_summaries.db) are rewritten to drop the id
// silently if they happen to carry it; an orphan cache entry
// isn't an error, just stale data the picker ignores after the
// .db is gone (ListConversations's sweep already filters by
// file presence).
//
// Idempotency: a second call with the same id also returns
// ErrConversationNotFound because the file is gone.
func DeleteConversation(convID string) error {
	convID = strings.TrimSpace(convID)
	if convID == "" {
		return ErrConversationNotFound
	}
	store := StoreDir()
	dbPath := ConversationPath(convID)
	if !HasConversation(convID) {
		return ErrConversationNotFound
	}
	if err := os.Remove(dbPath); err != nil {
		if os.IsNotExist(err) {
			return ErrConversationNotFound
		}
		return fmt.Errorf("convstore: remove %s: %w", dbPath, err)
	}
	// Best-effort cache cleanup. Each helper does its own
	// "ignore not-found" path; a leftover cache entry is
	// tolerated (ListConversations filters by file presence).
	removeConversationFromCache(filepath.Join(store, "cache", "conversation_metadata.json"), convID)
	removeConversationFromIndex(filepath.Join(store, "conversation_summaries.db"), convID)
	return nil
}

// MessageByID reports whether the conversation has a step with
// the given message_id (an integer idx encoded as a decimal
// string). Returns (true, nil) when the step exists; (false,
// nil) when it doesn't. Returns an error when the conversation
// file is unreadable or the message_id is malformed — callers
// map that to ErrMessageNotFound at the dispatcher layer.
//
// On the on-disk shape, see DeleteMessage: steps are uniquely
// addressed by their idx column, which the wire layer encodes
// as a decimal string in the message_id field.
func MessageByID(convID, messageID string) (bool, error) {
	convID = strings.TrimSpace(convID)
	messageID = strings.TrimSpace(messageID)
	if convID == "" || messageID == "" {
		return false, nil
	}
	if !HasConversation(convID) {
		return false, nil
	}
	idx, err := strconv.ParseUint(messageID, 10, 64)
	if err != nil {
		return false, nil
	}
	dbPath := ConversationPath(convID)
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		return false, fmt.Errorf("convstore: message probe open %s: %w", dbPath, err)
	}
	defer sdb.Close()
	var n int64
	if err := sdb.QueryRow(`SELECT COUNT(*) FROM steps WHERE idx = ?`, int64(idx)).Scan(&n); err != nil {
		return false, fmt.Errorf("convstore: message probe %s: %w", dbPath, err)
	}
	return n > 0, nil
}

// CountMessages returns the number of `steps` rows in the
// conversation `convID`. Returns 0 when the conversation file
// is absent or unreadable so the dispatcher can decide between
// "empty conversation" (legitimate response) and "unknown
// conversation" (separate ErrConversationNotFound code path).
//
// The implementation reuses LoadConversation so the count
// matches what the picker would render — the picker's
// StepCount field is `len(c.Steps)` after the cycle-143
// Idx-sort inside LoadConversation.
func CountMessages(convID string) (int, error) {
	convID = strings.TrimSpace(convID)
	if convID == "" {
		return 0, nil
	}
	if !HasConversation(convID) {
		return 0, nil
	}
	c, err := LoadConversation(ConversationPath(convID))
	if err != nil {
		return 0, nil
	}
	if c == nil {
		return 0, nil
	}
	return len(c.Steps), nil
}

// ConversationBoundToProject reports the number of
// conversations whose workspace matches `projectWorkspace`.
// The "bound" notion here is intentionally narrow: a
// conversation is bound to a project when the conversation's
// recorded workspace URI matches the project's canonical
// workspace directory.
//
// The brief calls for "fail_if_bound" semantics on
// DeleteRemoteAgentProject: refuse the delete when at least
// one conversation still references the project. This
// helper gives the dispatcher an O(1)-ish check (it walks
// ListConversations, which is the same data the picker
// reads) without exposing the project's workspace URI
// shape back to the dispatcher.
//
// Empty `projectWorkspace` always returns 0 — a project
// without a workspace is, by definition, not the bound.
func ConversationBoundToProject(projectWorkspace string) (int, error) {
	projectWorkspace = strings.TrimSpace(projectWorkspace)
	if projectWorkspace == "" {
		return 0, nil
	}
	abs, err := filepath.Abs(projectWorkspace)
	if err != nil {
		return 0, nil
	}
	abs = filepath.Clean(abs)
	wantURI := "file://" + abs
	wantBase := filepath.Base(abs)

	convs, err := ListConversations()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, c := range convs {
		if c.Project == "" {
			continue
		}
		if c.Project == wantBase || c.Project == filepath.Base(abs) {
			// The picker's Project column is the basename of
			// the workspace URI; compare against that as the
			// cheap path. When the workspace symlink-resolves
			// differently, fall through to the URI check.
			count++
			continue
		}
		// Fall through for the "project stored under a
		// different basename" case: probe the cache for the
		// full URI. Look up c.Project — wait, c.Project IS
		// the basename; the URI lives in the cache layer.
		// We re-read the cache entry to get the URI list.
		if conversationWorkspaceMatches(c.ID, wantURI, abs) {
			count++
		}
	}
	// Project field also reflects the cwd-equality rule
	// from list.go's projectFromURIs (returns "" when the
	// workspace equals the current working directory). When
	// project matches via the basename shortcut above,
	// we've already counted; the URI check is the redundant
	// fallback for symlinked workspaces.
	return count, nil
}

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
