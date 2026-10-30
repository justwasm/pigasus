package tree

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/convstore"
	_ "github.com/ncruces/go-sqlite3/driver"
)

// Summary is the reference's index row for one conversation.
//
// 【一手】 The reference keeps its conversation index in
// ~/.gemini/antigravity-cli/conversation_summaries.db; the table
// `conversation_summaries` carries one row per conversation, with the
// workspace URIs in a JSON array.
type Summary struct {
	// ID is the conversation id.
	ID string
	// Title is the model's own title of the conversation.
	Title string
	// Preview is the first user message.
	Preview string
	// Steps is the conversation's step count.
	Steps int
	// LastModifiedTime is when the conversation last changed.
	LastModifiedTime time.Time
	// WorkspaceURIs are the workspaces the conversation ran in.
	WorkspaceURIs []string
}

// Label is the row label the reference's picker prints: the model's title
// when it has one, else the preview.
func (s Summary) Label() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Preview
}

// StoreDir is the reference's store directory — the same one the clone's
// conversation store uses.
func StoreDir() string { return convstore.StoreDir() }

// LookupSummaries reads the reference's conversation index for the given
// conversation ids.
//
// The index is far bigger than the conversation list the picker shows — the
// IDE writes a row for every conversation it ever saw — so the lookup is
// restricted to the ids the picker is about to draw.
//
// A missing or unreadable index is not an error: the reference's picker then
// simply has no workspace information and groups everything under
// "[No Workspace]".
func LookupSummaries(ids []string) map[string]Summary {
	out := make(map[string]Summary, len(ids))
	if len(ids) == 0 {
		return out
	}
	dir := StoreDir()
	if dir == "" {
		return out
	}
	path := filepath.Join(dir, "conversation_summaries.db")
	if _, err := os.Stat(path); err != nil {
		return out
	}
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return out
	}
	defer db.Close()
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	rows, err := db.Query(
		`SELECT conversation_id, title, preview, step_count, last_modified_time, workspace_uris
		 FROM conversation_summaries`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var (
			id     string
			title  string
			prev   string
			steps  int
			modRaw string
			uriRaw string
		)
		if err := rows.Scan(&id, &title, &prev, &steps, &modRaw, &uriRaw); err != nil {
			continue
		}
		if !want[id] {
			continue
		}
		out[id] = Summary{
			ID:               id,
			Title:            title,
			Preview:          prev,
			Steps:            steps,
			LastModifiedTime: parseTime(modRaw),
			WorkspaceURIs:    parseURIs(uriRaw),
		}
	}
	return out
}

// LookupCacheSummaries reads the reference's metadata cache, which holds the
// same fields for the conversations the reference has loaded.
//
// 【一手】 ~/.gemini/antigravity-cli/cache/conversation_metadata.json maps a
// conversation id to {"summary": {…}} with the same shape as the index row.
func LookupCacheSummaries(ids []string) map[string]Summary {
	out := make(map[string]Summary, len(ids))
	dir := StoreDir()
	if dir == "" {
		return out
	}
	raw, err := os.ReadFile(filepath.Join(dir, "cache", "conversation_metadata.json"))
	if err != nil {
		return out
	}
	var cache struct {
		Conversations map[string]struct {
			Summary struct {
				ID            string   `json:"ID"`
				Title         string   `json:"Title"`
				Preview       string   `json:"Preview"`
				NumSteps      int      `json:"NumSteps"`
				UpdatedAt     string   `json:"UpdatedAt"`
				WorkspaceURIs []string `json:"WorkspaceURIs"`
			} `json:"summary"`
		} `json:"conversations"`
	}
	if err := json.Unmarshal(raw, &cache); err != nil {
		return out
	}
	for _, id := range ids {
		entry, ok := cache.Conversations[id]
		if !ok {
			continue
		}
		s := entry.Summary
		out[id] = Summary{
			ID:               id,
			Title:            s.Title,
			Preview:          s.Preview,
			Steps:            s.NumSteps,
			LastModifiedTime: parseTime(s.UpdatedAt),
			WorkspaceURIs:    s.WorkspaceURIs,
		}
	}
	return out
}

// parseURIs reads the JSON array of workspace URIs the index stores.
func parseURIs(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var uris []string
	if err := json.Unmarshal([]byte(raw), &uris); err != nil {
		return nil
	}
	return uris
}

// parseTime reads the timestamps the store writes. The index uses SQLite's
// datetime shape, the cache RFC 3339.
func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		"2006-01-02 15:04:05.999999-07:00",
		"2006-01-02 15:04:05.999999Z07:00",
		"2006-01-02 15:04:05-07:00",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
		time.RFC3339Nano,
	} {
		if t, err := time.Parse(layout, raw); err == nil {
			return t
		}
	}
	return time.Time{}
}
